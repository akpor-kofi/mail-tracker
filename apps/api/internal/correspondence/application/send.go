package application

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/akpor-kofi/mail-tracker/apps/api/internal/correspondence/domain"
	"github.com/google/uuid"
)

type Delivery struct {
	HTML               string
	ID                 string
	Recipients         []string
	ReplyAllRecipients []string
	Status             string
	Error              string
	GmailMessageID     string
	GmailThreadID      string
	RFCMessageID       string
	PixelToken         string
}
type Result struct {
	ConversationID string
	Deliveries     []Delivery
}
type Repository interface {
	CreateAttempt(context.Context, string, string, domain.Draft, string, []Delivery) (string, bool, error)
	GetResult(context.Context, string, string) (Result, error)
	DeleteDraft(context.Context, string, string) error
	UpdateDelivery(context.Context, string, string, SentMessage, string) error
}

const SendPermissionError = "Gmail sending permission is missing. Reconnect this account in Settings and allow sending mail, then try a new send."

var ErrSendPermission = errors.New(SendPermissionError)

var ErrAmbiguous = errors.New("send outcome uncertain")

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

type IdempotencyConflict struct {
	ConversationID string
	Legacy         bool
}

func (e *IdempotencyConflict) Error() string {
	if e.Legacy {
		return "this send predates request verification; inspect conversation " + e.ConversationID + " before starting a new send"
	}
	return "idempotency key was already used for a different message"
}

type SentMessage struct{ GmailID, ThreadID, RFCMessageID string }
type Sender interface {
	Send(context.Context, string, domain.Draft, domain.PlannedDelivery, string) (SentMessage, error)
}
type AttachmentStore interface {
	Load(context.Context, string, string) ([]byte, string, string, error)
	Delete(context.Context, string) error
}
type EventDelivery interface{ Publish(string) }
type Service struct {
	Repo      Repository
	Sender    Sender
	Files     AttachmentStore
	Events    EventDelivery
	PublicURL string
	Slots     chan struct{}
}

func (s Service) Send(ctx context.Context, owner, key, mode string, d domain.Draft) (Result, error) {
	if key == "" || len(key) > 128 {
		return Result{}, &ValidationError{Message: "idempotency key is required and must be at most 128 characters"}
	}
	plans, err := domain.Plan(d, mode)
	if err != nil {
		return Result{}, &ValidationError{Message: err.Error()}
	}
	deliveries := make([]Delivery, len(plans))
	for i, p := range plans {
		tokenBytes := make([]byte, 32)
		if _, err := rand.Read(tokenBytes); err != nil {
			return Result{}, err
		}
		deliveries[i] = Delivery{ID: uuid.NewString(), Recipients: p.Recipients, ReplyAllRecipients: append(append([]string{}, p.To...), p.Cc...), Status: "pending", PixelToken: base64.RawURLEncoding.EncodeToString(tokenBytes)}
	}
	cid, existing, err := s.Repo.CreateAttempt(ctx, owner, key, d, mode, deliveries)
	if err != nil {
		return Result{}, err
	}
	if existing {
		return s.Repo.GetResult(ctx, owner, cid)
	}
	response := append([]Delivery(nil), deliveries...)
	for i := range response {
		response[i].PixelToken = ""
	}
	if s.Events != nil {
		s.Events.Publish(owner)
	}
	go s.run(owner, d, plans, deliveries)
	return Result{ConversationID: cid, Deliveries: response}, nil
}

type sendOutcome struct {
	index int
	sent  SentMessage
	err   error
}

func (s Service) run(owner string, d domain.Draft, plans []domain.PlannedDelivery, deliveries []Delivery) {
	jobs := make(chan int)
	outcomes := make(chan sendOutcome)
	workers := min(4, len(plans))
	var running sync.WaitGroup
	for range workers {
		running.Add(1)
		go func() {
			defer running.Done()
			for i := range jobs {
				if s.Slots != nil {
					s.Slots <- struct{}{}
				}
				pixelURL := fmt.Sprintf("%s/p/%s.gif", s.PublicURL, deliveries[i].PixelToken)
				sendCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				draft := d
				if deliveries[i].HTML != "" {
					draft.HTML = deliveries[i].HTML
				}
				sent, sendErr := s.Sender.Send(sendCtx, d.MailboxID, draft, plans[i], pixelURL)
				cancel()
				if s.Slots != nil {
					<-s.Slots
				}
				outcomes <- sendOutcome{index: i, sent: sent, err: sendErr}
			}
		}()
	}
	go func() {
		for i := range plans {
			jobs <- i
		}
		close(jobs)
		running.Wait()
		close(outcomes)
	}()
	for outcome := range outcomes {
		i := outcome.index
		status := "sent"
		errText := ""
		if outcome.err != nil {
			status = "failed"
			log.Printf("Gmail send failed for delivery %s: %v", deliveries[i].ID, outcome.err)
			errText = "Gmail send failed; check the mailbox connection and try a new send"
			if errors.Is(outcome.err, ErrSendPermission) {
				errText = SendPermissionError
			}
			var ne net.Error
			if errors.Is(outcome.err, ErrAmbiguous) || errors.Is(outcome.err, context.DeadlineExceeded) || errors.Is(outcome.err, context.Canceled) || errors.As(outcome.err, &ne) {
				status = "unknown"
				errText = "Send status unknown; check Gmail Sent before trying again"
			}
		}
		var persistErr error
		for attempt := 0; attempt < 3; attempt++ {
			persistCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			persistErr = s.Repo.UpdateDelivery(persistCtx, deliveries[i].ID, status, outcome.sent, errText)
			cancel()
			if persistErr == nil {
				break
			}
			if attempt < 2 {
				time.Sleep(time.Duration(attempt+1) * 250 * time.Millisecond)
			}
		}
		if persistErr != nil {
			log.Printf("persist delivery %s: %v", deliveries[i].ID, persistErr)
			continue
		}
		deliveries[i].Status = status
		if s.Events != nil {
			s.Events.Publish(owner)
		}
	}
	allSent := true
	for _, delivery := range deliveries {
		if delivery.Status != "sent" {
			allSent = false
		}
	}
	if allSent && d.ID != "" {
		if err := s.Repo.DeleteDraft(context.Background(), owner, d.ID); err != nil {
			log.Printf("delete sent draft %s: %v", d.ID, err)
		}
	}
	if allSent && s.Files != nil {
		for _, id := range d.Attachments {
			if err := s.Files.Delete(context.Background(), id); err != nil {
				log.Printf("delete sent attachment %s: %v", id, err)
			}
		}
	}
}
