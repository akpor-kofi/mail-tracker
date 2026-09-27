package httpapi

import (
	"strings"
	"testing"
)

func TestStoredDeliveryErrorIsNotExposed(t *testing.T) {
	for _, status := range []string{"failed", "unknown"} {
		message := safeDeliveryError(status, "violates fk_deliveries_owner_id and secret adapter details")
		if message == nil || strings.Contains(*message, "fk_deliveries") || strings.Contains(*message, "adapter") {
			t.Fatalf("%s leaked stored error: %v", status, message)
		}
	}
}
