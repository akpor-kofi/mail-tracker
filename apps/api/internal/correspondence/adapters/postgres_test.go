package adapters

import (
	"bytes"
	"testing"

	"github.com/akpor-kofi/mail-tracker/apps/api/internal/correspondence/domain"
)

func TestRequestHashTracksContentAndMode(t *testing.T) {
	draft := domain.Draft{MailboxID: "mailbox", To: []string{"a@example.com"}, Subject: "Hello", HTML: "<p>Hello</p>"}
	first, err := requestHash(draft, "shared")
	if err != nil {
		t.Fatal(err)
	}
	again, err := requestHash(draft, "shared")
	if err != nil || !bytes.Equal(first[:], again[:]) {
		t.Fatal("the same request must have the same fingerprint")
	}
	draft.ID = "saved-after-first-attempt"
	again, err = requestHash(draft, "shared")
	if err != nil || !bytes.Equal(first[:], again[:]) {
		t.Fatal("saving a draft must not change the send fingerprint")
	}
	draft.To = []string{"b@example.com"}
	changed, err := requestHash(draft, "shared")
	if err != nil || bytes.Equal(first[:], changed[:]) {
		t.Fatal("changing recipients must change the fingerprint")
	}
	draft.To = []string{"a@example.com"}
	changed, err = requestHash(draft, "separate")
	if err != nil || bytes.Equal(first[:], changed[:]) {
		t.Fatal("changing send mode must change the fingerprint")
	}
}
