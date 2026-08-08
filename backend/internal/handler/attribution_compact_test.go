package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/attributionledger"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestAttributionInstallationTokensHaveDisjointScopes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := testdb.Open(t)
	ctx := context.Background()
	user := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SaveX(ctx)
	installations := attributionledger.NewInstallationService(client)
	credentials, err := installations.Ensure(ctx, user.ID, uuid.NewString(), "test machine", "test")
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	if _, err := installations.SetEnabled(ctx, user.ID, credentials.InstallationID, &enabled, &enabled); err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.POST("/reporter", requireInstallationToken(installations, false), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.POST("/otlp", requireInstallationToken(installations, true), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	tests := []struct {
		name   string
		path   string
		token  string
		status int
	}{
		{name: "reporter can write compact resources", path: "/reporter", token: credentials.ReporterToken, status: http.StatusNoContent},
		{name: "reporter cannot ingest OTLP", path: "/otlp", token: credentials.ReporterToken, status: http.StatusUnauthorized},
		{name: "OTLP cannot write compact resources", path: "/reporter", token: credentials.OTLPToken, status: http.StatusUnauthorized},
		{name: "OTLP can ingest OTLP", path: "/otlp", token: credentials.OTLPToken, status: http.StatusNoContent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, test.path, nil)
			request.Header.Set("Authorization", "Bearer "+test.token)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
		})
	}
}

