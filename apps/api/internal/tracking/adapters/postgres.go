package adapters

import (
	"context"
	"errors"
	"github.com/akpor-kofi/mail-tracker/apps/api/internal/tracking/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct{ Pool *pgxpool.Pool }

func (r Postgres) RecordOpen(ctx context.Context, hash []byte) (string, error) {
	var owner string
	err := r.Pool.QueryRow(ctx, `WITH ins AS (INSERT INTO open_events(delivery_id) SELECT id FROM deliveries WHERE pixel_token_hash=$1 RETURNING delivery_id) SELECT c.owner_id FROM ins JOIN deliveries d ON d.id=ins.delivery_id JOIN conversations c ON c.id=d.conversation_id`, hash).Scan(&owner)
	return owner, err
}
func (r Postgres) Prepare(ctx context.Context, owner, mailboxID, subject string, recipients []string, hash []byte) (string, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	id := uuid.NewString()
	tag, err := tx.Exec(ctx, `INSERT INTO conversations(id,owner_id,mailbox_id,subject,mode,status) SELECT $1,$2,$3,$4,'addon','prepared' WHERE EXISTS(SELECT 1 FROM mailboxes WHERE id=$3 AND owner_id=$2)`, id, owner, mailboxID, subject)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", errors.New("mailbox not found")
	}
	_, err = tx.Exec(ctx, `INSERT INTO deliveries(id,conversation_id,recipients,pixel_token_hash,status) VALUES($1,$2,$3,$4,'prepared')`, uuid.NewString(), id, recipients, hash)
	if err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}
func (r Postgres) List(ctx context.Context, owner, mailbox string) ([]domain.Conversation, error) {
	rows, err := r.Pool.Query(ctx, `SELECT c.id,c.mailbox_id,c.subject,c.status,CASE WHEN EXISTS(SELECT 1 FROM deliveries d JOIN open_events e ON e.delivery_id=d.id WHERE d.conversation_id=c.id) THEN 'open_detected' ELSE 'no_open_detected' END,c.updated_at FROM conversations c WHERE c.owner_id=$1 AND ($2='' OR c.mailbox_id::text=$2) ORDER BY c.updated_at DESC LIMIT 200`, owner, mailbox)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Conversation{}
	for rows.Next() {
		var c domain.Conversation
		if err := rows.Scan(&c.ID, &c.MailboxID, &c.Subject, &c.Status, &c.OpenStatus, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (r Postgres) Detail(ctx context.Context, owner, id string) (domain.Detail, error) {
	var d domain.Detail
	err := r.Pool.QueryRow(ctx, `SELECT c.id,c.mailbox_id,c.subject,c.status,CASE WHEN EXISTS(SELECT 1 FROM deliveries x JOIN open_events e ON e.delivery_id=x.id WHERE x.conversation_id=c.id) THEN 'open_detected' ELSE 'no_open_detected' END,c.updated_at FROM conversations c WHERE c.id=$1 AND c.owner_id=$2`, id, owner).Scan(&d.Conversation.ID, &d.Conversation.MailboxID, &d.Conversation.Subject, &d.Conversation.Status, &d.Conversation.OpenStatus, &d.Conversation.UpdatedAt)
	if err != nil {
		return d, err
	}
	rows, err := r.Pool.Query(ctx, `SELECT id,recipients,status,COALESCE(error,''),COALESCE(gmail_message_id,''),COALESCE(gmail_thread_id,''),COALESCE(rfc_message_id,'') FROM deliveries WHERE conversation_id=$1 ORDER BY created_at,id`, id)
	if err != nil {
		return d, err
	}
	d.Deliveries = []domain.Delivery{}
	for rows.Next() {
		var x domain.Delivery
		if err := rows.Scan(&x.ID, &x.Recipients, &x.Status, &x.Error, &x.GmailMessageID, &x.GmailThreadID, &x.RFCMessageID); err != nil {
			rows.Close()
			return d, err
		}
		d.Deliveries = append(d.Deliveries, x)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return d, err
	}
	events, err := r.Pool.Query(ctx, `SELECT e.delivery_id,e.occurred_at FROM open_events e JOIN deliveries x ON x.id=e.delivery_id WHERE x.conversation_id=$1 ORDER BY e.occurred_at DESC LIMIT 100`, id)
	if err != nil {
		return d, err
	}
	defer events.Close()
	d.Events = []domain.Event{}
	for events.Next() {
		var e domain.Event
		if err := events.Scan(&e.DeliveryID, &e.At); err != nil {
			return d, err
		}
		d.Events = append(d.Events, e)
	}
	return d, events.Err()
}
