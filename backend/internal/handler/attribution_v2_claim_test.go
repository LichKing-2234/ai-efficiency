package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/user"
	"github.com/google/uuid"
)

func TestV2ClaimHTTPReplayAuthorizationAndEpochIsolation(t *testing.T) {
	env := setupFullTestEnv(t)
	ctx := context.Background()
	installationID := uuid.NewString()
	enroll := doFullRequest(env, http.MethodPost, "/api/v1/attribution/installations", map[string]any{"installation_id": installationID})
	credentials := parseFullResponse(t, enroll)["data"].(map[string]any)
	reporterToken := credentials["reporter_token"].(string)
	if enable := doFullRequest(env, http.MethodPut, "/api/v1/attribution/installations/"+installationID, map[string]any{"reporting_enabled": true}); enable.Code != http.StatusOK {
		t.Fatalf("enable = %d: %s", enable.Code, enable.Body.String())
	}
	owner := env.client.User.Query().Where(user.UsernameEQ("fulladmin")).OnlyX(ctx)
	provider := env.client.RelayProvider.Create().SetName("relay-v2").SetDisplayName("Relay V2").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-key").SaveX(ctx)
	repoID := createFullTestRepo(t, env.client)
	checkpoint := env.client.CommitCheckpoint.Create().SetEventID("checkpoint-v2-http").SetUserID(owner.ID).SetWorkspaceID("workspace-v2").
		SetRepoConfigID(repoID).SetCommitSha("commit-v2").SetParentShas([]string{"parent-v2"}).SetBindingSource(commitcheckpoint.BindingSourceManual).SaveX(ctx)
	_ = checkpoint
	claim := map[string]any{"groups": []map[string]any{{
		"schema_version": 2, "group_id": "group-v2-http", "relay_provider_id": provider.ID,
		"thread_id": "thread-v2", "turn_id": "turn-v2", "evidence_digest": "evidence-v2",
		"calibration":        map[string]any{"digest": "calibration-v2", "input_tokens": 10, "output_tokens": 2, "total_tokens": 12},
		"commit_allocations": []map[string]any{{"sequence": 1, "repo_config_id": repoID, "workspace_id": "workspace-v2", "checkpoint_event_id": "checkpoint-v2-http", "commit_sha": "commit-v2", "evidence_digest": "evidence-v2"}},
		"request_ids":        []string{"req-v2"},
	}}}
	unauthorized := doFullRequestWithToken(env, http.MethodPost, "/api/v1/attribution/v2/claim-groups/batch", claim, "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}
	first := doFullRequestWithToken(env, http.MethodPost, "/api/v1/attribution/v2/claim-groups/batch", claim, reporterToken)
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d: %s", first.Code, first.Body.String())
	}
	firstResult := parseFullResponse(t, first)["data"].(map[string]any)
	if firstResult["ledger_epoch"] != "shadow_v2" || firstResult["results"].([]any)[0].(map[string]any)["group"].(map[string]any)["status"] != "persisted" {
		t.Fatalf("first result = %+v", firstResult)
	}
	replay := doFullRequestWithToken(env, http.MethodPost, "/api/v1/attribution/v2/claim-groups/batch", claim, reporterToken)
	replayResult := parseFullResponse(t, replay)["data"].(map[string]any)
	if replayResult["results"].([]any)[0].(map[string]any)["group"].(map[string]any)["status"] != "duplicate_identical" {
		t.Fatalf("replay result = %+v", replayResult)
	}
	if env.client.AttributionClaimGroup.Query().CountX(ctx) != 1 || env.client.AttributionRequestClaim.Query().CountX(ctx) != 1 || env.client.AttributionUsageBucket.Query().CountX(ctx) != 0 {
		t.Fatal("v2 replay duplicated hot claims or changed the formal v1 ledger")
	}
	invalid := doFullRequestWithToken(env, http.MethodPost, "/api/v1/attribution/v2/claim-groups/batch", map[string]any{"groups": []any{}}, reporterToken)
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty batch status = %d: %s", invalid.Code, invalid.Body.String())
	}
}
