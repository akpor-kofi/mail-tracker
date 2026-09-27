package domain

import "testing"

func TestPlan(t *testing.T) {
	d := Draft{MailboxID: "m", To: []string{"a@example.com", "b@example.com"}, Subject: "Hello"}
	separate, err := Plan(d, "separate")
	if err != nil || len(separate) != 2 || len(separate[0].Recipients) != 1 {
		t.Fatalf("separate: %v %#v", err, separate)
	}
	shared, err := Plan(d, "shared")
	if err != nil || len(shared) != 1 || len(shared[0].Recipients) != 2 {
		t.Fatalf("shared: %v %#v", err, shared)
	}
	d.Cc = []string{"c@example.com"}
	if _, err := Plan(d, "separate"); err == nil {
		t.Fatal("expected Cc rejection")
	}
}
