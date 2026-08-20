package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/attributionledger"
	"github.com/google/uuid"
)

func TestV2ClaimHTTPReplayAuthorizationAndEpochIsolation(t *testing.T) {
	env := setupFullTestEnv(t)
	ctx := context.Background()
	installationID := uuid.NewString()
	enroll := doFullRequest(env, http.MethodPost, "/api/v1/attribution/installations", map[string]any{"installation_id": installationID})
	credentials := parseFullResponse(t, enroll)["data"].(map[string]any)
	protocol := credentials["protocol"].(map[string]any)
	if protocol["ledger_epoch"] != "shadow_v2" || protocol["v1_write_policy"] != "accept" {
		t.Fatalf("enrollment protocol = %+v", protocol)
	}
	reporterToken := credentials["reporter_token"].(string)
	if retired := doFullRequest(env, http.MethodPut, "/api/v1/attribution/installations/"+installationID, map[string]any{"reporting_enabled": true, "otel_enabled": true}); retired.Code != http.StatusBadRequest {
		t.Fatalf("retired OTel enable = %d: %s", retired.Code, retired.Body.String())
	}
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
	if firstResult["ledger_epoch"] != "shadow_v2" || firstResult["v1_write_policy"] != "accept" || firstResult["results"].([]any)[0].(map[string]any)["group"].(map[string]any)["status"] != "persisted" {
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

func TestAttributionHTTPFormalProtocolRemovesEveryV1WriteAndAcceptsV2(t *testing.T) {
	protocol := attributionledger.ProtocolContract{
		LedgerEpoch:       attributionledger.LedgerEpochFormalV2,
		V1WritePolicy:     attributionledger.V1WritePolicyUpgradeNeeded,
		MinimumCLIVersion: "0.2.0-preview.5",
	}
	env := setupFullTestEnvWithOptions(t, RouterOptions{AttributionProtocol: protocol})
	ctx := context.Background()
	installationID := uuid.NewString()
	enroll := doFullRequest(env, http.MethodPost, "/api/v1/attribution/installations", map[string]any{"installation_id": installationID})
	credentials := parseFullResponse(t, enroll)["data"].(map[string]any)
	if got := credentials["protocol"].(map[string]any); got["ledger_epoch"] != "formal_v2" || got["v1_write_policy"] != "upgrade_required" || got["minimum_cli_version"] != "0.2.0-preview.5" {
		t.Fatalf("enrollment protocol = %+v", got)
	}
	reporterToken := credentials["reporter_token"].(string)
	if enable := doFullRequest(env, http.MethodPut, "/api/v1/attribution/installations/"+installationID, map[string]any{"reporting_enabled": true}); enable.Code != http.StatusOK {
		t.Fatalf("enable = %d: %s", enable.Code, enable.Body.String())
	}

	for _, path := range []string{
		"/api/v1/attribution/usage-buckets/batch",
		"/api/v1/attribution/usage-buckets/missing-bucket/revisions",
		"/api/v1/attribution/otel/v1/traces",
	} {
		response := doFullRequestWithToken(env, http.MethodPost, path, map[string]any{}, reporterToken)
		if response.Code != http.StatusNotFound {
			t.Fatalf("removed route %s status = %d: %s", path, response.Code, response.Body.String())
		}
	}
	for _, path := range []string{
		"/api/v1/attribution/report",
		"/api/v1/activity/scope",
		"/api/v1/activity/summary",
		"/api/v1/activity/members",
		"/api/v1/activity/members/1",
		"/api/v1/activity/teams/team-alpha",
		"/api/v1/activity/repos/1",
		"/api/v1/activity/buckets/legacy",
	} {
		response := doFullRequest(env, http.MethodGet, path, nil)
		if response.Code != http.StatusNotFound {
			t.Fatalf("removed route %s status = %d: %s", path, response.Code, response.Body.String())
		}
	}
	if env.client.AttributionUsageBucket.Query().CountX(ctx) != 0 {
		t.Fatal("formal protocol persisted a v1 usage bucket")
	}

	owner := env.client.User.Query().Where(user.UsernameEQ("fulladmin")).OnlyX(ctx)
	provider := env.client.RelayProvider.Create().SetName("relay-formal").SetDisplayName("Relay Formal").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-key").SaveX(ctx)
	repoID := createFullTestRepo(t, env.client)
	env.client.CommitCheckpoint.Create().SetEventID("checkpoint-formal-http").SetUserID(owner.ID).SetWorkspaceID("workspace-formal").
		SetRepoConfigID(repoID).SetCommitSha("commit-formal").SetParentShas([]string{"parent-formal"}).SetBindingSource(commitcheckpoint.BindingSourceManual).SaveX(ctx)
	claim := map[string]any{"groups": []map[string]any{{
		"schema_version": 2, "group_id": "group-formal-http", "relay_provider_id": provider.ID,
		"thread_id": "thread-formal", "turn_id": "turn-formal", "evidence_digest": "evidence-formal",
		"commit_allocations": []map[string]any{{"sequence": 1, "repo_config_id": repoID, "workspace_id": "workspace-formal", "checkpoint_event_id": "checkpoint-formal-http", "commit_sha": "commit-formal", "evidence_digest": "evidence-formal"}},
		"request_ids":        []string{"req-formal-http"},
	}}}
	v2 := doFullRequestWithToken(env, http.MethodPost, "/api/v1/attribution/v2/claim-groups/batch", claim, reporterToken)
	if v2.Code != http.StatusCreated {
		t.Fatalf("v2 status = %d: %s", v2.Code, v2.Body.String())
	}
	result := parseFullResponse(t, v2)["data"].(map[string]any)
	if result["ledger_epoch"] != "formal_v2" || result["v1_write_policy"] != "upgrade_required" || result["minimum_cli_version"] != "0.2.0-preview.5" {
		t.Fatalf("v2 protocol ACK = %+v", result)
	}
}
