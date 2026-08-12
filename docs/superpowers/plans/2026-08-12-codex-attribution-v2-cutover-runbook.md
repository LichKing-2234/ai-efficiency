# Codex Attribution v2 Cutover Runbook

**Date:** 2026-08-12  
**Status:** #251 execution complete. The immutable candidate, separate CLI and platform releases, staging rehearsal/reset, production two-phase deployment, verified export, formal Request-to-Activity canary, exact failed-canary cleanup, production v1 reset, and all final readbacks are green. The seven-day legacy-code cleanup remains exclusively tracked by #252 and was not executed.
**Owner ticket:** [#250](https://github.com/LichKing-2234/ai-efficiency/issues/250)  
**Execution ticket:** [#251](https://github.com/LichKing-2234/ai-efficiency/issues/251)

This runbook records the reversible evidence and exact operator sequence used
for the completed #251 cutover. The later #252 legacy cleanup remains blocked
until `2026-08-19T12:59:09Z` and may proceed only if every stable-window gate is
still green.

## Execution Status

- [x] Record explicit #251 deletion, CLI/platform release, staging rehearsal,
  production deployment, and cutover authority.
- [x] Add one frozen UTC `cutover_at` configuration seam; reject missing formal
  boundaries and shadow/formal contradictions before router startup.
- [x] Replace first-formal-pool comparison inference with the frozen boundary;
  focused tests prove a zero-data previous period wholly after cutover remains
  comparable.
- [x] Pass local backend/ae-cli/frontend defaults, focused race and 20-repeat
  cutover tests, `go vet`, frontend build measurement, ae-cli discover E2E,
  deploy/compose contracts, and the built production-preview role matrix
  (126/126). The first development-server matrix was invalidated by Vite
  dependency-optimization reloads during its first lazy dialog load; no product
  assertion failed on the production-preview rerun.
- [x] Pass candidate-wide backend, ae-cli, frontend, build, E2E, race, repeat,
  vet, scale, query-plan, and synthetic gates at one immutable SHA.
- [x] Create and verify separate CLI and platform releases.
- [x] Complete the staging formal-v2 rehearsal and reset verification. Revision
  122 runs `v0.1.0-preview.83` at the frozen
  `2026-08-12T11:01:31Z` boundary; the formal canary reconciled to one
  43,046-Token pool, formal-only Activity/readiness passed, the verified
  private export manifest SHA-256 is
  `9b142652b010abd7005dc9ee9abcb1f709bfbbfed2ada1c4de225ce6afe88b1f`,
  and the atomic reset removed 22 revisions plus 21 buckets with both v1
  tables reading back zero.
- [x] Execute the ordered production cutover/reset and complete all readbacks.
  Production revision 83 runs `v0.1.0-preview.83` with frozen
  `cutover_at=2026-08-12T11:22:58Z`; the private export manifest SHA-256 is
  `9b142652b010abd7005dc9ee9abcb1f709bfbbfed2ada1c4de225ce6afe88b1f`.
  A response-header Request canary reconciled to one 4,395-Token formal pool
  and is visible in Activity. One request-side-ID canary was rejected by the
  upstream reader and then removed under separately confirmed exact-match
  guards: one unmaterialized `pending/not_found` claim and its one empty group,
  with no pool mutation. Pending returned from 4 to the baseline 3 before the
  production reset removed 22 revisions plus 21 buckets. Both v1 tables and
  all four v1 totals read back zero; formal and shadow pools remain isolated.

## 1. Hard Go/No-Go Gates

Record every value in the #251 execution log. Any missing or failing item is a
no-go.

- [x] Candidate commit is immutable and its hosted backend, ae-cli, frontend,
  deploy-static, role E2E, build-measurement, race, repeat, vet, scale, query-plan,
  and synthetic contract gates are green at that exact SHA.
- [x] One separately authorized real Request-to-commit-to-Activity canary has
  passed in `shadow_v2`; its pool is absent from formal Activity/readiness.
- [x] The candidate rejects v1 writes with `upgrade_required`, freezes one UTC
  `cutover_at`, writes new accepted claims to the formal epoch, and reads only
  that formal epoch.
- [x] Shadow/canary pool, claim-group, and Request-claim counts are captured and
  excluded or cleaned before formal reads are enabled.
- [x] `ai_efficiency_attribution_groups_near_expiry == 0`; no finalization or
  cleanup error increase is present; P0/P1 findings are zero.
- [x] Database backup/restore is current and tested. The v1 evidence export and
  SHA-256 manifest below exist outside the database host's ephemeral storage.
- [x] Release, deployment, and v1 deletion are separately and explicitly
  authorized in the #251 execution turn.

## 2. Observability Dashboard And Alerts

Use a 5-minute rate window during cutover and a 24-hour comparison window after
cutover. Keep `release` visible so old and candidate replicas cannot be mixed.

| Panel / alert | PromQL | Gate |
| --- | --- | --- |
| Pending claims | `ai_efficiency_attribution_requests_pending` | stable or decreasing |
| Oldest pending age | `ai_efficiency_attribution_oldest_pending_age_seconds` | below the agreed upstream lookup SLO |
| Final-attempt boundary | `ai_efficiency_attribution_groups_near_expiry` | exactly `0` before cutover |
| Reconciliation outcomes | `sum by (outcome, release) (rate(ai_efficiency_attribution_reconciliations_total[5m]))` | no unexplained mismatch/ambiguous/invalid increase |
| Reconciliation p95 | `histogram_quantile(0.95, sum by (le, release) (rate(ai_efficiency_attribution_reconciliation_age_seconds_bucket[5m])))` | no regression against the qualification baseline |
| Lifecycle failures | `sum by (operation, outcome, release) (increase(ai_efficiency_attribution_lifecycle_total{outcome="error"}[5m]))` | exactly `0` |
| Hard expiry | `sum by (operation, release) (increase(ai_efficiency_attribution_lifecycle_total{outcome="hard_expired"}[5m]))` | exactly `0` during cutover |
| Activity 5xx | `sum by (route, release) (rate(ai_efficiency_http_requests_total{route=~"/api/v1/activity.*",status_class="5xx"}[5m]))` | exactly `0` |
| Activity p95 | `histogram_quantile(0.95, sum by (le, route, release) (rate(ai_efficiency_http_request_duration_seconds_bucket{route=~"/api/v1/activity.*"}[5m])))` | within the accepted production SLO |
| Relay lookup failures | `sum by (operation, status_class, release) (rate(ai_efficiency_dependency_requests_total{dependency="relay",status_class=~"4xx|5xx"}[5m]))` | no unexplained increase |

Database readbacks (aggregate only; never copy Request IDs into tickets or
dashboards):

```sql
SELECT ledger_epoch, count(*) AS pools,
       coalesce(sum(total_tokens), 0) AS tokens,
       coalesce(sum(request_count), 0) AS requests,
       coalesce(sum(coverage_gap_count), 0) AS gaps
FROM attribution_usage_pools
GROUP BY ledger_epoch ORDER BY ledger_epoch;

SELECT status, count(*)
FROM attribution_request_claims
GROUP BY status ORDER BY status;

SELECT ledger_epoch, count(*) AS groups,
       count(*) FILTER (WHERE finalized_at IS NULL) AS hot_groups
FROM attribution_claim_groups
GROUP BY ledger_epoch ORDER BY ledger_epoch;
```

## 3. Exact v1 Evidence Export

Create a private directory with restrictive permissions. Do not commit any
export. Replace only the output directory and database connection; do not edit
the SQL predicates during execution.

```bash
export AE_V1_EXPORT_DIR="$(mktemp -d /tmp/ae-v1-export.XXXXXX)"
chmod 700 "$AE_V1_EXPORT_DIR"
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -c "\copy (SELECT * FROM attribution_usage_buckets ORDER BY id) TO '$AE_V1_EXPORT_DIR/attribution_usage_buckets.csv' CSV HEADER"
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -c "\copy (SELECT * FROM attribution_allocation_revisions ORDER BY usage_bucket_id, sequence, id) TO '$AE_V1_EXPORT_DIR/attribution_allocation_revisions.csv' CSV HEADER"
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -c "\copy (SELECT count(*) AS bucket_count, coalesce(sum(provider_total_tokens),0) AS provider_total_tokens, coalesce(sum(processed_total_tokens),0) AS processed_total_tokens, coalesce(sum(request_count),0) AS request_count FROM attribution_usage_buckets) TO '$AE_V1_EXPORT_DIR/summary.csv' CSV HEADER"
(cd "$AE_V1_EXPORT_DIR" && sha256sum *.csv > SHA256SUMS)
```

Copy the directory to the approved durable evidence location, then verify it
there with `sha256sum -c SHA256SUMS`. The execution log records only the summary
counts/totals and manifest hash, never raw rows.

## 4. Exact v1 Reset Procedure

At cutover, this was the complete v1 POC dataset: immutable
`attribution_usage_buckets` plus its append-only
`attribution_allocation_revisions`. Reporting installations, checkpoints,
repositories, PRs, users, and every v2 table are deliberately preserved.

Run only after the deployed candidate rejects new v1 writes and after the
export has been verified. The transaction resolves the exact bucket ID set
under an exclusive lock, deletes dependent revisions first because the current
foreign key is `NO ACTION`, verifies the resolved set, and commits atomically.

```sql
BEGIN;
LOCK TABLE attribution_usage_buckets,
           attribution_allocation_revisions
  IN ACCESS EXCLUSIVE MODE;

CREATE TEMP TABLE resolved_v1_bucket_ids ON COMMIT DROP AS
SELECT id FROM attribution_usage_buckets;

SELECT count(*) AS resolved_bucket_count,
       coalesce(sum(provider_total_tokens), 0) AS provider_total_tokens,
       coalesce(sum(processed_total_tokens), 0) AS processed_total_tokens,
       coalesce(sum(request_count), 0) AS request_count
FROM attribution_usage_buckets
WHERE id IN (SELECT id FROM resolved_v1_bucket_ids);

DELETE FROM attribution_allocation_revisions
WHERE usage_bucket_id IN (SELECT id FROM resolved_v1_bucket_ids);

DELETE FROM attribution_usage_buckets
WHERE id IN (SELECT id FROM resolved_v1_bucket_ids);

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM attribution_usage_buckets) OR
     EXISTS (SELECT 1 FROM attribution_allocation_revisions) THEN
    RAISE EXCEPTION 'v1 attribution reset verification failed';
  END IF;
END $$;

COMMIT;
```

Immediately read back both v1 counts as zero, confirm a v1 write returns
`upgrade_required`, and confirm a new v2 claim is accepted into the formal
epoch.

## 5. Ordered Cutover

1. Freeze operator changes and record candidate SHA, UTC start time, active
   release replicas, database backup ID, and explicit authorities.
2. Deploy the #251 candidate with v1 writes gated, but do not enable formal v2
   reads yet. Verify liveness/readiness and v1 `upgrade_required`.
3. Freeze one UTC `cutover_at`. Stop if replicas disagree on the cutover epoch
   or configuration.
4. Export and verify v1 evidence. Capture shadow/canary aggregate counts.
5. Exclude or clean all pre-cutover shadow/canary v2 data using the exact #251
   migration generated for the frozen epoch. Never relabel shadow rows as
   formal history.
6. Enable formal v2 writes and Activity/readiness reads at `cutover_at`.
7. Execute the exact v1 reset transaction above.
8. Read back: v1 rejection, v2 acceptance, formal-only Activity/readiness,
   ratio omission across the boundary, zero v1 totals, and all dashboard gates.
9. Keep the evidence export and rollback window. Start the seven-day stable
   observation clock only after every readback is green.

## 6. Rollback

Rollback is forward-only for accounting safety. Never restore v1 as formal
truth after its reset.

- Before the v1 reset: disable/hide v2 reads, keep v2 writes isolated, roll the
  application back, and leave v1 data untouched.
- After the v1 reset: disable/hide v2 reads if necessary, keep or pause formal
  v2 ingestion according to the incident, and roll the application forward to
  a fixed v2 build. Do not import the v1 export into a serving database or
  re-enable v1 reads.
- Database restore is disaster recovery only and requires separate authority.
  Restoring a pre-cutover snapshot also restores old credentials and unrelated
  application state, so it is not the normal attribution rollback mechanism.
- A failed pool/read migration must preserve the frozen `cutover_at`; never
  choose a later boundary that would double-count or silently omit accepted
  formal claims.

Rollback completion requires healthy probes, stable metrics, an explicit
statement of whether formal v2 writes are paused or continuing, and a retained
copy of every pre-cutover evidence artifact.
