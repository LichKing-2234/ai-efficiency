package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/owneraccountoperation"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/owneraccount"
)

// TestOwnerAccountOwnershipMatchingAndInactiveOwner covers the email-derived
// ownership rules: normalization, the email OR email-shaped-name match, every
// exclusion, list hiding, and 404 vs 403.
func TestOwnerAccountOwnershipMatchingAndInactiveOwner(t *testing.T) {
	env := setupOwnerAccountEnv(t, nil)

	env.upstream.setAccounts(
		// Matched through credentials.email.
		ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"),
		// Matched through an email-shaped name that is only normalized by trim+lower.
		ownerAccountWire(12, "Alice@Example.com ", "different@example.org", "openai", "oauth"),
		// Matched through a credentials.email that needs normalization.
		ownerAccountWire(13, "alice", " ALICE@Example.com ", "openai", "oauth"),
		// Excluded: shadow account.
		ownerAccountShadow(ownerAccountWire(14, "Alice", "alice@example.com", "openai", "oauth"), 99),
		// Excluded: non-OpenAI platform.
		ownerAccountWithPlatform(ownerAccountWire(15, "Alice", "alice@example.com", "anthropic", "oauth"), "anthropic"),
		// Excluded: non-OAuth account type.
		ownerAccountWithType(ownerAccountWire(16, "Alice", "alice@example.com", "openai", "api_key"), "api_key"),
		// Excluded: the name is not email-shaped, so it cannot participate.
		ownerAccountWire(17, "alice", "carol@example.org", "openai", "oauth"),
		// Another owner's account.
		ownerAccountWire(18, "Bob@Example.org", "bob@example.org", "openai", "oauth"),
	)

	scope := env.request(http.MethodGet, "/api/v1/user/owner-accounts/scope", nil, env.aliceAuth)
	if scope.Code != http.StatusOK {
		t.Fatalf("scope status = %d, body: %s", scope.Code, scope.Body.String())
	}
	scopeData := env.data(t, scope)
	if got := scopeData["account_count"]; got != float64(3) {
		t.Fatalf("alice account_count = %v, want 3", got)
	}
	if got := scopeData["mutations_enabled"]; got != true {
		t.Fatalf("mutations_enabled = %v, want true", got)
	}

	list := env.request(http.MethodGet, "/api/v1/user/owner-accounts", nil, env.aliceAuth)
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, body: %s", list.Code, list.Body.String())
	}
	listData := env.data(t, list)
	if got := listData["total"]; got != float64(3) {
		t.Fatalf("alice total = %v, want 3", got)
	}
	if got := ownerAccountIDs(t, listData); !equalInt64s(got, []int64{11, 12, 13}) {
		t.Fatalf("alice account ids = %v, want [11 12 13]", got)
	}

	bobList := env.request(http.MethodGet, "/api/v1/user/owner-accounts", nil, env.bobAuth)
	if got := env.data(t, bobList)["total"]; got != float64(1) {
		t.Fatalf("bob total = %v, want 1", got)
	}
	if got := ownerAccountIDs(t, env.data(t, bobList)); !equalInt64s(got, []int64{18}) {
		t.Fatalf("bob account ids = %v, want [18]", got)
	}

	// Another owner's account is indistinguishable from a missing one.
	notOwned := env.request(http.MethodPost, "/api/v1/user/owner-accounts/18/reset-credit", map[string]any{"confirm": true}, env.aliceAuth)
	if notOwned.Code != http.StatusNotFound {
		t.Fatalf("not-owned reset status = %d, want 404, body: %s", notOwned.Code, notOwned.Body.String())
	}
	if got := env.errorCode(t, notOwned); got != "account-not-found" {
		t.Fatalf("not-owned error_code = %q, want account-not-found", got)
	}

	// A locally disabled owner sees nothing and receives 403.
	if err := env.client.User.UpdateOneID(env.alice.ID).SetRelayDisabledAt(time.Now()).Exec(context.Background()); err != nil {
		t.Fatalf("disable alice: %v", err)
	}
	inactive := env.request(http.MethodGet, "/api/v1/user/owner-accounts", nil, env.aliceAuth)
	if inactive.Code != http.StatusForbidden {
		t.Fatalf("disabled list status = %d, want 403, body: %s", inactive.Code, inactive.Body.String())
	}
	if got := env.errorCode(t, inactive); got != "owner-inactive" {
		t.Fatalf("disabled error_code = %q, want owner-inactive", got)
	}

	// The auth middleware already rejects a token whose local user no longer
	// exists; the service still fails closed if it is reached directly.
	missingToken := ownerAccountTokenForMissingUser(t, env)
	missing := env.request(http.MethodGet, "/api/v1/user/owner-accounts/scope", nil, missingToken)
	if missing.Code != http.StatusUnauthorized {
		t.Fatalf("missing user scope status = %d, want 401 from the auth middleware, body: %s", missing.Code, missing.Body.String())
	}
	if _, err := env.service.Scope(context.Background(), 999999); err == nil {
		t.Fatal("service scope accepted a missing local user")
	} else {
		var classified *owneraccount.Error
		if !errors.As(err, &classified) || classified.Code != "owner-inactive" {
			t.Fatalf("service scope error = %v, want owner-inactive", err)
		}
	}
}

