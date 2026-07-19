# Sub2API Team Summary Range Fallback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restore selected-window Team Usage summary totals on staging when the connected Sub2API batch usage endpoint omits `range_actual_cost` and `range_total_tokens`.

**Architecture:** Keep Team Usage Summary dependent only on `relay.TeamUsageSummaryProvider`. The first-party Sub2API adapter consumes complete batch range fields without extra requests, but for returned users with missing range fields it uses the existing bounded-concurrency trend method and aggregates trend points inside the adapter. A failed or incomplete compatibility fallback leaves the range fields incomplete so the existing Summary service returns `range_aggregation_unavailable` without turning the whole endpoint into an error.

**Tech Stack:** Go 1.24, `net/http`, `encoding/json`, Go tests, Docker Buildx, Helm, Kubernetes.

## Global Constraints

- Work only in `/Users/admin/ai-efficiency/.worktrees/feat-platform-loading-performance` on `feat/platform-loading-performance` for application code and documentation.
- Current code and `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md` are the implementation contract.
- Do not modify Sub2API source or introduce direct database coupling to Sub2API.
- Do not add a `TeamMemberTrendProvider` dependency to `backend/internal/teamusage` Summary code.
- Call trend only for batch-response users whose `range_actual_cost` or `range_total_tokens` is missing; do not add trend requests for complete batch responses.
- Preserve primary batch request errors as endpoint-level failures. Swallow only compatibility fallback errors and preserve incomplete range pointers.
- Treat an empty successful trend as zero cost and zero tokens. If any returned trend point omits `total_tokens`, keep that user's `range_total_tokens` incomplete.
- Tests and examples use only synthetic identities and credentials.
- Update each checkbox immediately after the action is actually complete.
- Deploy only `ai-efficiency-staging` in namespace `la3-ai-efficiency-prod`; do not upgrade, restart, or retag `ai-efficiency-prod`.

**Status:** Complete. Task 1 RED and Task 2 focused/full/race verification passed. Image `staging-35153d0430f3299a507b234b86da55a4ddad6736` is deployed as staging revision 22. Authenticated cold and warm Team Usage summary checks pass, and production remains unchanged.

---

### Task 1: Lock The Legacy Sub2API Response Behavior With Tests

**Files:**
- Modify: `backend/internal/relay/sub2api_test.go`
- Test: `backend/internal/relay/sub2api_test.go`

**Interfaces:**
- Consumes: `relay.TeamUsageSummaryProvider.GetBatchUserUsageStats(context.Context, []int64, relay.TeamUsageSummaryParams)`.
- Produces: regression coverage for selective trend fallback, range-parameter propagation, direct-field fast path, and fallback-error degradation.

- [x] **Step 1: Add a RED selective fallback test**

Add `TestSub2APIGetBatchUserUsageStatsBackfillsMissingRangeFromTrend`. Its batch handler returns user `1001` with complete range fields and user `1002` without them. Its trend handler records the request and returns two selected-window points:

```go
mux.HandleFunc("/api/v1/admin/dashboard/trend", func(w http.ResponseWriter, r *http.Request) {
    requestedTrend = append(requestedTrend, map[string]string{
        "user_id":     r.URL.Query().Get("user_id"),
        "start_date":  r.URL.Query().Get("start_date"),
        "end_date":    r.URL.Query().Get("end_date"),
        "granularity": r.URL.Query().Get("granularity"),
        "timezone":    r.URL.Query().Get("timezone"),
    })
    _ = json.NewEncoder(w).Encode(map[string]any{
        "success": true,
        "data": []map[string]any{
            {"date": "2026-07-01", "actual_cost": 1.25, "total_tokens": 100},
            {"date": "2026-07-02", "actual_cost": 2.50, "total_tokens": 200},
        },
    })
})
```

Call the provider with:

