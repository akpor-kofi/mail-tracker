package analytics

import (
	"context"
	"github.com/akpor-kofi/mail-tracker/apps/api/internal/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testStore(t *testing.T) Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	schema := "analytics_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { pool.Close(); _, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); admin.Close() })
	dir, e := filepath.Abs("../../migrations")
	if e != nil {
		t.Fatal(e)
	}
	if e = db.Migrate(ctx, pool, dir); e != nil {
		t.Fatal(e)
	}
	return Store{Pool: pool, PublicURL: "https://tracker.example"}
}
func fixture(t *testing.T, s Store) (string, string, []byte) {
	t.Helper()
	ctx := context.Background()
	m, c, d := uuid.NewString(), uuid.NewString(), uuid.NewString()
	_, hash, _ := Token()
	_, e := s.Pool.Exec(ctx, `INSERT INTO mailboxes(id,owner_id,google_sub,email,encrypted_refresh_token) VALUES($1,'owner','sub','sender@example.com','x');`, m)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Pool.Exec(ctx, `INSERT INTO conversations(id,owner_id,mailbox_id,subject,mode,status) VALUES($1,'owner',$2,'test','shared','sent')`, c, m)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Pool.Exec(ctx, `INSERT INTO deliveries(id,conversation_id,recipients,pixel_token_hash,status) VALUES($1,$2,'{recipient@example.com}',$3,'sent')`, d, c, hash)
	if e != nil {
		t.Fatal(e)
	}
	return c, d, hash
}
func TestObservationDedupAndOwnerIsolation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c, d, h := fixture(t, s)
	if e := s.Pixel(ctx, h, "Mozilla"); e != nil {
		t.Fatal(e)
	}
	if e := s.Pixel(ctx, h, "GoogleImageProxy"); e != nil {
		t.Fatal(e)
	}
	for range 2 {
		if e := s.Record(ctx, d, "viewer_activity", "browser", "", nil, map[string]any{"activeMs": 10000}, "same-batch"); e != nil {
			t.Fatal(e)
		}
	}
	sum, e := s.Summary(ctx, "owner", c)
	if e != nil {
		t.Fatal(e)
	}
	if sum.Images.Raw != 2 || sum.Images.Sessions != 1 || sum.Images.Automation != 1 || sum.ActiveSeconds != 10 {
		t.Fatalf("bad metrics: %+v", sum)
	}
	if _, e := s.Summary(ctx, "different-owner", c); e == nil {
		t.Fatal("owner isolation failed")
	}
	page, e := s.Events(ctx, "owner", c, "", "")
	if e != nil || len(page.Items) != 3 {
		t.Fatalf("dedupe/events: %+v %v", page, e)
	}
	_ = time.Second
}
func TestTransactionalLinks(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	_, d, _ := fixture(t, s)
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	body, e := RewriteLinks(ctx, tx, d, `<a href="https://example.com/a?b=c#d">Go</a><a href="mailto:a@example.com">Email</a><a data-no-track href="https://example.com/signed">Private</a>`, s.PublicURL)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(body, s.PublicURL+"/c/") || !strings.Contains(body, "mailto:") {
		t.Fatal(body)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	var n int
	if e = s.Pool.QueryRow(ctx, `SELECT count(*) FROM tracked_links WHERE delivery_id=$1`, d).Scan(&n); e != nil || n != 1 {
		t.Fatalf("links %d %v", n, e)
	}
}

func TestOutboxAcknowledgesAfterPublish(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	_, d, _ := fixture(t, s)
	if e := s.Record(ctx, d, "image_request", "unclassified", "", nil, map[string]any{}, ""); e != nil {
		t.Fatal(e)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected simulated crash")
			}
		}()
		_ = s.DrainOutbox(ctx, func(string) { panic("crash before acknowledgement") })
	}()
	var n int
	if e := s.Pool.QueryRow(ctx, `SELECT count(*) FROM activity_outbox`).Scan(&n); e != nil || n != 1 {
		t.Fatalf("committed outbox lost: %d %v", n, e)
	}
	published := 0
	if e := s.DrainOutbox(ctx, func(o string) {
		if o != "owner" {
			t.Fatal(o)
		}
		published++
	}); e != nil {
		t.Fatal(e)
	}
	if e := s.DrainOutbox(ctx, func(string) { published++ }); e != nil {
		t.Fatal(e)
	}
	if published != 1 {
		t.Fatalf("published %d times", published)
	}
}
