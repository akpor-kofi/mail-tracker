package httpapi

import (
	"errors"
	"github.com/akpor-kofi/mail-tracker/apps/api/internal/analytics"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strconv"
)

func (s *Server) OwnerRoute(next fiber.Handler) fiber.Handler {
	return func(c fiber.Ctx) error {
		o, e := s.Auth.Verify(c.Context(), c.Get("Authorization"))
		if e != nil {
			return fiber.NewError(401, "unauthorized")
		}
		c.Locals("owner", o)
		return next(c)
	}
}
func apiError(e error) error {
	if errors.Is(e, pgx.ErrNoRows) {
		return fiber.NewError(404, "not found")
	}
	return internalError("analytics", e)
}
func (s *Server) RegisterAnalytics(r fiber.Router) {
	r.Post("/addon/link", func(c fiber.Ctx) error {
		var b struct {
			IdentityToken  string `json:"identityToken"`
			ConversationID string `json:"conversationId"`
			Destination    string `json:"destination"`
		}
		if c.Bind().JSON(&b) != nil || !analytics.ValidDestination(b.Destination) {
			return fiber.NewError(400, "Enter an HTTP or HTTPS link")
		}
		sub, e := s.Addon.Identity.Verify(c.Context(), b.IdentityToken)
		if e != nil {
			return fiber.NewError(401, "add-on identity could not be verified")
		}
		mid, e := s.Addon.Pairs.PairedMailbox(c.Context(), sub)
		if e != nil {
			return fiber.NewError(401, "pair this account before inserting links")
		}
		m, e := s.Addon.Pairs.Mailbox(c.Context(), mid)
		if e != nil {
			return apiError(e)
		}
		token, hash, e := analytics.Token()
		if e != nil {
			return apiError(e)
		}
		tag, e := s.Analytics.Pool.Exec(c.Context(), `INSERT INTO tracked_links(id,delivery_id,token_hash,destination) SELECT $1,d.id,$2,$3 FROM deliveries d JOIN conversations c ON c.id=d.conversation_id WHERE c.id=$4 AND c.owner_id=$5 AND c.mailbox_id=$6 AND c.mode='addon' LIMIT 1`, uuid.NewString(), hash, b.Destination, b.ConversationID, m.OwnerID, mid)
		if e != nil {
			return apiError(e)
		}
		if tag.RowsAffected() == 0 {
			return fiber.NewError(404, "prepared draft not found")
		}
		return c.JSON(fiber.Map{"url": s.Analytics.PublicURL + "/c/" + token})
	})
	r.Get("/conversations/:id/analytics", s.OwnerRoute(func(c fiber.Ctx) error {
		v, e := s.Analytics.Summary(c.Context(), c.Locals("owner").(string), c.Params("id"))
		if e != nil {
			return apiError(e)
		}
		return c.JSON(v)
	}))
	r.Get("/conversations/:id/events", s.OwnerRoute(func(c fiber.Ctx) error {
		v, e := s.Analytics.Events(c.Context(), c.Locals("owner").(string), c.Params("id"), c.Query("cursor"), c.Query("kind"))
		if e != nil {
			return apiError(e)
		}
		return c.JSON(v)
	}))
	r.Get("/conversation-pages", s.OwnerRoute(func(c fiber.Ctx) error {
		offset, e := strconv.Atoi(c.Query("offset", "0"))
		if e != nil || offset < 0 || offset > 100000 {
			return fiber.NewError(400, "invalid offset")
		}
		rows, e := s.Analytics.Pool.Query(c.Context(), `SELECT id FROM conversations WHERE owner_id=$1 AND ($2='' OR mailbox_id::text=$2) ORDER BY updated_at DESC,id LIMIT 51 OFFSET $3`, c.Locals("owner"), c.Query("mailboxId"), offset)
		if e != nil {
			return apiError(e)
		}
		defer rows.Close()
		ids := []string{}
		for rows.Next() {
			var id string
			if e := rows.Scan(&id); e != nil {
				return apiError(e)
			}
			ids = append(ids, id)
		}
		if e := rows.Err(); e != nil {
			return apiError(e)
		}
		more := len(ids) > 50
		if more {
			ids = ids[:50]
		}
		items := []any{}
		for _, id := range ids {
			d, e := s.Tracking.Detail(c.Context(), c.Locals("owner").(string), id)
			if e != nil {
				return apiError(e)
			}
			items = append(items, convOut(d.Conversation))
		}
		return c.JSON(fiber.Map{"items": items, "hasMore": more})
	}))
}
func (s *Server) LinkRedirect(c fiber.Ctx) error {
	h, e := analytics.TokenHash(c.Params("token"))
	if e != nil {
		return fiber.NewError(404, "link not found")
	}
	dest, e := s.Analytics.Redirect(c.Context(), h, c.Get("User-Agent"), c.Method() == "GET")
	if e != nil {
		return apiError(e)
	}
	c.Set("Cache-Control", "no-store")
	c.Set("Referrer-Policy", "no-referrer")
	return c.Redirect().Status(302).To(dest)
}
