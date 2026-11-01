package relay

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestUpstreamHTTPStatusCarriesResponseStatus(t *testing.T) {
	err := ownerAccountStatusError("relay: refresh OpenAI quota for account 7", http.StatusUnauthorized, []byte(`{"message":"upstream returned 401"}`), true)
	status, ok := UpstreamHTTPStatus(err)
	if !ok || status != http.StatusUnauthorized {
		t.Fatalf("status = %d ok = %t, want 401", status, ok)
	}
	if got, want := err.Error(), "relay: refresh OpenAI quota for account 7: unexpected status 401: upstream returned 401"; got != want {
		t.Fatalf("error message = %q, want %q", got, want)
	}
	wrapped := fmt.Errorf("outer: %w", err)
	if status, ok := UpstreamHTTPStatus(wrapped); !ok || status != http.StatusUnauthorized {
		t.Fatalf("wrapped status = %d ok = %t, want 401", status, ok)
	}
}

func TestUpstreamHTTPStatusIgnoresSentinelAndLocalErrors(t *testing.T) {
	cases := []error{
		fmt.Errorf("%w: operation", ErrCapabilityUnsupported),
		fmt.Errorf("%w: operation", ErrComplianceAcknowledgementRequired),
		fmt.Errorf("%w: operation", ErrUpstreamOutcomeUnknown),
		errors.New("transport failure"),
	}
	for _, err := range cases {
		if status, ok := UpstreamHTTPStatus(err); ok {
			t.Fatalf("error %v reported status %d, want none", err, status)
		}
	}
	if _, ok := UpstreamHTTPStatus(nil); ok {
		t.Fatalf("nil error reported a status")
	}
}
