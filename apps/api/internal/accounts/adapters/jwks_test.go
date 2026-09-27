package adapters

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestJWKSUnknownKidsShareRefreshAndNegativeCache(t *testing.T) {
	public, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		fmt.Fprintf(w, `{"keys":[{"kid":"known","kty":"OKP","crv":"Ed25519","x":%q}]}`, base64.RawURLEncoding.EncodeToString(public))
	}))
	defer site.Close()
	v := &Verifier{JWKSURL: site.URL}
	if _, err := v.key(context.Background(), "missing-1"); err != errKeyNotFound {
		t.Fatalf("first missing key: %v", err)
	}
	for i := 0; i < 50; i++ {
		if _, err := v.key(context.Background(), fmt.Sprintf("missing-%d", i)); err != errKeyNotFound {
			t.Fatalf("missing key %d: %v", i, err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("unknown kids caused %d fetches, want 1", got)
	}
}

func TestJWKSRefreshDoesNotBlockCachedKeys(t *testing.T) {
	public, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		fmt.Fprint(w, `{"keys":[]}`)
	}))
	defer site.Close()
	v := &Verifier{JWKSURL: site.URL, keys: map[string]ed25519.PublicKey{"known": public}, updated: time.Now().Add(-time.Minute)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = v.key(context.Background(), "unknown")
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := v.key(ctx, "known"); err != nil {
		t.Fatalf("cached key blocked by refresh: %v", err)
	}
	close(release)
	<-done
}
