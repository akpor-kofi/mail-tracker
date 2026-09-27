package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

type rejectingOwner struct{}

func (rejectingOwner) Verify(context.Context, string) (string, error) {
	return "", errors.New("no session")
}

func TestDomainRoutesRequireOwner(t *testing.T) {
	app := fiber.New()
	RegisterRoutes(app.Group("/api/v1"), &Server{Auth: rejectingOwner{}})
	for _, path := range []string{"/api/v1/mailboxes", "/api/v1/drafts", "/api/v1/conversations"} {
		response, err := app.Test(httptest.NewRequest("GET", path, nil))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 401 {
			t.Errorf("%s: got %d, want 401", path, response.StatusCode)
		}
	}
	response, err := app.Test(httptest.NewRequest("GET", "/api/v1/health", nil))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Errorf("health: got %d, want 200", response.StatusCode)
	}
}

func TestPixelRemainsValidGIF(t *testing.T) {
	app := fiber.New()
	server := &Server{}
	app.Get("/p/:token", server.Pixel)
	response, err := app.Test(httptest.NewRequest("GET", "/p/invalid.gif", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "image/gif" {
		t.Fatalf("pixel response: status %d, content type %q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 43 {
		t.Fatalf("pixel is %d bytes, want 43", len(data))
	}
}

func TestStoredDeliveryErrorIsNotExposed(t *testing.T) {
	for _, status := range []string{"failed", "unknown"} {
		message := safeDeliveryError(status, "violates fk_deliveries_owner_id and secret adapter details")
		if message == nil || strings.Contains(*message, "fk_deliveries") || strings.Contains(*message, "adapter") {
			t.Fatalf("%s leaked stored error: %v", status, message)
		}
	}
}
