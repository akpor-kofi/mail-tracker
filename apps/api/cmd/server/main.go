package main

import (
	"context"
	"errors"
	"github.com/akpor-kofi/mail-tracker/apps/api/internal/analytics"
	"github.com/akpor-kofi/mail-tracker/apps/api/internal/documents"
	"github.com/akpor-kofi/mail-tracker/apps/api/internal/mailsync"
	"github.com/akpor-kofi/mail-tracker/apps/api/internal/outcomes"
	"github.com/akpor-kofi/raildrop/sdk/go"
	"github.com/akpor-kofi/raildrop/sdk/go/s3store"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	accountdb "github.com/akpor-kofi/mail-tracker/apps/api/internal/accounts/adapters"
	corrdb "github.com/akpor-kofi/mail-tracker/apps/api/internal/correspondence/adapters"
	corrapp "github.com/akpor-kofi/mail-tracker/apps/api/internal/correspondence/application"
	"github.com/akpor-kofi/mail-tracker/apps/api/internal/httpapi"
	mailboxdb "github.com/akpor-kofi/mail-tracker/apps/api/internal/mailboxes/adapters"
	mailboxapp "github.com/akpor-kofi/mail-tracker/apps/api/internal/mailboxes/application"
	platformcrypto "github.com/akpor-kofi/mail-tracker/apps/api/internal/platform/crypto"
	"github.com/akpor-kofi/mail-tracker/apps/api/internal/platform/db"
	trackingdb "github.com/akpor-kofi/mail-tracker/apps/api/internal/tracking/adapters"
	trackingapp "github.com/akpor-kofi/mail-tracker/apps/api/internal/tracking/application"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"
)

// A process-wide budget caps database work even when callers rotate tokens or IPs.
func publicLimit(max int) fiber.Handler {
	return limiter.New(limiter.Config{
		Max: max, Expiration: time.Minute,
		KeyGenerator: func(fiber.Ctx) string { return "public" },
	})
}

