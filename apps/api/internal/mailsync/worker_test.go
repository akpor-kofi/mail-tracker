package mailsync

import (
	"encoding/base64"
	"testing"
)

func TestBounceRequiresFailedDSN(t *testing.T) {
	var p Part
	p.MimeType = "message/delivery-status"
	p.Body.Data = base64.RawURLEncoding.EncodeToString([]byte("Reporting-MTA: dns; example.com\r\n\r\nFinal-Recipient: rfc822; recipient@example.com\r\nAction: failed\r\nStatus: 5.1.1\r\n\r\n"))
	r := BounceRecipients(p)
	if len(r) != 1 || r[0] != "recipient@example.com" {
		t.Fatalf("DSN not parsed: %+v", r)
	}
	p.Body.Data = base64.RawURLEncoding.EncodeToString([]byte("Final-Recipient: rfc822; recipient@example.com\r\nAction: delayed\r\nStatus: 4.1.1\r\n\r\n"))
	if len(BounceRecipients(p)) != 0 {
		t.Fatal("delay incorrectly labeled a hard bounce")
	}
}
func TestAutoReplyClassification(t *testing.T) {
	if !IsAutomatic(Part{Headers: []Header{{Name: "Auto-Submitted", Value: "auto-replied"}}}) {
		t.Fatal("automatic reply included")
	}
	if IsAutomatic(Part{Headers: []Header{{Name: "Auto-Submitted", Value: "no"}}}) {
		t.Fatal("human mail excluded")
	}
}
func TestReferenceMatching(t *testing.T) {
	p := Part{Headers: []Header{{Name: "In-Reply-To", Value: "<original@example.com>"}}}
	if References(p) != "<original@example.com> " {
		t.Fatal("reply references lost")
	}
}
