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
	if report.Checks[2].Message != "unavailable" {
		t.Fatalf("expected sanitized message unavailable, got %q", report.Checks[2].Message)
	}
}

func TestHealthServiceDatabaseDownIsNotReady(t *testing.T) {
	svc := NewHealthService(
		pingStub{err: errors.New("db timeout")},
		pingStub{},
		pingStub{},
		CurrentVersion(),
	)

	report := svc.Ready(context.Background())
	if report.Status != "not_ready" {
		t.Fatalf("expected not_ready, got %q", report.Status)
	}
}

func TestHealthServiceNilRelayPingerIsDegradedAndNotConfigured(t *testing.T) {
	svc := NewHealthService(
		pingStub{},
		pingStub{},
		nil,
		CurrentVersion(),
	)

	report := svc.Ready(context.Background())
	if report.Status != "degraded" {
		t.Fatalf("expected degraded, got %q", report.Status)
	}
	if report.Checks[2].Status != "not_configured" {
		t.Fatalf("expected relay check not_configured, got %q", report.Checks[2].Status)
	}
}
