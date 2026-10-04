package documents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/akpor-kofi/mail-tracker/apps/api/internal/analytics"
	"github.com/akpor-kofi/raildrop/sdk/go"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"strings"
	"time"
)

const MaxBytes = 20 << 20

var ErrInvalid = errors.New("only PDFs, JPEG and PNG images up to 20 MB are supported")

type Store struct {
	Pool      *pgxpool.Pool
	Storage   raildrop.Storage
	PublicURL string
}
type Document struct {
	ID            string    `json:"id"`
	Filename      string    `json:"filename"`
	Type          string    `json:"contentType"`
	Size          int64     `json:"sizeBytes"`
	CreatedAt     time.Time `json:"createdAt"`
	Sessions      int       `json:"sessions"`
	ActiveSeconds float64   `json:"activeSeconds"`
	Downloads     int       `json:"downloads"`
}

func Validate(data []byte) (string, error) {
	if len(data) == 0 || len(data) > MaxBytes {
		return "", ErrInvalid
	}
	if bytes.HasPrefix(data, []byte("%PDF-")) {
		return "application/pdf", nil
	}
	mime := http.DetectContentType(data)
	if mime != "image/jpeg" && mime != "image/png" {
		return "", ErrInvalid
	}
	cfg, _, e := image.DecodeConfig(bytes.NewReader(data))
	if e != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 40000000 {
		return "", ErrInvalid
	}
	return mime, nil
}
func (s Store) Upload(ctx context.Context, o, name string, data []byte) (Document, error) {
	var d Document
	if s.Storage == nil {
		return d, errors.New("document storage is not configured")
	}
	typ, e := Validate(data)
	if e != nil {
		return d, e
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 240 || strings.ContainsAny(name, "\r\n\x00") {
		return d, ErrInvalid
	}
	d = Document{ID: uuid.NewString(), Filename: name, Type: typ, Size: int64(len(data))}
	key := "private/documents/" + d.ID
	sum := sha256.Sum256(data)
	// Reserve an orphan-cleanup record before uploading. Completed documents are excluded from cleanup.
	if _, e = s.Pool.Exec(ctx, `INSERT INTO document_uploads(object_key) VALUES($1)`, key); e != nil {
		return d, e
	}
	_, e = s.Storage.Put(ctx, raildrop.PutArgs{Key: key, Body: data, ContentType: typ, CacheControl: "private, no-store", Metadata: map[string]string{"access": "private", "owner": o}})
	if e != nil {
		return d, e
	}
	e = s.Pool.QueryRow(ctx, `INSERT INTO documents(id,owner_id,filename,content_type,object_key,size_bytes,checksum) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING created_at`, d.ID, o, name, typ, key, d.Size, hex.EncodeToString(sum[:])).Scan(&d.CreatedAt)
	if e != nil {
		_ = s.Storage.Delete(ctx, key)
	} else {
		_, _ = s.Pool.Exec(ctx, `DELETE FROM document_uploads WHERE object_key=$1`, key)
	}
	return d, e
}
func (s Store) List(ctx context.Context, o string) ([]Document, error) {
	rows, e := s.Pool.Query(ctx, `SELECT d.id,d.filename,d.content_type,d.size_bytes,d.created_at,count(DISTINCT v.id) FILTER(WHERE v.loaded),COALESCE(sum(v.active_ms),0)/1000.0,(SELECT count(*) FROM viewer_events ve JOIN viewer_sessions vs ON vs.id=ve.session_id JOIN document_shares ds ON ds.id=vs.share_id WHERE ds.document_id=d.id AND ve.kind='download_request') FROM documents d LEFT JOIN document_shares s ON s.document_id=d.id LEFT JOIN viewer_sessions v ON v.share_id=s.id WHERE d.owner_id=$1 GROUP BY d.id ORDER BY d.created_at DESC LIMIT 200`, o)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Document{}
	for rows.Next() {
		var d Document
		if e := rows.Scan(&d.ID, &d.Filename, &d.Type, &d.Size, &d.CreatedAt, &d.Sessions, &d.ActiveSeconds, &d.Downloads); e != nil {
			return nil, e
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

type Share struct {
	ID            string     `json:"id"`
	DocumentID    string     `json:"documentId"`
	URL           string     `json:"url,omitempty"`
	ExpiresAt     time.Time  `json:"expiresAt"`
	RevokedAt     *time.Time `json:"revokedAt"`
	AllowDownload bool       `json:"allowDownload"`
}

func CreateShare(ctx context.Context, tx pgx.Tx, o, doc, delivery, base string, days int, download bool) (Share, error) {
	var name string
	if e := tx.QueryRow(ctx, `SELECT filename FROM documents WHERE id=$1 AND owner_id=$2 FOR SHARE`, doc, o).Scan(&name); e != nil {
		return Share{}, e
	}
	if delivery != "" {
		var who string
		if e := tx.QueryRow(ctx, `SELECT c.owner_id FROM deliveries d JOIN conversations c ON c.id=d.conversation_id WHERE d.id=$1`, delivery).Scan(&who); e != nil {
			return Share{}, e
		}
		if who != o {
			return Share{}, pgx.ErrNoRows
		}
	}
	t, h, e := analytics.Token()
	if e != nil {
		return Share{}, e
	}
	v := Share{ID: uuid.NewString(), DocumentID: doc, URL: base + "/d/" + t, ExpiresAt: time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour), AllowDownload: download}
	_, e = tx.Exec(ctx, `INSERT INTO document_shares(id,document_id,delivery_id,token_hash,expires_at,allow_download) VALUES($1,$2,NULLIF($3,'')::uuid,$4,$5,$6)`, v.ID, doc, delivery, h, v.ExpiresAt, download)
	return v, e
}
func (s Store) Share(ctx context.Context, o, doc string, days int, download bool) (Share, error) {
	if days < 1 || days > 365 {
		return Share{}, errors.New("expiry must be 1 to 365 days")
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return Share{}, e
	}
	defer tx.Rollback(ctx)
	v, e := CreateShare(ctx, tx, o, doc, "", s.PublicURL, days, download)
	if e != nil {
		return v, e
	}
	return v, tx.Commit(ctx)
}
func (s Store) Shares(ctx context.Context, o, doc string) ([]Share, error) {
	rows, e := s.Pool.Query(ctx, `SELECT s.id,s.document_id,s.expires_at,s.revoked_at,s.allow_download FROM document_shares s JOIN documents d ON d.id=s.document_id WHERE d.owner_id=$1 AND d.id=$2 ORDER BY s.created_at DESC LIMIT 100`, o, doc)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Share{}
	for rows.Next() {
		var s Share
		if e := rows.Scan(&s.ID, &s.DocumentID, &s.ExpiresAt, &s.RevokedAt, &s.AllowDownload); e != nil {
			return nil, e
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
func (s Store) Revoke(ctx context.Context, o, id string) error {
	tag, e := s.Pool.Exec(ctx, `UPDATE document_shares s SET revoked_at=now() FROM documents d WHERE d.id=s.document_id AND d.owner_id=$1 AND s.id=$2`, o, id)
	if e == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return e
}

type Session struct {
	ID            string    `json:"id"`
	DocumentID    string    `json:"documentId"`
	Filename      string    `json:"filename"`
	Type          string    `json:"contentType"`
	Size          int64     `json:"sizeBytes"`
	AllowDownload bool      `json:"allowDownload"`
	ExpiresAt     time.Time `json:"expiresAt"`
	Key           string    `json:"-"`
	DeliveryID    string    `json:"-"`
	Secret        string    `json:"-"`
}

func (s Store) Start(ctx context.Context, token string) (Session, error) {
	h, e := analytics.TokenHash(token)
	if e != nil {
		return Session{}, pgx.ErrNoRows
	}
	v := Session{ID: uuid.NewString()}
	var share string
	var exp time.Time
	e = s.Pool.QueryRow(ctx, `SELECT s.id,d.id,d.filename,d.content_type,d.size_bytes,s.allow_download,s.expires_at FROM document_shares s JOIN documents d ON d.id=s.document_id WHERE s.token_hash=$1 AND s.revoked_at IS NULL AND s.expires_at>now()`, h).Scan(&share, &v.DocumentID, &v.Filename, &v.Type, &v.Size, &v.AllowDownload, &exp)
	if e != nil {
		return v, e
	}
	secret, hash, e := analytics.Token()
	if e != nil {
		return v, e
	}
	v.Secret = secret
	v.ExpiresAt = time.Now().UTC().Add(time.Hour)
	if exp.Before(v.ExpiresAt) {
		v.ExpiresAt = exp
	}
	_, e = s.Pool.Exec(ctx, `INSERT INTO viewer_sessions(id,share_id,secret_hash,expires_at) VALUES($1,$2,$3,$4)`, v.ID, share, hash, v.ExpiresAt)
	return v, e
}
func (s Store) Session(ctx context.Context, id, secret string) (Session, error) {
	h, e := analytics.TokenHash(secret)
	if e != nil {
		return Session{}, pgx.ErrNoRows
	}
	var v Session
	e = s.Pool.QueryRow(ctx, `SELECT v.id,d.id,d.filename,d.content_type,d.size_bytes,s.allow_download,v.expires_at,d.object_key,COALESCE(s.delivery_id::text,'') FROM viewer_sessions v JOIN document_shares s ON s.id=v.share_id JOIN documents d ON d.id=s.document_id WHERE v.id=$1 AND v.secret_hash=$2 AND v.expires_at>now() AND s.expires_at>now() AND s.revoked_at IS NULL`, id, h).Scan(&v.ID, &v.DocumentID, &v.Filename, &v.Type, &v.Size, &v.AllowDownload, &v.ExpiresAt, &v.Key, &v.DeliveryID)
	return v, e
}

type Batch struct {
	ID       string `json:"batchId"`
	Kind     string `json:"kind"`
	ActiveMS int    `json:"activeMs"`
	Pages    []int  `json:"pages"`
}

func (s Store) Observe(ctx context.Context, v Session, b Batch) error {
	if _, e := uuid.Parse(b.ID); e != nil {
		return errors.New("invalid batch id")
	}
	if b.Kind != "viewer_loaded" && b.Kind != "viewer_activity" && b.Kind != "page_exposure" && b.Kind != "download_request" {
		return errors.New("invalid event type")
	}
	if b.Kind == "download_request" && !v.AllowDownload {
		return errors.New("downloads disabled")
	}
	if b.ActiveMS < 0 || b.ActiveMS > 15000 || len(b.Pages) > 100 {
		return errors.New("invalid telemetry")
	}
	for _, p := range b.Pages {
		if p < 1 || p > 1000 {
			return errors.New("invalid page")
		}
	}
	if b.Kind != "viewer_activity" {
		b.ActiveMS = 0
	}
	if b.Pages == nil {
		b.Pages = []int{}
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var active, wall int64
	e = tx.QueryRow(ctx, `SELECT active_ms,GREATEST(0,EXTRACT(EPOCH FROM now()-created_at)*1000)::bigint FROM viewer_sessions WHERE id=$1 FOR UPDATE`, v.ID).Scan(&active, &wall)
	if e != nil {
		return e
	}
	if active+int64(b.ActiveMS) > wall {
		b.ActiveMS = int(max(int64(0), wall-active))
	}
	tag, e := tx.Exec(ctx, `INSERT INTO viewer_events(session_id,batch_id,kind,active_ms,pages) VALUES($1,$2,$3,$4,$5) ON CONFLICT(session_id,batch_id) DO NOTHING`, v.ID, b.ID, b.Kind, b.ActiveMS, b.Pages)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	_, e = tx.Exec(ctx, `UPDATE viewer_sessions SET active_ms=active_ms+$2,loaded=loaded OR $3='viewer_loaded' WHERE id=$1`, v.ID, b.ActiveMS, b.Kind)
	if e != nil {
		return e
	}
	if v.DeliveryID != "" {
		var own string
		e = tx.QueryRow(ctx, `UPDATE deliveries d SET activity_count=activity_count+1 WHERE d.id=$1 AND activity_count<10000 RETURNING (SELECT owner_id FROM conversations WHERE id=d.conversation_id)`, v.DeliveryID).Scan(&own)
		if e == nil {
			meta, _ := json.Marshal(map[string]any{"activeMs": b.ActiveMS, "pages": b.Pages, "documentId": v.DocumentID})
			_, e = tx.Exec(ctx, `INSERT INTO activity_events(delivery_id,kind,source,session_id,metadata,dedupe_key) VALUES($1,$2,'browser_session',$3,$4,$5) ON CONFLICT(delivery_id,dedupe_key) DO NOTHING`, v.DeliveryID, b.Kind, v.ID, meta, b.ID)
			if e != nil {
				return e
			}
			_, e = tx.Exec(ctx, `INSERT INTO activity_outbox(owner_id) VALUES($1)`, own)
			if e != nil {
				return e
			}
		} else if errors.Is(e, pgx.ErrNoRows) {
			_, e = tx.Exec(ctx, `UPDATE deliveries SET activity_capped=true WHERE id=$1`, v.DeliveryID)
			if e != nil {
				return e
			}
		} else {
			return e
		}
	}
	return tx.Commit(ctx)
}

func (s Store) Cleanup(ctx context.Context) error {
	if s.Storage == nil {
		return nil
	}
	rows, e := s.Pool.Query(ctx, `SELECT object_key FROM document_uploads WHERE expires_at<now() AND NOT EXISTS(SELECT 1 FROM documents WHERE documents.object_key=document_uploads.object_key) LIMIT 100`)
	if e != nil {
		return e
	}
	keys := []string{}
	for rows.Next() {
		var k string
		if e := rows.Scan(&k); e != nil {
			rows.Close()
			return e
		}
		keys = append(keys, k)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, k := range keys {
		if e := s.Storage.Delete(ctx, k); e != nil {
			return e
		}
		if _, e := s.Pool.Exec(ctx, `DELETE FROM document_uploads WHERE object_key=$1`, k); e != nil {
			return e
		}
	}
	return nil
}

// Delete invalidates every share immediately and queues private object removal durably.
func (s Store) Delete(ctx context.Context, owner, id string) error {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var key string
	if e = tx.QueryRow(ctx, `DELETE FROM documents WHERE id=$1 AND owner_id=$2 RETURNING object_key`, id, owner).Scan(&key); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO document_uploads(object_key,expires_at) VALUES($1,now()) ON CONFLICT(object_key) DO UPDATE SET expires_at=now()`, key); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
