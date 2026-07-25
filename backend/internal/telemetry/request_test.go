package telemetry

import (
	"context"
	"testing"
)

func TestRequestIDRoundTripsThroughContext(t *testing.T) {
	ctx := WithRequestID(context.Background(), "request-alpha")

	if got := RequestID(ctx); got != "request-alpha" {
		t.Fatalf("RequestID() = %q, want %q", got, "request-alpha")
	}
}

func TestRequestIDReturnsEmptyWithoutCorrelation(t *testing.T) {
	if got := RequestID(context.Background()); got != "" {
		t.Fatalf("RequestID() = %q, want empty", got)
	}
}
