package adapters

import (
	"context"
	"errors"
	"mime"
	"os"
	"path/filepath"

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
	var path string
	err := f.Pool.QueryRow(ctx, `DELETE FROM attachments WHERE id=$1 RETURNING path`, id).Scan(&path)
	if err != nil {
		return err
	}
	return os.Remove(path)
}
