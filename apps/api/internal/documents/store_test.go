package documents

import (
	"bytes"
	"context"
	"github.com/akpor-kofi/mail-tracker/apps/api/internal/platform/db"
	rdtest "github.com/akpor-kofi/raildrop/sdk/go/testing"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"image"
	"image/png"
	"os"
	"strings"
	"testing"
)

func setup(t *testing.T) Store {
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
	schema := "docs_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e := admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
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
	if e := db.Migrate(ctx, pool, "../../migrations"); e != nil {
		t.Fatal(e)
	}
	return Store{Pool: pool, Storage: rdtest.NewMemoryStorage(), PublicURL: "https://tracker.example"}
}
func TestPrivateDocumentLifecycle(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	d, e := s.Upload(ctx, "owner", "proposal.pdf", []byte("%PDF-1.7 test"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e := s.Share(ctx, "other", d.ID, 30, true); e == nil {
		t.Fatal("share crossed owner")
	}
	share, e := s.Share(ctx, "owner", d.ID, 1, true)
	if e != nil {
		t.Fatal(e)
	}
	v, e := s.Start(ctx, strings.TrimPrefix(share.URL, s.PublicURL+"/d/"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e := s.Session(ctx, v.ID, "invalid"); e == nil {
		t.Fatal("session did not require secret")
	}
	authenticated, e := s.Session(ctx, v.ID, v.Secret)
	if e != nil {
		t.Fatal(e)
	}
	batch := Batch{ID: uuid.NewString(), Kind: "viewer_loaded"}
	if e := s.Observe(ctx, authenticated, batch); e != nil {
		t.Fatal(e)
	}
	if e := s.Observe(ctx, authenticated, batch); e != nil {
		t.Fatal(e)
	}
	list, e := s.List(ctx, "owner")
	if e != nil || len(list) != 1 || list[0].Sessions != 1 {
		t.Fatalf("sessions %+v %v", list, e)
	}
	if e := s.Revoke(ctx, "owner", share.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Session(ctx, v.ID, v.Secret); e == nil {
		t.Fatal("revoked share remained accessible")
	}

	if e := s.Delete(ctx, "other", d.ID); e == nil {
		t.Fatal("document deletion crossed owner")
	}
	if e := s.Delete(ctx, "owner", d.ID); e != nil {
		t.Fatal(e)
	}
	if e := s.Cleanup(ctx); e != nil {
		t.Fatal(e)
	}
	list, e = s.List(ctx, "owner")
	if e != nil || len(list) != 0 {
		t.Fatal("deleted document remains", e)
	}
	var pending int
	if e := s.Pool.QueryRow(ctx, `SELECT count(*) FROM document_uploads`).Scan(&pending); e != nil || pending != 0 {
		t.Fatal("object deletion did not drain", e)
	}
}
func TestUploadValidation(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("<html>evil</html>"), []byte("<svg></svg>"), make([]byte, MaxBytes+1)} {
		if _, e := Validate(data); e == nil {
			t.Fatal("unsafe file accepted")
		}
	}
	var b bytes.Buffer
	png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	if typ, e := Validate(b.Bytes()); e != nil || typ != "image/png" {
		t.Fatal(typ, e)
	}
}
