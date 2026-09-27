package adapters

import (
	"bytes"
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

type testAttachment struct{ data []byte }

func (a testAttachment) Load(context.Context, string, string) ([]byte, string, string, error) {
	return a.data, "report.pdf", "application/pdf", nil
}

func TestBuildMIMEWrapsBase64Parts(t *testing.T) {
	d := domain.Draft{To: []string{"a@example.com"}, Subject: "Hello", HTML: "<p>" + strings.Repeat("a", 200) + "</p>", Attachments: []string{"attachment"}}
	attachment := bytes.Repeat([]byte{42}, 300)
	raw, _, err := BuildMIME(context.Background(), mailboxdomain.Mailbox{Email: "sender@example.com"}, d, domain.PlannedDelivery{To: d.To}, "https://example.com/p/token.gif", testAttachment{attachment}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	message, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	_, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	mixed := multipart.NewReader(message.Body, params["boundary"])
	for {
		part, err := mixed.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(part.Header.Get("Content-Type"), "multipart/alternative") {
			_, altParams, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
			if err != nil {
				t.Fatal(err)
			}
			alternative := multipart.NewReader(part, altParams["boundary"])
			for {
				nested, err := alternative.NextPart()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				assertBase64Lines(t, nested)
			}
		} else {
			assertBase64Lines(t, part)
		}
	}
}

func assertBase64Lines(t *testing.T, part io.Reader) {
	t.Helper()
	body, err := io.ReadAll(part)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range bytes.Split(body, []byte("\r\n")) {
		if len(line) > 76 {
			t.Fatalf("MIME base64 line has %d characters", len(line))
		}
	}
	if _, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, bytes.NewReader(body))); err != nil {
		t.Fatalf("MIME base64 cannot be decoded: %v", err)
	}
}

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