func TestOwnerAccountListShapePaginationAndMutationsFlag(t *testing.T) {
	env := setupOwnerAccountEnv(t, nil)

	accounts := make([]map[string]any, 0, 25)
	for index := 0; index < 25; index++ {
		account := ownerAccountWire(int64(100+index), "Alice", "alice@example.com", "openai", "oauth")
		if index == 0 {
			account = ownerAccountCreditsBalance(account, "12.5", false)
		}
		accounts = append(accounts, account)
	}
	env.upstream.setAccounts(accounts...)

	first := env.request(http.MethodGet, "/api/v1/user/owner-accounts", nil, env.aliceAuth)
	if first.Code != http.StatusOK {
		t.Fatalf("list status = %d, body: %s", first.Code, first.Body.String())
	}
	firstData := env.data(t, first)
	if got := firstData["total"]; got != float64(25) {
		t.Fatalf("total = %v, want 25", got)
	}
	if got := firstData["page"]; got != float64(1) {
		t.Fatalf("page = %v, want 1", got)
	}
	if got := firstData["page_size"]; got != float64(20) {
		t.Fatalf("page_size = %v, want 20", got)
	}
	if items := firstData["items"].([]any); len(items) != 20 {
		t.Fatalf("page 1 items = %d, want 20", len(items))
	}
	if got := firstData["mutations_enabled"]; got != true {
		t.Fatalf("mutations_enabled = %v, want true", got)
	}

	shape := firstData["items"].([]any)[0].(map[string]any)
	if got := shape["id"]; got != float64(100) {
		t.Fatalf("first account id = %v, want 100", got)
	}
	if got := shape["name"]; got != "Alice" {
		t.Fatalf("name = %v", got)
	}
	if got := shape["email"]; got != "alice@example.com" {
		t.Fatalf("email = %v, want the account email", got)
	}
	if got := shape["platform"]; got != "openai" {
		t.Fatalf("platform = %v", got)
	}
	if got := shape["type"]; got != "oauth" {
		t.Fatalf("type = %v", got)
	}
	if got := shape["status"]; got != "active" {
		t.Fatalf("status = %v", got)
	}
	if got := stringSlice(shape["group_names"]); !equalStrings(got, []string{"Group Alpha"}) {
		t.Fatalf("group_names = %v, want [Group Alpha]", got)
	}
	quota, ok := shape["quota"].(map[string]any)
	if !ok {
		t.Fatalf("quota missing: %#v", shape["quota"])
	}
	if got := quota["reset_credits_remaining"]; got != float64(1) {
		t.Fatalf("reset_credits_remaining = %v, want 1", got)
	}
	if got := quota["reset_credit_expiries"].([]any); len(got) != 1 {
		t.Fatalf("reset_credit_expiries = %v, want 1 entry", got)
	}
	if got := quota["codex_credits_balance"]; got != float64(12.5) {
		t.Fatalf("codex_credits_balance = %v, want 12.5", got)
	}
	if got := quota["codex_credits_unlimited"]; got != false {
		t.Fatalf("codex_credits_unlimited = %v, want false", got)
	}
	if got := quota["auto_reset_credit"]; got != true {
		t.Fatalf("auto_reset_credit = %v, want true", got)
	}
	if got := quota["observed_at"]; got == nil || got == "" {
		t.Fatalf("quota observed_at missing: %#v", quota)
	}
	referral, ok := shape["referral"].(map[string]any)
	if !ok {
		t.Fatalf("referral missing: %#v", shape["referral"])
	}
	if got := referral["eligible"]; got != true {
		t.Fatalf("referral eligible = %v, want true", got)
	}
	if got := referral["program_id"]; got != "program-1" {
		t.Fatalf("referral program_id = %v, want program-1", got)
	}
	if got := referral["remaining_invites"]; got != float64(3) {
		t.Fatalf("remaining_invites = %v, want 3", got)
	}
	if got := referral["consent_required"]; got != true {
		t.Fatalf("consent_required = %v, want true", got)
	}
	health, ok := shape["health"].(map[string]any)
	if !ok {
		t.Fatalf("health missing: %#v", shape["health"])
	}
	if got := health["schedulable"]; got != true {
		t.Fatalf("health.schedulable = %v, want true", got)
	}
	if got := health["rate_limit_reset_at"]; got == nil {
		t.Fatalf("health.rate_limit_reset_at missing")
	}
	if got, ok := shape["blocked_actions"].([]any); !ok || len(got) != 0 {
		t.Fatalf("blocked_actions = %#v, want empty array", shape["blocked_actions"])
	}

	// The owner-safe surface never leaks credentials, notes, proxies, fingerprints
	// or parent identity.
	for _, secret := range []string{"must-not-surface", "access_token", "refresh_token", "fingerprint_seed"} {
		if strings.Contains(first.Body.String(), secret) {
			t.Fatalf("list response leaked %q: %s", secret, first.Body.String())
		}
	}

	second := env.request(http.MethodGet, "/api/v1/user/owner-accounts?page=2", nil, env.aliceAuth)
	secondData := env.data(t, second)
	if items := secondData["items"].([]any); len(items) != 5 {
		t.Fatalf("page 2 items = %d, want 5", len(items))
	}
	if got := ownerAccountIDs(t, secondData); !equalInt64s(got, []int64{120, 121, 122, 123, 124}) {
		t.Fatalf("page 2 ids = %v", got)
	}

	large := env.request(http.MethodGet, "/api/v1/user/owner-accounts?page_size=50", nil, env.aliceAuth)
	largeData := env.data(t, large)
	if items := largeData["items"].([]any); len(items) != 25 {
		t.Fatalf("page_size 50 items = %d, want 25", len(items))
	}
	if got := largeData["page_size"]; got != float64(50) {
		t.Fatalf("page_size = %v, want 50", got)
	}

	beyond := env.request(http.MethodGet, "/api/v1/user/owner-accounts?page=9&page_size=20", nil, env.aliceAuth)
	beyondData := env.data(t, beyond)
	if items := beyondData["items"].([]any); len(items) != 0 {
		t.Fatalf("beyond-end items = %d, want 0", len(items))
	}
	if got := beyondData["total"]; got != float64(25) {
		t.Fatalf("beyond-end total = %v, want exact total 25", got)
	}

	for _, query := range []string{"page_size=25", "page_size=0", "page=abc", "page_size=abc"} {
		w := env.request(http.MethodGet, "/api/v1/user/owner-accounts?"+query, nil, env.aliceAuth)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want 400, body: %s", query, w.Code, w.Body.String())
		}
		if got := env.errorCode(t, w); got != "invalid-pagination" {
			t.Fatalf("%s error_code = %q, want invalid-pagination", query, got)
		}
	}
}

