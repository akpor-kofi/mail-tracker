package main

import (
	"context"
	"log"
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
)

func required(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("%s is required", name)
	}
	return value
}
func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := db.Open(ctx, required("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool, "migrations"); err != nil {
		log.Fatal(err)
	}
	sealer, err := platformcrypto.NewSealer(required("INSTANCE_SECRET"))
	if err != nil {
		log.Fatal(err)
	}
	publicURL := strings.TrimRight(required("PUBLIC_URL"), "/")
	mailboxes := mailboxdb.Postgres{Pool: pool}
	oauth := mailboxapp.OAuthService{Repo: mailboxes, Sealer: sealer, ClientID: required("GOOGLE_CLIENT_ID"), ClientSecret: required("GOOGLE_CLIENT_SECRET"), BaseURL: publicURL}
	correspondence := corrdb.Postgres{Pool: pool}
	if err := correspondence.RecoverInterrupted(ctx); err != nil {
		log.Fatal(err)
	}
	tracking := trackingdb.Postgres{Pool: pool}
	broker := trackingdb.NewBroker()
	files := corrdb.Files{Pool: pool, Dir: required("ATTACHMENT_DIR")}
	sender := corrdb.Gmail{OAuth: oauth, Attachments: files}
	addon := trackingapp.AddonService{Pairs: trackingdb.PairingPostgres{Pool: pool}, Tracking: tracking, Identity: trackingdb.GoogleIdentity{ClientID: required("GOOGLE_ADDON_CLIENT_ID")}, PublicURL: publicURL, Events: broker}
	server := &httpapi.Server{Auth: &accountdb.Verifier{JWKSURL: required("JWKS_URL"), Issuer: publicURL, OwnerEmail: required("OWNER_EMAIL"), Pool: pool}, Mailbox: oauth, MailboxRepo: mailboxes, Correspondence: correspondence, Send: corrapp.Service{Repo: correspondence, Sender: sender, Files: files, Events: broker, PublicURL: publicURL}, Tracking: tracking, Addon: addon}
	app := fiber.New(fiber.Config{BodyLimit: 21 << 20, ErrorHandler: func(c fiber.Ctx, err error) error {
		code := 500
		message := "internal error"
		if fe, ok := err.(*fiber.Error); ok {
			code = fe.Code
			message = fe.Message
		}
		return c.Status(code).JSON(fiber.Map{"error": message})
	}})
	app.Get("/p/:token", server.Pixel)
	app.Get("/oauth/google/callback", server.OAuthCallback)
	app.Get("/api/v1/events", server.Events)
	app.Post("/api/v1/attachments", func(c fiber.Ctx) error {
		owner, err := server.Auth.Verify(c.Context(), c.Get("Authorization"))
		if err != nil {
			return fiber.NewError(401, "unauthorized")
		}
		data := c.Body()
		id, err := files.Save(c.Context(), owner, c.Get("X-File-Name"), c.Get("Content-Type"), data)
		if err != nil {
			return fiber.NewError(400, err.Error())
		}
		return c.JSON(fiber.Map{"id": id})
	})
	httpapi.RegisterHandlers(app.Group("/api/v1"), httpapi.NewStrictHandler(server, []httpapi.StrictMiddlewareFunc{server.Middleware()}))
	log.Fatal(app.Listen(":8080"))
}
