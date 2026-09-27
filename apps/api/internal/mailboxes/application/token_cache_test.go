package application

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akpor-kofi/mail-tracker/apps/api/internal/mailboxes/domain"
	platformcrypto "github.com/akpor-kofi/mail-tracker/apps/api/internal/platform/crypto"
	"golang.org/x/oauth2"
)

type tokenRepo struct {
	stateRepo
	encrypted []byte
	reads     atomic.Int32
}

func (r *tokenRepo) Get(context.Context, string) (domain.Mailbox, []byte, error) {
	r.reads.Add(1)
	return domain.Mailbox{ID: "mailbox", GoogleSub: "google-sub", Email: "owner@example.com"}, r.encrypted, nil
}

type tokenTransport func(*http.Request) (*http.Response, error)

func (f tokenTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAccessTokenCachedAcrossConcurrentDeliveries(t *testing.T) {
	sealer, err := platformcrypto.NewSealer(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := sealer.Seal("refresh-token")
	if err != nil {
		t.Fatal(err)
	}
	repo := &tokenRepo{encrypted: encrypted}
	var exchanges atomic.Int32
	client := &http.Client{Transport: tokenTransport(func(*http.Request) (*http.Response, error) {
		exchanges.Add(1)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"access_token":"access-token","token_type":"Bearer","expires_in":3600}`))}, nil
	})}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, client)
	service := OAuthService{Repo: repo, Sealer: sealer, Cache: &AccessTokenCache{}, ClientID: "client", ClientSecret: "secret"}
	var running sync.WaitGroup
	for i := 0; i < 20; i++ {
		running.Add(1)
		go func() {
			defer running.Done()
			mailbox, token, err := service.Credentials(ctx, "mailbox")
			if err != nil || mailbox.Email != "owner@example.com" || token.AccessToken != "access-token" {
				t.Errorf("credentials: %v, %+v, %+v", err, mailbox, token)
			}
		}()
	}
	running.Wait()
	if got := repo.reads.Load(); got != 1 {
		t.Fatalf("mailbox read %d times, want 1", got)
	}
	if got := exchanges.Load(); got != 1 {
		t.Fatalf("token exchanged %d times, want 1", got)
	}
	service.ForgetToken("mailbox")
	if _, _, err := service.Credentials(ctx, "mailbox"); err != nil {
		t.Fatal(err)
	}
	if repo.reads.Load() != 2 || exchanges.Load() != 2 {
		t.Fatal("invalidating mailbox did not refresh credentials")
	}
}

func TestAccessTokenExpiresBeforeUse(t *testing.T) {
	cache := &AccessTokenCache{}
	cache.put("mailbox", cachedCredentials{token: oauth2.Token{AccessToken: "old", Expiry: time.Now().Add(30 * time.Second)}})
	if _, ok := cache.get("mailbox"); ok {
		t.Fatal("nearly expired token was cached")
	}
}

func TestFailedRefreshIsBrieflyCached(t *testing.T) {
	sealer, err := platformcrypto.NewSealer(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := sealer.Seal("invalid-refresh-token")
	if err != nil {
		t.Fatal(err)
	}
	repo := &tokenRepo{encrypted: encrypted}
	var exchanges atomic.Int32
	client := &http.Client{Transport: tokenTransport(func(*http.Request) (*http.Response, error) {
		exchanges.Add(1)
		return &http.Response{StatusCode: 400, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":"invalid_grant"}`))}, nil
	})}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, client)
	service := OAuthService{Repo: repo, Sealer: sealer, Cache: &AccessTokenCache{}, ClientID: "client", ClientSecret: "secret"}
	for i := 0; i < 10; i++ {
		if _, _, err := service.Credentials(ctx, "mailbox"); err == nil {
			t.Fatal("invalid refresh token was accepted")
		}
	}
	if exchanges.Load() != 1 {
		t.Fatalf("failed refresh retried %d times", exchanges.Load())
	}
	service.ForgetToken("mailbox")
	if _, _, err := service.Credentials(ctx, "mailbox"); err == nil || exchanges.Load() != 2 {
		t.Fatalf("invalidation did not allow a new refresh: %v", err)
	}
}