func TestOwnerAccountMutationsFlagOff(t *testing.T) {
	env := setupOwnerAccountEnv(t, func(options *owneraccount.Options) {
		options.MutationsEnabled = false
	})
	env.upstream.setAccounts(ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"))

	scope := env.data(t, env.request(http.MethodGet, "/api/v1/user/owner-accounts/scope", nil, env.aliceAuth))
	if got := scope["mutations_enabled"]; got != false {
		t.Fatalf("scope mutations_enabled = %v, want false", got)
	}
	list := env.data(t, env.request(http.MethodGet, "/api/v1/user/owner-accounts", nil, env.aliceAuth))
	if got := list["mutations_enabled"]; got != false {
		t.Fatalf("list mutations_enabled = %v, want false", got)
	}
	reset := env.request(http.MethodPost, "/api/v1/user/owner-accounts/11/reset-credit", map[string]any{"confirm": true}, env.aliceAuth)
	if reset.Code != http.StatusForbidden {
		t.Fatalf("reset status = %d, want 403, body: %s", reset.Code, reset.Body.String())
	}
	if got := env.errorCode(t, reset); got != "actions-disabled" {
		t.Fatalf("reset error_code = %q, want actions-disabled", got)
	}
	invite := env.request(http.MethodPost, "/api/v1/user/owner-accounts/11/invitations", map[string]any{
		"email": "bob@example.org", "program_id": "program-1", "consent": true,
	}, env.aliceAuth)
	if invite.Code != http.StatusForbidden {
		t.Fatalf("invite status = %d, want 403, body: %s", invite.Code, invite.Body.String())
	}
	if calls, _, _, resets, invites := env.upstream.snapshot(); resets != 0 || invites != 0 {
		t.Fatalf("mutations disabled but upstream calls happened: %v", calls)
	}
}

// TestOwnerAccountUsageBatchChunksAndReportsPartialErrors covers chunked batch
// reads, per-account partial failures, and the account-not-found shape for ids the
// actor does not own.
func TestOwnerAccountUsageBatchChunksAndReportsPartialErrors(t *testing.T) {
	env := setupOwnerAccountEnv(t, nil)

	accountIDs := make([]int64, 0, 25)
	accounts := make([]map[string]any, 0, 25)
	for index := 0; index < 25; index++ {
		accountID := int64(100 + index)
		accountIDs = append(accountIDs, accountID)
		accounts = append(accounts, ownerAccountWire(accountID, "Alice", "alice@example.com", "openai", "oauth"))
	}
	accounts = append(accounts, ownerAccountWire(900, "Bob", "bob@example.org", "openai", "oauth"))
	env.upstream.setAccounts(accounts...)
	env.upstream.setUsage(101, ownerAccountUsagePayload(21))
	env.upstream.setUsageError(102, "upstream unauthorized")
	env.upstream.setUsageError(105, "upstream forbidden")

	// Page load reads the list only; it must never force a usage probe or refresh
	// quota/referral.
	list := env.request(http.MethodGet, "/api/v1/user/owner-accounts?page_size=50", nil, env.aliceAuth)
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, body: %s", list.Code, list.Body.String())
	}
	calls, _, forces, _, _ := env.upstream.snapshot()
	for _, call := range calls {
		if strings.Contains(call, "usage/batch") || strings.Contains(call, "quota/refresh") || strings.Contains(call, "referrals/refresh") {
			t.Fatalf("page load triggered %q", call)
		}
	}
	if len(forces) != 0 {
		t.Fatalf("page load issued usage reads: %v", forces)
	}

	requested := append(append([]int64(nil), accountIDs...), 900, 9999)
	w := env.request(http.MethodPost, "/api/v1/user/owner-accounts/usage", map[string]any{
		"account_ids": requested,
	}, env.aliceAuth)
	if w.Code != http.StatusOK {
		t.Fatalf("usage status = %d, body: %s", w.Code, w.Body.String())
	}
	data := env.data(t, w)
	items := data["items"].([]any)
	errs := data["errors"].([]any)
	if len(items) != 23 {
		t.Fatalf("usage items = %d, want 23", len(items))
	}
	if len(errs) != 4 {
		t.Fatalf("usage errors = %d, want 4: %v", len(errs), errs)
	}
	errorCodes := map[int64]string{}
	for _, raw := range errs {
		entry := raw.(map[string]any)
		errorCodes[int64(entry["account_id"].(float64))] = entry["code"].(string)
	}
	for _, accountID := range []int64{102, 900, 9999} {
		if errorCodes[accountID] == "" {
			t.Fatalf("missing usage error for %d: %v", accountID, errs)
		}
	}
	if errorCodes[102] != "usage-unavailable" {
		t.Fatalf("account 102 code = %q, want usage-unavailable", errorCodes[102])
	}
	if errorCodes[900] != "account-not-found" || errorCodes[9999] != "account-not-found" {
		t.Fatalf("non-owned codes = %v, want account-not-found", errorCodes)
	}

	_, batches, forces, _, _ := env.upstream.snapshot()
	if len(batches) != 2 {
		t.Fatalf("upstream usage batches = %d, want 2", len(batches))
	}
	if len(batches[0]) != 20 || len(batches[1]) != 5 {
		t.Fatalf("batch sizes = %d/%d, want 20/5", len(batches[0]), len(batches[1]))
	}
	for index, force := range forces {
		if force {
			t.Fatalf("batch %d forced on page load", index)
		}
	}

	forced := env.request(http.MethodPost, "/api/v1/user/owner-accounts/usage", map[string]any{
		"account_ids": []int64{100, 101}, "force": true,
	}, env.aliceAuth)
	if forced.Code != http.StatusOK {
		t.Fatalf("forced usage status = %d, body: %s", forced.Code, forced.Body.String())
	}
	_, _, forces, _, _ = env.upstream.snapshot()
	if !forces[len(forces)-1] {
		t.Fatal("explicit force=true was not passed to the upstream usage read")
	}

	tooMany := make([]int64, 0, 101)
	for index := 0; index < 101; index++ {
		tooMany = append(tooMany, int64(1000+index))
	}
	over := env.request(http.MethodPost, "/api/v1/user/owner-accounts/usage", map[string]any{"account_ids": tooMany}, env.aliceAuth)
	if over.Code != http.StatusBadRequest {
		t.Fatalf("over-limit usage status = %d, want 400, body: %s", over.Code, over.Body.String())
	}
}