```go
params := relay.TeamUsageSummaryParams{
    StartDate: "2026-07-01", EndDate: "2026-07-07",
    Granularity: "day", Timezone: "Asia/Shanghai",
}
got, err := summary.GetBatchUserUsageStats(context.Background(), []int64{1001, 1002}, params)
```

Assert user `1001` keeps its direct batch totals, user `1002` receives `3.75` cost and `300` tokens, and the only trend query is exactly:

```go
[]map[string]string{{
    "user_id": "1002", "start_date": "2026-07-01", "end_date": "2026-07-07",
    "granularity": "day", "timezone": "Asia/Shanghai",
}}
```

- [x] **Step 2: Add fast-path, incomplete-token, empty-trend, and fallback-error assertions**

Extend `TestSub2APIGetBatchUserUsageStatsPostsUserIDs` with an atomic trend-request counter and a trend handler, then assert `trendRequests.Load() == 0` because the batch response is complete.

Add `TestSub2APIGetBatchUserUsageStatsKeepsIncompleteRangeWhenTrendFails`. Return a successful batch item without range fields, return HTTP 502 from `/api/v1/admin/dashboard/trend`, and assert:

```go
if err != nil {
    t.Fatalf("GetBatchUserUsageStats() fallback error = %v, want nil", err)
}
if trendRequests.Load() != 1 {
    t.Fatalf("trend requests = %d, want 1", trendRequests.Load())
}
if got[1001].RangeActualCost != nil || got[1001].RangeTotalTokens != nil {
    t.Fatalf("range fields = %#v/%#v, want incomplete", got[1001].RangeActualCost, got[1001].RangeTotalTokens)
}
if got[1001].TodayActualCost != 1 || got[1001].TotalActualCost != 10 {
    t.Fatalf("comparison totals changed: %#v", got[1001])
}
```

Add `TestSub2APIGetBatchUserUsageStatsRequiresCompleteTrendTokens` with two cases: a successful trend containing one point without `total_tokens` must fill cost but leave tokens nil, while an empty successful trend must fill both range values with zero.

- [x] **Step 3: Run the focused tests and record RED**

Run:

```bash
cd backend
go test ./internal/relay -run '^TestSub2APIGetBatchUserUsageStats' -count=1
```

Expected: the selective fallback test fails because range fields remain nil, the fallback-error test fails because no trend request occurs, and the existing complete-field test passes.

RED evidence (2026-07-18): `TestSub2APIGetBatchUserUsageStatsBackfillsMissingRangeFromTrend` observed nil range fields for user `1002`; `TestSub2APIGetBatchUserUsageStatsKeepsIncompleteRangeWhenTrendFails` observed zero trend requests; both incomplete-token and empty-trend cases observed nil range cost.

---

### Task 2: Backfill Missing Range Totals Inside The Adapter

**Files:**
- Modify: `backend/internal/relay/sub2api.go`
- Modify: `backend/internal/relay/sub2api_test.go`
- Test: `backend/internal/teamusage/service_test.go`

**Interfaces:**
- Consumes: `sub2apiRelay.GetUsageTrendForUsers(context.Context, []int64, TeamMemberTrendParams)` and `UsageTrendPoint{ActualCost float64; TotalTokens *int64}`.
- Produces: batch result entries whose range pointers are filled only when the trend response can authoritatively supply them.

- [x] **Step 1: Add the minimal aggregation helper**

Add this private helper near `GetBatchUserUsageStats`:

```go
func summarizeTeamUsageRange(points []UsageTrendPoint) (float64, int64, bool) {
    var actualCost float64
    var totalTokens int64
    tokensComplete := true
    for _, point := range points {
        actualCost += point.ActualCost
        if point.TotalTokens == nil {
            tokensComplete = false
            continue
        }
        totalTokens += *point.TotalTokens
    }
    return actualCost, totalTokens, tokensComplete
}
```

- [x] **Step 2: Fill missing users after the primary batch decode**

