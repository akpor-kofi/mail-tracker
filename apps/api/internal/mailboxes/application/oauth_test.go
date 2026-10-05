package application

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"golang.org/x/oauth2"
	"io"
	"net/http"
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
	upserts         int
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
func (r *stateRepo) Upsert(context.Context, domain.Mailbox, []byte) error     { r.upserts++; return nil }
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
	if !strings.Contains(q.Get("scope"), "gmail.send") || q.Get("access_type") != "offline" || q.Get("include_granted_scopes") != "true" {
		t.Fatal("wrong OAuth scopes or access type")
	}
}

type readRepo struct{ stateRepo }

func (r *readRepo) Get(context.Context, string) (domain.Mailbox, []byte, error) {
	return domain.Mailbox{ID: "mailbox", OwnerID: "owner"}, nil, nil
}
func TestReadScopeIsOptIn(t *testing.T) {
	repo := &readRepo{}
	service := OAuthService{Repo: repo, ClientID: "client", BaseURL: "https://example.com"}
	raw, e := service.StartRead(context.Background(), "owner", "mailbox")
	if e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse(raw)
	if !strings.Contains(u.Query().Get("scope"), "gmail.readonly") || !strings.HasPrefix(repo.verifier, "read:mailbox:") {
		t.Fatal("read grant not tied to mailbox")
	}
	raw, e = service.Start(context.Background(), "owner")
	if e != nil {
		t.Fatal(e)
	}
	u, _ = url.Parse(raw)
	if strings.Contains(u.Query().Get("scope"), "gmail.readonly") {
		t.Fatal("send-only connection expanded silently")
	}
}

func TestPartialGrantDoesNotReplaceExistingConnection(t *testing.T) {
	for _, tc := range []struct {
		name, verifier, scope string
		want                  error
	}{
		{"new send connection", "verifier", "openid email", ErrSendPermission},
		{"send reconnect", "send:mailbox:verifier", "openid email", ErrSendPermission},
		{"read reconnect missing send", "read:mailbox:verifier", "openid email https://www.googleapis.com/auth/gmail.readonly", ErrSendPermission},
		{"read reconnect missing read", "read:mailbox:verifier", "openid email https://www.googleapis.com/auth/gmail.send", ErrReadPermission},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &stateRepo{owner: "owner", verifier: tc.verifier}
			client := &http.Client{Transport: tokenTransport(func(req *http.Request) (*http.Response, error) {
				if err := req.ParseForm(); err != nil {
					t.Fatal(err)
				}
				if req.Form.Get("code_verifier") != "verifier" {
					t.Fatal("PKCE verifier lost")
				}
				body, _ := json.Marshal(map[string]any{"access_token": "partial-token", "refresh_token": "partial-refresh", "token_type": "Bearer", "scope": tc.scope})
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
			})}
			ctx := context.WithValue(context.Background(), oauth2.HTTPClient, client)
			service := OAuthService{Repo: repo, ClientID: "client", ClientSecret: "secret"}
			_, err := service.Complete(ctx, "state", "code")
			if !errors.Is(err, tc.want) || repo.upserts != 0 {
				t.Fatalf("partial grant replaced connection: err=%v upserts=%d", err, repo.upserts)
			}
		})
	}
}

type reconnectRepo struct {
	stateRepo
	mailbox domain.Mailbox
}

func (r *reconnectRepo) Get(context.Context, string) (domain.Mailbox, []byte, error) {
	return r.mailbox, nil, nil
}
func TestReconnectPreservesReadChoiceAndOwner(t *testing.T) {
	for _, read := range []bool{false, true} {
		repo := &reconnectRepo{mailbox: domain.Mailbox{ID: "mailbox", OwnerID: "owner", SyncEnabled: read}}
		service := OAuthService{Repo: repo, ClientID: "client"}
		if _, err := service.StartReconnect(context.Background(), "other-owner", "mailbox"); err == nil {
			t.Fatal("another owner started reconnect")
		}
		raw, err := service.StartReconnect(context.Background(), "owner", "mailbox")
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(raw)
		if strings.Contains(u.Query().Get("scope"), "gmail.readonly") != read {
			t.Fatal("reconnect changed read choice")
		}
		purpose := "send:mailbox:"
		if read {
			purpose = "read:mailbox:"
		}
		if !strings.HasPrefix(repo.verifier, purpose) {
			t.Fatal("reconnect lost target mailbox")
		}
	}
}
