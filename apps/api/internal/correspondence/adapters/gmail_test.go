package adapters

import (
	"context"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"

	"github.com/akpor-kofi/mail-tracker/apps/api/internal/correspondence/domain"
	mailboxdomain "github.com/akpor-kofi/mail-tracker/apps/api/internal/mailboxes/domain"
)

func TestBuildMIMESanitizesAndAppendsPixel(t *testing.T) {
	d := domain.Draft{To: []string{"a@example.com"}, Subject: "Hello", HTML: `<p>Hello</p><script>alert(1)</script>`, ReplyToMessageID: "<prior@example.com>", ThreadID: "thread"}
	planned := domain.PlannedDelivery{To: d.To, Recipients: d.To}
	raw, rfc, err := BuildMIME(context.Background(), mailboxdomain.Mailbox{Email: "sender@example.com"}, d, planned, "https://example.com/p/token.gif", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Get("In-Reply-To") != "<prior@example.com>" || msg.Header.Get("Message-ID") != rfc {
		t.Fatal("reply headers incorrect")
	}
	_, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	mixed := multipart.NewReader(msg.Body, params["boundary"])
	altPart, err := mixed.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	_, altParams, err := mime.ParseMediaType(altPart.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	alt := multipart.NewReader(altPart, altParams["boundary"])
	plain, err := alt.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	plainBytes, _ := io.ReadAll(base64.NewDecoder(base64.StdEncoding, plain))
	if strings.Contains(string(plainBytes), "<") || !strings.Contains(string(plainBytes), "Hello") {
		t.Fatal("bad plain text")
	}
	htmlPart, err := alt.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	htmlBytes, _ := io.ReadAll(base64.NewDecoder(base64.StdEncoding, htmlPart))
	body := string(htmlBytes)
	if strings.Contains(body, "<script") || !strings.Contains(body, `width="1"`) || !strings.Contains(body, "https://example.com/p/token.gif") {
		t.Fatalf("bad sanitized HTML: %q", body)
	}
}
