package httpapi

import (
	"errors"
	"github.com/akpor-kofi/mail-tracker/apps/api/internal/documents"
	"github.com/akpor-kofi/raildrop/sdk/go"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"mime"
	"net/url"
	"strings"
)

func (s *Server) SameOrigin(c fiber.Ctx) error {
	if strings.TrimRight(c.Get("Origin"), "/") != s.Analytics.PublicURL {
		return fiber.NewError(403, "origin not allowed")
	}
	return nil
}
func (s *Server) ViewerSession(c fiber.Ctx) (documents.Session, error) {
	id := c.Params("session")
	if _, e := uuid.Parse(id); e != nil {
		return documents.Session{}, fiber.NewError(404, "viewer session not found")
	}
	v, e := s.Documents.Session(c.Context(), id, c.Cookies("mt_viewer_"+id))
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return v, fiber.NewError(410, "This document link or session has expired or been revoked. Ask the sender for a new link.")
		}
		return v, apiError(e)
	}
	return v, nil
}
func (s *Server) RegisterDocuments(r fiber.Router) {
	r.Delete("/documents/:document", s.OwnerRoute(func(c fiber.Ctx) error {
		if e := s.Documents.Delete(c.Context(), c.Locals("owner").(string), c.Params("document")); e != nil {
			return apiError(e)
		}
		return c.JSON(fiber.Map{"deleted": true})
	}))
	r.Get("/documents", s.OwnerRoute(func(c fiber.Ctx) error {
		v, e := s.Documents.List(c.Context(), c.Locals("owner").(string))
		if e != nil {
			return apiError(e)
		}
		return c.JSON(v)
	}))
	r.Post("/documents", s.OwnerRoute(func(c fiber.Ctx) error {
		if s.Documents.Storage == nil {
			return fiber.NewError(503, "Document storage is not configured")
		}
		name, e := url.PathUnescape(c.Get("X-File-Name"))
		if e != nil {
			return fiber.NewError(400, "invalid filename")
		}
		d, e := s.Documents.Upload(c.Context(), c.Locals("owner").(string), name, c.Body())
		if errors.Is(e, documents.ErrInvalid) {
			return fiber.NewError(400, e.Error())
		}
		if e != nil {
			return apiError(e)
		}
		return c.JSON(d)
	}))
	r.Post("/documents/:document/shares", s.OwnerRoute(func(c fiber.Ctx) error {
		var b struct {
			Days          int  `json:"days"`
			AllowDownload bool `json:"allowDownload"`
		}
		if e := c.Bind().JSON(&b); e != nil || b.Days < 1 || b.Days > 365 {
			return fiber.NewError(400, "expiry must be 1 to 365 days")
		}
		v, e := s.Documents.Share(c.Context(), c.Locals("owner").(string), c.Params("document"), b.Days, b.AllowDownload)
		if e != nil {
			return apiError(e)
		}
		return c.JSON(v)
	}))
	r.Get("/documents/:document/shares", s.OwnerRoute(func(c fiber.Ctx) error {
		v, e := s.Documents.Shares(c.Context(), c.Locals("owner").(string), c.Params("document"))
		if e != nil {
			return apiError(e)
		}
		return c.JSON(v)
	}))
	r.Post("/document-shares/:share/revoke", s.OwnerRoute(func(c fiber.Ctx) error {
		if e := s.Documents.Revoke(c.Context(), c.Locals("owner").(string), c.Params("share")); e != nil {
			return apiError(e)
		}
		return c.JSON(fiber.Map{"revoked": true})
	}))
	r.Post("/viewer/sessions", func(c fiber.Ctx) error {
		if e := s.SameOrigin(c); e != nil {
			return e
		}
		var b struct {
			Token string `json:"token"`
		}
		if e := c.Bind().JSON(&b); e != nil {
			return fiber.NewError(400, "invalid request")
		}
		v, e := s.Documents.Start(c.Context(), b.Token)
		if e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				return fiber.NewError(410, "This document link has expired or been revoked. Ask the sender for a new link.")
			}
			return apiError(e)
		}
		c.Cookie(&fiber.Cookie{Name: "mt_viewer_" + v.ID, Value: v.Secret, Path: "/api/v1/viewer/" + v.ID, HTTPOnly: true, Secure: strings.HasPrefix(s.Analytics.PublicURL, "https://"), SameSite: "Strict", Expires: v.ExpiresAt})
		return c.JSON(v)
	})
	r.Get("/viewer/:session", func(c fiber.Ctx) error {
		v, e := s.ViewerSession(c)
		if e != nil {
			return e
		}
		c.Set("Cache-Control", "no-store")
		return c.JSON(v)
	})
	r.Post("/viewer/:session/events", func(c fiber.Ctx) error {
		if e := s.SameOrigin(c); e != nil {
			return e
		}
		v, e := s.ViewerSession(c)
		if e != nil {
			return e
		}
		var b documents.Batch
		if e := c.Bind().JSON(&b); e != nil {
			return fiber.NewError(400, "invalid telemetry")
		}
		if b.Kind == "download_request" {
			return fiber.NewError(400, "use download endpoint")
		}
		if e := s.Documents.Observe(c.Context(), v, b); e != nil {
			return fiber.NewError(400, "invalid telemetry")
		}
		return c.JSON(fiber.Map{"ok": true})
	})
	r.Get("/viewer/:session/file", s.ViewerFile)
	r.Get("/viewer/:session/download", func(c fiber.Ctx) error {
		v, e := s.ViewerSession(c)
		if e != nil {
			return e
		}
		if !v.AllowDownload {
			return fiber.NewError(403, "Downloads disabled")
		}
		if e = s.Documents.Observe(c.Context(), v, documents.Batch{ID: uuid.NewString(), Kind: "download_request"}); e != nil {
			return apiError(e)
		}
		c.Locals("download", true)
		return s.ViewerFile(c)
	})
}
func (s *Server) ViewerFile(c fiber.Ctx) error {
	v, e := s.ViewerSession(c)
	if e != nil {
		return e
	}
	if s.Documents.Storage == nil {
		return fiber.NewError(503, "Document storage unavailable")
	}
	obj, e := s.Documents.Storage.Get(c.Context(), v.Key, c.Get("Range"))
	if errors.Is(e, raildrop.ErrRangeNotSatisfiable) {
		return fiber.NewError(416, "Requested byte range is unavailable")
	}
	if e != nil {
		return fiber.NewError(502, "Document could not be loaded. Try again.")
	}
	c.Set("Content-Type", v.Type)
	c.Set("Cache-Control", "private, no-store")
	c.Set("X-Content-Type-Options", "nosniff")
	c.Set("Referrer-Policy", "no-referrer")
	c.Set("Accept-Ranges", "bytes")
	if obj.ContentRange != nil {
		c.Status(206)
		c.Set("Content-Range", *obj.ContentRange)
	}
	if c.Locals("download") == true {
		c.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": v.Filename}))
	}
	data, e := io.ReadAll(io.LimitReader(obj.Body, documents.MaxBytes+1))
	obj.Close()
	if e != nil || len(data) > documents.MaxBytes {
		return fiber.NewError(502, "Document could not be loaded")
	}
	return c.Send(data)
}