After building `out`, retain requested user order while collecting only returned incomplete entries, then run the existing bounded trend method:

```go
missingRangeUserIDs := make([]int64, 0)
for _, userID := range userIDs {
    item, ok := out[userID]
    if ok && (item.RangeActualCost == nil || item.RangeTotalTokens == nil) {
        missingRangeUserIDs = append(missingRangeUserIDs, userID)
    }
}
if len(missingRangeUserIDs) == 0 {
    return out, nil
}

pointsByUser, fallbackErr := s.GetUsageTrendForUsers(ctx, missingRangeUserIDs, TeamMemberTrendParams{
    StartDate: strings.TrimSpace(params.StartDate), EndDate: strings.TrimSpace(params.EndDate),
    Granularity: strings.TrimSpace(params.Granularity), Timezone: strings.TrimSpace(params.Timezone),
})
if fallbackErr != nil {
    return out, nil
}
for _, userID := range missingRangeUserIDs {
    points, ok := pointsByUser[userID]
    if !ok {
        continue
    }
    actualCost, totalTokens, tokensComplete := summarizeTeamUsageRange(points)
    item := out[userID]
    item.RangeActualCost = &actualCost
    if tokensComplete {
        item.RangeTotalTokens = &totalTokens
    }
    out[userID] = item
}
return out, nil
```

Do not change `TeamUsageSummaryProvider`, `TeamMemberTrendProvider`, or Team Usage service code.

- [x] **Step 3: Format and run focused GREEN tests**

Run:

```bash
cd backend
gofmt -w internal/relay/sub2api.go internal/relay/sub2api_test.go
go test ./internal/relay -run '^TestSub2APIGetBatchUserUsageStats' -count=1
go test ./internal/teamusage -run 'SummaryRangeUnavailable|SummaryRangeIndependent' -count=1
```

Expected: all focused tests pass. The service tests confirm incomplete adapter results still map to `range_aggregation_unavailable`.

GREEN evidence (2026-07-18): all `TestSub2APIGetBatchUserUsageStats*` tests passed in `internal/relay`; the `SummaryRangeUnavailable|SummaryRangeIndependent` selection passed in `internal/teamusage`.

- [x] **Step 4: Run backend regression and race checks**

Run:

```bash
cd backend
go test ./...
go test -race ./internal/relay ./internal/teamusage
cd ..
git diff --check
```

Expected: all packages pass, race checks pass, and `git diff --check` exits zero.

Verification evidence (2026-07-18): `go test ./...` passed; `go test -race ./internal/relay ./internal/teamusage` passed.

- [x] **Step 5: Commit and push the application fix**

Run:

```bash
git add backend/internal/relay/sub2api.go backend/internal/relay/sub2api_test.go docs/superpowers/plans/2026-07-18-sub2api-team-summary-range-fallback.md
git commit -m "fix(relay): backfill team usage range totals"
git push origin feat/platform-loading-performance
```

Expected: the branch is clean and `origin/feat/platform-loading-performance` points at the new fix commit.

Delivery evidence (2026-07-18): application commit `23db57cb` was published to `origin/feat/platform-loading-performance`. GitHub API verification returned the exact local commit SHA.

---

### Task 3: Publish And Verify The Staging Fix

**Files:**
- Modify in `/Users/admin/helm`: `ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json`
- Reference in `/Users/admin/helm`: `docs/staging-playbook.md`

**Interfaces:**
- Consumes: the exact application commit from `feat/platform-loading-performance`.
- Produces: immutable image `ghcr.io/lichking-2234/ai-efficiency:staging-<full-commit>` and Helm release `ai-efficiency-staging` using the matching restore snapshot ID.

- [x] **Step 1: Build and verify the immutable multi-architecture image**

Run from `/Users/admin/helm`:

