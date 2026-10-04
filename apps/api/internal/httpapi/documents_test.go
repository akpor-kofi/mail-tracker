package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/akpor-kofi/mail-tracker/apps/api/internal/analytics"
	"github.com/akpor-kofi/mail-tracker/apps/api/internal/documents"
	"github.com/akpor-kofi/mail-tracker/apps/api/internal/platform/db"
	rdtest "github.com/akpor-kofi/raildrop/sdk/go/testing"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestViewerCookieOriginRangeAndRevocation(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	schema := "httpdocs_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	if e = db.Migrate(ctx, pool, "../../migrations"); e != nil {
		t.Fatal(e)
	}
	base := "https://tracker.example"
	store := documents.Store{Pool: pool, Storage: rdtest.NewMemoryStorage(), PublicURL: base}
	doc, e := store.Upload(ctx, "owner", "sample.pdf", []byte("%PDF-1.7 test"))
	if e != nil {
		t.Fatal(e)
	}
	share, e := store.Share(ctx, "owner", doc.ID, 1, false)
	if e != nil {
		t.Fatal(e)
	}
	server := &Server{Documents: store, Analytics: analytics.Store{Pool: pool, PublicURL: base}}
	app := fiber.New()
	server.RegisterDocuments(app.Group("/api/v1"))
	token := strings.TrimPrefix(share.URL, base+"/d/")
	start := func(origin string) *http.Response {
		req := httptest.NewRequest("POST", "/api/v1/viewer/sessions", strings.NewReader(`{"token":"`+token+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		r, e := app.Test(req)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	r := start("https://evil.example")
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatal("cross-origin session accepted", r.StatusCode)
	}
	r = start(base)
	if r.StatusCode != 200 {
		t.Fatal("session exchange", r.StatusCode)
	}
	var session documents.Session
	if e = json.NewDecoder(r.Body).Decode(&session); e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	cookies := r.Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe session cookie")
	}
	get := func(suffix string, cookie bool, rangeValue string) *http.Response {
		req := httptest.NewRequest("GET", "/api/v1/viewer/"+session.ID+suffix, nil)
		if cookie {
			req.AddCookie(cookies[0])
		}
		if rangeValue != "" {
			req.Header.Set("Range", rangeValue)
		}
		r, e := app.Test(req)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	r = get("/file", false, "")
	r.Body.Close()
	if r.StatusCode != 410 {
		t.Fatal("file without cookie", r.StatusCode)
	}
	r = get("/file", true, "bytes=0-4")
	data, e := io.ReadAll(r.Body)
	r.Body.Close()
	if e != nil || r.StatusCode != 206 || string(data) != "%PDF-" || r.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("range response %d %q %v", r.StatusCode, data, e)
	}
	r = get("/download", true, "")
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatal("download policy bypass", r.StatusCode)
	}
	var events int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM viewer_events`).Scan(&events); e != nil || events != 0 {
		t.Fatal("range fetch counted as visit", events, e)
	}
	if e = store.Revoke(ctx, "owner", share.ID); e != nil {
		t.Fatal(e)
	}
	r = get("/file", true, "")
	r.Body.Close()
	if r.StatusCode != 410 {
		t.Fatal("revoked file still accessible", r.StatusCode)
	}
}
