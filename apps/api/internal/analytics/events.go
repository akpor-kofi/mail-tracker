package analytics

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/net/html"
)

type Store struct {
	Pool      *pgxpool.Pool
	PublicURL string
}

func Token() (string, []byte, error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", nil, e
	}
	t := base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(t))
	return t, h[:], nil
}
func TokenHash(t string) ([]byte, error) {
	b, e := base64.RawURLEncoding.DecodeString(t)
	if e != nil || len(b) != 32 {
		return nil, errors.New("invalid token")
	}
	h := sha256.Sum256([]byte(t))
	return h[:], nil
}
func Source(ua string) string {
	s := strings.ToLower(ua)
	switch {
	case strings.Contains(s, "googleimageproxy"):
		return "image_proxy"
	case strings.Contains(s, "bot"), strings.Contains(s, "scanner"), strings.Contains(s, "spider"), strings.Contains(s, "proofpoint"):
		return "suspected_automation"
	default:
		return "unclassified"
	}
}
func ValidDestination(s string) bool {
	u, e := url.Parse(s)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && !strings.ContainsAny(s, "\r\n")
}

func BrowserFamily(ua string) string {
	u := strings.ToLower(ua)
	switch {
	case Source(ua) == "image_proxy":
		return "proxy"
	case strings.Contains(u, "edg/"):
		return "Edge"
	case strings.Contains(u, "chrome/"):
		return "Chrome"
	case strings.Contains(u, "firefox/"):
		return "Firefox"
	case strings.Contains(u, "safari/"):
		return "Safari"
	default:
		return "unknown"
	}
}

// RewriteLinks runs inside send preparation. A retry returning an existing attempt never issues new tokens.
func RewriteLinks(ctx context.Context, tx pgx.Tx, delivery, body, base string) (string, error) {
	return RewriteLinksWithAttribution(ctx, tx, delivery, body, base, false)
}
func RewriteLinksWithAttribution(ctx context.Context, tx pgx.Tx, delivery, body, base string, conversion bool) (string, error) {
	root, e := html.Parse(strings.NewReader(body))
	if e != nil {
		return "", e
	}
	var walk func(*html.Node) error
	walk = func(n *html.Node) error {
		if n.Type == html.ElementNode && n.Data == "a" {
			skip := false
			for _, a := range n.Attr {
				if a.Key == "data-no-track" || a.Key == "rel" && strings.Contains(a.Val, "unsubscribe") {
					skip = true
				}
			}
			for i, a := range n.Attr {
				if a.Key != "href" || skip || !ValidDestination(a.Val) || strings.HasPrefix(a.Val, base+"/c/") {
					continue
				}
				t, h, e := Token()
				if e != nil {
					return e
				}
				dest := a.Val
				var ah []byte
				if conversion {
					ref, h, e := Token()
					if e != nil {
						return e
					}
					ah = h
					u, _ := url.Parse(dest)
					q := u.Query()
					q.Set("mt_ref", ref)
					u.RawQuery = q.Encode()
					dest = u.String()
				}
				_, e = tx.Exec(ctx, `INSERT INTO tracked_links(id,delivery_id,token_hash,destination,attribution_hash) VALUES($1,$2,$3,$4,$5)`, uuid.NewString(), delivery, h, dest, ah)
				if e != nil {
					return e
				}
				n.Attr[i].Val = base + "/c/" + t
			}
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			if e := walk(ch); e != nil {
				return e
			}
		}
		return nil
	}
	if e := walk(root); e != nil {
		return "", e
	}
	var b strings.Builder
	e = html.Render(&b, root)
	return b.String(), e
}
func (s Store) Record(ctx context.Context, delivery, kind, source, link string, session *string, meta any, dedupe string) error {
	return s.RecordAt(ctx, delivery, kind, source, link, session, meta, dedupe, time.Now().UTC())
}
func (s Store) RecordAt(ctx context.Context, delivery, kind, source, link string, session *string, meta any, dedupe string, at time.Time) error {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var own string
	e = tx.QueryRow(ctx, `UPDATE deliveries d SET activity_count=activity_count+1 WHERE d.id=$1 AND activity_count<10000 RETURNING (SELECT owner_id FROM conversations WHERE id=d.conversation_id)`, delivery).Scan(&own)
	if errors.Is(e, pgx.ErrNoRows) {
		_, e = tx.Exec(ctx, `UPDATE deliveries SET activity_capped=true WHERE id=$1`, delivery)
		if e != nil {
			return e
		}
		return tx.Commit(ctx)
	}
	if e != nil {
		return e
	}
	raw, e := json.Marshal(meta)
	if e != nil {
		return e
	}
	tag, e := tx.Exec(ctx, `INSERT INTO activity_events(delivery_id,kind,source,link_id,session_id,metadata,dedupe_key,occurred_at) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,NULLIF($7,''),$8) ON CONFLICT(delivery_id,dedupe_key) DO NOTHING`, delivery, kind, source, link, session, raw, dedupe, at)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		return nil
	} // Roll back the quota reservation on duplicate.
	_, e = tx.Exec(ctx, `INSERT INTO activity_outbox(owner_id) VALUES($1)`, own)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s Store) Pixel(ctx context.Context, hash []byte, ua string) error {
	var d string
	e := s.Pool.QueryRow(ctx, `SELECT id FROM deliveries WHERE pixel_token_hash=$1`, hash).Scan(&d)
	if e != nil {
		return e
	}
	return s.Record(ctx, d, "image_request", Source(ua), "", nil, map[string]any{"browserFamily": BrowserFamily(ua), "classifierVersion": "v1", "network": "not_collected"}, "")
}
func (s Store) Redirect(ctx context.Context, hash []byte, ua string, log bool) (string, error) {
	var id, dest, d string
	var enabled bool
	e := s.Pool.QueryRow(ctx, `SELECT id,delivery_id,destination,logging_enabled FROM tracked_links WHERE token_hash=$1`, hash).Scan(&id, &d, &dest, &enabled)
	if e != nil {
		return "", e
	}
	if !ValidDestination(dest) {
		return "", errors.New("invalid destination")
	}
	if log && enabled {
		_ = s.Record(ctx, d, "link_request", Source(ua), id, nil, map[string]any{"browserFamily": BrowserFamily(ua), "classifierVersion": "v1", "network": "not_collected"}, "")
	}
	return dest, nil
}

// DrainOutbox is a durable invalidation queue; dashboard polling also reconciles after reconnect.
func (s Store) DrainOutbox(ctx context.Context, publish func(string)) error {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, `SELECT id,owner_id FROM activity_outbox ORDER BY id LIMIT 1000 FOR UPDATE SKIP LOCKED`)
	if e != nil {
		return e
	}
	ids := []int64{}
	seen := map[string]bool{}
	for rows.Next() {
		var id int64
		var o string
		if e := rows.Scan(&id, &o); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
		seen[o] = true
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	// Publish before acknowledging. A crash may repeat an invalidation, never discard it.
	for o := range seen {
		publish(o)
	}
	if _, e = tx.Exec(ctx, `DELETE FROM activity_outbox WHERE id=ANY($1)`, ids); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s Store) Run(ctx context.Context, publish func(string)) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = s.DrainOutbox(ctx, publish)
		}
	}
}