```bash
APP_WORKTREE=/Users/admin/ai-efficiency/.worktrees/feat-platform-loading-performance
COMMIT="$(git -C "${APP_WORKTREE}" rev-parse HEAD)"
IMAGE_TAG="staging-${COMMIT}"
IMAGE="ghcr.io/lichking-2234/ai-efficiency:${IMAGE_TAG}"
BUILDER=ai-eff-staging-builder

docker run --privileged --rm tonistiigi/binfmt --install amd64,arm64
BUILDER_PLATFORMS="$(docker buildx inspect "${BUILDER}" --bootstrap | awk -F': ' '/^Platforms:/ {print $2}')"
grep -q 'linux/amd64' <<<"${BUILDER_PLATFORMS}"
grep -q 'linux/arm64' <<<"${BUILDER_PLATFORMS}"

docker buildx build \
  --builder "${BUILDER}" \
  --platform linux/amd64,linux/arm64 \
  --file "${APP_WORKTREE}/deploy/Dockerfile" \
  --tag "${IMAGE}" \
  --build-arg APP_VERSION="staging-${COMMIT:0:7}" \
  --build-arg APP_COMMIT="${COMMIT}" \
  --build-arg APP_BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --push "${APP_WORKTREE}"

docker buildx imagetools inspect "${IMAGE}"
```

Expected: the remote manifest contains both `linux/amd64` and `linux/arm64`.

Publication evidence (2026-07-18): the exact image was published with manifest digest `sha256:deabe0e4227e6cee2236778f90306e70d6c4504adca19c44924d49445973bb86`; remote inspection reported `linux/amd64` and `linux/arm64`. The planned staging builder did not advertise amd64, so the already configured `static-spaces-release-builder` was used after verifying both required platforms.

- [x] **Step 2: Update and commit only the static staging selectors**

Run from `/Users/admin/helm`:

```bash
TMP_VALUES="$(mktemp ai-efficiency/.secrets/.ai-efficiency-staging-upgrade.XXXXXX)"
jq --arg imageTag "${IMAGE_TAG}" --arg snapshotId "${COMMIT:0:12}" \
  '.image.tag = $imageTag | .postgres.restore.snapshotId = $snapshotId' \
  ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json >"${TMP_VALUES}"
chmod 600 "${TMP_VALUES}"
mv "${TMP_VALUES}" ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json
git diff --check
git add ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json
git commit -m "chore(ai-efficiency): publish staging ${COMMIT:0:7}"
git push origin main
```

Expected: only the staging image tag and restore snapshot ID change.

Helm delivery evidence (2026-07-18): `/Users/admin/helm` commit `eb50c36` changed only the two staging selectors and was pushed to `origin/main`.

- [x] **Step 3: Execute the required paused and restore-enabled Helm phases**

Run from `/Users/admin/helm`:

```bash
helm upgrade --install ai-efficiency-staging ./ai-efficiency \
  -n la3-ai-efficiency-prod \
  -f ./ai-efficiency/values.yaml \
  -f ./ai-efficiency/values-staging.yaml \
  -f ./ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json \
  --set replicaCount=0 \
  --set postgres.restore.enabled=false \
  --atomic --wait --timeout 20m --dry-run=server --hide-secret >/dev/null

helm upgrade --install ai-efficiency-staging ./ai-efficiency \
  -n la3-ai-efficiency-prod \
  -f ./ai-efficiency/values.yaml \
  -f ./ai-efficiency/values-staging.yaml \
  -f ./ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json \
  --set replicaCount=0 \
  --set postgres.restore.enabled=false \
  --atomic --wait --timeout 20m

APP_PODS="$(kubectl get pod -n la3-ai-efficiency-prod \
  -l 'app.kubernetes.io/instance=ai-efficiency-staging,!app.kubernetes.io/component' \
  -o name)"
if [[ -n "${APP_PODS}" ]]; then
  kubectl wait --for=delete pod -n la3-ai-efficiency-prod \
    -l 'app.kubernetes.io/instance=ai-efficiency-staging,!app.kubernetes.io/component' \
    --timeout=180s
fi

helm upgrade --install ai-efficiency-staging ./ai-efficiency \
  -n la3-ai-efficiency-prod \
  -f ./ai-efficiency/values.yaml \
  -f ./ai-efficiency/values-staging.yaml \
  -f ./ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json \
  --atomic --wait --wait-for-jobs --timeout 20m --dry-run=server --hide-secret >/dev/null

helm upgrade --install ai-efficiency-staging ./ai-efficiency \
  -n la3-ai-efficiency-prod \
  -f ./ai-efficiency/values.yaml \
  -f ./ai-efficiency/values-staging.yaml \
  -f ./ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json \
  --atomic --wait --wait-for-jobs --timeout 20m
```

