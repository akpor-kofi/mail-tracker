package adapters

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	ownerdomain "github.com/akpor-kofi/mail-tracker/apps/api/internal/accounts/domain"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Verifier struct {
	JWKSURL, Issuer, OwnerEmail string
	Pool                        *pgxpool.Pool
	Client                      *http.Client
	mu                          sync.Mutex
	keys                        map[string]ed25519.PublicKey
	updated                     time.Time
}

func (v *Verifier) key(ctx context.Context, kid string) (ed25519.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if time.Since(v.updated) > 5*time.Minute || v.keys == nil || v.keys[kid] == nil {
		req, err := http.NewRequestWithContext(ctx, "GET", v.JWKSURL, nil)
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
		if resp.StatusCode != 200 {
			return nil, errors.New("JWKS unavailable")
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
		v.keys = keys
		v.updated = time.Now()
	}
	key := v.keys[kid]
	if key == nil {
		return nil, errors.New("JWT signing key not found")
	}
	return key, nil
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
