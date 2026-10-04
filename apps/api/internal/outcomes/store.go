package outcomes

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/akpor-kofi/mail-tracker/apps/api/internal/analytics"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"regexp"
	"strings"
	"time"
)

type Store struct{ Pool *pgxpool.Pool }
type Goal struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	WindowDays int    `json:"windowDays"`
}

func (s Store) Goals(ctx context.Context, o string) ([]Goal, error) {
	rows, e := s.Pool.Query(ctx, `SELECT id,name,window_days FROM goals WHERE owner_id=$1 ORDER BY created_at`, o)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Goal{}
	for rows.Next() {
		var g Goal
		if e := rows.Scan(&g.ID, &g.Name, &g.WindowDays); e != nil {
			return nil, e
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
func (s Store) CreateGoal(ctx context.Context, o, name string, days int) (Goal, error) {
	g := Goal{ID: uuid.NewString(), Name: strings.TrimSpace(name), WindowDays: days}
	if g.Name == "" || len(g.Name) > 100 || days < 1 || days > 365 {
		return g, errors.New("goal name and a 1–365 day window are required")
	}
	_, e := s.Pool.Exec(ctx, `INSERT INTO goals(id,owner_id,name,window_days) VALUES($1,$2,$3,$4)`, g.ID, o, g.Name, days)
	return g, e
}

type Input struct {
	GoalID         string    `json:"goalId"`
	DeliveryID     string    `json:"deliveryId,omitempty"`
	AttributionRef string    `json:"attributionRef,omitempty"`
	ExternalID     string    `json:"externalId,omitempty"`
	At             time.Time `json:"occurredAt"`
	Value          *string   `json:"value,omitempty"`
	Currency       *string   `json:"currency,omitempty"`
}
type Conversion struct {
	ID         string     `json:"id"`
	GoalID     string     `json:"goalId"`
	DeliveryID *string    `json:"deliveryId"`
	Source     string     `json:"source"`
	At         time.Time  `json:"occurredAt"`
	ReversedAt *time.Time `json:"reversedAt"`
	Value      *string    `json:"value"`
	Currency   *string    `json:"currency"`
}

var amount = regexp.MustCompile(`^(0|[1-9][0-9]{0,12})(\.[0-9]{1,2})?$`)
var curr = regexp.MustCompile(`^[A-Z]{3}$`)

func (s Store) Record(ctx context.Context, o, source string, b Input) (Conversion, error) {
	v := Conversion{ID: uuid.NewString(), GoalID: b.GoalID, Source: source, At: b.At, Value: b.Value, Currency: b.Currency}
	if _, e := uuid.Parse(b.GoalID); e != nil {
		return v, errors.New("invalid goal")
	}
	if b.At.IsZero() || b.At.After(time.Now().Add(time.Minute)) || b.At.Before(time.Now().AddDate(-2, 0, 0)) {
		return v, errors.New("event time must be within the last two years")
	}
	if b.Value != nil && (b.Currency == nil || !amount.MatchString(*b.Value) || !curr.MatchString(*b.Currency)) {
		return v, errors.New("value requires a nonnegative decimal amount and uppercase currency")
	}
	if b.Currency != nil && b.Value == nil {
		return v, errors.New("currency requires value")
	}
	if source == "webhook" && (b.ExternalID == "" || len(b.ExternalID) > 128) {
		return v, errors.New("externalId is required")
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return v, e
	}
	defer tx.Rollback(ctx)
	var days int
	var goalOwner string
	e = tx.QueryRow(ctx, `SELECT owner_id,window_days FROM goals WHERE id=$1`, b.GoalID).Scan(&goalOwner, &days)
	if e != nil {
		return v, e
	}
	if source == "manual" && goalOwner != o {
		return v, pgx.ErrNoRows
	}
	o = goalOwner
	delivery := b.DeliveryID
	if source == "webhook" {
		delivery = ""
		if h, e := analytics.TokenHash(b.AttributionRef); e == nil {
			_ = tx.QueryRow(ctx, `SELECT l.delivery_id FROM tracked_links l JOIN deliveries d ON d.id=l.delivery_id JOIN conversations c ON c.id=d.conversation_id WHERE l.attribution_hash=$1 AND c.owner_id=$2 AND d.confirmed_sent_at IS NOT NULL AND $3 BETWEEN d.confirmed_sent_at AND d.confirmed_sent_at+($4::int*interval '1 day') AND EXISTS(SELECT 1 FROM activity_events e WHERE e.link_id=l.id AND e.source NOT IN ('image_proxy','suspected_automation') AND e.occurred_at<=$3 AND e.occurred_at>=$3-($4::int*interval '1 day')) ORDER BY l.created_at DESC LIMIT 1`, h, o, b.At, days).Scan(&delivery)
		}
	}
	if delivery != "" {
		var own string
		e = tx.QueryRow(ctx, `SELECT c.owner_id FROM deliveries d JOIN conversations c ON c.id=d.conversation_id WHERE d.id=$1`, delivery).Scan(&own)
		if e != nil {
			return v, e
		}
		if own != o {
			return v, pgx.ErrNoRows
		}
		v.DeliveryID = &delivery
	}
	encoded, _ := json.Marshal(b)
	hash := sha256.Sum256(encoded)
	tag, e := tx.Exec(ctx, `INSERT INTO conversions(id,goal_id,delivery_id,owner_id,source,external_id,occurred_at,value,currency,request_hash) VALUES($1,$2,NULLIF($3,'')::uuid,$4,$5,NULLIF($6,''),$7,$8,$9,$10) ON CONFLICT(owner_id,source,external_id) DO NOTHING`, v.ID, b.GoalID, delivery, o, source, b.ExternalID, b.At, b.Value, b.Currency, hash[:])
	if e != nil {
		return v, e
	}
	if tag.RowsAffected() == 0 {
		var saved []byte
		e = tx.QueryRow(ctx, `SELECT id,delivery_id,request_hash FROM conversions WHERE owner_id=$1 AND source=$2 AND external_id=$3`, o, source, b.ExternalID).Scan(&v.ID, &v.DeliveryID, &saved)
		if e != nil {
			return v, e
		}
		if string(saved) != string(hash[:]) {
			return v, errors.New("externalId was reused with different data")
		}
		return v, nil
	}
	if delivery != "" {
		meta, _ := json.Marshal(map[string]any{"goalId": b.GoalID, "conversionId": v.ID, "evidence": source})
		_, e = tx.Exec(ctx, `INSERT INTO activity_events(delivery_id,kind,source,occurred_at,metadata,dedupe_key) VALUES($1,'conversion',$2,$3,$4,$5)`, delivery, source, b.At, meta, "conversion:"+v.ID)
		if e != nil {
			return v, e
		}
		_, e = tx.Exec(ctx, `INSERT INTO activity_outbox(owner_id) VALUES($1)`, o)
		if e != nil {
			return v, e
		}
	}
	return v, tx.Commit(ctx)
}
func (s Store) List(ctx context.Context, o, c string) ([]Conversion, error) {
	rows, e := s.Pool.Query(ctx, `SELECT x.id,x.goal_id,x.delivery_id,x.source,x.occurred_at,x.reversed_at,x.value::text,x.currency FROM conversions x LEFT JOIN deliveries d ON d.id=x.delivery_id WHERE x.owner_id=$1 AND ($2='' OR d.conversation_id::text=$2) ORDER BY x.recorded_at DESC LIMIT 100`, o, c)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Conversion{}
	for rows.Next() {
		var v Conversion
		if e := rows.Scan(&v.ID, &v.GoalID, &v.DeliveryID, &v.Source, &v.At, &v.ReversedAt, &v.Value, &v.Currency); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s Store) Reverse(ctx context.Context, o, id string) error {
	tag, e := s.Pool.Exec(ctx, `UPDATE conversions SET reversed_at=COALESCE(reversed_at,now()) WHERE owner_id=$1 AND id=$2`, o, id)
	if e == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return e
}

type Report struct {
	Sent        int       `json:"sent"`
	ImageActive int       `json:"imageActive"`
	Linked      int       `json:"linked"`
	Clicked     int       `json:"clicked"`
	Replied     int       `json:"replied"`
	Bounced     int       `json:"bounceReported"`
	Converted   int       `json:"converted"`
	Prepared    int       `json:"prepared"`
	Unknown     int       `json:"unknown"`
	From        time.Time `json:"from"`
	To          time.Time `json:"to"`
	Policy      string    `json:"policy"`
}

func (s Store) Report(ctx context.Context, o, mailbox, goal string, from, to time.Time) (Report, error) {
	v := Report{From: from, To: to, Policy: "v1: confirmed sends; unique deliveries; known proxies/scanners excluded; event cutoff is cohort end; conversions use each goal's send-based window"}
	e := s.Pool.QueryRow(ctx, `WITH cohort AS (SELECT d.* FROM deliveries d JOIN conversations c ON c.id=d.conversation_id WHERE c.owner_id=$1 AND ($2='' OR c.mailbox_id::text=$2) AND d.confirmed_sent_at >=$3 AND d.confirmed_sent_at<$4) SELECT count(*),count(*) FILTER(WHERE EXISTS(SELECT 1 FROM activity_events e WHERE e.delivery_id=d.id AND e.kind='image_request' AND e.source NOT IN ('image_proxy','suspected_automation','legacy') AND e.occurred_at BETWEEN d.confirmed_sent_at AND $4)),count(*) FILTER(WHERE EXISTS(SELECT 1 FROM tracked_links l WHERE l.delivery_id=d.id)),count(*) FILTER(WHERE EXISTS(SELECT 1 FROM activity_events e WHERE e.delivery_id=d.id AND e.kind='link_request' AND e.source NOT IN ('image_proxy','suspected_automation') AND e.occurred_at BETWEEN d.confirmed_sent_at AND $4)),count(*) FILTER(WHERE EXISTS(SELECT 1 FROM activity_events e WHERE e.delivery_id=d.id AND e.kind='reply' AND e.occurred_at BETWEEN d.confirmed_sent_at AND $4)),count(*) FILTER(WHERE EXISTS(SELECT 1 FROM activity_events e WHERE e.delivery_id=d.id AND e.kind='bounce' AND e.occurred_at BETWEEN d.confirmed_sent_at AND $4)),count(*) FILTER(WHERE EXISTS(SELECT 1 FROM conversions x JOIN goals g ON g.id=x.goal_id WHERE x.delivery_id=d.id AND x.reversed_at IS NULL AND ($5='' OR x.goal_id::text=$5) AND x.occurred_at BETWEEN d.confirmed_sent_at AND LEAST($4,d.confirmed_sent_at+(g.window_days*interval '1 day')))) FROM cohort d`, o, mailbox, from, to, goal).Scan(&v.Sent, &v.ImageActive, &v.Linked, &v.Clicked, &v.Replied, &v.Bounced, &v.Converted)
	if e != nil {
		return v, e
	}
	e = s.Pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE d.status='prepared'),count(*) FILTER(WHERE d.status='unknown') FROM deliveries d JOIN conversations c ON c.id=d.conversation_id WHERE c.owner_id=$1 AND ($2='' OR c.mailbox_id::text=$2) AND d.created_at >=$3 AND d.created_at<$4`, o, mailbox, from, to).Scan(&v.Prepared, &v.Unknown)
	return v, e
}