Expected: Helm records a paused revision before the restore-enabled revision, the restore Job completes, and the application Deployment becomes ready.

Rollout evidence (2026-07-18): paused revision 21 completed with no application Pod; restore-enabled revision 22 completed after Job `ai-efficiency-staging-postgres-restore-35153d0430f3` reached `1/1` and the application Deployment became ready.

- [x] **Step 4: Verify staging health, image identity, and the original failing API contract**

Run:

```bash
helm status ai-efficiency-staging -n la3-ai-efficiency-prod
kubectl rollout status statefulset/ai-efficiency-staging-postgres -n la3-ai-efficiency-prod --timeout=180s
kubectl rollout status deployment/ai-efficiency-staging -n la3-ai-efficiency-prod --timeout=180s
kubectl get deployment ai-efficiency-staging -n la3-ai-efficiency-prod \
  -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'
curl -fsS --max-time 20 https://ai-efficiency-staging.la3.agoralab.co/api/v1/health/live
curl -fsS --max-time 20 https://ai-efficiency-staging.la3.agoralab.co/api/v1/health/ready

SUMMARY_RESPONSE="$(curl -fsS --max-time 30 \
  -H "Authorization: Bearer ${STAGING_ACCESS_TOKEN:?set an authenticated staging access token}" \
  'https://ai-efficiency-staging.la3.agoralab.co/api/v1/user/team-usage/summary?start_date=2026-06-19&end_date=2026-07-18&granularity=day&timezone=Asia%2FShanghai')"
jq -e '
  .summary.unavailable == false and
  .summary.unavailable_reason == null and
  .summary.range_actual_cost != null and
  .summary.range_total_tokens != null and
  .summary.member_count == 4 and
  .summary.relay_member_count == 4
' <<<"${SUMMARY_RESPONSE}"
```

Expected: the Deployment image equals `${IMAGE}`, both health endpoints succeed, and the original four-member range returns complete non-null totals.

Runtime evidence (2026-07-19): staging live/ready checks passed with build commit `35153d0430f3299a507b234b86da55a4ddad6736`. Authenticated cold requests for both `2026-06-19..2026-07-18` and `2026-06-20..2026-07-19` returned HTTP 200, `cache_status=miss`, `source_status=ok`, `unavailable=false`, 4/4 members, and non-null range cost/tokens. A repeated current-range request returned `cache_status=fresh` with the same available range contract in 0.58 seconds.

- [x] **Step 5: Confirm production was not changed**

Run:

```bash
helm status ai-efficiency-prod -n la3-ai-efficiency-prod
curl -fsS --max-time 20 https://ai-efficiency.la3.agoralab.co/api/v1/health/ready
git -C /Users/admin/ai-efficiency/.worktrees/feat-platform-loading-performance status --short --branch
git -C /Users/admin/helm status --short --branch
```

Expected: production remains on its pre-existing release and is ready; both repositories are clean and synchronized with their remotes.

Isolation evidence (2026-07-19): production remained ready at Helm revision 68 on `v0.1.0-preview.72`; the staging release alone advanced to revision 22.
