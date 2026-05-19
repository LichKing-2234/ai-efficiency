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

func TestPRHandlerGet_IncludesLastAttributionRunDetails(t *testing.T) {
	env := setupMockTestEnv(t, nil, nil, nil, nil)
	rc := createMockTestRepo(t, env.client)
	pr := env.client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(31).
		SetTitle("Attribution details").
		SetAuthor("alice").
		SetSourceBranch("feat/details").
		SetTargetBranch("main").
		SetStatus("open").
		SetAttributionStatus("clear").
		SetPrimaryTokenCount(321).
		SetPrimaryTokenCost(1.23).
		SetMetadataSummary(map[string]any{
			"intervals": []map[string]any{{
				"commit_sha":    "abc123",
				"total_tokens":  321,
				"total_cost":    1.23,
				"source":        "tool_usage_events",
				"checkpoint_id": 7,
			}},
		}).
		SaveX(context.Background())

	run := env.client.PrAttributionRun.Create().
		SetPrRecordID(pr.ID).
		SetTriggerMode("manual").
		SetTriggeredBy("alice").
		SetStatus("completed").
		SetResultClassification("clear").
		SetMatchedCommitShas([]string{"abc123", "def456"}).
		SetMatchedSessionIds([]string{"sess-1"}).
		SetPrimaryUsageSummary(map[string]any{"total_tokens": 321, "total_cost": 1.23}).
		SetMetadataSummary(map[string]any{"matched_commit_count": 2}).
		SetValidationSummary(map[string]any{"result": "consistent", "reason": "all_matched_checkpoints_bound"}).
		SaveX(context.Background())

	env.client.PrRecord.UpdateOneID(pr.ID).
		SetLastAttributionRunID(run.ID).
		SaveX(context.Background())

	w := doMockRequest(env, "GET", fmt.Sprintf("/api/v1/prs/%d", pr.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	resp := parseMockResponse(t, w)
	data := resp["data"].(map[string]interface{})
	edges := data["edges"].(map[string]interface{})
	lastRun := edges["last_attribution_run"].(map[string]interface{})
	matched := lastRun["matched_commit_shas"].([]interface{})
	if len(matched) != 2 || matched[0] != "abc123" || matched[1] != "def456" {
		t.Fatalf("matched_commit_shas = %v, want [abc123 def456]", matched)
	}
	validation := lastRun["validation_summary"].(map[string]interface{})
	if validation["reason"] != "all_matched_checkpoints_bound" {
		t.Fatalf("validation_summary.reason = %v, want all_matched_checkpoints_bound", validation["reason"])
	}
}
