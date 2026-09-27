package adapters

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	ownerdomain "github.com/akpor-kofi/mail-tracker/apps/api/internal/accounts/domain"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/singleflight"
)

const (
	keyCacheLifetime = 5 * time.Minute
	unknownKeyPause  = 30 * time.Second
	failedFetchPause = 5 * time.Second
)

var (
	errKeyNotFound     = errors.New("JWT signing key not found")
	errJWKSUnavailable = errors.New("JWKS unavailable")
)

type Verifier struct {
	JWKSURL, Issuer, OwnerEmail string
	Pool                        *pgxpool.Pool
	Client                      *http.Client
	mu                          sync.RWMutex
	keys                        map[string]ed25519.PublicKey
	updated                     time.Time
	retryAfter                  time.Time
	refresh                     singleflight.Group
}

func (v *Verifier) key(ctx context.Context, kid string) (ed25519.PublicKey, error) {
	if key, done, err := v.cachedKey(kid); done {
		return key, err
	}
	result := v.refresh.DoChan("jwks", func() (any, error) {
		if _, done, err := v.cachedKey(kid); done {
			return nil, err
		}
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		keys, err := v.fetchKeys(fetchCtx)
		v.mu.Lock()
		if err != nil {
			v.retryAfter = time.Now().Add(failedFetchPause)
		} else {
			v.keys = keys
			v.updated = time.Now()
			v.retryAfter = time.Time{}
		}
		v.mu.Unlock()
		return nil, err
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case refresh := <-result:
		if refresh.Err != nil && !errors.Is(refresh.Err, errKeyNotFound) {
			return nil, refresh.Err
		}
		key, done, err := v.cachedKey(kid)
		if !done {
			return v.key(ctx, kid)
		}
		return key, err
	}
}

func (v *Verifier) cachedKey(kid string) (ed25519.PublicKey, bool, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	key := v.keys[kid]
	if key != nil && time.Since(v.updated) < keyCacheLifetime {
		return key, true, nil
	}
	if key == nil && v.keys != nil && time.Since(v.updated) < unknownKeyPause {
		return nil, true, errKeyNotFound
	}
	if time.Now().Before(v.retryAfter) {
		return nil, true, errJWKSUnavailable
	}
	return nil, false, nil
}

func (v *Verifier) fetchKeys(ctx context.Context) (map[string]ed25519.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.JWKSURL, nil)
	if err != nil {
		return nil, err
	}
	client := v.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errJWKSUnavailable
	}
	var payload struct {
		Keys []struct{ Kid, Kty, Crv, X string }
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	keys := map[string]ed25519.PublicKey{}
	for _, k := range payload.Keys {
		if k.Kty != "OKP" || k.Crv != "Ed25519" {
			continue
		}
		b, err := base64.RawURLEncoding.DecodeString(k.X)
		if err == nil && len(b) == ed25519.PublicKeySize {
			keys[k.Kid] = ed25519.PublicKey(b)
		}
	}
	if len(keys) == 0 {
		return nil, errJWKSUnavailable
	}
	return keys, nil
}
func (v *Verifier) Verify(ctx context.Context, authorization string) (string, error) {
	if !strings.HasPrefix(authorization, "Bearer ") {
		return "", errors.New("missing bearer token")
	}
	raw := strings.TrimPrefix(authorization, "Bearer ")
	claims := &jwt.RegisteredClaims{}
	token, err := jwt.NewParser(jwt.WithValidMethods([]string{"EdDSA"}), jwt.WithIssuer(v.Issuer), jwt.WithAudience(v.Issuer), jwt.WithExpirationRequired()).ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("missing kid")
		}
		return v.key(ctx, kid)
	})
	if err != nil || !token.Valid || claims.Subject == "" {
		return "", errors.New("invalid owner token")
	}
	var email string
	err = v.Pool.QueryRow(ctx, `SELECT email FROM "user" WHERE id=$1`, claims.Subject).Scan(&email)
	if err != nil || !ownerdomain.IsConfiguredOwner(email, v.OwnerEmail) {
		return "", errors.New("not the configured owner")
	}
	return claims.Subject, nil
}
