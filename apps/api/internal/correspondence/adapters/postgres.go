package adapters

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/akpor-kofi/mail-tracker/apps/api/internal/correspondence/application"
	"github.com/akpor-kofi/mail-tracker/apps/api/internal/correspondence/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct{ Pool *pgxpool.Pool }

func (r Postgres) CreateAttempt(ctx context.Context, owner, key string, d domain.Draft, mode string, ds []application.Delivery) (string, bool, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback(ctx)
	id := uuid.NewString()
	tag, err := tx.Exec(ctx, `INSERT INTO conversations(id,owner_id,mailbox_id,subject,mode,status,idempotency_key) SELECT $1,$2,$3,$4,$5,'pending',$6 WHERE EXISTS(SELECT 1 FROM mailboxes WHERE id=$3 AND owner_id=$2) ON CONFLICT(owner_id,idempotency_key) DO NOTHING`, id, owner, d.MailboxID, d.Subject, mode, key)
	if err != nil {
		return "", false, err
	}
	if tag.RowsAffected() == 0 {
		err := tx.QueryRow(ctx, `SELECT id FROM conversations WHERE owner_id=$1 AND idempotency_key=$2`, owner, key).Scan(&id)
		if err != nil {
			return "", false, errors.New("mailbox not found or idempotency conflict")
		}
		return id, true, nil
	}
	for _, delivery := range ds {
		hash := sha256.Sum256([]byte(delivery.PixelToken))
		_, err = tx.Exec(ctx, `INSERT INTO deliveries(id,conversation_id,recipients,pixel_token_hash,status) VALUES($1,$2,$3,$4,'pending')`, delivery.ID, id, delivery.Recipients, hash[:])
		if err != nil {
			return "", false, err
		}
	}
	return id, false, tx.Commit(ctx)
}
func (r Postgres) UpdateDelivery(ctx context.Context, id, status string, sent application.SentMessage, errText string) error {
	_, err := r.Pool.Exec(ctx, `UPDATE deliveries SET status=$2,gmail_message_id=NULLIF($3,''),gmail_thread_id=NULLIF($4,''),rfc_message_id=NULLIF($5,''),error=NULLIF($6,'') WHERE id=$1`, id, status, sent.GmailID, sent.ThreadID, sent.RFCMessageID, errText)
	if err != nil {
		return err
	}
	_, err = r.Pool.Exec(ctx, `UPDATE conversations SET updated_at=now(),status=CASE WHEN EXISTS(SELECT 1 FROM deliveries WHERE conversation_id=conversations.id AND status='unknown') THEN 'unknown' WHEN EXISTS(SELECT 1 FROM deliveries WHERE conversation_id=conversations.id AND status='failed') THEN 'partial_or_failed' WHEN EXISTS(SELECT 1 FROM deliveries WHERE conversation_id=conversations.id AND status='pending') THEN 'pending' ELSE 'sent' END WHERE id=(SELECT conversation_id FROM deliveries WHERE id=$1)`, id)
	return err
}
func (r Postgres) GetResult(ctx context.Context, owner, cid string) (application.Result, error) {
	var exists bool
	err := r.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations WHERE id=$1 AND owner_id=$2)`, cid, owner).Scan(&exists)
	if err != nil || !exists {
		return application.Result{}, errors.New("conversation not found")
	}
	rows, err := r.Pool.Query(ctx, `SELECT id,recipients,status,COALESCE(error,''),COALESCE(gmail_message_id,''),COALESCE(gmail_thread_id,''),COALESCE(rfc_message_id,'') FROM deliveries WHERE conversation_id=$1 ORDER BY created_at,id`, cid)
	if err != nil {
		return application.Result{}, err
	}
	defer rows.Close()
	result := application.Result{ConversationID: cid, Deliveries: []application.Delivery{}}
	for rows.Next() {
		var d application.Delivery
		if err := rows.Scan(&d.ID, &d.Recipients, &d.Status, &d.Error, &d.GmailMessageID, &d.GmailThreadID, &d.RFCMessageID); err != nil {
			return result, err
		}
		result.Deliveries = append(result.Deliveries, d)
	}
	return result, rows.Err()
}

type DraftRecord struct {
	ID        string
	Content   domain.Draft
	UpdatedAt time.Time
}

func (r Postgres) SaveDraft(ctx context.Context, owner, id string, d domain.Draft) (DraftRecord, error) {
	if id == "" {
		id = uuid.NewString()
	}
	content, err := json.Marshal(d)
	if err != nil {
		return DraftRecord{}, err
	}
	var at time.Time
	err = r.Pool.QueryRow(ctx, `INSERT INTO local_drafts(id,owner_id,mailbox_id,content) SELECT $1,$2,$3,$4 WHERE EXISTS(SELECT 1 FROM mailboxes WHERE id=$3 AND owner_id=$2) ON CONFLICT(id) DO UPDATE SET content=excluded.content,mailbox_id=excluded.mailbox_id,updated_at=now() WHERE local_drafts.owner_id=$2 RETURNING updated_at`, id, owner, d.MailboxID, content).Scan(&at)
	if err != nil {
		return DraftRecord{}, err
	}
	return DraftRecord{ID: id, Content: d, UpdatedAt: at}, nil
}
func (r Postgres) GetDraft(ctx context.Context, owner, id string) (DraftRecord, error) {
	var row DraftRecord
	var raw []byte
	err := r.Pool.QueryRow(ctx, `SELECT id,content,updated_at FROM local_drafts WHERE id=$1 AND owner_id=$2`, id, owner).Scan(&row.ID, &raw, &row.UpdatedAt)
	if err != nil {
		return row, err
	}
	err = json.Unmarshal(raw, &row.Content)
	return row, err
}
func (r Postgres) ListDrafts(ctx context.Context, owner string) ([]DraftRecord, error) {
	rows, err := r.Pool.Query(ctx, `SELECT id,content,updated_at FROM local_drafts WHERE owner_id=$1 ORDER BY updated_at DESC`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DraftRecord{}
	for rows.Next() {
		var row DraftRecord
		var raw []byte
		if err := rows.Scan(&row.ID, &raw, &row.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &row.Content); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
func (r Postgres) DeleteDraft(ctx context.Context, owner, id string) error {
	tag, err := r.Pool.Exec(ctx, `DELETE FROM local_drafts WHERE id=$1 AND owner_id=$2`, id, owner)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// RecoverInterrupted marks attempts left pending by a previous process as uncertain.
// A new process must never send those attempts again automatically.
func (r Postgres) RecoverInterrupted(ctx context.Context) error {
	_, err := r.Pool.Exec(ctx, `UPDATE deliveries SET status='unknown',error='Service stopped during send; check Gmail Sent' WHERE status='pending'`)
	if err != nil {
		return err
	}
	_, err = r.Pool.Exec(ctx, `UPDATE conversations c SET status='unknown',updated_at=now() WHERE c.status='pending' AND EXISTS(SELECT 1 FROM deliveries d WHERE d.conversation_id=c.id AND d.status='unknown')`)
	return err
}
