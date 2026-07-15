package health

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/buildinfo"
)

type pingStub struct {
	err error
}

func (p pingStub) Ping(context.Context) error {
	return p.err
}

type blockingPinger struct{}

func (blockingPinger) Ping(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

type barrierPinger struct {
	started chan<- struct{}
	release <-chan struct{}
}

func (p barrierPinger) Ping(ctx context.Context) error {
	p.started <- struct{}{}
	select {
	case <-p.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type panicPinger struct{}

func (panicPinger) Ping(context.Context) error {
	panic("private panic from alice@example.com")
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

func TestServiceReadyHonorsOverallDeadline(t *testing.T) {
	svc := NewService(
		blockingPinger{},
		pingStub{},
		pingStub{},
		buildinfo.CurrentVersion(),
		WithReadyTimeout(40*time.Millisecond),
	)

	started := time.Now()
	report := svc.Ready(context.Background())
	elapsed := time.Since(started)

	if elapsed >= time.Second {
		t.Fatalf("Ready() took %s, want less than one second", elapsed)
	}
	if report.Status != "not_ready" {
		t.Fatalf("status = %q, want not_ready", report.Status)
	}
	if got := report.Checks[0]; got.Status != "down" || got.Message != "unavailable" {
		t.Fatalf("database check = %+v, want down/unavailable", got)
	}
}

func TestServiceReadyChecksDependenciesInParallel(t *testing.T) {
	started := make(chan struct{}, 3)
	release := make(chan struct{})
	pinger := barrierPinger{started: started, release: release}
	svc := NewService(
		pinger,
		pinger,
		pinger,
		buildinfo.CurrentVersion(),
		WithReadyTimeout(5*time.Second),
	)

	reportDone := make(chan ReadyReport, 1)
	go func() {
		reportDone <- svc.Ready(context.Background())
	}()

	for index := 0; index < 3; index++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatalf("only %d probes started before release; checks are not concurrent", index)
		}
	}
	close(release)

	var report ReadyReport
	select {
	case report = <-reportDone:
	case <-time.After(time.Second):
		t.Fatal("Ready() did not return after all probes were released")
	}

	if report.Status != "ready" {
		t.Fatalf("status = %q, want ready; checks: %+v", report.Status, report.Checks)
	}
	wantNames := []string{"database", "redis", "relay"}
	if len(report.Checks) != len(wantNames) {
		t.Fatalf("checks length = %d, want %d", len(report.Checks), len(wantNames))
	}
	for i, wantName := range wantNames {
		if got := report.Checks[i]; got.Name != wantName || got.Status != "up" {
			t.Fatalf("check[%d] = %+v, want %s/up", i, got, wantName)
		}
	}
}

func TestServiceReadyContainsPingerPanicAndSanitizesResult(t *testing.T) {
	svc := NewService(
		pingStub{},
		pingStub{},
		panicPinger{},
		buildinfo.CurrentVersion(),
	)

	report := svc.Ready(context.Background())

	if report.Status != "degraded" {
		t.Fatalf("status = %q, want degraded", report.Status)
	}
	if got := report.Checks[2]; got.Status != "down" || got.Message != "unavailable" {
		t.Fatalf("relay check = %+v, want down/unavailable", got)
	}
}
