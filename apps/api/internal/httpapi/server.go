package httpapi

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	accounts "github.com/akpor-kofi/mail-tracker/apps/api/internal/accounts/application"
	corrdb "github.com/akpor-kofi/mail-tracker/apps/api/internal/correspondence/adapters"
	corrapp "github.com/akpor-kofi/mail-tracker/apps/api/internal/correspondence/application"
	corrdomain "github.com/akpor-kofi/mail-tracker/apps/api/internal/correspondence/domain"
	mailboxapp "github.com/akpor-kofi/mail-tracker/apps/api/internal/mailboxes/application"
	trackingapp "github.com/akpor-kofi/mail-tracker/apps/api/internal/tracking/application"
	trackingdomain "github.com/akpor-kofi/mail-tracker/apps/api/internal/tracking/domain"
	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
)

type ownerKey struct{}

func owner(ctx context.Context) string { v, _ := ctx.Value(ownerKey{}).(string); return v }
func internalError(operation string, err error) error {
	log.Printf("%s: %v", operation, err)
	return fiber.NewError(500, "internal error")
}
func ptr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func draftIn(d DraftInput) corrdomain.Draft {
	return corrdomain.Draft{ID: str(d.Id), MailboxID: d.MailboxId, To: d.To, Cc: d.Cc, Bcc: d.Bcc, Subject: d.Subject, HTML: d.Html, ReplyToMessageID: str(d.ReplyToMessageId), ThreadID: str(d.ThreadId), Attachments: func() []string {
		if d.Attachments == nil {
			return nil
		}
		return *d.Attachments
	}()}
}
func draftOut(d corrdb.DraftRecord) Draft {
	return Draft{Id: d.ID, MailboxId: d.Content.MailboxID, To: d.Content.To, Cc: d.Content.Cc, Bcc: d.Content.Bcc, Subject: d.Content.Subject, Html: d.Content.HTML, ReplyToMessageId: ptr(d.Content.ReplyToMessageID), ThreadId: ptr(d.Content.ThreadID), Attachments: &d.Content.Attachments, UpdatedAt: d.UpdatedAt}
}
func convOut(c trackingdomain.Conversation) Conversation {
	return Conversation{Id: c.ID, MailboxId: c.MailboxID, Subject: c.Subject, Status: c.Status, OpenStatus: ConversationOpenStatus(c.OpenStatus), UpdatedAt: c.UpdatedAt}
}
func deliveryOut(d corrapp.Delivery) Delivery {
	return Delivery{Id: d.ID, Recipients: d.Recipients, ReplyAllRecipients: d.ReplyAllRecipients, Status: DeliveryStatus(d.Status), Error: safeDeliveryError(d.Status, d.Error), GmailMessageId: ptr(d.GmailMessageID), GmailThreadId: ptr(d.GmailThreadID), RfcMessageId: ptr(d.RFCMessageID)}
}
func safeDeliveryError(status, stored string) *string {
	if stored == "" {
		return nil
	}
	if status == "unknown" {
		return ptr("Send status unknown; check Gmail Sent before trying again")
	}
	return ptr("Gmail send failed; check the mailbox connection and try a new send")
}
func detailOut(d trackingdomain.Detail) ConversationDetail {
	out := ConversationDetail{Id: d.Conversation.ID, MailboxId: d.Conversation.MailboxID, Subject: d.Conversation.Subject, Status: d.Conversation.Status, OpenStatus: ConversationDetailOpenStatus(d.Conversation.OpenStatus), UpdatedAt: d.Conversation.UpdatedAt, Deliveries: []Delivery{}, Events: []OpenEvent{}}
	for _, x := range d.Deliveries {
		out.Deliveries = append(out.Deliveries, Delivery{Id: x.ID, Recipients: x.Recipients, ReplyAllRecipients: x.ReplyAllRecipients, Status: DeliveryStatus(x.Status), Error: safeDeliveryError(x.Status, x.Error), GmailMessageId: ptr(x.GmailMessageID), GmailThreadId: ptr(x.GmailThreadID), RfcMessageId: ptr(x.RFCMessageID)})
	}
	for _, e := range d.Events {
		out.Events = append(out.Events, OpenEvent{DeliveryId: e.DeliveryID, At: e.At})
	}
	return out
}

type Server struct {
	Auth           accounts.OwnerVerifier
	Mailbox        mailboxapp.OAuthService
	MailboxRepo    mailboxapp.Repository
	Correspondence corrdb.Postgres
	Send           corrapp.Service
	Tracking       trackingapp.Repository
	Addon          trackingapp.AddonService
}

var _ StrictServerInterface = (*Server)(nil)

