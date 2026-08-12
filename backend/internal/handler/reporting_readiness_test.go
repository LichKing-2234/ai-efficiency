package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/attributionusagepoolcommit"
	"github.com/ai-efficiency/backend/ent/reportinginstallation"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/attributionledger"
	platformauth "github.com/ai-efficiency/backend/internal/auth"
	"github.com/gin-gonic/gin"
)

func TestAuthMeDefaultsReportingCapabilitiesOff(t *testing.T) {
	env := setupFullTestEnv(t)
	response := doFullRequest(env, http.MethodGet, "/api/v1/auth/me", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	data := parseFullResponse(t, response)["data"].(map[string]any)
	capabilities, ok := data["reporting_capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("reporting_capabilities = %#v", data["reporting_capabilities"])
	}
	if capabilities["setup_available"] != false || capabilities["readiness_available"] != false {
		t.Fatalf("reporting_capabilities = %#v, want false/false", capabilities)
	}
}

func TestAuthMeReportsConfiguredReportingCapabilities(t *testing.T) {
	for _, test := range []struct {
		name      string
		options   RouterOptions
		setup     bool
		readiness bool
	}{
		{name: "setup only", options: RouterOptions{AttributionSetupAvailable: true}, setup: true},
		{name: "setup and readiness", options: formalReadinessRouterOptions(), setup: true, readiness: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			env := setupFullTestEnvWithOptions(t, test.options)
			response := doFullRequest(env, http.MethodGet, "/api/v1/auth/me", nil)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
			}
			data := parseFullResponse(t, response)["data"].(map[string]any)
			capabilities := data["reporting_capabilities"].(map[string]any)
			if capabilities["setup_available"] != test.setup || capabilities["readiness_available"] != test.readiness {
				t.Fatalf("reporting_capabilities = %#v", capabilities)
			}
		})
	}
}

func TestAttributionStatusReportsNotEnrolledWithoutDeviceMetadata(t *testing.T) {
	env := setupFullTestEnvWithOptions(t, RouterOptions{
		AttributionSetupAvailable:     true,
		AttributionReadinessAvailable: true,
		AttributionProtocol: attributionledger.ProtocolContract{
			LedgerEpoch:       attributionledger.LedgerEpochFormalV2,
			V1WritePolicy:     attributionledger.V1WritePolicyUpgradeNeeded,
			MinimumCLIVersion: "0.2.0-preview.5",
		},
	})
	response := doFullRequest(env, http.MethodGet, "/api/v1/attribution/status", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
	}
	data := parseFullResponse(t, response)["data"].(map[string]any)
	if data["state"] != "not_enrolled" || data["retryable"] != false {
		t.Fatalf("status data = %#v", data)
	}
	for _, forbidden := range []string{"installation_count", "installations", "installation_id", "last_seen_at"} {
		if _, ok := data[forbidden]; ok {
			t.Fatalf("status exposes %s: %#v", forbidden, data)
		}
	}
}

func TestAttributionStatusAggregatesInstallationStateByUser(t *testing.T) {
	env := setupFullTestEnvWithOptions(t, formalReadinessRouterOptions())
	assertState := func(want string) {
		t.Helper()
		response := doFullRequest(env, http.MethodGet, "/api/v1/attribution/status", nil)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
		}
		if got := parseFullResponse(t, response)["data"].(map[string]any)["state"]; got != want {
			t.Fatalf("state = %v, want %s", got, want)
		}
	}

	firstID := "11111111-1111-4111-8111-111111111111"
	if response := doFullRequest(env, http.MethodPost, "/api/v1/attribution/installations", map[string]any{"installation_id": firstID}); response.Code != http.StatusOK {
		t.Fatalf("enroll first installation: status=%d body=%s", response.Code, response.Body.String())
	}
	assertState("disabled")

	first := env.client.ReportingInstallation.Query().Where(reportinginstallation.InstallationIDEQ(firstID)).OnlyX(context.Background())
	first.Update().SetStatus(reportinginstallation.StatusRevoked).ExecX(context.Background())
	assertState("revoked")

	secondID := "22222222-2222-4222-8222-222222222222"
	if response := doFullRequest(env, http.MethodPost, "/api/v1/attribution/installations", map[string]any{"installation_id": secondID}); response.Code != http.StatusOK {
		t.Fatalf("enroll second installation: status=%d body=%s", response.Code, response.Body.String())
	}
	assertState("disabled")
	if response := doFullRequest(env, http.MethodPut, "/api/v1/attribution/installations/"+secondID, map[string]any{"reporting_enabled": true}); response.Code != http.StatusOK {
		t.Fatalf("enable second installation: status=%d body=%s", response.Code, response.Body.String())
	}
	assertState("waiting_for_data")
}

