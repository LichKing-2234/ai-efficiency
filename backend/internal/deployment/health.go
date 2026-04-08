package deployment

import "context"

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
	Status  string        `json:"status"`
	Version VersionInfo   `json:"version"`
	Checks  []CheckResult `json:"checks"`
}

type HealthService struct {
	db      Pinger
	redis   Pinger
	relay   Pinger
	version VersionInfo
}

func NewHealthService(db, redis, relay Pinger, version VersionInfo) *HealthService {
	return &HealthService{
		db:      db,
		redis:   redis,
		relay:   relay,
		version: version,
	}
}

func (s *HealthService) Live() map[string]any {
	return map[string]any{
		"status":  "live",
		"version": s.version,
	}
}

func (s *HealthService) Ready(ctx context.Context) ReadyReport {
	dbCheck := runCheck(ctx, "database", s.db)
	redisCheck := runCheck(ctx, "redis", s.redis)
	relayCheck := runCheck(ctx, "relay", s.relay)

	status := "ready"
	if dbCheck.Status == "down" {
		status = "not_ready"
	} else if redisCheck.Status == "down" || relayCheck.Status == "down" {
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
		return CheckResult{Name: name, Status: "up", Message: "not configured"}
	}
	if err := pinger.Ping(ctx); err != nil {
		return CheckResult{Name: name, Status: "down", Message: err.Error()}
	}
	return CheckResult{Name: name, Status: "up"}
}
