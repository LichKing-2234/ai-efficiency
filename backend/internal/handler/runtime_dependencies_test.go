package handler

import (
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/internal/testdb"
	"go.uber.org/zap"
)

func TestSetupRouterReportsAllMissingPerformanceInputs(t *testing.T) {
	router, err := SetupRouter(
		nil, nil, nil, nil, nil, nil, nil, "", "", nil, nil, nil, nil, nil, nil,
		RouterOptions{},
	)
	if err == nil {
		t.Fatal("SetupRouter() error = nil, want missing dependency error")
	}
	if router != nil {
		t.Fatalf("SetupRouter() router = %v, want nil", router)
	}
	for _, expected := range []string{
		"provider runtime",
		"cursor secret",
		"directory service",
		"personal usage cache",
		"work items cache",
		"work items revision store",
		"representative scope cache",
		"team usage snapshot cache",
		"team usage origin cache",
		"webhook HTTP client",
		"request logger",
		"request observer",
		"Web Vitals handler",
		"release",
		"request timeout",
	} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("SetupRouter() error = %q, want %q", err, expected)
		}
	}
}

func TestNewProviderHandlerRequiresExplicitRuntime(t *testing.T) {
	client := testdb.Open(t)
	handler, err := NewProviderHandler(client, "test-encryption-key", zap.NewNop(), nil)
	if err == nil || !strings.Contains(err.Error(), "relay runtime") {
		t.Fatalf("NewProviderHandler() handler=%v error=%v, want relay runtime error", handler, err)
	}
}

func TestNewTeamUsageServiceRejectsImplicitUncachedFallback(t *testing.T) {
	client := testdb.Open(t)
	service, err := newTeamUsageService(client, nil, nil, nil, nil, nil, "test-cursor-secret")
	if err == nil || !strings.Contains(err.Error(), "snapshot cache") {
		t.Fatalf("newTeamUsageService() service=%v error=%v, want snapshot cache error", service, err)
	}
}
