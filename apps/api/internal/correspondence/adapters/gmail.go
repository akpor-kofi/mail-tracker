package adapters

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	nethtml "golang.org/x/net/html"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"regexp"
	"strings"

	corrapp "github.com/akpor-kofi/mail-tracker/apps/api/internal/correspondence/application"
	"github.com/akpor-kofi/mail-tracker/apps/api/internal/correspondence/domain"
	mailboxapp "github.com/akpor-kofi/mail-tracker/apps/api/internal/mailboxes/application"
	mailboxdomain "github.com/akpor-kofi/mail-tracker/apps/api/internal/mailboxes/domain"
	"github.com/google/uuid"
	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/oauth2"
)

type Gmail struct {
	OAuth       mailboxapp.OAuthService
	Attachments interface {
		Load(context.Context, string, string) ([]byte, string, string, error)
	}
}

var tags = regexp.MustCompile(`<[^>]+>`)

func cleanHeader(s string) string { return strings.NewReplacer("\r", "", "\n", "").Replace(s) }
func plainText(clean string) string {
	root, err := nethtml.Parse(strings.NewReader(clean))
	if err != nil {
		return strings.TrimSpace(html.UnescapeString(tags.ReplaceAllString(clean, " ")))
	}
	var out strings.Builder
	var walk func(*nethtml.Node)
	walk = func(n *nethtml.Node) {
		if n.Type == nethtml.TextNode {
			out.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
		if n.Type == nethtml.ElementNode {
			if n.Data == "a" {
				for _, a := range n.Attr {
					if a.Key == "href" {
						out.WriteString(" (" + a.Val + ")")
					}
				}
			}
			if n.Data == "p" || n.Data == "br" || n.Data == "div" {
				out.WriteString("\n")
			}
		}
	}
	walk(root)
	return strings.TrimSpace(out.String())
}
func writeMIMEBase64(w io.Writer, data []byte) error {
	var encoded [76]byte
	for len(data) > 0 {
		chunkSize := 57
		if len(data) < chunkSize {
			chunkSize = len(data)
		}
		encodedLength := base64.StdEncoding.EncodedLen(chunkSize)
		base64.StdEncoding.Encode(encoded[:encodedLength], data[:chunkSize])
		n, err := w.Write(encoded[:encodedLength])
		if err != nil {
			return err
		}
		if n != encodedLength {
			return io.ErrShortWrite
		}
		data = data[chunkSize:]
		if len(data) > 0 {
			if _, err := io.WriteString(w, "\r\n"); err != nil {
				return err
			}
		}
	}
	return nil
}
func writePart(w *multipart.Writer, contentType, body string) error {
	h := textproto.MIMEHeader{}
	h.Set("Content-Type", contentType+"; charset=UTF-8")
	h.Set("Content-Transfer-Encoding", "base64")
	part, err := w.CreatePart(h)
	if err != nil {
		return err
	}
	return writeMIMEBase64(part, []byte(body))
}
func BuildMIME(ctx context.Context, m mailboxdomain.Mailbox, d domain.Draft, p domain.PlannedDelivery, pixel string, attachments interface {
	Load(context.Context, string, string) ([]byte, string, string, error)
}, owner string) (string, string, error) {
	policy := bluemonday.UGCPolicy()
	body := policy.Sanitize(d.HTML)
	safe := body + `<img src="` + html.EscapeString(pixel) + `" width="1" height="1" alt="" style="display:none" />`
	var altBuf bytes.Buffer
	alt := multipart.NewWriter(&altBuf)
	if err := writePart(alt, "text/plain", plainText(body)); err != nil {
		return "", "", err
	}
	if err := writePart(alt, "text/html", safe); err != nil {
		return "", "", err
	}
	if err := alt.Close(); err != nil {
		return "", "", err
	}
	var mixedBuf bytes.Buffer
	mixed := multipart.NewWriter(&mixedBuf)
	h := textproto.MIMEHeader{}
	h.Set("Content-Type", `multipart/alternative; boundary="`+alt.Boundary()+`"`)
	part, err := mixed.CreatePart(h)
	if err != nil {
		return "", "", err
	}
	if _, err := part.Write(altBuf.Bytes()); err != nil {
		return "", "", err
	}
	for _, id := range d.Attachments {
		if attachments == nil {
			return "", "", errors.New("attachment storage unavailable")
		}
		data, name, kind, err := attachments.Load(ctx, owner, id)
		if err != nil {
			return "", "", err
		}
		h := textproto.MIMEHeader{}
		h.Set("Content-Type", mime.FormatMediaType(kind, map[string]string{"name": name}))
		h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
		h.Set("Content-Transfer-Encoding", "base64")
		part, err := mixed.CreatePart(h)
		if err != nil {
			return "", "", err
		}
		if err := writeMIMEBase64(part, data); err != nil {
			return "", "", err
		}
	}
	if err := mixed.Close(); err != nil {
		return "", "", err
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "From: %s\r\n", cleanHeader(m.Email))
	fmt.Fprintf(&buf, "To: %s\r\n", cleanHeader(strings.Join(p.To, ", ")))
	if len(p.Cc) > 0 {
		fmt.Fprintf(&buf, "Cc: %s\r\n", cleanHeader(strings.Join(p.Cc, ", ")))
	}
	if len(p.Bcc) > 0 {
		fmt.Fprintf(&buf, "Bcc: %s\r\n", cleanHeader(strings.Join(p.Bcc, ", ")))
	}
	fmt.Fprintf(&buf, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", cleanHeader(d.Subject)))
	u, _ := url.Parse(pixel)
	domainName := u.Hostname()
	if domainName == "" {
		domainName = "mail-tracker.local"
	}
	rfcID := "<" + uuid.NewString() + "@" + domainName + ">"
	fmt.Fprintf(&buf, "Message-ID: %s\r\n", rfcID)
	if d.ReplyToMessageID != "" {
		fmt.Fprintf(&buf, "In-Reply-To: %s\r\nReferences: %s\r\n", cleanHeader(d.ReplyToMessageID), cleanHeader(d.ReplyToMessageID))
	}
	fmt.Fprintf(&buf, "MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=%q\r\n\r\n", mixed.Boundary())
	buf.Write(mixedBuf.Bytes())
	return buf.String(), rfcID, nil
}
func (g Gmail) Send(ctx context.Context, mailboxID string, d domain.Draft, p domain.PlannedDelivery, pixelURL string) (corrapp.SentMessage, error) {
	m, tok, err := g.OAuth.Credentials(ctx, mailboxID)
	if err != nil {
		return corrapp.SentMessage{}, err
	}
	raw, rfcID, err := BuildMIME(ctx, m, d, p, pixelURL, g.Attachments, m.OwnerID)
	if err != nil {
		return corrapp.SentMessage{}, err
	}
	body := map[string]string{"raw": base64.RawURLEncoding.EncodeToString([]byte(raw))}
	if d.ThreadID != "" {
		body["threadId"] = d.ThreadID
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return corrapp.SentMessage{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://gmail.googleapis.com/gmail/v1/users/me/messages/send", bytes.NewReader(encoded))
	if err != nil {
		return corrapp.SentMessage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := oauth2.NewClient(ctx, oauth2.StaticTokenSource(tok))
	resp, err := client.Do(req)
	if err != nil {
		return corrapp.SentMessage{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusUnauthorized {
		g.OAuth.ForgetToken(mailboxID)
	}
	if resp.StatusCode >= 500 {
		return corrapp.SentMessage{}, fmt.Errorf("%w: Gmail API %d", corrapp.ErrAmbiguous, resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return corrapp.SentMessage{}, fmt.Errorf("Gmail API %d: %s", resp.StatusCode, data)
	}
	var sent struct {
		ID       string `json:"id"`
		ThreadID string `json:"threadId"`
	}
	if err := json.Unmarshal(data, &sent); err != nil {
		return corrapp.SentMessage{}, fmt.Errorf("%w: invalid Gmail response", corrapp.ErrAmbiguous)
	}
	if sent.ID == "" {
		return corrapp.SentMessage{}, fmt.Errorf("%w: missing Gmail message ID", corrapp.ErrAmbiguous)
	}
	return corrapp.SentMessage{GmailID: sent.ID, ThreadID: sent.ThreadID, RFCMessageID: rfcID}, nil
}