func TestOwnerAccountRefreshEndpointsAndCooldown(t *testing.T) {
	env := setupOwnerAccountEnv(t, nil)
	env.upstream.setAccounts(
		ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"),
		ownerAccountWire(900, "Bob", "bob@example.org", "openai", "oauth"),
	)
	env.upstream.setUsage(11, ownerAccountUsagePayload(21))

	quota := env.request(http.MethodPost, "/api/v1/user/owner-accounts/11/quota/refresh", nil, env.aliceAuth)
	if quota.Code != http.StatusOK {
		t.Fatalf("quota refresh status = %d, body: %s", quota.Code, quota.Body.String())
	}
	quotaData := env.data(t, quota)
	if got := quotaData["account_id"]; got != float64(11) {
		t.Fatalf("quota account_id = %v", got)
	}
	quotaView := quotaData["quota"].(map[string]any)
	if got := quotaView["reset_credits_remaining"]; got != float64(1) {
		t.Fatalf("quota reset_credits_remaining = %v, want 1", got)
	}
	if got := quotaView["codex_credits_balance"]; got != float64(9.5) {
		t.Fatalf("quota codex_credits_balance = %v, want 9.5", got)
	}
	if got := quotaView["observed_at"]; got == nil {
		t.Fatalf("quota observed_at missing")
	}

	// All three refresh endpoints share one per-account cooldown, derived from the
	// local refresh row.
	for _, path := range []string{
		"/api/v1/user/owner-accounts/11/quota/refresh",
		"/api/v1/user/owner-accounts/11/usage/refresh",
		"/api/v1/user/owner-accounts/11/referral/refresh",
	} {
		w := env.request(http.MethodPost, path, nil, env.aliceAuth)
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("%s status = %d, want 429, body: %s", path, w.Code, w.Body.String())
		}
		if got := env.errorCode(t, w); got != "refresh-cooldown" {
			t.Fatalf("%s error_code = %q, want refresh-cooldown", path, got)
		}
		details := env.errorDetails(t, w)
		if retry, ok := details["retry_after_seconds"].(float64); !ok || retry < 1 {
			t.Fatalf("%s retry_after_seconds = %v", path, details["retry_after_seconds"])
		}
		if details["cooldown_until"] == nil {
			t.Fatalf("%s cooldown_until missing: %v", path, details)
		}
	}

	list := env.data(t, env.request(http.MethodGet, "/api/v1/user/owner-accounts", nil, env.aliceAuth))
	first := list["items"].([]any)[0].(map[string]any)
	if first["refresh_cooldown_until"] == nil {
		t.Fatalf("list did not expose refresh_cooldown_until: %#v", first)
	}

	// After the cooldown window the explicit query is allowed again.
	env.backdateRefreshes(t, 2*time.Minute)
	usage := env.request(http.MethodPost, "/api/v1/user/owner-accounts/11/usage/refresh", nil, env.aliceAuth)
	if usage.Code != http.StatusOK {
		t.Fatalf("usage refresh status = %d, body: %s", usage.Code, usage.Body.String())
	}
	usageData := env.data(t, usage)
	fiveHour := usageData["usage"].(map[string]any)["five_hour"].(map[string]any)
	if got := fiveHour["utilization_percent"]; got != float64(21) {
		t.Fatalf("usage five_hour utilization = %v, want 21", got)
	}
	_, _, forces, _, _ := env.upstream.snapshot()
	if !forces[len(forces)-1] {
		t.Fatal("explicit usage refresh did not force the upstream probe")
	}

	// A referral refresh returns the fresh eligibility shape.
	env.backdateRefreshes(t, 2*time.Minute)
	referral := env.request(http.MethodPost, "/api/v1/user/owner-accounts/11/referral/refresh", nil, env.aliceAuth)
	if referral.Code != http.StatusOK {
		t.Fatalf("referral refresh status = %d, body: %s", referral.Code, referral.Body.String())
	}
	referralView := env.data(t, referral)["referral"].(map[string]any)
	if got := referralView["eligible"]; got != true {
		t.Fatalf("referral eligible = %v", got)
	}
	if got := referralView["program_id"]; got != "program-1" {
		t.Fatalf("referral program_id = %v", got)
	}

	// Upstream failures map to stable provider codes and never forward text.
	env.upstream.setQuotaStatus(11, http.StatusInternalServerError)
	env.backdateRefreshes(t, 2*time.Minute)
	failed := env.request(http.MethodPost, "/api/v1/user/owner-accounts/11/quota/refresh", nil, env.aliceAuth)
	if failed.Code != http.StatusBadGateway {
		t.Fatalf("failed quota refresh status = %d, want 502, body: %s", failed.Code, failed.Body.String())
	}
	if got := env.errorCode(t, failed); got != "upstream-unavailable" {
		t.Fatalf("failed quota error_code = %q, want upstream-unavailable", got)
	}
	if strings.Contains(failed.Body.String(), "upstream failure") {
		t.Fatalf("upstream error text leaked: %s", failed.Body.String())
	}
	env.backdateRefreshes(t, 2*time.Minute)

	env.upstream.setQuotaStatus(11, http.StatusNotFound)
	unsupported := env.request(http.MethodPost, "/api/v1/user/owner-accounts/11/quota/refresh", nil, env.aliceAuth)
	if unsupported.Code != http.StatusNotImplemented {
		t.Fatalf("unsupported quota status = %d, want 501, body: %s", unsupported.Code, unsupported.Body.String())
	}
	if got := env.errorCode(t, unsupported); got != "capability-unsupported" {
		t.Fatalf("unsupported error_code = %q, want capability-unsupported", got)
	}
	env.backdateRefreshes(t, 2*time.Minute)

	env.upstream.setQuotaStatus(11, http.StatusLocked)
	locked := env.request(http.MethodPost, "/api/v1/user/owner-accounts/11/quota/refresh", nil, env.aliceAuth)
	if locked.Code != http.StatusLocked {
		t.Fatalf("compliance status = %d, want 423, body: %s", locked.Code, locked.Body.String())
	}
	if got := env.errorCode(t, locked); got != "compliance-ack-required" {
		t.Fatalf("compliance error_code = %q, want compliance-ack-required", got)
	}

	notOwned := env.request(http.MethodPost, "/api/v1/user/owner-accounts/900/quota/refresh", nil, env.aliceAuth)
	if notOwned.Code != http.StatusNotFound {
		t.Fatalf("non-owned refresh status = %d, want 404, body: %s", notOwned.Code, notOwned.Body.String())
	}
}

