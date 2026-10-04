package analytics

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"strconv"
	"time"
)

type Metric struct {
	Raw        int        `json:"raw"`
	Sessions   int        `json:"sessions"`
	Automation int        `json:"automation"`
	Legacy     int        `json:"legacy"`
	First      *time.Time `json:"first"`
	Last       *time.Time `json:"last"`
}
type Link struct {
	ID          string `json:"id"`
	Destination string `json:"destination"`
	Requests    int    `json:"requests"`
	Eligible    int    `json:"eligible"`
}
type Summary struct {
	Images           Metric    `json:"images"`
	Clicks           Metric    `json:"clicks"`
	DocumentSessions int       `json:"documentSessions"`
	ActiveSeconds    float64   `json:"activeSeconds"`
	Downloads        int       `json:"downloads"`
	Links            []Link    `json:"links"`
	Capped           bool      `json:"capped"`
	Policy           string    `json:"policy"`
	AsOf             time.Time `json:"asOf"`
}

func (s Store) Owns(ctx context.Context, o, c string) error {
	var id string
	return s.Pool.QueryRow(ctx, `SELECT id FROM conversations WHERE owner_id=$1 AND id=$2`, o, c).Scan(&id)
}
func (s Store) Summary(ctx context.Context, o, c string) (Summary, error) {
	out := Summary{Links: []Link{}, Policy: "activity-v1: 5-minute sessions; known proxies/scanners excluded", AsOf: time.Now().UTC()}
	if e := s.Owns(ctx, o, c); e != nil {
		return out, e
	}
	rows, e := s.Pool.Query(ctx, `WITH observed AS (SELECT e.*,lag(e.occurred_at) OVER (PARTITION BY e.delivery_id,e.kind ORDER BY e.occurred_at,e.id) prev FROM activity_events e JOIN deliveries d ON d.id=e.delivery_id WHERE d.conversation_id=$1 AND source NOT IN ('image_proxy','suspected_automation')) SELECT kind,count(*),count(*) FILTER (WHERE prev IS NULL OR occurred_at-prev>interval '5 minutes'),count(*) FILTER(WHERE source='legacy'),min(occurred_at),max(occurred_at) FROM observed WHERE kind IN ('image_request','link_request') GROUP BY kind`, c)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var m Metric
		if e := rows.Scan(&k, &m.Raw, &m.Sessions, &m.Legacy, &m.First, &m.Last); e != nil {
			return out, e
		}
		if k == "image_request" {
			out.Images = m
		} else {
			out.Clicks = m
		}
	}
	if e := rows.Err(); e != nil {
		return out, e
	}
	rows.Close()
	rows, e = s.Pool.Query(ctx, `SELECT kind,count(*) FROM activity_events e JOIN deliveries d ON d.id=e.delivery_id WHERE d.conversation_id=$1 AND source IN ('image_proxy','suspected_automation') GROUP BY kind`, c)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var k string
		var n int
		if e := rows.Scan(&k, &n); e != nil {
			rows.Close()
			return out, e
		}
		if k == "image_request" {
			out.Images.Automation = n
			out.Images.Raw += n
		} else if k == "link_request" {
			out.Clicks.Automation = n
			out.Clicks.Raw += n
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	e = s.Pool.QueryRow(ctx, `SELECT COALESCE(bool_or(activity_capped),false) FROM deliveries WHERE conversation_id=$1`, c).Scan(&out.Capped)
	if e != nil {
		return out, e
	}
	rows, e = s.Pool.Query(ctx, `SELECT l.id,l.destination,count(e.id),count(e.id) FILTER(WHERE e.source NOT IN ('image_proxy','suspected_automation')) FROM tracked_links l JOIN deliveries d ON d.id=l.delivery_id LEFT JOIN activity_events e ON e.link_id=l.id WHERE d.conversation_id=$1 GROUP BY l.id ORDER BY l.created_at,l.id`, c)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var l Link
		if e := rows.Scan(&l.ID, &l.Destination, &l.Requests, &l.Eligible); e != nil {
			rows.Close()
			return out, e
		}
		out.Links = append(out.Links, l)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	e = s.Pool.QueryRow(ctx, `SELECT count(DISTINCT session_id) FILTER(WHERE kind='viewer_loaded'),COALESCE(sum((metadata->>'activeMs')::numeric) FILTER(WHERE kind='viewer_activity'),0)/1000,count(*) FILTER(WHERE kind='download_request') FROM activity_events e JOIN deliveries d ON d.id=e.delivery_id WHERE d.conversation_id=$1`, c).Scan(&out.DocumentSessions, &out.ActiveSeconds, &out.Downloads)
	return out, e
}

type Event struct {
	ID         string          `json:"id"`
	DeliveryID string          `json:"deliveryId"`
	Kind       string          `json:"kind"`
	Source     string          `json:"source"`
	At         time.Time       `json:"at"`
	Metadata   json.RawMessage `json:"metadata"`
}
type EventPage struct {
	Items      []Event `json:"items"`
	NextCursor string  `json:"nextCursor"`
}

func (s Store) Events(ctx context.Context, o, c, cursor, kind string) (EventPage, error) {
	out := EventPage{Items: []Event{}}
	if e := s.Owns(ctx, o, c); e != nil {
		return out, e
	}
	before := int64(9223372036854775807)
	if cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil {
			return out, errors.New("invalid cursor")
		}
		before, e = strconv.ParseInt(string(b), 10, 64)
		if e != nil {
			return out, e
		}
	}
	rows, e := s.Pool.Query(ctx, `SELECT e.id,e.delivery_id,e.kind,e.source,e.occurred_at,e.metadata FROM activity_events e JOIN deliveries d ON d.id=e.delivery_id WHERE d.conversation_id=$1 AND e.id<$2 AND ($3='' OR e.kind=$3) ORDER BY e.id DESC LIMIT 51`, c, before, kind)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var v Event
		var id int64
		if e := rows.Scan(&id, &v.DeliveryID, &v.Kind, &v.Source, &v.At, &v.Metadata); e != nil {
			return out, e
		}
		v.ID = strconv.FormatInt(id, 10)
		out.Items = append(out.Items, v)
	}
	if len(out.Items) > 50 {
		out.Items = out.Items[:50]
		out.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(out.Items[49].ID))
	}
	return out, rows.Err()
}

var _ = pgx.ErrNoRows
