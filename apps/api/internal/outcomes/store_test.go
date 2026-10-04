package outcomes

import (
	"context"
	"github.com/akpor-kofi/mail-tracker/apps/api/internal/analytics"
	"github.com/akpor-kofi/mail-tracker/apps/api/internal/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
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
	schema := "outcomes_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	return Store{Pool: pool}
}
func TestWebhookDedupAndReportDenominators(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	m, c, d := uuid.NewString(), uuid.NewString(), uuid.NewString()
	_, hash, _ := analytics.Token()
	sent := time.Now().Add(-time.Hour)
	for _, q := range []struct {
		sql  string
		args []any
	}{{`INSERT INTO mailboxes(id,owner_id,google_sub,email,encrypted_refresh_token) VALUES($1,'owner','sub','sender@example.com','x')`, []any{m}}, {`INSERT INTO conversations(id,owner_id,mailbox_id,subject,mode,status) VALUES($1,'owner',$2,'test','shared','sent')`, []any{c, m}}, {`INSERT INTO deliveries(id,conversation_id,recipients,pixel_token_hash,status,confirmed_sent_at) VALUES($1,$2,'{recipient@example.com}',$3,'sent',$4)`, []any{d, c, hash, sent}}} {
		if _, e := s.Pool.Exec(ctx, q.sql, q.args...); e != nil {
			t.Fatal(e)
		}
	}
	g, e := s.CreateGoal(ctx, "owner", "Meeting booked", 30)
	if e != nil {
		t.Fatal(e)
	}
	b := Input{GoalID: g.ID, DeliveryID: d, At: time.Now().Add(-time.Minute)}
	if _, e := s.Record(ctx, "other", "manual", b); e == nil {
		t.Fatal("manual owner isolation failed")
	}
	v, e := s.Record(ctx, "owner", "manual", b)
	if e != nil {
		t.Fatal(e)
	}
	report, e := s.Report(ctx, "owner", "", g.ID, sent.Add(-time.Hour), time.Now())
	if e != nil || report.Sent != 1 || report.Converted != 1 {
		t.Fatalf("report %+v %v", report, e)
	}
	if e := s.Reverse(ctx, "owner", v.ID); e != nil {
		t.Fatal(e)
	}
	report, e = s.Report(ctx, "owner", "", g.ID, sent.Add(-time.Hour), time.Now())
	if e != nil || report.Converted != 0 {
		t.Fatalf("reversal %+v %v", report, e)
	}
	b.DeliveryID = ""
	b.ExternalID = "event-1"
	first, e := s.Record(ctx, "", "webhook", b)
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.Record(ctx, "", "webhook", b)
	if e != nil || first.ID != again.ID || again.DeliveryID != nil {
		t.Fatal("webhook dedup/unattributed event failed", e)
	}
	b.At = b.At.Add(time.Second)
	if _, e = s.Record(ctx, "", "webhook", b); e == nil {
		t.Fatal("conflicting external ID accepted")
	}
}

func TestConversionRequiresEligibleClickWithinWindow(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	m, c, d := uuid.NewString(), uuid.NewString(), uuid.NewString()
	_, ph, _ := analytics.Token()
	sent := time.Now().Add(-2 * time.Hour)
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO mailboxes(id,owner_id,google_sub,email,encrypted_refresh_token) VALUES($1,'owner','sub','sender@example.com','x')`, []any{m}},
		{`INSERT INTO conversations(id,owner_id,mailbox_id,subject,mode,status) VALUES($1,'owner',$2,'test','shared','sent')`, []any{c, m}},
		{`INSERT INTO deliveries(id,conversation_id,recipients,pixel_token_hash,status,confirmed_sent_at) VALUES($1,$2,'{recipient@example.com}',$3,'sent',$4)`, []any{d, c, ph, sent}},
	} {
		if _, e := s.Pool.Exec(ctx, q.sql, q.args...); e != nil {
			t.Fatal(e)
		}
	}
	g, e := s.CreateGoal(ctx, "owner", "Booked", 1)
	if e != nil {
		t.Fatal(e)
	}
	ref, ah, _ := analytics.Token()
	_, lh, _ := analytics.Token()
	link := uuid.NewString()
	if _, e = s.Pool.Exec(ctx, `INSERT INTO tracked_links(id,delivery_id,token_hash,destination,attribution_hash) VALUES($1,$2,$3,'https://example.com',$4)`, link, d, lh, ah); e != nil {
		t.Fatal(e)
	}
	b := Input{GoalID: g.ID, AttributionRef: ref, ExternalID: "scanner-only", At: time.Now().Add(-time.Minute)}
	if e = (analytics.Store{Pool: s.Pool}).RecordAt(ctx, d, "link_request", "suspected_automation", link, nil, map[string]any{}, "scanner", sent.Add(time.Minute)); e != nil {
		t.Fatal(e)
	}
	v, e := s.Record(ctx, "", "webhook", b)
	if e != nil || v.DeliveryID != nil {
		t.Fatalf("scanner attributed %+v %v", v, e)
	}
	if e = (analytics.Store{Pool: s.Pool}).RecordAt(ctx, d, "link_request", "unclassified", link, nil, map[string]any{}, "click", sent.Add(2*time.Minute)); e != nil {
		t.Fatal(e)
	}
	b.ExternalID = "eligible"
	v, e = s.Record(ctx, "", "webhook", b)
	if e != nil || v.DeliveryID == nil || *v.DeliveryID != d {
		t.Fatalf("eligible click missing %+v %v", v, e)
	}
	b.ExternalID = "before-click"
	b.At = sent.Add(30 * time.Second)
	v, e = s.Record(ctx, "", "webhook", b)
	if e != nil || v.DeliveryID != nil {
		t.Fatalf("future click attributed %+v %v", v, e)
	}
	b.ExternalID = "expired"
	b.At = time.Now().Add(-time.Minute)
	if _, e = s.Pool.Exec(ctx, `UPDATE deliveries SET confirmed_sent_at=$2 WHERE id=$1`, d, time.Now().Add(-48*time.Hour)); e != nil {
		t.Fatal(e)
	}
	v, e = s.Record(ctx, "", "webhook", b)
	if e != nil || v.DeliveryID != nil {
		t.Fatalf("outside window attributed %+v %v", v, e)
	}
}
