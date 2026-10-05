package adapters

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/akpor-kofi/mail-tracker/apps/api/internal/mailboxes/domain"
	"github.com/akpor-kofi/mail-tracker/apps/api/internal/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStoredGrantAndReadChoiceSurviveMailboxReads(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "mailbox_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE") }()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	dir, err := filepath.Abs("../../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx, pool, dir); err != nil {
		t.Fatal(err)
	}
	repo := Postgres{Pool: pool}
	m := domain.Mailbox{ID: uuid.NewString(), OwnerID: "owner", GoogleSub: "sub", Email: "sender@example.com", SyncEnabled: true, GrantedScopes: []string{"openid", "https://www.googleapis.com/auth/gmail.send", "https://www.googleapis.com/auth/gmail.readonly"}}
	if err := repo.Upsert(ctx, m, []byte("encrypted")); err != nil {
		t.Fatal(err)
	}
	got, token, err := repo.Get(ctx, m.ID)
	if err != nil || string(token) != "encrypted" {
		t.Fatalf("get: %+v %v", got, err)
	}
	bySub, err := repo.FindByGoogleSub(ctx, m.GoogleSub)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := repo.List(ctx, m.OwnerID)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list: %+v %v", listed, err)
	}
	for _, got := range []domain.Mailbox{got, bySub, listed[0]} {
		if !got.SyncEnabled || !reflect.DeepEqual(got.GrantedScopes, m.GrantedScopes) {
			t.Fatalf("grant lost on read: %+v", got)
		}
	}
	// Reconnect updates the token in place, preserving references to this mailbox.
	m.ID = uuid.NewString()
	if err := repo.Upsert(ctx, m, []byte("replacement")); err != nil {
		t.Fatal(err)
	}
	got, err = repo.FindByGoogleSub(ctx, m.GoogleSub)
	if err != nil || got.ID == m.ID {
		t.Fatalf("reconnect changed mailbox identity: %+v %v", got, err)
	}
}
