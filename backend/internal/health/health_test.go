package health

import (
	"context"
	"errors"
	"testing"

	"github.com/ai-efficiency/backend/internal/buildinfo"
)

type pingStub struct {
	err error
}

func (p pingStub) Ping(context.Context) error {
	return p.err
}

func TestServiceReadyAndDegradedStates(t *testing.T) {
	svc := NewService(
		pingStub{},
		pingStub{},
		pingStub{err: errors.New("relay down")},
		buildinfo.CurrentVersion(),
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

func TestServiceDatabaseDownIsNotReady(t *testing.T) {
	svc := NewService(
		pingStub{err: errors.New("db timeout")},
		pingStub{},
		pingStub{},
		buildinfo.CurrentVersion(),
	)

	report := svc.Ready(context.Background())
	if report.Status != "not_ready" {
		t.Fatalf("expected not_ready, got %q", report.Status)
	}
}

func TestServiceNilRelayPingerIsDegradedAndNotConfigured(t *testing.T) {
	svc := NewService(
		pingStub{},
		pingStub{},
		nil,
		buildinfo.CurrentVersion(),
	)

	report := svc.Ready(context.Background())
	if report.Status != "degraded" {
		t.Fatalf("expected degraded, got %q", report.Status)
	}
	if report.Checks[2].Status != "not_configured" {
		t.Fatalf("expected relay check not_configured, got %q", report.Checks[2].Status)
	}
}