func TestCompactAttributionHTTPVerticalSlice(t *testing.T) {
	env := setupFullTestEnv(t)
	installationID := uuid.NewString()
	enroll := doFullRequest(env, http.MethodPost, "/api/v1/attribution/installations", map[string]any{
		"installation_id": installationID,
		"label":           "test machine",
		"client_version":  "test",
	})
	if enroll.Code != http.StatusOK {
		t.Fatalf("enroll status = %d, body=%s", enroll.Code, enroll.Body.String())
	}
	credentials := parseFullResponse(t, enroll)["data"].(map[string]any)
	oldReporterToken := credentials["reporter_token"].(string)
	enable := doFullRequest(env, http.MethodPut, "/api/v1/attribution/installations/"+installationID, map[string]any{"reporting_enabled": true})
	if enable.Code != http.StatusOK {
		t.Fatalf("enable status = %d, body=%s", enable.Code, enable.Body.String())
	}
	rotate := doFullRequest(env, http.MethodPost, "/api/v1/attribution/installations/"+installationID+"/credentials/rotate", map[string]any{})
	if rotate.Code != http.StatusOK {
		t.Fatalf("rotate status = %d, body=%s", rotate.Code, rotate.Body.String())
	}
	rotated := parseFullResponse(t, rotate)["data"].(map[string]any)
	reporterToken := rotated["reporter_token"].(string)
	if reporterToken == oldReporterToken {
		t.Fatal("credential rotation returned the old reporter token")
	}
	oldTokenProbe := doFullRequestWithToken(env, http.MethodPost, "/api/v1/attribution/usage-buckets/batch", map[string]any{"buckets": []any{}}, oldReporterToken)
	if oldTokenProbe.Code != http.StatusUnauthorized {
		t.Fatalf("old reporter token status = %d, want 401", oldTokenProbe.Code)
	}

	repoID := createFullTestRepo(t, env.client)
	resolved := doFullRequestWithToken(env, http.MethodPost, "/api/v1/attribution/repos/resolve-remote", map[string]any{
		"remote_url": "git@github.com:org/cov-repo.git", "branch": "main", "client_cache_version": "repo-eligibility-v1",
	}, reporterToken)
	if resolved.Code != http.StatusOK {
		t.Fatalf("reporter resolve status = %d, body=%s", resolved.Code, resolved.Body.String())
	}
	resolvedData := parseFullResponse(t, resolved)["data"].(map[string]any)
	if resolvedData["eligible"] != true || int(resolvedData["repo_config_id"].(float64)) != repoID {
		t.Fatalf("reporter resolve = %+v", resolvedData)
	}
	observed := time.Date(2026, 8, 5, 13, 0, 0, 0, time.UTC)
	rawCheckpoint := doFullRequestWithToken(env, http.MethodPost, "/api/v1/attribution/checkpoints/commit", map[string]any{
		"event_id": "compact-checkpoint-raw", "repo_config_id": repoID, "workspace_id": "workspace-http",
		"commit_sha": "commit-raw", "binding_source": "manual", "captured_at": observed,
		"agent_snapshot": map[string]any{"codex": map[string]any{"raw_payload": "must-not-be-retained"}},
	}, reporterToken)
	if rawCheckpoint.Code != http.StatusBadRequest {
		t.Fatalf("compact checkpoint with raw payload status = %d, want 400: %s", rawCheckpoint.Code, rawCheckpoint.Body.String())
	}
	checkpoint := doFullRequestWithToken(env, http.MethodPost, "/api/v1/attribution/checkpoints/commit", map[string]any{
		"event_id": "compact-checkpoint-http", "repo_config_id": repoID, "workspace_id": "workspace-http",
		"commit_sha": "commit-http", "binding_source": "manual", "captured_at": observed.Add(time.Minute),
	}, reporterToken)
	if checkpoint.Code != http.StatusCreated {
		t.Fatalf("checkpoint status = %d, body=%s", checkpoint.Code, checkpoint.Body.String())
	}
	tokens := map[string]any{
		"fresh_input_tokens": 70, "cache_read_tokens": 30, "cache_write_tokens": 0,
		"output_tokens": 40, "reasoning_tokens": 15, "provider_total_tokens": 140, "processed_total_tokens": 140,
	}
	bucket := doFullRequestWithToken(env, http.MethodPost, "/api/v1/attribution/usage-buckets/batch", map[string]any{
		"buckets": []map[string]any{{
			"schema_version": 1, "bucket_id": "bucket-http", "tool": "codex", "model": "gpt-test",
			"session_slices":    []map[string]any{{"conversation_id": "conversation-http", "observed_start_at": observed, "observed_end_at": observed.Add(30 * time.Second), "token_atom_count": 1, "atom_set_digest": "atoms-http"}},
			"observed_start_at": observed, "observed_end_at": observed.Add(30 * time.Second), "tokens": tokens,
			"request_count": 1, "source_event_count": 1, "source_digest": "source-http",
			"extractor_version": "test", "normalization_version": 1, "token_quality": "measured", "coverage_gap_count": 0,
			"initial_revision": map[string]any{
				"revision_id": "revision-http-1", "sequence": 1, "reason": "checkpoint", "evidence_version": "test", "restated_at": observed.Add(time.Minute),
				"allocations": []map[string]any{{"target": map[string]any{"status": "bound_auto", "repo_config_id": repoID, "repo_key": "repo:test", "workspace_id": "workspace-http", "commit_sha": "commit-http", "branch": "feature/test"}, "tokens": tokens}},
			},
		}},
	}, reporterToken)
	if bucket.Code != http.StatusCreated {
		t.Fatalf("bucket status = %d, body=%s", bucket.Code, bucket.Body.String())
	}
	report := doFullRequest(env, http.MethodGet, "/api/v1/attribution/report?from=2026-08-05T00:00:00Z&to=2026-08-06T00:00:00Z", nil)
	if report.Code != http.StatusOK {
		t.Fatalf("report status = %d, body=%s", report.Code, report.Body.String())
	}
	data := parseFullResponse(t, report)["data"].(map[string]any)
	if data["measured_tokens"].(float64) != 140 || data["bound_tokens"].(float64) != 140 || data["unbound_tokens"].(float64) != 0 {
		t.Fatalf("report = %+v", data)
	}
	repositories, ok := data["repositories"].([]any)
	if !ok || len(repositories) != 1 {
		t.Fatalf("report repositories must be a populated JSON array: %#v", data["repositories"])
	}
	repository := repositories[0].(map[string]any)
	for _, field := range []string{"worktrees", "branches", "commits"} {
		if _, ok := repository[field].([]any); !ok {
			t.Fatalf("repository %s must be a JSON array: %#v", field, repository[field])
		}
	}
	commits := repository["commits"].([]any)
	if len(commits) != 1 {
		t.Fatalf("report commits = %#v, want one commit", commits)
	}
	commit := commits[0].(map[string]any)
	for _, field := range []string{"inherited_from_commit_shas", "prs"} {
		if _, ok := commit[field].([]any); !ok {
			t.Fatalf("commit %s must be a JSON array: %#v", field, commit[field])
		}
	}
	if _, ok := data["buckets"].([]any); !ok {
		t.Fatalf("report buckets must be a JSON array: %#v", data["buckets"])
	}

	emptyReport := doFullRequest(env, http.MethodGet, "/api/v1/attribution/report?from=2026-08-01T00:00:00Z&to=2026-08-02T00:00:00Z", nil)
	if emptyReport.Code != http.StatusOK {
		t.Fatalf("empty report status = %d, body=%s", emptyReport.Code, emptyReport.Body.String())
	}
	emptyData := parseFullResponse(t, emptyReport)["data"].(map[string]any)
	for _, field := range []string{"repositories", "buckets"} {
		if values, ok := emptyData[field].([]any); !ok || len(values) != 0 {
			t.Fatalf("empty report %s must be an empty JSON array: %#v", field, emptyData[field])
		}
	}
	reporterRead := doFullRequestWithToken(env, http.MethodGet, "/api/v1/attribution/report", nil, reporterToken)
	if reporterRead.Code != http.StatusUnauthorized {
		t.Fatalf("reporter read status = %d, want 401", reporterRead.Code)
	}
}
