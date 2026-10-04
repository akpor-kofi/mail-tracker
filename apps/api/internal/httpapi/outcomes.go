package httpapi

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/akpor-kofi/mail-tracker/apps/api/internal/outcomes"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strconv"
	"strings"
	"time"
)

func ValidWebhook(secret, timestamp, signature string, body []byte, now time.Time) bool {
	secs, e := strconv.ParseInt(timestamp, 10, 64)
	if e != nil || secret == "" || now.Sub(time.Unix(secs, 0)) > 5*time.Minute || time.Unix(secs, 0).Sub(now) > 5*time.Minute {
		return false
	}
	provided, e := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if e != nil {
		return false
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(timestamp + "."))
	m.Write(body)
	return hmac.Equal(provided, m.Sum(nil))
}
func (s *Server) RegisterOutcomes(r fiber.Router) {
	r.Get("/goals", s.OwnerRoute(func(c fiber.Ctx) error {
		v, e := s.Outcomes.Goals(c.Context(), c.Locals("owner").(string))
		if e != nil {
			return apiError(e)
		}
		return c.JSON(v)
	}))
	r.Post("/goals", s.OwnerRoute(func(c fiber.Ctx) error {
		var b struct {
			Name string `json:"name"`
			Days int    `json:"windowDays"`
		}
		if c.Bind().JSON(&b) != nil || strings.TrimSpace(b.Name) == "" || len(b.Name) > 100 || b.Days < 1 || b.Days > 365 {
			return fiber.NewError(400, "Enter a goal name and a 1–365 day window")
		}
		v, e := s.Outcomes.CreateGoal(c.Context(), c.Locals("owner").(string), b.Name, b.Days)
		if e != nil {
			return apiError(e)
		}
		return c.JSON(v)
	}))
	r.Get("/conversations/:id/outcomes", s.OwnerRoute(func(c fiber.Ctx) error {
		if e := s.Analytics.Owns(c.Context(), c.Locals("owner").(string), c.Params("id")); e != nil {
			return apiError(e)
		}
		v, e := s.Outcomes.List(c.Context(), c.Locals("owner").(string), c.Params("id"))
		if e != nil {
			return apiError(e)
		}
		return c.JSON(v)
	}))
	r.Post("/conversations/:id/outcomes", s.OwnerRoute(func(c fiber.Ctx) error {
		if e := s.Analytics.Owns(c.Context(), c.Locals("owner").(string), c.Params("id")); e != nil {
			return apiError(e)
		}
		var b outcomes.Input
		if c.Bind().JSON(&b) != nil {
			return fiber.NewError(400, "invalid outcome")
		}
		var cid string
		e := s.Analytics.Pool.QueryRow(c.Context(), `SELECT conversation_id FROM deliveries WHERE id=$1`, b.DeliveryID).Scan(&cid)
		if e != nil || cid != c.Params("id") {
			return fiber.NewError(404, "delivery not found")
		}
		v, e := s.Outcomes.Record(c.Context(), c.Locals("owner").(string), "manual", b)
		if e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				return apiError(e)
			}
			return fiber.NewError(400, "Invalid goal, amount, currency or event time")
		}
		return c.JSON(v)
	}))
	r.Post("/outcomes/:outcome/reverse", s.OwnerRoute(func(c fiber.Ctx) error {
		if e := s.Outcomes.Reverse(c.Context(), c.Locals("owner").(string), c.Params("outcome")); e != nil {
			return apiError(e)
		}
		return c.JSON(fiber.Map{"reversed": true})
	}))
	r.Post("/webhooks/conversions", func(c fiber.Ctx) error {
		if s.WebhookSecret == "" {
			return fiber.NewError(503, "conversion webhook is disabled")
		}
		if len(c.Body()) > 65536 {
			return fiber.NewError(413, "payload too large")
		}
		if !ValidWebhook(s.WebhookSecret, c.Get("X-Webhook-Timestamp"), c.Get("X-Webhook-Signature"), c.Body(), time.Now()) {
			return fiber.NewError(401, "invalid webhook signature")
		}
		var b outcomes.Input
		if json.Unmarshal(c.Body(), &b) != nil {
			return fiber.NewError(400, "invalid event")
		}
		v, e := s.Outcomes.Record(c.Context(), "", "webhook", b)
		if e != nil {
			return fiber.NewError(400, "Invalid event, goal, time or duplicate externalId")
		}
		return c.JSON(fiber.Map{"recorded": true, "attributed": v.DeliveryID != nil, "id": v.ID})
	})
	r.Get("/reports", s.OwnerRoute(s.Report))
	r.Get("/reports/export", s.OwnerRoute(s.Report))
	r.Get("/reminders", s.OwnerRoute(func(c fiber.Ctx) error {
		_, e := s.Analytics.Pool.Exec(c.Context(), `UPDATE reminders r SET done_at=now() WHERE owner_id=$1 AND done_at IS NULL AND (EXISTS(SELECT 1 FROM activity_events e JOIN deliveries d ON d.id=e.delivery_id WHERE d.conversation_id=r.conversation_id AND e.kind='reply') OR EXISTS(SELECT 1 FROM conversions x JOIN deliveries d ON d.id=x.delivery_id WHERE d.conversation_id=r.conversation_id AND x.reversed_at IS NULL))`, c.Locals("owner"))
		if e != nil {
			return apiError(e)
		}
		rows, e := s.Analytics.Pool.Query(c.Context(), `SELECT r.id,r.conversation_id,c.subject,r.due_at FROM reminders r JOIN conversations c ON c.id=r.conversation_id WHERE r.owner_id=$1 AND r.done_at IS NULL ORDER BY r.due_at LIMIT 100`, c.Locals("owner"))
		if e != nil {
			return apiError(e)
		}
		defer rows.Close()
		out := []any{}
		for rows.Next() {
			var id, cid, subject string
			var at time.Time
			if e := rows.Scan(&id, &cid, &subject, &at); e != nil {
				return apiError(e)
			}
			out = append(out, fiber.Map{"id": id, "conversationId": cid, "subject": subject, "dueAt": at})
		}
		if e := rows.Err(); e != nil {
			return apiError(e)
		}
		return c.JSON(out)
	}))
	r.Post("/conversations/:id/reminders", s.OwnerRoute(func(c fiber.Ctx) error {
		if e := s.Analytics.Owns(c.Context(), c.Locals("owner").(string), c.Params("id")); e != nil {
			return apiError(e)
		}
		var b struct {
			DueAt time.Time `json:"dueAt"`
		}
		if c.Bind().JSON(&b) != nil || !b.DueAt.After(time.Now()) || b.DueAt.After(time.Now().AddDate(1, 0, 0)) {
			return fiber.NewError(400, "Choose a future reminder within one year")
		}
		_, e := s.Analytics.Pool.Exec(c.Context(), `INSERT INTO reminders(id,owner_id,conversation_id,due_at) VALUES($1,$2,$3,$4)`, uuid.NewString(), c.Locals("owner"), c.Params("id"), b.DueAt)
		if e != nil {
			return apiError(e)
		}
		return c.JSON(fiber.Map{"created": true})
	}))
	r.Post("/reminders/:reminder/done", s.OwnerRoute(func(c fiber.Ctx) error {
		tag, e := s.Analytics.Pool.Exec(c.Context(), `UPDATE reminders SET done_at=now() WHERE owner_id=$1 AND id=$2`, c.Locals("owner"), c.Params("reminder"))
		if e != nil {
			return apiError(e)
		}
		if tag.RowsAffected() == 0 {
			return fiber.NewError(404, "reminder not found")
		}
		return c.JSON(fiber.Map{"done": true})
	}))
}
func (s *Server) Report(c fiber.Ctx) error {
	to := time.Now().UTC()
	from := to.AddDate(0, -1, 0)
	var e error
	if v := c.Query("from"); v != "" {
		from, e = time.Parse(time.RFC3339, v)
		if e != nil {
			return fiber.NewError(400, "invalid from time")
		}
	}
	if v := c.Query("to"); v != "" {
		to, e = time.Parse(time.RFC3339, v)
		if e != nil {
			return fiber.NewError(400, "invalid to time")
		}
	}
	if !to.After(from) || to.Sub(from) > 366*24*time.Hour {
		return fiber.NewError(400, "Select a date range of at most one year")
	}
	v, e := s.Outcomes.Report(c.Context(), c.Locals("owner").(string), c.Query("mailboxId"), c.Query("goalId"), from, to)
	if e != nil {
		return apiError(e)
	}
	if strings.HasSuffix(c.Path(), "/export") {
		var b bytes.Buffer
		w := csv.NewWriter(&b)
		w.Write([]string{"sent", "image_active", "linked", "clicked", "replied", "bounce_reported", "converted", "prepared_excluded", "unknown_excluded", "from", "to"})
		w.Write([]string{strconv.Itoa(v.Sent), strconv.Itoa(v.ImageActive), strconv.Itoa(v.Linked), strconv.Itoa(v.Clicked), strconv.Itoa(v.Replied), strconv.Itoa(v.Bounced), strconv.Itoa(v.Converted), strconv.Itoa(v.Prepared), strconv.Itoa(v.Unknown), v.From.Format(time.RFC3339), v.To.Format(time.RFC3339)})
		w.Flush()
		c.Set("Content-Type", "text/csv")
		c.Set("Content-Disposition", `attachment; filename="mail-tracker-report.csv"`)
		return c.Send(b.Bytes())
	}
	return c.JSON(v)
}
