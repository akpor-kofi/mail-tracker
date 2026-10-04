package analytics

import "testing"

func TestCapabilityTokens(t *testing.T) {
	a, h, e := Token()
	if e != nil {
		t.Fatal(e)
	}
	b, _, e := Token()
	if e != nil || a == b || len(h) != 32 {
		t.Fatal("token entropy/format")
	}
	v, e := TokenHash(a)
	if e != nil || string(v) != string(h) {
		t.Fatal("token hashes differ")
	}
	for _, bad := range []string{"", "abc", "../../secret"} {
		if _, e := TokenHash(bad); e == nil {
			t.Fatal("accepted invalid token")
		}
	}
}
func TestDestinationSafety(t *testing.T) {
	for _, bad := range []string{"javascript:alert(1)", "file:///tmp/x", "https://user:pass@example.com", "https://example.com\r\nx: y", "//example.com"} {
		if ValidDestination(bad) {
			t.Fatalf("unsafe destination %q", bad)
		}
	}
	if !ValidDestination("https://example.com/a?b=c#d") {
		t.Fatal("valid URL rejected")
	}
}
func TestEvidenceClassification(t *testing.T) {
	if Source("Mozilla GoogleImageProxy") != "image_proxy" || Source("Proofpoint scanner") != "suspected_automation" || Source("Mozilla") != "unclassified" {
		t.Fatal("classification must not label browsers verified human")
	}
}