func TestAttributionStatusUsesFormalActivityReadinessAndLatestAcceptance(t *testing.T) {
	env := setupFullTestEnvWithOptions(t, RouterOptions{
		AttributionSetupAvailable:     true,
		AttributionReadinessAvailable: true,
		AttributionProtocol: attributionledger.ProtocolContract{
			LedgerEpoch:       attributionledger.LedgerEpochFormalV2,
			V1WritePolicy:     attributionledger.V1WritePolicyUpgradeNeeded,
			MinimumCLIVersion: "0.2.0-preview.5",
		},
	})
	installationID := "33333333-3333-4333-8333-333333333333"
	if response := doFullRequest(env, http.MethodPost, "/api/v1/attribution/installations", map[string]any{"installation_id": installationID}); response.Code != http.StatusOK {
		t.Fatalf("enroll installation: status=%d body=%s", response.Code, response.Body.String())
	}
	if response := doFullRequest(env, http.MethodPut, "/api/v1/attribution/installations/"+installationID, map[string]any{"reporting_enabled": true}); response.Code != http.StatusOK {
		t.Fatalf("enable installation: status=%d body=%s", response.Code, response.Body.String())
	}

	ctx := context.Background()
	actor := env.client.User.Query().Where(entuser.UsernameEQ("fulladmin")).OnlyX(ctx)
	provider := env.client.RelayProvider.Create().SetName("readiness-provider").SetDisplayName("Readiness Provider").SetBaseURL("https://relay.example.com").SetAdminAPIKey("encrypted-test-key").SetDefaultModel("example-model").SetEnabled(true).SaveX(ctx)
	repo := env.client.RepoConfig.Create().SetName("readiness").SetFullName("example/readiness").SetCloneURL("https://example.com/example/readiness.git").SaveX(ctx)
	firstAccepted := time.Date(2026, 8, 1, 1, 2, 3, 0, time.UTC)
	latestAccepted := time.Date(2026, 8, 3, 4, 5, 6, 0, time.UTC)
	seedReportingReadinessPool(t, env, actor.ID, provider.ID, repo.ID, "formal-direct", "formal_v2", attributionusagepoolcommit.RelationKindDirect, firstAccepted)
	seedReportingReadinessPool(t, env, actor.ID, provider.ID, repo.ID, "formal-shared", "formal_v2", attributionusagepoolcommit.RelationKindShared, latestAccepted)
	seedReportingReadinessPool(t, env, actor.ID, provider.ID, repo.ID, "newer-shadow", "shadow_v2", attributionusagepoolcommit.RelationKindDirect, latestAccepted.Add(time.Hour))
	revokedID := "44444444-4444-4444-8444-444444444444"
	if enroll := doFullRequest(env, http.MethodPost, "/api/v1/attribution/installations", map[string]any{"installation_id": revokedID}); enroll.Code != http.StatusOK {
		t.Fatalf("enroll revoked peer: status=%d body=%s", enroll.Code, enroll.Body.String())
	}
	env.client.ReportingInstallation.Query().Where(reportinginstallation.InstallationIDEQ(revokedID)).OnlyX(ctx).Update().SetStatus(reportinginstallation.StatusRevoked).ExecX(ctx)

	response := doFullRequest(env, http.MethodGet, "/api/v1/attribution/status", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	data := parseFullResponse(t, response)["data"].(map[string]any)
	if data["state"] != "active" || data["latest_accepted_at"] != latestAccepted.Format(time.RFC3339) {
		t.Fatalf("status data = %#v", data)
	}
}

func TestAttributionStatusRejectsNonFormalNonCountingAndLegacyEvidence(t *testing.T) {
	env := setupFullTestEnvWithOptions(t, formalReadinessRouterOptions())
	installationID := "55555555-5555-4555-8555-555555555555"
	if response := doFullRequest(env, http.MethodPost, "/api/v1/attribution/installations", map[string]any{"installation_id": installationID}); response.Code != http.StatusOK {
		t.Fatalf("enroll installation: status=%d body=%s", response.Code, response.Body.String())
	}
	if response := doFullRequest(env, http.MethodPut, "/api/v1/attribution/installations/"+installationID, map[string]any{"reporting_enabled": true}); response.Code != http.StatusOK {
		t.Fatalf("enable installation: status=%d body=%s", response.Code, response.Body.String())
	}
	assertWaiting := func() {
		t.Helper()
		response := doFullRequest(env, http.MethodGet, "/api/v1/attribution/status", nil)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
		}
		data := parseFullResponse(t, response)["data"].(map[string]any)
		if data["state"] != "waiting_for_data" {
			t.Fatalf("status data = %#v, want waiting_for_data", data)
		}
		if _, ok := data["latest_accepted_at"]; ok {
			t.Fatalf("waiting status exposes latest_accepted_at: %#v", data)
		}
	}
	assertWaiting()

	ctx := context.Background()
	actor := env.client.User.Query().Where(entuser.UsernameEQ("fulladmin")).OnlyX(ctx)
	installation := env.client.ReportingInstallation.Query().Where(reportinginstallation.InstallationIDEQ(installationID)).OnlyX(ctx)
	provider := env.client.RelayProvider.Create().SetName("negative-readiness-provider").SetDisplayName("Negative Readiness Provider").SetBaseURL("https://negative-relay.example.com").SetAdminAPIKey("encrypted-test-key").SetDefaultModel("example-model").SetEnabled(true).SaveX(ctx)
	repo := env.client.RepoConfig.Create().SetName("negative-readiness").SetFullName("example/negative-readiness").SetCloneURL("https://example.com/example/negative-readiness.git").SaveX(ctx)
	observed := time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)
	env.client.AttributionUsageBucket.Create().
		SetBucketID("legacy-v1-bucket").
		SetReportingInstallationID(installation.ID).
		SetUserID(actor.ID).
		SetTool("codex").
		SetSessionSlices([]map[string]any{}).
		SetObservedStartAt(observed).
		SetObservedEndAt(observed.Add(time.Minute)).
		SetSourceDigest("legacy-source").
		SetImmutableDigest("legacy-immutable").
		SetExtractorVersion("test").
		SetTokenQuality("measured").
		SaveX(ctx)
	assertWaiting()

	env.client.AttributionUsagePool.Create().
		SetCanonicalPoolKey("formal-uncommitted").
		SetLedgerEpoch("formal_v2").
		SetRelayProviderID(provider.ID).
		SetUserID(actor.ID).
		SetRequestedModel("model-test").
		SetBucketStartUtc(observed).
		SetTotalTokens(100).
		SaveX(ctx)
	assertWaiting()
	seedReportingReadinessPool(t, env, actor.ID, provider.ID, repo.ID, "shadow-direct", "shadow_v2", attributionusagepoolcommit.RelationKindDirect, observed.Add(time.Hour))
	assertWaiting()
	seedReportingReadinessPool(t, env, actor.ID, provider.ID, repo.ID, "formal-inherited", "formal_v2", attributionusagepoolcommit.RelationKindInheritedNonCounting, observed.Add(2*time.Hour))
	assertWaiting()
}

