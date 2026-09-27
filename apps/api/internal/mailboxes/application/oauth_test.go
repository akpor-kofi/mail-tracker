package application

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/akpor-kofi/mail-tracker/apps/api/internal/mailboxes/domain"
)

type stateRepo struct {
	hash            []byte
	owner, verifier string
	expiry          time.Time
}

func (r *stateRepo) SaveState(_ context.Context, hash []byte, owner, verifier string, expiry time.Time) error {
	r.hash = hash
	r.owner = owner
	r.verifier = verifier
	r.expiry = expiry
	return nil
}
func (r *stateRepo) ConsumeState(context.Context, []byte) (string, string, error) {
	return r.owner, r.verifier, nil
}
func (r *stateRepo) Upsert(context.Context, domain.Mailbox, []byte) error     { return nil }
func (r *stateRepo) UpdateRefreshToken(context.Context, string, []byte) error { return nil }
func (r *stateRepo) List(context.Context, string) ([]domain.Mailbox, error)   { return nil, nil }
func (r *stateRepo) Get(context.Context, string) (domain.Mailbox, []byte, error) {
	return domain.Mailbox{}, nil, nil
}
func (r *stateRepo) Delete(context.Context, string, string) error { return nil }
func (r *stateRepo) FindByGoogleSub(context.Context, string) (domain.Mailbox, error) {
	return domain.Mailbox{}, nil
}
func TestStartStoresHashedStateAndPKCE(t *testing.T) {
	repo := &stateRepo{}
	service := OAuthService{Repo: repo, ClientID: "client", ClientSecret: "secret", BaseURL: "https://example.com"}
	raw, err := service.Start(context.Background(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	state := q.Get("state")
	hash := sha256.Sum256([]byte(state))
	if string(hash[:]) != string(repo.hash) || repo.owner != "owner" || time.Until(repo.expiry) < 9*time.Minute {
		t.Fatal("state was not stored securely")
	}
	challenge := sha256.Sum256([]byte(repo.verifier))
	if q.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(challenge[:]) || q.Get("code_challenge_method") != "S256" {
		t.Fatal("PKCE challenge missing")
	}
	if !strings.Contains(q.Get("scope"), "gmail.send") || q.Get("access_type") != "offline" {
		t.Fatal("wrong OAuth scopes or access type")
	}
}
