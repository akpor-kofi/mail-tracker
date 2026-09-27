package main

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestPublicLimiterCapsRequestsAcrossTokens(t *testing.T) {
	app := fiber.New()
	app.Get("/p/:token", publicLimit(2), func(c fiber.Ctx) error { return c.SendStatus(200) })
	for i, path := range []string{"/p/one", "/p/two", "/p/three"} {
		resp, err := app.Test(httptest.NewRequest("GET", path, nil))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		want := 200
		if i == 2 {
			want = 429
		}
		if resp.StatusCode != want {
			t.Fatalf("request %d: got %d, want %d", i, resp.StatusCode, want)
		}
	}
}

func TestPublicLimiterOnGeneratedAddonPath(t *testing.T) {
	app := fiber.New()
	app.Use("/api/v1/addon/pair", publicLimit(1))
	app.Post("/api/v1/addon/pair", func(c fiber.Ctx) error { return c.SendStatus(200) })
	for i := 0; i < 2; i++ {
		resp, err := app.Test(httptest.NewRequest("POST", "/api/v1/addon/pair", nil))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		want := 200
		if i == 1 {
			want = 429
		}
		if resp.StatusCode != want {
			t.Fatalf("request %d: got %d, want %d", i, resp.StatusCode, want)
		}
	}
}
