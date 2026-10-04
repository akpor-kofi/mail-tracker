package mailsync

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/akpor-kofi/mail-tracker/apps/api/internal/analytics"
	mailboxapp "github.com/akpor-kofi/mail-tracker/apps/api/internal/mailboxes/application"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log"
	"net/http"
	"net/mail"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Worker struct {
	Pool     *pgxpool.Pool
	OAuth    mailboxapp.OAuthService
	Activity analytics.Store
}
type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
type Part struct {
	MimeType string   `json:"mimeType"`
	Headers  []Header `json:"headers"`
	Body     struct {
		Data string `json:"data"`
	} `json:"body"`
	Parts []Part `json:"parts"`
}
type Message struct {
	ID           string   `json:"id"`
	ThreadID     string   `json:"threadId"`
	Snippet      string   `json:"snippet"`
	InternalDate string   `json:"internalDate"`
	Payload      Part     `json:"payload"`
	Labels       []string `json:"labelIds"`
}

func header(p Part, n string) string {
	for _, h := range p.Headers {
		if strings.EqualFold(h.Name, n) {
			return h.Value
		}
	}
	return ""
}
func References(p Part) string {
	s := header(p, "In-Reply-To") + " " + header(p, "References")
	for _, child := range p.Parts {
		s += " " + References(child)
		if child.MimeType == "message/rfc822" {
			s += " " + header(child, "Message-ID")
		}
	}
	return s
}
func BounceRecipients(p Part) []string {
	out := []string{}
	if p.MimeType == "message/delivery-status" {
		b, e := base64.RawURLEncoding.DecodeString(p.Body.Data)
		if e == nil {
			r := textproto.NewReader(bufio.NewReader(strings.NewReader(strings.ReplaceAll(string(b), "\r\n", "\n"))))
			for {
				h, e := r.ReadMIMEHeader()
				if e != nil {
					break
				}
				if strings.EqualFold(h.Get("Action"), "failed") && strings.HasPrefix(h.Get("Status"), "5.") {
					recipient := strings.TrimSpace(h.Get("Final-Recipient"))
					if i := strings.Index(recipient, ";"); i >= 0 {
						recipient = strings.TrimSpace(recipient[i+1:])
					}
					out = append(out, recipient)
				}
			}
		}
	}
	for _, child := range p.Parts {
		out = append(out, BounceRecipients(child)...)
	}
	return out
}
func IsAutomatic(p Part) bool {
	return header(p, "Auto-Submitted") != "" && !strings.EqualFold(header(p, "Auto-Submitted"), "no") || strings.EqualFold(header(p, "Precedence"), "bulk") || strings.EqualFold(header(p, "Precedence"), "list")
}
func (w Worker) get(ctx context.Context, token, path string, out any) error {
	req, e := http.NewRequestWithContext(ctx, "GET", "https://gmail.googleapis.com/gmail/v1/users/me/"+path, nil)
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, e := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return errors.New("history_expired")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Gmail sync HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 5<<20)).Decode(out)
}
func (w Worker) Sync(ctx context.Context, id string) error {
	m, tok, e := w.OAuth.Credentials(ctx, id)
	if e != nil {
		return e
	}
	var cursor string
	_ = w.Pool.QueryRow(ctx, `SELECT history_id FROM mailbox_sync WHERE mailbox_id=$1`, id).Scan(&cursor)
	var profile struct {
		HistoryID string `json:"historyId"`
	}
	if e = w.get(ctx, tok.AccessToken, "profile", &profile); e != nil {
		return e
	}
	ids := map[string]bool{}
	nextCursor := profile.HistoryID
	if cursor != "" {
		page := ""
		for i := 0; i < 20; i++ {
			var h struct {
				HistoryID string `json:"historyId"`
				Next      string `json:"nextPageToken"`
				History   []struct {
					Added []struct {
						Message struct {
							ID string `json:"id"`
						} `json:"message"`
					} `json:"messagesAdded"`
				} `json:"history"`
			}
			path := "history?startHistoryId=" + url.QueryEscape(cursor) + "&historyTypes=messageAdded&maxResults=100"
			if page != "" {
				path += "&pageToken=" + url.QueryEscape(page)
			}
			e = w.get(ctx, tok.AccessToken, path, &h)
			if e != nil {
				if e.Error() == "history_expired" {
					cursor = ""
					break
				}
				return e
			}
			for _, r := range h.History {
				for _, a := range r.Added {
					ids[a.Message.ID] = true
				}
			}
			nextCursor = h.HistoryID
			page = h.Next
			if page == "" {
				break
			}
			if i == 19 {
				return errors.New("sync backlog exceeds one pass")
			}
		}
	}
	if cursor == "" {
		var list struct {
			Messages []struct {
				ID string `json:"id"`
			} `json:"messages"`
			Next string `json:"nextPageToken"`
		}
		page := ""
		for i := 0; i < 10; i++ {
			path := "messages?maxResults=100&q=" + url.QueryEscape("newer_than:30d")
			if page != "" {
				path += "&pageToken=" + url.QueryEscape(page)
			}
			if e = w.get(ctx, tok.AccessToken, path, &list); e != nil {
				return e
			}
			for _, v := range list.Messages {
				ids[v.ID] = true
			}
			page = list.Next
			if page == "" {
				break
			}
			if i == 9 {
				return errors.New("initial sync exceeds 1000 recent messages")
			}
		}
	}
	for msgID := range ids {
		var msg Message
		if e = w.get(ctx, tok.AccessToken, "messages/"+url.PathEscape(msgID)+"?format=full", &msg); e != nil {
			if e.Error() == "history_expired" {
				continue
			}
			return e
		}
		if e = w.Save(ctx, id, m.Email, msg); e != nil {
			return e
		}
	}
	_, e = w.Pool.Exec(ctx, `INSERT INTO mailbox_sync(mailbox_id,history_id,last_success_at,status,error) VALUES($1,$2,now(),'ready','') ON CONFLICT(mailbox_id) DO UPDATE SET history_id=excluded.history_id,last_success_at=now(),status='ready',error=''`, id, nextCursor)
	return e
}
func (w Worker) Save(ctx context.Context, mailbox, sender string, msg Message) error {
	from := header(msg.Payload, "From")
	parsed, e := mail.ParseAddress(from)
	direction := "incoming"
	if e == nil && strings.EqualFold(parsed.Address, sender) {
		direction = "outgoing"
	}
	ms, e := strconv.ParseInt(msg.InternalDate, 10, 64)
	if e != nil {
		return nil
	}
	at := time.UnixMilli(ms).UTC()
	refs := References(msg.Payload)
	bounces := BounceRecipients(msg.Payload)
	rows, e := w.Pool.Query(ctx, `SELECT d.id,COALESCE(d.rfc_message_id,''),d.recipients FROM deliveries d JOIN conversations c ON c.id=d.conversation_id WHERE c.mailbox_id=$1 AND d.gmail_thread_id=$2`, mailbox, msg.ThreadID)
	if e != nil {
		return e
	}
	type match struct {
		id, ref    string
		recipients []string
	}
	matches := []match{}
	for rows.Next() {
		var x match
		if e := rows.Scan(&x.id, &x.ref, &x.recipients); e != nil {
			rows.Close()
			return e
		}
		matches = append(matches, x)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if len(matches) == 0 {
		return nil
	}
	_, e = w.Pool.Exec(ctx, `INSERT INTO synced_messages(id,mailbox_id,gmail_id,thread_id,subject,sender,snippet,direction,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(mailbox_id,gmail_id) DO NOTHING`, uuid.NewString(), mailbox, msg.ID, msg.ThreadID, header(msg.Payload, "Subject"), from, msg.Snippet, direction, at)
	if e != nil {
		return e
	}
	if direction == "outgoing" {
		return nil
	}
	for _, x := range matches {
		if x.ref == "" || !strings.Contains(refs, x.ref) {
			continue
		}
		kind := "reply"
		source := "gmail_headers"
		if len(bounces) > 0 {
			kind = "bounce"
			source = "gmail_delivery_report"
			matched := false
			for _, a := range bounces {
				for _, b := range x.recipients {
					if strings.EqualFold(a, b) {
						matched = true
					}
				}
			}
			if !matched {
				continue
			}
		} else {
			if IsAutomatic(msg.Payload) {
				continue
			}
			allowed := false
			if parsed != nil {
				for _, to := range x.recipients {
					if strings.EqualFold(parsed.Address, to) {
						allowed = true
					}
				}
			}
			if !allowed {
				continue
			}
		}
		// A deterministic dedupe key makes replayed history notifications safe.
		if e = w.Activity.RecordAt(ctx, x.id, kind, source, "", nil, map[string]any{"gmailMessageId": msg.ID, "messageAt": at}, "gmail:"+msg.ID+":"+kind, at); e != nil {
			return e
		}
	}
	return nil
}
func (w Worker) Run(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			rows, e := w.Pool.Query(ctx, `SELECT id FROM mailboxes WHERE sync_enabled ORDER BY connected_at LIMIT 20`)
			if e != nil {
				continue
			}
			ids := []string{}
			for rows.Next() {
				var id string
				if rows.Scan(&id) == nil {
					ids = append(ids, id)
				}
			}
			rows.Close()
			for _, id := range ids {
				syncCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
				e := w.Sync(syncCtx, id)
				cancel()
				if e != nil {
					log.Print("mailbox sync delayed; check Settings")
					_, _ = w.Pool.Exec(ctx, `INSERT INTO mailbox_sync(mailbox_id,status,error) VALUES($1,'error','Read synchronization failed. Check the connection and retry.') ON CONFLICT(mailbox_id) DO UPDATE SET status='error',error=excluded.error`, id)
				}
			}
		}
	}
}
