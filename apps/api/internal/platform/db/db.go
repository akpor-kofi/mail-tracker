package db

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Shared with apps/dashboard/scripts/migrate.mjs. Both migrators must hold the
// same session lock before changing schema in this database.
const migrationLockNamespace = 1297371723
const migrationLockID = 1

func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	p, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := p.Ping(ctx); err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}

func Migrate(ctx context.Context, p *pgxpool.Pool, dir string) error {
	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })
	conn, err := p.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	// Closing the session releases its advisory lock, including after a canceled
	// migration. Never return a still-locked connection to the pool.
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = conn.Conn().Close(closeCtx)
		conn.Release()
	}()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1::integer, $2::integer)`, migrationLockNamespace, migrationLockID); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY)`); err != nil {
		return err
	}
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".sql") {
			continue
		}
		var exists bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, f.Name()).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		b, err := os.ReadFile(dir + "/" + f.Name())
		if err != nil {
			return err
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, string(b)); err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, f.Name())
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", f.Name(), err)
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}
