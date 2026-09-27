package db

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestConcurrentMigratorsApplyEachFileOnce(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set TEST_DATABASE_URL to run the PostgreSQL migration test")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "migration_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE") }()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "001_probe.sql"), []byte("CREATE TABLE migration_probe (id integer PRIMARY KEY); SELECT pg_sleep(0.2);"), 0o600); err != nil {
		t.Fatal(err)
	}
	newPool := func() *pgxpool.Pool {
		config, err := pgxpool.ParseConfig(databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		if config.ConnConfig.RuntimeParams == nil {
			config.ConnConfig.RuntimeParams = make(map[string]string)
		}
		config.ConnConfig.RuntimeParams["search_path"] = schema
		pool, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			t.Fatal(err)
		}
		return pool
	}
	first, second := newPool(), newPool()
	defer first.Close()
	defer second.Close()

	start := make(chan struct{})
	results := make(chan error, 2)
	var runners sync.WaitGroup
	for _, pool := range []*pgxpool.Pool{first, second} {
		runners.Add(1)
		go func(pool *pgxpool.Pool) {
			defer runners.Done()
			<-start
			results <- Migrate(ctx, pool, dir)
		}(pool)
	}
	close(start)
	runners.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent migration failed: %v", err)
		}
	}
	var count int
	if err := first.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("got %d migration records, want 1", count)
	}
}