func required(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("%s is required", name)
	}
	return value
}
func main() {
	connectCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	pool, err := db.Open(connectCtx, required("DATABASE_URL"))
	cancel()
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	if err := db.Migrate(context.Background(), pool, "migrations"); err != nil {
		log.Fatal(err)
	}
	sealer, err := platformcrypto.NewSealer(required("INSTANCE_SECRET"))
	if err != nil {
		log.Fatal(err)
	}
	publicURL := strings.TrimRight(required("PUBLIC_URL"), "/")
	mailboxes := mailboxdb.Postgres{Pool: pool}
	pairings := trackingdb.PairingPostgres{Pool: pool}
	oauth := mailboxapp.OAuthService{Repo: mailboxes, Sealer: sealer, Cache: &mailboxapp.AccessTokenCache{}, ClientID: required("GOOGLE_CLIENT_ID"), ClientSecret: required("GOOGLE_CLIENT_SECRET"), BaseURL: publicURL}
	correspondence := corrdb.Postgres{Pool: pool, PublicURL: publicURL}
	if err := correspondence.RecoverInterrupted(context.Background()); err != nil {
		log.Fatal(err)
	}
	tracking := trackingdb.Postgres{Pool: pool}
	broker := trackingdb.NewBroker()
	files := corrdb.Files{Pool: pool, Dir: required("ATTACHMENT_DIR")}
	cleanupCtx, stopCleanup := context.WithCancel(context.Background())
	defer stopCleanup()
	go func() {
		sweepAttachments := func() {
			for batch := 0; batch < 10; batch++ {
				ctx, cancel := context.WithTimeout(cleanupCtx, time.Minute)
				count, err := files.CleanupOrphans(ctx, time.Now().Add(-24*time.Hour), 100)
				cancel()
				if err != nil {
					if cleanupCtx.Err() == nil {
						log.Printf("attachment cleanup: %v", err)
					}
					return
				}
				if count < 100 {
					return
				}
			}
		}
		deleteExpired := func() {
			ctx, cancel := context.WithTimeout(cleanupCtx, 15*time.Second)
			if err := mailboxes.DeleteExpiredStates(ctx); err != nil && cleanupCtx.Err() == nil {
				log.Printf("OAuth state cleanup: %v", err)
			}
			cancel()
			ctx, cancel = context.WithTimeout(cleanupCtx, 15*time.Second)
			if err := pairings.DeleteExpiredCodes(ctx); err != nil && cleanupCtx.Err() == nil {
				log.Printf("pairing code cleanup: %v", err)
			}
			cancel()
		}
		sweepAttachments()
		deleteExpired()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-cleanupCtx.Done():
				return
			case <-ticker.C:
				sweepAttachments()
				deleteExpired()
			}
		}
	}()
	sender := corrdb.Gmail{OAuth: oauth, Attachments: files}
	addon := trackingapp.AddonService{Pairs: pairings, Tracking: tracking, Identity: trackingdb.GoogleIdentity{ClientID: required("GOOGLE_ADDON_CLIENT_ID")}, PublicURL: publicURL, Events: broker}
	activity := analytics.Store{Pool: pool, PublicURL: publicURL}
	go activity.Run(cleanupCtx, broker.Publish)
	var objectStorage raildrop.Storage
	if os.Getenv("RAILDROP_BUCKET") != "" {
		cfg, e := s3store.BucketConfigFromEnv()
		if e != nil {
			log.Fatal("invalid document bucket configuration")
		}
		objectStorage, e = s3store.NewRailwayBucketStorage(cfg, &http.Client{Timeout: 45 * time.Second})
		if e != nil {
			log.Fatal("invalid document bucket configuration")
		}
	}
	documentStore := documents.Store{Pool: pool, Storage: objectStorage, PublicURL: publicURL}
	if objectStorage != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		_, e := objectStorage.List(ctx, "private/documents/", "")
		cancel()
		if e != nil {
			log.Fatal("document bucket connection failed")
		}
		log.Print("private document bucket ready")
	}
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-cleanupCtx.Done():
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(cleanupCtx, time.Minute)
				if e := documentStore.Cleanup(ctx); e != nil {
					log.Print("document cleanup failed")
				}
				cancel()
			}
		}
	}()

	go (mailsync.Worker{Pool: pool, OAuth: oauth, Activity: activity}).Run(cleanupCtx)
	server := &httpapi.Server{Outcomes: outcomes.Store{Pool: pool}, WebhookSecret: os.Getenv("CONVERSION_WEBHOOK_SECRET"), Documents: documentStore, Analytics: activity, Auth: &accountdb.Verifier{JWKSURL: required("JWKS_URL"), Issuer: publicURL, OwnerEmail: required("OWNER_EMAIL"), Pool: pool}, Mailbox: oauth, MailboxRepo: mailboxes, Correspondence: correspondence, Send: corrapp.Service{Repo: correspondence, Sender: sender, Files: files, Events: broker, PublicURL: publicURL, Slots: make(chan struct{}, 4)}, Tracking: tracking, Addon: addon}
	app := fiber.New(fiber.Config{BodyLimit: 21 << 20, ErrorHandler: func(c fiber.Ctx, err error) error {
		code := 500
		message := "internal error"
		if fe, ok := err.(*fiber.Error); ok {
			code = fe.Code
			if code < 500 {
				message = fe.Message
			}
		}
		if code >= 500 {
			log.Printf("HTTP %d %s %s: %v", code, c.Method(), "[path redacted]", err)
		}
		return c.Status(code).JSON(fiber.Map{"error": message})
	}})
	app.Get("/c/:token", publicLimit(600), server.LinkRedirect)
	app.Head("/c/:token", publicLimit(600), server.LinkRedirect)
	app.Get("/p/:token", publicLimit(600), server.Pixel)
	app.Get("/oauth/google/callback", publicLimit(30), server.OAuthCallback)
	app.Get("/api/v1/events", server.Events)
	app.Post("/api/v1/attachments", func(c fiber.Ctx) error {
		owner, err := server.Auth.Verify(c.Context(), c.Get("Authorization"))
		if err != nil {
			return fiber.NewError(401, "unauthorized")
		}
		data := c.Body()
		name := c.Get("X-File-Name")
		encoding := c.Get("X-File-Name-Encoding")
		if encoding == "percent" {
			decoded, err := url.PathUnescape(name)
			if err != nil {
				return fiber.NewError(400, "invalid attachment filename")
			}
			name = decoded
		} else if encoding != "" {
			return fiber.NewError(400, "invalid attachment filename encoding")
		}
		if strings.TrimSpace(name) == "" {
			return fiber.NewError(400, "invalid attachment filename")
		}
		id, err := files.Save(c.Context(), owner, name, c.Get("Content-Type"), data)
		if err != nil {
			if errors.Is(err, corrdb.ErrInvalidAttachmentSize) {
				return fiber.NewError(400, corrdb.ErrInvalidAttachmentSize.Error())
			}
			log.Printf("save attachment: %v", err)
			return fiber.NewError(500, "internal error")
		}
		return c.JSON(fiber.Map{"id": id})
	})
	app.Use("/api/v1/addon/pair", publicLimit(20))
	app.Use("/api/v1/addon/prepare", publicLimit(60))
	app.Use("/api/v1/addon/link", publicLimit(60))
	app.Use("/api/v1/webhooks", publicLimit(60))
	app.Use("/api/v1/viewer", publicLimit(600))
	httpapi.RegisterRoutes(app.Group("/api/v1"), server)
	log.Fatal(app.Listen(":8080"))
}
