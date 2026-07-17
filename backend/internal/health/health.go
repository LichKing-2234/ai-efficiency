package health

import (
	"context"
	"time"

	"github.com/ai-efficiency/backend/internal/buildinfo"
)

type Pinger interface {
	Ping(context.Context) error
}

type FuncPinger func(context.Context) error

func (f FuncPinger) Ping(ctx context.Context) error {
	return f(ctx)
}

type CheckResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type ReadyReport struct {
	Status  string                `json:"status"`
	Version buildinfo.VersionInfo `json:"version"`
	Checks  []CheckResult         `json:"checks"`
}

const defaultReadyTimeout = 2 * time.Second

type Option func(*Service)

func WithReadyTimeout(timeout time.Duration) Option {
	return func(service *Service) {
		service.readyTimeout = timeout
	}
}

type Service struct {
	db           Pinger
	redis        Pinger
	relay        Pinger
	version      buildinfo.VersionInfo
	readyTimeout time.Duration
}

func NewService(db, redis, relay Pinger, version buildinfo.VersionInfo, options ...Option) *Service {
	service := &Service{
		db:           db,
		redis:        redis,
		relay:        relay,
		version:      version,
		readyTimeout: defaultReadyTimeout,
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

func (s *Service) Live() map[string]any {
	return map[string]any{
		"status":  "live",
		"version": s.version,
	}
}

func (s *Service) Ready(ctx context.Context) ReadyReport {
	readyCtx, cancel := context.WithTimeout(ctx, s.readyTimeout)
	defer cancel()

	probes := []struct {
		name   string
		pinger Pinger
	}{
		{name: "database", pinger: s.db},
		{name: "redis", pinger: s.redis},
		{name: "relay", pinger: s.relay},
	}
	checks := make([]CheckResult, len(probes))
	results := make(chan indexedCheckResult, len(probes))
	for index, probe := range probes {
		checks[index] = unavailableCheck(probe.name)
		go func(index int, probeName string, pinger Pinger) {
			results <- indexedCheckResult{
				index: index,
				check: runCheck(readyCtx, probeName, pinger),
			}
		}(index, probe.name, probe.pinger)
	}

	remaining := len(probes)
	for remaining > 0 {
		select {
		case result := <-results:
			checks[result.index] = result.check
			remaining--
		case <-readyCtx.Done():
			remaining = 0
		}
	}

	status := "ready"
	if checks[0].Status == "down" {
		status = "not_ready"
	} else if isDegradedDependency(checks[1].Status) || isDegradedDependency(checks[2].Status) {
		status = "degraded"
	}

	return ReadyReport{
		Status:  status,
		Version: s.version,
		Checks:  checks,
	}
}

type indexedCheckResult struct {
	index int
	check CheckResult
}

func runCheck(ctx context.Context, name string, pinger Pinger) (result CheckResult) {
	result = unavailableCheck(name)
	defer func() {
		if recover() != nil {
			result = unavailableCheck(name)
		}
	}()

	if pinger == nil {
		return CheckResult{Name: name, Status: "not_configured", Message: "not configured"}
	}
	if err := pinger.Ping(ctx); err != nil || ctx.Err() != nil {
		return unavailableCheck(name)
	}
	return CheckResult{Name: name, Status: "up"}
}

func unavailableCheck(name string) CheckResult {
	return CheckResult{Name: name, Status: "down", Message: "unavailable"}
}

func isDegradedDependency(status string) bool {
	return status == "down" || status == "not_configured"
}
