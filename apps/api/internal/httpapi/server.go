package httpapi

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	accounts "github.com/akpor-kofi/mail-tracker/apps/api/internal/accounts/application"
	corrdb "github.com/akpor-kofi/mail-tracker/apps/api/internal/correspondence/adapters"
	corrapp "github.com/akpor-kofi/mail-tracker/apps/api/internal/correspondence/application"
	corrapi "github.com/akpor-kofi/mail-tracker/apps/api/internal/httpapi/correspondence"
	mailboxapi "github.com/akpor-kofi/mail-tracker/apps/api/internal/httpapi/mailboxes"
	trackingapi "github.com/akpor-kofi/mail-tracker/apps/api/internal/httpapi/tracking"
	mailboxapp "github.com/akpor-kofi/mail-tracker/apps/api/internal/mailboxes/application"
	trackingapp "github.com/akpor-kofi/mail-tracker/apps/api/internal/tracking/application"
	"github.com/gofiber/fiber/v3"
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
func safeDeliveryError(status, stored string) *string {
	if stored == "" {
		return nil
	}
	if status == "unknown" {
		return ptr("Send status unknown; check Gmail Sent before trying again")
	}
	return ptr("Gmail send failed; check the mailbox connection and try a new send")
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

var _ mailboxapi.StrictServerInterface = (*Server)(nil)
var _ corrapi.StrictServerInterface = (*Server)(nil)
var _ trackingapi.StrictServerInterface = (*Server)(nil)

func withOwner[H ~func(fiber.Ctx, any) (any, error)](s *Server, next H) H {
	return H(func(c fiber.Ctx, arg any) (any, error) {
		id, err := s.Auth.Verify(c.Context(), c.Get("Authorization"))
		if err != nil {
			return nil, fiber.NewError(401, "unauthorized")
		}
		c.SetContext(context.WithValue(c.Context(), ownerKey{}, id))
		return next(c, arg)
	})
}
func (s *Server) MailboxesMiddleware() mailboxapi.StrictMiddlewareFunc {
	return func(next mailboxapi.StrictHandlerFunc, _ string) mailboxapi.StrictHandlerFunc {
		return withOwner(s, next)
	}
}
func (s *Server) CorrespondenceMiddleware() corrapi.StrictMiddlewareFunc {
	return func(next corrapi.StrictHandlerFunc, _ string) corrapi.StrictHandlerFunc {
		return withOwner(s, next)
	}
}
func (s *Server) TrackingMiddleware() trackingapi.StrictMiddlewareFunc {
	return func(next trackingapi.StrictHandlerFunc, op string) trackingapi.StrictHandlerFunc {
		if op == "PairAddon" || op == "PrepareAddonDraft" {
			return next
		}
		return withOwner(s, next)
	}
}

func RegisterRoutes(router fiber.Router, s *Server) {
	router.Get("/health", s.Health)
	mailboxapi.RegisterHandlers(router, mailboxapi.NewStrictHandler(s, []mailboxapi.StrictMiddlewareFunc{s.MailboxesMiddleware()}))
	corrapi.RegisterHandlers(router, corrapi.NewStrictHandler(s, []corrapi.StrictMiddlewareFunc{s.CorrespondenceMiddleware()}))
	trackingapi.RegisterHandlers(router, trackingapi.NewStrictHandler(s, []trackingapi.StrictMiddlewareFunc{s.TrackingMiddleware()}))
}

func (s *Server) Health(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"status": "ok"})
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
