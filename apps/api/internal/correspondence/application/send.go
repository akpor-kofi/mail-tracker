package application

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/akpor-kofi/mail-tracker/apps/api/internal/correspondence/domain"
	"github.com/google/uuid"
)

type Delivery struct {
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

var ErrAmbiguous = errors.New("send outcome uncertain")

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
}

func (s Service) Send(ctx context.Context, owner, key, mode string, d domain.Draft) (Result, error) {
	if key == "" || len(key) > 128 {
		return Result{}, errors.New("idempotency key is required")
	}
	plans, err := domain.Plan(d, mode)
	if err != nil {
		return Result{}, err
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
	result := Result{ConversationID: cid, Deliveries: deliveries}
	for i, p := range plans {
		pixelURL := fmt.Sprintf("%s/p/%s.gif", s.PublicURL, deliveries[i].PixelToken)
		sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		sent, sendErr := s.Sender.Send(sendCtx, d.MailboxID, d, p, pixelURL)
		cancel()
		status := "sent"
		errText := ""
		if sendErr != nil {
			status = "failed"
			errText = sendErr.Error()
			var ne net.Error
			if errors.Is(sendErr, ErrAmbiguous) || errors.Is(sendErr, context.DeadlineExceeded) || errors.Is(sendErr, context.Canceled) || errors.As(sendErr, &ne) {
				status = "unknown"
			}
		}
		persistCtx, persistCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err = s.Repo.UpdateDelivery(persistCtx, deliveries[i].ID, status, sent, errText)
		persistCancel()
		if err != nil {
			return Result{}, err
		}
		deliveries[i].Status = status
		deliveries[i].GmailMessageID = sent.GmailID
		deliveries[i].GmailThreadID = sent.ThreadID
		deliveries[i].RFCMessageID = sent.RFCMessageID
		deliveries[i].Error = errText
		deliveries[i].PixelToken = ""
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
		_ = s.Repo.DeleteDraft(context.WithoutCancel(ctx), owner, d.ID)
	}
	if allSent && s.Files != nil {
		for _, id := range d.Attachments {
			_ = s.Files.Delete(context.WithoutCancel(ctx), id)
		}
	}
	return result, nil
}
