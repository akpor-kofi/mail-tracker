package adapters

import (
	"context"
	"errors"
	mailboxdomain "github.com/akpor-kofi/mail-tracker/apps/api/internal/mailboxes/domain"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type PairingPostgres struct{ Pool *pgxpool.Pool }

func (r PairingPostgres) CreateCode(ctx context.Context, hash []byte, id string, expiry time.Time) error {
	_, err := r.Pool.Exec(ctx, `INSERT INTO pairing_codes(code_hash,mailbox_id,expires_at) VALUES($1,$2,$3)`, hash, id, expiry)
	return err
}
func (r PairingPostgres) ConsumeCode(ctx context.Context, hash []byte, sub string) (string, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var id, mailboxSub string
	err = tx.QueryRow(ctx, `SELECT p.mailbox_id,m.google_sub FROM pairing_codes p JOIN mailboxes m ON m.id=p.mailbox_id WHERE p.code_hash=$1 AND p.expires_at>now() AND p.used_at IS NULL FOR UPDATE OF p`, hash).Scan(&id, &mailboxSub)
	if err != nil {
		return "", err
	}
	if sub != mailboxSub {
		return "", errors.New("add-on identity does not match mailbox")
	}
	if _, err := tx.Exec(ctx, `UPDATE pairing_codes SET used_at=now() WHERE code_hash=$1`, hash); err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}
func (r PairingPostgres) SavePair(ctx context.Context, sub, id string) error {
	_, err := r.Pool.Exec(ctx, `INSERT INTO addon_pairs(google_sub,mailbox_id) VALUES($1,$2) ON CONFLICT(google_sub) DO UPDATE SET mailbox_id=excluded.mailbox_id,paired_at=now()`, sub, id)
	return err
}
func (r PairingPostgres) PairedMailbox(ctx context.Context, sub string) (string, error) {
	var id string
	err := r.Pool.QueryRow(ctx, `SELECT mailbox_id FROM addon_pairs WHERE google_sub=$1`, sub).Scan(&id)
	return id, err
}
func (r PairingPostgres) Mailbox(ctx context.Context, id string) (mailboxdomain.Mailbox, error) {
	var m mailboxdomain.Mailbox
	err := r.Pool.QueryRow(ctx, `SELECT id,owner_id,google_sub,email,connected_at FROM mailboxes WHERE id=$1`, id).Scan(&m.ID, &m.OwnerID, &m.GoogleSub, &m.Email, &m.ConnectedAt)
	return m, err
}