func TestAttributionStatusReadinessFailureIsLocalAndRetryable(t *testing.T) {
	env := setupFullTestEnvWithOptions(t, formalReadinessRouterOptions())
	installationID := "66666666-6666-4666-8666-666666666666"
	if response := doFullRequest(env, http.MethodPost, "/api/v1/attribution/installations", map[string]any{"installation_id": installationID}); response.Code != http.StatusOK {
		t.Fatalf("enroll installation: status=%d body=%s", response.Code, response.Body.String())
	}
	if response := doFullRequest(env, http.MethodPut, "/api/v1/attribution/installations/"+installationID, map[string]any{"reporting_enabled": true}); response.Code != http.StatusOK {
		t.Fatalf("enable installation: status=%d body=%s", response.Code, response.Body.String())
	}
	if err := env.sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	response := doFullRequest(env, http.MethodGet, "/api/v1/attribution/status", nil)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	details, ok := parseFullResponse(t, response)["details"].(map[string]any)
	if !ok {
		t.Fatalf("missing retryable details: body=%s", response.Body.String())
	}
	if details["retryable"] != true {
		t.Fatalf("details = %#v", details)
	}
	if _, ok := details["state"]; ok {
		t.Fatalf("readiness failure was converted to a product state: %#v", details)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
}

func TestAttributionStatusInstallationFailureIsLocalAndRetryable(t *testing.T) {
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/attribution/status", nil)
	c.Set(platformauth.ContextKeyUser, &platformauth.UserContext{UserID: 1})
	protocol := attributionledger.DefaultProtocolContract()
	handler := NewAttributionHandler(attributionledger.NewInstallationService(nil, protocol), nil, nil, nil, protocol, nil)
	handler.Status(c)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	details, ok := parseFullResponse(t, response)["details"].(map[string]any)
	if !ok {
		t.Fatalf("missing retryable details: body=%s", response.Body.String())
	}
	if details["retryable"] != true {
		t.Fatalf("details = %#v", details)
	}
	if _, ok := details["state"]; ok {
		t.Fatalf("installation failure was converted to a product state: %#v", details)
	}
}

func TestAttributionStatusIsUnavailableWhenReadinessCapabilityIsOff(t *testing.T) {
	for _, options := range []RouterOptions{{}, {AttributionSetupAvailable: true}} {
		env := setupFullTestEnvWithOptions(t, options)
		response := doFullRequest(env, http.MethodGet, "/api/v1/attribution/status", nil)
		if response.Code != http.StatusNotFound {
			t.Fatalf("options=%+v status=%d body=%s", options, response.Code, response.Body.String())
		}
	}
}

func formalReadinessRouterOptions() RouterOptions {
	return RouterOptions{
		AttributionSetupAvailable:     true,
		AttributionReadinessAvailable: true,
		AttributionProtocol: attributionledger.ProtocolContract{
			LedgerEpoch:       attributionledger.LedgerEpochFormalV2,
			V1WritePolicy:     attributionledger.V1WritePolicyUpgradeNeeded,
			MinimumCLIVersion: "0.2.0-preview.5",
		},
	}
}

func seedReportingReadinessPool(t *testing.T, env *fullTestEnv, userID, providerID, repoID int, key, epoch string, relation attributionusagepoolcommit.RelationKind, acceptedAt time.Time) {
	t.Helper()
	ctx := context.Background()
	pool := env.client.AttributionUsagePool.Create().
		SetCanonicalPoolKey(key).
		SetLedgerEpoch(epoch).
		SetRelayProviderID(providerID).
		SetUserID(userID).
		SetRequestedModel("model-test").
		SetBucketStartUtc(acceptedAt.Add(-time.Minute)).
		SetTotalTokens(100).
		SetCreatedAt(acceptedAt).
		SaveX(ctx)
	env.client.AttributionUsagePoolCommit.Create().
		SetPoolID(pool.ID).
		SetRepoConfigID(repoID).
		SetCommitSha(key).
		SetRelationKind(relation).
		SaveX(ctx)
}
