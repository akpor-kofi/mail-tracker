package adapters

import (
	"context"
	"errors"
	"github.com/akpor-kofi/mail-tracker/apps/api/internal/mailboxes/domain"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type Postgres struct{ Pool *pgxpool.Pool }

func (r Postgres) SaveState(ctx context.Context, hash []byte, owner, verifier string, expiry time.Time) error {
	_, err := r.Pool.Exec(ctx, `INSERT INTO oauth_states(state_hash,owner_id,verifier,expires_at) VALUES($1,$2,$3,$4)`, hash, owner, verifier, expiry)
	return err
}
func (r Postgres) ConsumeState(ctx context.Context, hash []byte) (string, string, error) {
	var owner, verifier string
	err := r.Pool.QueryRow(ctx, `DELETE FROM oauth_states WHERE state_hash=$1 AND expires_at>now() RETURNING owner_id,verifier`, hash).Scan(&owner, &verifier)
	return owner, verifier, err
}
func (r Postgres) DeleteExpiredStates(ctx context.Context) error {
	_, err := r.Pool.Exec(ctx, `DELETE FROM oauth_states WHERE expires_at < now()`)
	return err
}
func (r Postgres) Upsert(ctx context.Context, m domain.Mailbox, encrypted []byte) error {
	_, err := r.Pool.Exec(ctx, `INSERT INTO mailboxes(id,owner_id,google_sub,email,encrypted_refresh_token) VALUES($1,$2,$3,$4,$5) ON CONFLICT(owner_id,google_sub) DO UPDATE SET email=excluded.email,encrypted_refresh_token=excluded.encrypted_refresh_token,connected_at=now()`, m.ID, m.OwnerID, m.GoogleSub, m.Email, encrypted)
	return err
}
func (r Postgres) List(ctx context.Context, owner string) ([]domain.Mailbox, error) {
	rows, err := r.Pool.Query(ctx, `SELECT id,owner_id,google_sub,email,connected_at FROM mailboxes WHERE owner_id=$1 ORDER BY email`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Mailbox{}
	for rows.Next() {
		var m domain.Mailbox
		if err := rows.Scan(&m.ID, &m.OwnerID, &m.GoogleSub, &m.Email, &m.ConnectedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (r Postgres) Get(ctx context.Context, id string) (domain.Mailbox, []byte, error) {
	var m domain.Mailbox
	var encrypted []byte
	err := r.Pool.QueryRow(ctx, `SELECT id,owner_id,google_sub,email,connected_at,encrypted_refresh_token FROM mailboxes WHERE id=$1`, id).Scan(&m.ID, &m.OwnerID, &m.GoogleSub, &m.Email, &m.ConnectedAt, &encrypted)
	return m, encrypted, err
}
func (r Postgres) Delete(ctx context.Context, owner, id string) error {
	tag, err := r.Pool.Exec(ctx, `DELETE FROM mailboxes WHERE owner_id=$1 AND id=$2`, owner, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("mailbox not found")
	}
	return nil
}
func (r Postgres) FindByGoogleSub(ctx context.Context, sub string) (domain.Mailbox, error) {
	var m domain.Mailbox
	err := r.Pool.QueryRow(ctx, `SELECT id,owner_id,google_sub,email,connected_at FROM mailboxes WHERE google_sub=$1`, sub).Scan(&m.ID, &m.OwnerID, &m.GoogleSub, &m.Email, &m.ConnectedAt)
	return m, err
}

func (r Postgres) UpdateRefreshToken(ctx context.Context, id string, encrypted []byte) error {
	_, err := r.Pool.Exec(ctx, `UPDATE mailboxes SET encrypted_refresh_token=$2 WHERE id=$1`, id, encrypted)
	return err
}
