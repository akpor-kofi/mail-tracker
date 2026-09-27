package adapters

import (
	"context"
	"errors"
	"mime"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Files struct {
	Pool *pgxpool.Pool
	Dir  string
}

func (f Files) Save(ctx context.Context, owner, name, kind string, data []byte) (string, error) {
	if len(data) == 0 || len(data) > 20<<20 {
		return "", errors.New("attachment must be 1 byte to 20 MB")
	}
	if mediaType, _, err := mime.ParseMediaType(kind); err == nil {
		kind = mediaType
	} else {
		kind = "application/octet-stream"
	}
	if err := os.MkdirAll(f.Dir, 0700); err != nil {
		return "", err
	}
	id := uuid.NewString()
	path := filepath.Join(f.Dir, id)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", err
	}
	_, err := f.Pool.Exec(ctx, `INSERT INTO attachments(id,owner_id,path,filename,content_type,size_bytes) VALUES($1,$2,$3,$4,$5,$6)`, id, owner, path, filepath.Base(name), kind, len(data))
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return id, nil
}
func (f Files) Load(ctx context.Context, owner, id string) ([]byte, string, string, error) {
	var path, name, kind string
	err := f.Pool.QueryRow(ctx, `SELECT path,filename,content_type FROM attachments WHERE id=$1 AND owner_id=$2`, id, owner).Scan(&path, &name, &kind)
	if err != nil {
		return nil, "", "", err
	}
	data, err := os.ReadFile(path)
	return data, name, kind, err
}
func (f Files) Delete(ctx context.Context, id string) error {
	tx, err := f.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var path string
	err = tx.QueryRow(ctx, `DELETE FROM attachments WHERE id=$1 RETURNING path`, id).Scan(&path)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO pending_attachment_deletions(path) VALUES($1) ON CONFLICT DO NOTHING`, path); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return f.removeQueuedFile(ctx, path)
}

// CleanupOrphans removes old uploads that no saved draft still references.
// The age threshold leaves time for an upload or an in-progress send to finish.
func (f Files) CleanupOrphans(ctx context.Context, olderThan time.Time, limit int) (int, error) {
	tx, err := f.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT a.id,a.path FROM attachments a
		WHERE a.created_at<$1 AND NOT EXISTS (
			SELECT 1 FROM local_drafts d
			WHERE d.owner_id=a.owner_id AND (d.content->'attachments') ? a.id::text
		)
		ORDER BY a.created_at LIMIT $2 FOR UPDATE OF a SKIP LOCKED`, olderThan, limit)
	if err != nil {
		return 0, err
	}
	type orphan struct{ id, path string }
	var candidates []orphan
	for rows.Next() {
		var candidate orphan
		if err := rows.Scan(&candidate.id, &candidate.path); err != nil {
			rows.Close()
			return 0, err
		}
		candidates = append(candidates, candidate)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, candidate := range candidates {
		if _, err := tx.Exec(ctx, `INSERT INTO pending_attachment_deletions(path) VALUES($1) ON CONFLICT DO NOTHING`, candidate.path); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM attachments WHERE id=$1`, candidate.id); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	removedFiles, err := f.cleanupPendingFiles(ctx, limit)
	return len(candidates) + removedFiles, err
}

func (f Files) cleanupPendingFiles(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	rows, err := f.Pool.Query(ctx, `SELECT path FROM pending_attachment_deletions ORDER BY queued_at,path LIMIT $1`, limit)
	if err != nil {
		return 0, err
	}
	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			rows.Close()
			return 0, err
		}
		paths = append(paths, path)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	removed := 0
	var firstError error
	for _, path := range paths {
		if err := f.removeQueuedFile(ctx, path); err != nil {
			_, _ = f.Pool.Exec(ctx, `UPDATE pending_attachment_deletions SET queued_at=now() WHERE path=$1`, path)
			if firstError == nil {
				firstError = err
			}
			continue
		}
		removed++
	}
	return removed, firstError
}

func (f Files) removeQueuedFile(ctx context.Context, path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, err := f.Pool.Exec(ctx, `DELETE FROM pending_attachment_deletions WHERE path=$1`, path)
	return err
}
