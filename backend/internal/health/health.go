package health

import (
	"context"

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

type Service struct {
	db      Pinger
	redis   Pinger
	relay   Pinger
	version buildinfo.VersionInfo
}

func NewService(db, redis, relay Pinger, version buildinfo.VersionInfo) *Service {
	return &Service{
		db:      db,
		redis:   redis,
		relay:   relay,
		version: version,
	}
}

func (s *Service) Live() map[string]any {
	return map[string]any{
		"status":  "live",
		"version": s.version,
	}
}

func (s *Service) Ready(ctx context.Context) ReadyReport {
	dbCheck := runCheck(ctx, "database", s.db)
	redisCheck := runCheck(ctx, "redis", s.redis)
	relayCheck := runCheck(ctx, "relay", s.relay)

	status := "ready"
	if dbCheck.Status == "down" {
		status = "not_ready"
	} else if isDegradedDependency(redisCheck.Status) || isDegradedDependency(relayCheck.Status) {
		status = "degraded"
	}

	return ReadyReport{
		Status:  status,
		Version: s.version,
		Checks:  []CheckResult{dbCheck, redisCheck, relayCheck},
	}
}

func runCheck(ctx context.Context, name string, pinger Pinger) CheckResult {
	if pinger == nil {
		return CheckResult{Name: name, Status: "not_configured", Message: "not configured"}
	}
	if err := pinger.Ping(ctx); err != nil {
		return CheckResult{Name: name, Status: "down", Message: "unavailable"}
	}
	return CheckResult{Name: name, Status: "up"}
}

func isDegradedDependency(status string) bool {
	return status == "down" || status == "not_configured"
}
