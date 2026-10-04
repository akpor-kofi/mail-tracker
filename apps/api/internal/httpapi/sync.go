package httpapi

import (
	"github.com/gofiber/fiber/v3"
	"time"
)

func (s *Server) RegisterSync(r fiber.Router) {
	r.Post("/mailboxes/connect-read", s.OwnerRoute(func(c fiber.Ctx) error {
		var b struct {
			MailboxID string `json:"mailboxId"`
		}
		if c.Bind().JSON(&b) != nil {
			return fiber.NewError(400, "mailbox required")
		}
		u, e := s.Mailbox.StartRead(c.Context(), c.Locals("owner").(string), b.MailboxID)
		if e != nil {
			return fiber.NewError(404, "mailbox not found")
		}
		return c.JSON(fiber.Map{"url": u})
	}))
	r.Get("/mailboxes/health", s.OwnerRoute(func(c fiber.Ctx) error {
		rows, e := s.Analytics.Pool.Query(c.Context(), `SELECT m.id,m.email,m.sync_enabled,COALESCE(s.status,'not_started'),COALESCE(s.error,''),s.last_success_at,EXISTS(SELECT 1 FROM addon_pairs a WHERE a.mailbox_id=m.id) FROM mailboxes m LEFT JOIN mailbox_sync s ON s.mailbox_id=m.id WHERE m.owner_id=$1 ORDER BY m.email`, c.Locals("owner"))
		if e != nil {
			return apiError(e)
		}
		defer rows.Close()
		out := []any{}
		for rows.Next() {
			var id, email, status, msg string
			var enabled, paired bool
			var at *time.Time
			if e := rows.Scan(&id, &email, &enabled, &status, &msg, &at, &paired); e != nil {
				return apiError(e)
			}
			out = append(out, fiber.Map{"id": id, "email": email, "syncEnabled": enabled, "syncStatus": status, "error": msg, "lastSyncAt": at, "addonPaired": paired})
		}
		if e := rows.Err(); e != nil {
			return apiError(e)
		}
		return c.JSON(out)
	}))
	r.Post("/mailboxes/:mailbox/pause-sync", s.OwnerRoute(func(c fiber.Ctx) error {
		tag, e := s.Analytics.Pool.Exec(c.Context(), `UPDATE mailboxes SET sync_enabled=false WHERE id=$1 AND owner_id=$2`, c.Params("mailbox"), c.Locals("owner"))
		if e != nil {
			return apiError(e)
		}
		if tag.RowsAffected() == 0 {
			return fiber.NewError(404, "mailbox not found")
		}
		return c.JSON(fiber.Map{"paused": true})
	}))
	r.Get("/conversations/:id/messages", s.OwnerRoute(func(c fiber.Ctx) error {
		if e := s.Analytics.Owns(c.Context(), c.Locals("owner").(string), c.Params("id")); e != nil {
			return apiError(e)
		}
		rows, e := s.Analytics.Pool.Query(c.Context(), `SELECT DISTINCT m.gmail_id,m.subject,m.sender,m.snippet,m.direction,m.occurred_at FROM synced_messages m JOIN conversations c ON c.mailbox_id=m.mailbox_id JOIN deliveries d ON d.conversation_id=c.id AND d.gmail_thread_id=m.thread_id WHERE c.id=$1 ORDER BY m.occurred_at LIMIT 100`, c.Params("id"))
		if e != nil {
			return apiError(e)
		}
		defer rows.Close()
		out := []any{}
		for rows.Next() {
			var id, subject, sender, snippet, direction string
			var at time.Time
			if e := rows.Scan(&id, &subject, &sender, &snippet, &direction, &at); e != nil {
				return apiError(e)
			}
			out = append(out, fiber.Map{"id": id, "subject": subject, "sender": sender, "snippet": snippet, "direction": direction, "at": at})
		}
		if e := rows.Err(); e != nil {
			return apiError(e)
		}
		return c.JSON(out)
	}))
}