func TestOwnerAccountAdminOperationsReadOnlyAndHistory(t *testing.T) {
	env := setupOwnerAccountEnv(t, nil)
	env.upstream.setAccounts(
		ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"),
		ownerAccountWire(900, "Bob", "bob@example.org", "openai", "oauth"),
	)
	now := time.Now().UTC()
	unknown := env.seedOperation(t, env.alice.ID, ownerAccountSeed{
		AccountID: 11, Action: owneraccountoperation.ActionResetCredit,
		Status: owneraccountoperation.StatusUnknown, CreatedAt: now.Add(-2 * time.Hour),
		Upstream: "upstream-outcome-unknown",
	})
	invite := env.seedOperation(t, env.alice.ID, ownerAccountSeed{
		AccountID: 11, Action: owneraccountoperation.ActionSendInvitation,
		Status: owneraccountoperation.StatusSucceeded, CreatedAt: now.Add(-time.Hour),
		InviteEmail: "bob@example.org", Result: map[string]any{"sent": true},
	})
	bobOperation := env.seedOperation(t, env.bob.ID, ownerAccountSeed{
		AccountID: 900, Action: owneraccountoperation.ActionRefreshQuota,
		Status: owneraccountoperation.StatusSucceeded, CreatedAt: now.Add(-30 * time.Minute),
	})

	adminList := env.request(http.MethodGet, "/api/v1/admin/owner-operations", nil, env.adminAuth)
	if adminList.Code != http.StatusOK {
		t.Fatalf("admin list status = %d, body: %s", adminList.Code, adminList.Body.String())
	}
	adminData := env.data(t, adminList)
	if got := adminData["total"]; got != float64(3) {
		t.Fatalf("admin total = %v, want 3", got)
	}
	items := adminData["items"].([]any)
	if got := items[0].(map[string]any)["account_id"]; got != float64(900) {
		t.Fatalf("admin list is not newest-first: %#v", items[0])
	}

	unknownFilter := env.data(t, env.request(http.MethodGet, "/api/v1/admin/owner-operations?status=unknown", nil, env.adminAuth))
	if got := unknownFilter["total"]; got != float64(1) {
		t.Fatalf("unknown filter total = %v, want 1", got)
	}
	unknownItem := unknownFilter["items"].([]any)[0].(map[string]any)
	if got := unknownItem["id"]; got != float64(unknown.ID) {
		t.Fatalf("unknown filter id = %v, want %d", got, unknown.ID)
	}
	if got := unknownItem["error_code"]; got != "upstream-outcome-unknown" {
		t.Fatalf("unknown error_code = %v", got)
	}

	actionFilter := env.data(t, env.request(http.MethodGet, "/api/v1/admin/owner-operations?action=send_invitation", nil, env.adminAuth))
	if got := actionFilter["total"]; got != float64(1) {
		t.Fatalf("action filter total = %v, want 1", got)
	}
	inviteItem := actionFilter["items"].([]any)[0].(map[string]any)
	if got := inviteItem["id"]; got != float64(invite.ID) {
		t.Fatalf("action filter id = %v, want %d", got, invite.ID)
	}
	if got := inviteItem["invite_email"]; got != "bob@example.org" {
		t.Fatalf("admin invite_email = %v, want bob@example.org", got)
	}
	if got := inviteItem["result"].(map[string]any)["sent"]; got != true {
		t.Fatalf("admin result.sent = %v, want true", got)
	}

	accountFilter := env.data(t, env.request(http.MethodGet, "/api/v1/admin/owner-operations?account_id=900", nil, env.adminAuth))
	if got := accountFilter["total"]; got != float64(1) {
		t.Fatalf("account filter total = %v, want 1", got)
	}
	if got := accountFilter["items"].([]any)[0].(map[string]any)["id"]; got != float64(bobOperation.ID) {
		t.Fatalf("account filter id = %v, want %d", got, bobOperation.ID)
	}

	badStatus := env.request(http.MethodGet, "/api/v1/admin/owner-operations?status=bogus", nil, env.adminAuth)
	if badStatus.Code != http.StatusBadRequest {
		t.Fatalf("bad status filter = %d, want 400, body: %s", badStatus.Code, badStatus.Body.String())
	}
	if got := env.errorCode(t, badStatus); got != "invalid-filter" {
		t.Fatalf("bad status filter code = %q", got)
	}
	badPagination := env.request(http.MethodGet, "/api/v1/admin/owner-operations?page_size=7", nil, env.adminAuth)
	if badPagination.Code != http.StatusBadRequest {
		t.Fatalf("bad admin pagination = %d, want 400", badPagination.Code)
	}

	// Regular users cannot read the support surface, and there is no admin mutation.
	forbidden := env.request(http.MethodGet, "/api/v1/admin/owner-operations", nil, env.aliceAuth)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("regular user admin list = %d, want 403", forbidden.Code)
	}
	mutation := env.request(http.MethodPost, "/api/v1/admin/owner-operations", map[string]any{}, env.adminAuth)
	if mutation.Code != http.StatusNotFound {
		t.Fatalf("admin mutation route = %d, want 404", mutation.Code)
	}

	// Per-account history stays owner-scoped.
	history := env.request(http.MethodGet, "/api/v1/user/owner-accounts/11/operations", nil, env.aliceAuth)
	if history.Code != http.StatusOK {
		t.Fatalf("history status = %d, body: %s", history.Code, history.Body.String())
	}
	historyData := env.data(t, history)
	if got := historyData["total"]; got != float64(2) {
		t.Fatalf("history total = %v, want 2", got)
	}
	notOwnedHistory := env.request(http.MethodGet, "/api/v1/user/owner-accounts/900/operations", nil, env.aliceAuth)
	if notOwnedHistory.Code != http.StatusNotFound {
		t.Fatalf("non-owned history = %d, want 404", notOwnedHistory.Code)
	}
	notOwnedOperation := env.request(http.MethodGet, fmt.Sprintf("/api/v1/user/owner-accounts/operations/%d", bobOperation.ID), nil, env.aliceAuth)
	if notOwnedOperation.Code != http.StatusNotFound {
		t.Fatalf("non-actor operation poll = %d, want 404, body: %s", notOwnedOperation.Code, notOwnedOperation.Body.String())
	}
	if got := env.errorCode(t, notOwnedOperation); got != "operation-not-found" {
		t.Fatalf("non-actor operation code = %q", got)
	}
	poll := env.request(http.MethodGet, fmt.Sprintf("/api/v1/user/owner-accounts/operations/%d", unknown.ID), nil, env.aliceAuth)
	if poll.Code != http.StatusOK {
		t.Fatalf("actor operation poll = %d, body: %s", poll.Code, poll.Body.String())
	}
}

func ownerAccountIDs(t *testing.T, data map[string]any) []int64 {
	t.Helper()
	items, ok := data["items"].([]any)
	if !ok {
		t.Fatalf("items missing: %#v", data)
	}
	ids := make([]int64, 0, len(items))
	for _, raw := range items {
		entry, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("item is not an object: %#v", raw)
		}
		ids = append(ids, int64(entry["id"].(float64)))
	}
	return ids
}

func ownerAccountTokenForMissingUser(t *testing.T, env *ownerAccountTestEnv) string {
	t.Helper()
	pair, err := env.auth.GenerateTokenPairForUser(&auth.UserInfo{
		ID: 999999, Username: "ghost", Role: "user",
	})
	if err != nil {
		t.Fatalf("generate missing-user token: %v", err)
	}
	return pair.AccessToken
}

func stringSlice(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, entry := range raw {
		text, ok := entry.(string)
		if ok {
			out = append(out, text)
		}
	}
	return out
}

func equalInt64s(left, right []int64) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