func (s *Server) Middleware() StrictMiddlewareFunc {
	return func(next StrictHandlerFunc, op string) StrictHandlerFunc {
		return func(c fiber.Ctx, arg any) (any, error) {
			if op == "GetHealth" || op == "PairAddon" || op == "PrepareAddonDraft" {
				return next(c, arg)
			}
			id, err := s.Auth.Verify(c.Context(), c.Get("Authorization"))
			if err != nil {
				return nil, fiber.NewError(401, "unauthorized")
			}
			c.SetContext(context.WithValue(c.Context(), ownerKey{}, id))
			return next(c, arg)
		}
	}
}
func (s *Server) GetHealth(ctx context.Context, _ GetHealthRequestObject) (GetHealthResponseObject, error) {
	return GetHealth200JSONResponse{Status: "ok"}, nil
}
func (s *Server) ListMailboxes(ctx context.Context, _ ListMailboxesRequestObject) (ListMailboxesResponseObject, error) {
	ms, err := s.MailboxRepo.List(ctx, owner(ctx))
	if err != nil {
		return nil, err
	}
	out := ListMailboxes200JSONResponse{}
	for _, m := range ms {
		out = append(out, Mailbox{Id: m.ID, Email: m.Email, ConnectedAt: m.ConnectedAt})
	}
	return out, nil
}
func (s *Server) StartMailboxConnect(ctx context.Context, _ StartMailboxConnectRequestObject) (StartMailboxConnectResponseObject, error) {
	url, err := s.Mailbox.Start(ctx, owner(ctx))
	if err != nil {
		return nil, err
	}
	return StartMailboxConnect200JSONResponse{Url: url}, nil
}
func (s *Server) RemoveMailbox(ctx context.Context, r RemoveMailboxRequestObject) (RemoveMailboxResponseObject, error) {
	if err := s.MailboxRepo.Delete(ctx, owner(ctx), r.MailboxId); err != nil {
		return nil, err
	}
	return RemoveMailbox204Response{}, nil
}
func (s *Server) ListDrafts(ctx context.Context, _ ListDraftsRequestObject) (ListDraftsResponseObject, error) {
	ds, err := s.Correspondence.ListDrafts(ctx, owner(ctx))
	if err != nil {
		return nil, err
	}
	out := ListDrafts200JSONResponse{}
	for _, d := range ds {
		out = append(out, draftOut(d))
	}
	return out, nil
}
func (s *Server) GetDraft(ctx context.Context, r GetDraftRequestObject) (GetDraftResponseObject, error) {
	d, err := s.Correspondence.GetDraft(ctx, owner(ctx), r.DraftId)
	if err != nil {
		return nil, err
	}
	return GetDraft200JSONResponse(draftOut(d)), nil
}
func (s *Server) SaveDraft(ctx context.Context, r SaveDraftRequestObject) (SaveDraftResponseObject, error) {
	if r.Body == nil {
		return nil, fiber.NewError(400, "missing draft")
	}
	d, err := s.Correspondence.SaveDraft(ctx, owner(ctx), str(r.Body.Id), draftIn(*r.Body))
	if err != nil {
		if errors.Is(err, corrdb.ErrDraftAttachment) {
			return nil, fiber.NewError(400, corrdb.ErrDraftAttachment.Error())
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fiber.NewError(400, "mailbox or draft not found")
		}
		return nil, internalError("save draft", err)
	}
	return SaveDraft200JSONResponse(draftOut(d)), nil
}
func (s *Server) DeleteDraft(ctx context.Context, r DeleteDraftRequestObject) (DeleteDraftResponseObject, error) {
	if err := s.Correspondence.DeleteDraft(ctx, owner(ctx), r.DraftId); err != nil {
		return nil, err
	}
	return DeleteDraft204Response{}, nil
}
func (s *Server) SendTrackedMessage(ctx context.Context, r SendTrackedMessageRequestObject) (SendTrackedMessageResponseObject, error) {
	if r.Body == nil {
		return nil, fiber.NewError(400, "missing request")
	}
	result, err := s.Send.Send(ctx, owner(ctx), r.Body.IdempotencyKey, string(r.Body.SendMode), draftIn(r.Body.Draft))
	if err != nil {
		var conflict *corrapp.IdempotencyConflict
		if errors.As(err, &conflict) {
			return nil, fiber.NewError(409, conflict.Error())
		}
		var validation *corrapp.ValidationError
		if errors.As(err, &validation) {
			return nil, fiber.NewError(400, validation.Error())
		}
		return nil, internalError("send tracked message", err)
	}
	out := SendTrackedMessage200JSONResponse{ConversationId: result.ConversationID, Deliveries: []Delivery{}}
	for _, d := range result.Deliveries {
		out.Deliveries = append(out.Deliveries, deliveryOut(d))
	}
	return out, nil
}
func (s *Server) ListConversations(ctx context.Context, r ListConversationsRequestObject) (ListConversationsResponseObject, error) {
	cs, err := s.Tracking.List(ctx, owner(ctx), str(r.Params.MailboxId))
	if err != nil {
		return nil, err
	}
	out := ListConversations200JSONResponse{}
	for _, c := range cs {
		out = append(out, convOut(c))
	}
	return out, nil
}
func (s *Server) GetConversation(ctx context.Context, r GetConversationRequestObject) (GetConversationResponseObject, error) {
	d, err := s.Tracking.Detail(ctx, owner(ctx), r.ConversationId)
	if err != nil {
		return nil, err
	}
	return GetConversation200JSONResponse(detailOut(d)), nil
}
func (s *Server) CreatePairingCode(ctx context.Context, r CreatePairingCodeRequestObject) (CreatePairingCodeResponseObject, error) {
	if r.Body == nil {
		return nil, fiber.NewError(400, "missing mailbox")
	}
	m, _, err := s.MailboxRepo.Get(ctx, r.Body.MailboxId)
	if err != nil || m.OwnerID != owner(ctx) {
		return nil, fiber.NewError(404, "mailbox not found")
	}
	code, at, err := s.Addon.Code(ctx, m.ID)
	if err != nil {
		return nil, err
	}
	return CreatePairingCode200JSONResponse{Code: code, ExpiresAt: at}, nil
}
func (s *Server) PairAddon(ctx context.Context, r PairAddonRequestObject) (PairAddonResponseObject, error) {
	if r.Body == nil {
		return nil, fiber.NewError(400, "missing request")
	}
	id, err := s.Addon.Pair(ctx, r.Body.Code, r.Body.IdentityToken)
	if err != nil {
		log.Printf("pair add-on: %v", err)
		return nil, fiber.NewError(401, "invalid pairing code or identity")
	}
	return PairAddon200JSONResponse{MailboxId: id}, nil
}
func (s *Server) PrepareAddonDraft(ctx context.Context, r PrepareAddonDraftRequestObject) (PrepareAddonDraftResponseObject, error) {
	if r.Body == nil {
		return nil, fiber.NewError(400, "missing request")
	}
	cid, url, err := s.Addon.Prepare(ctx, r.Body.IdentityToken, r.Body.Subject, r.Body.Recipients)
	if err != nil {
		log.Printf("prepare add-on draft: %v", err)
		return nil, fiber.NewError(401, "add-on not paired or invalid identity")
	}
	return PrepareAddonDraft200JSONResponse{ConversationId: cid, PixelUrl: url}, nil
}
func (s *Server) Pixel(c fiber.Ctx) error {
	token := strings.TrimSuffix(c.Params("token"), ".gif")
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err == nil && len(decoded) == 32 {
		hash := sha256.Sum256([]byte(token))
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		who, err := s.Tracking.RecordOpen(ctx, hash[:])
		if err == nil && s.Send.Events != nil {
			s.Send.Events.Publish(who)
		}
	}
	c.Set("Content-Type", "image/gif")
	c.Set("Cache-Control", "no-store, no-cache, max-age=0")
	c.Set("X-Content-Type-Options", "nosniff")
	return c.Send([]byte{71, 73, 70, 56, 57, 97, 1, 0, 1, 0, 128, 0, 0, 0, 0, 0, 255, 255, 255, 33, 249, 4, 1, 0, 0, 0, 0, 44, 0, 0, 0, 0, 1, 0, 1, 0, 0, 2, 2, 68, 1, 0, 59})
}
func (s *Server) OAuthCallback(c fiber.Ctx) error {
	email, err := s.Mailbox.Complete(c.Context(), c.Query("state"), c.Query("code"))
	if err != nil {
		log.Printf("Google OAuth callback: %v", err)
		return fiber.NewError(400, "Gmail connection failed. Please try again.")
	}
	return c.Redirect().To("/settings?connected=" + url.QueryEscape(email))
}
func (s *Server) Events(c fiber.Ctx) error {
	id, err := s.Auth.Verify(c.Context(), c.Get("Authorization"))
	if err != nil {
		return fiber.NewError(401, "unauthorized")
	}
	events, unsubscribe := s.Addon.Events.Subscribe(id)
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	return c.SendStreamWriter(func(w *bufio.Writer) {
		defer unsubscribe()
		tick := time.NewTicker(20 * time.Second)
		lifetime := time.NewTimer(14 * time.Minute)
		defer tick.Stop()
		defer lifetime.Stop()
		fmt.Fprint(w, "event: ready\ndata: {}\n\n")
		if w.Flush() != nil {
			return
		}
		for {
			select {
			case _, ok := <-events:
				if !ok {
					return
				}
				fmt.Fprint(w, "event: changed\ndata: {}\n\n")
			case <-lifetime.C:
				return
			case <-tick.C:
				fmt.Fprint(w, ": heartbeat\n\n")
			}
			if w.Flush() != nil {
				return
			}
		}
	})
}
