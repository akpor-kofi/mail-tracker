package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"
	"time"
)

func TestWebhookSignatureAndReplayWindow(t *testing.T) {
	now := time.Now()
	ts := strconv.FormatInt(now.Unix(), 10)
	body := []byte(`{"externalId":"booking-1"}`)
	m := hmac.New(sha256.New, []byte("secret"))
	m.Write([]byte(ts + "."))
	m.Write(body)
	sig := hex.EncodeToString(m.Sum(nil))
	if !ValidWebhook("secret", ts, sig, body, now) {
		t.Fatal("valid signature rejected")
	}
	if ValidWebhook("other", ts, sig, body, now) || ValidWebhook("secret", ts, sig, []byte("changed"), now) || ValidWebhook("secret", ts, sig, body, now.Add(6*time.Minute)) {
		t.Fatal("tampering/replay accepted")
	}
}
