package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/reportinginstallation"
	"github.com/ai-efficiency/backend/internal/attributionledger"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestAttributionReadinessHTTPStates(t *testing.T) {
	tests := []struct {
		name                  string
		installations         []struct{ enabled, revoked bool }
		bucket                bool
		wantState             string
		wantInstallations     int
		wantEnabled           int
		wantLatestBucketField bool
	}{
		{name: "not enrolled", wantState: "not_enrolled"},
		{name: "disabled", installations: []struct{ enabled, revoked bool }{{}}, wantState: "disabled", wantInstallations: 1},
		{name: "waiting for first bucket", installations: []struct{ enabled, revoked bool }{{enabled: true}}, wantState: "waiting_for_data", wantInstallations: 1, wantEnabled: 1},
		{name: "first bucket is active regardless of allocation", installations: []struct{ enabled, revoked bool }{{enabled: true}}, bucket: true, wantState: "active", wantInstallations: 1, wantEnabled: 1, wantLatestBucketField: true},
		{name: "only revoked", installations: []struct{ enabled, revoked bool }{{enabled: true, revoked: true}}, wantState: "revoked", wantInstallations: 1},
		{name: "active disabled wins over revoked", installations: []struct{ enabled, revoked bool }{{enabled: true, revoked: true}, {}}, wantState: "disabled", wantInstallations: 2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := setupFullTestEnv(t)
			var bucketInstallation *ent.ReportingInstallation
			for index, input := range test.installations {
				create := env.client.ReportingInstallation.Create().
					SetInstallationID(uuid.NewString()).
					SetUserID(1).
					SetReporterTokenHash(uuid.NewString()).
					SetOtlpTokenHash(uuid.NewString()).
					SetReportingEnabled(input.enabled)
				if input.revoked {
					create.SetStatus(reportinginstallation.StatusRevoked)
				}
				row := create.SaveX(context.Background())
				if index == len(test.installations)-1 {
					bucketInstallation = row
				}
			}
			if test.bucket {
				observed := time.Date(2026, 8, 10, 9, 30, 0, 0, time.UTC)
				accepted := time.Date(2026, 8, 10, 10, 45, 0, 0, time.UTC)
				env.client.AttributionUsageBucket.Create().
					SetBucketID(uuid.NewString()).
					SetReportingInstallationID(bucketInstallation.ID).
					SetUserID(1).
					SetTool("codex").
					SetSessionSlices([]map[string]any{}).
					SetObservedStartAt(observed.Add(-time.Minute)).
					SetObservedEndAt(observed).
					SetSourceDigest("source-digest").
					SetImmutableDigest("immutable-digest").
					SetExtractorVersion("test").
					SetTokenQuality("measured").
					SetCreatedAt(accepted).
					SaveX(context.Background())
			}

			response := doFullRequest(env, http.MethodGet, "/api/v1/attribution/status", nil)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
			}
			data := parseFullResponse(t, response)["data"].(map[string]any)
			if data["state"] != test.wantState {
				t.Fatalf("state = %v, want %s", data["state"], test.wantState)
			}
			if int(data["installation_count"].(float64)) != test.wantInstallations {
				t.Fatalf("installation_count = %v, want %d", data["installation_count"], test.wantInstallations)
			}
			if int(data["enabled_installation_count"].(float64)) != test.wantEnabled {
				t.Fatalf("enabled_installation_count = %v, want %d", data["enabled_installation_count"], test.wantEnabled)
			}
			_, hasLatestBucket := data["latest_bucket_at"]
			if hasLatestBucket != test.wantLatestBucketField {
				t.Fatalf("latest_bucket_at present = %t, want %t", hasLatestBucket, test.wantLatestBucketField)
			}
			if test.wantLatestBucketField && data["latest_bucket_at"] != "2026-08-10T10:45:00Z" {
				t.Fatalf("latest_bucket_at = %v, want server acceptance time", data["latest_bucket_at"])
			}
			if _, exposed := data["installations"]; exposed {
				t.Fatal("response exposed installation rows")
			}
			if _, exposed := data["last_seen_at"]; exposed {
				t.Fatal("response exposed last_seen_at")
			}
		})
	}
}

func TestAttributionEnrollmentReactivatesRevokedInstallationWithNewCredentials(t *testing.T) {
	env := setupFullTestEnv(t)
	installationID := uuid.NewString()
	revoked := env.client.ReportingInstallation.Create().
		SetInstallationID(installationID).
		SetUserID(1).
		SetReporterTokenHash("old-reporter-hash").
		SetOtlpTokenHash("old-otlp-hash").
		SetReportingEnabled(true).
		SetOtelEnabled(true).
		SetStatus(reportinginstallation.StatusRevoked).
		SaveX(context.Background())
	other := env.client.User.Create().
		SetUsername("bob").
		SetEmail("bob@example.org").
		SetAuthSource("ldap").
		SaveX(context.Background())
	if _, err := attributionledger.NewInstallationService(env.client).Ensure(context.Background(), other.ID, installationID, "other machine", "test"); !errors.Is(err, attributionledger.ErrInstallationForbidden) {
		t.Fatalf("other user re-enrollment error = %v, want forbidden", err)
	}

	response := doFullRequest(env, http.MethodPost, "/api/v1/attribution/installations", map[string]any{
		"installation_id": installationID,
		"label":           "re-enrolled machine",
		"client_version":  "test",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	data := parseFullResponse(t, response)["data"].(map[string]any)
	if data["reporter_token"] == "" || data["otlp_token"] == "" {
		t.Fatalf("reactivation did not return replacement credentials: %+v", data)
	}
	reactivated := env.client.ReportingInstallation.GetX(context.Background(), revoked.ID)
	if reactivated.Status != reportinginstallation.StatusActive || reactivated.ReportingEnabled || reactivated.OtelEnabled {
		t.Fatalf("reactivated installation = %+v", reactivated)
	}
	if reactivated.ReporterTokenHash == "old-reporter-hash" || reactivated.OtlpTokenHash == "old-otlp-hash" {
		t.Fatal("reactivation retained revoked credential hashes")
	}
}

func TestAttributionReadinessRequiresAuthentication(t *testing.T) {
	env := setupFullTestEnv(t)
	response := doFullRequestWithToken(env, http.MethodGet, "/api/v1/attribution/status", nil, "")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestAttributionReadinessIsScopedToCurrentUser(t *testing.T) {
	env := setupFullTestEnv(t)
	other := env.client.User.Create().
		SetUsername("bob").
		SetEmail("bob@example.org").
		SetAuthSource("ldap").
		SaveX(context.Background())
	env.client.ReportingInstallation.Create().
		SetInstallationID(uuid.NewString()).
		SetUserID(other.ID).
		SetReporterTokenHash(uuid.NewString()).
		SetOtlpTokenHash(uuid.NewString()).
		SetReportingEnabled(true).
		SaveX(context.Background())

	response := doFullRequest(env, http.MethodGet, "/api/v1/attribution/status", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	data := parseFullResponse(t, response)["data"].(map[string]any)
	if data["state"] != "not_enrolled" || data["installation_count"] != float64(0) {
		t.Fatalf("readiness leaked another user's installation: %+v", data)
	}
}

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
