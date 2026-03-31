package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/attribution"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/scm"
)

type mockAttributionSettler struct {
	settleFn func(ctx context.Context, provider scm.SCMProvider, pr *ent.PrRecord, triggeredBy string) (*attribution.SettleResult, error)
}

func (m *mockAttributionSettler) Settle(ctx context.Context, provider scm.SCMProvider, pr *ent.PrRecord, triggeredBy string) (*attribution.SettleResult, error) {
	return m.settleFn(ctx, provider, pr, triggeredBy)
}

func attachSettleRoute(t *testing.T, env *mockTestEnv, repoSCM repoSCMProvider, settleSvc prAttributionSettler) {
	t.Helper()
	prHandler := NewPRHandler(env.client, repoSCM, nil, settleSvc)
	api := env.router.Group("/api/v1")
	api.Use(auth.RequireAuth(env.authSvc))
	api.POST("/prs/:id/settle", prHandler.Settle)
}

func noopSettler(t *testing.T) prAttributionSettler {
	return &mockAttributionSettler{
		settleFn: func(ctx context.Context, provider scm.SCMProvider, pr *ent.PrRecord, triggeredBy string) (*attribution.SettleResult, error) {
			t.Fatal("unexpected settle call")
			return nil, nil
		},
	}
}

func TestPRHandlerSettle_InvalidID(t *testing.T) {
	env := setupMockTestEnv(t, nil, nil, nil, nil)
	attachSettleRoute(t, env, nil, noopSettler(t))

	w := doMockRequest(env, "POST", "/api/v1/prs/abc/settle", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestPRHandlerSettle_NotFound(t *testing.T) {
	env := setupMockTestEnv(t, nil, nil, nil, nil)
	attachSettleRoute(t, env, nil, noopSettler(t))

	w := doMockRequest(env, "POST", "/api/v1/prs/99999/settle", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func doRawMockRequest(env *mockTestEnv, method, path string, body []byte) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+env.token)
	env.router.ServeHTTP(w, req)
	return w
}

func TestPRHandlerSettle_BadJSONPayload(t *testing.T) {
	env := setupMockTestEnv(t, nil, nil, nil, nil)
	attachSettleRoute(t, env, nil, noopSettler(t))
	rc := createMockTestRepo(t, env.client)
	pr := env.client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(1).
		SaveX(context.Background())

	w := doRawMockRequest(env, "POST", fmt.Sprintf("/api/v1/prs/%d/settle", pr.ID), []byte("{"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestPRHandlerSettle_ProviderLookupError(t *testing.T) {
	repoSCM := &mockRepoSCMProvider{
		getSCMProviderFn: func(ctx context.Context, id int) (scm.SCMProvider, *ent.RepoConfig, error) {
			return nil, nil, errors.New("provider unavailable")
		},
	}
	env := setupMockTestEnv(t, nil, nil, repoSCM, nil)
	attachSettleRoute(t, env, repoSCM, &mockAttributionSettler{
		settleFn: func(ctx context.Context, provider scm.SCMProvider, pr *ent.PrRecord, triggeredBy string) (*attribution.SettleResult, error) {
			t.Fatal("settle should not be called when provider lookup fails")
			return nil, nil
		},
	})
	rc := createMockTestRepo(t, env.client)
	pr := env.client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(2).
		SaveX(context.Background())

	w := doMockRequest(env, "POST", fmt.Sprintf("/api/v1/prs/%d/settle", pr.ID), nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestPRHandlerSettle_Success(t *testing.T) {
	repoSCM := &mockRepoSCMProvider{
		getSCMProviderFn: func(ctx context.Context, id int) (scm.SCMProvider, *ent.RepoConfig, error) {
			return &mockSCMProvider{}, &ent.RepoConfig{ID: id, FullName: "org/mock-repo"}, nil
		},
	}
	env := setupMockTestEnv(t, nil, nil, repoSCM, nil)
	attachSettleRoute(t, env, repoSCM, &mockAttributionSettler{
		settleFn: func(ctx context.Context, provider scm.SCMProvider, pr *ent.PrRecord, triggeredBy string) (*attribution.SettleResult, error) {
			return &attribution.SettleResult{
				PRRecordID:            pr.ID,
				ResultClassification:  "clear",
				AttributionStatus:     "clear",
				AttributionConfidence: "high",
				PrimaryTokenCount:     123,
				PrimaryTokenCost:      4.56,
			}, nil
		},
	})
	rc := createMockTestRepo(t, env.client)
	pr := env.client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(3).
		SaveX(context.Background())

	w := doMockRequest(env, "POST", fmt.Sprintf("/api/v1/prs/%d/settle", pr.ID), map[string]any{"triggered_by": "alice"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	resp := parseMockResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["result_classification"] != "clear" {
		t.Fatalf("result_classification = %v, want clear", data["result_classification"])
	}
}
