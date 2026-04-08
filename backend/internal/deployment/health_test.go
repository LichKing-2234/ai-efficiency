package deployment

import (
	"context"
	"errors"
	"testing"
)

type pingStub struct {
	err error
}

func (p pingStub) Ping(context.Context) error {
	return p.err
}

func TestHealthServiceReadyAndDegradedStates(t *testing.T) {
	svc := NewHealthService(
		pingStub{},
		pingStub{},
		pingStub{err: errors.New("relay down")},
		CurrentVersion(),
	)

	report := svc.Ready(context.Background())
	if report.Status != "degraded" {
		t.Fatalf("expected degraded, got %q", report.Status)
	}
	if len(report.Checks) != 3 {
		t.Fatalf("expected 3 checks, got %d", len(report.Checks))
	}
}
