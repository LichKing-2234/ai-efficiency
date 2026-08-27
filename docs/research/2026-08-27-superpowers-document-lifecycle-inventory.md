# Superpowers Document Lifecycle Inventory

**Date:** 2026-08-27

**Research ticket:** [research(docs): classify the Superpowers document corpus against live state](https://github.com/LichKing-2234/ai-efficiency/issues/408)

**Snapshot:** hosted `origin/main` at [`022670ac`](https://github.com/LichKing-2234/ai-efficiency/commit/022670ac02624e814c434b3657c244150a6eecec)

## Scope and method

This inventory covers every Markdown file committed under
`docs/superpowers/specs/` and `docs/superpowers/plans/` in the snapshot above:
62 specs and 101 plans, 163 files total. It does not classify uncommitted files
from another checkout.

Each disposition was assigned after comparing the document's own status,
checklists, and linked Issues/PRs with these primary sources:

- the hosted [`docs/superpowers` snapshot](https://github.com/LichKing-2234/ai-efficiency/tree/022670ac02624e814c434b3657c244150a6eecec/docs/superpowers);
- current [`AGENTS.md`](https://github.com/LichKing-2234/ai-efficiency/blob/022670ac02624e814c434b3657c244150a6eecec/AGENTS.md),
  [`docs/architecture.md`](https://github.com/LichKing-2234/ai-efficiency/blob/022670ac02624e814c434b3657c244150a6eecec/docs/architecture.md),
  and the code paths they identify as current owners;
- hosted Issue, PR, merge, and release state, especially the loading-performance
  integration [PR #160](https://github.com/LichKing-2234/ai-efficiency/pull/160),
  its [production qualification](https://github.com/LichKing-2234/ai-efficiency/issues/136),
  the later [Overview removal](https://github.com/LichKing-2234/ai-efficiency/pull/198),
  attribution cleanup [PR #330](https://github.com/LichKing-2234/ai-efficiency/pull/330)
  and [PR #332](https://github.com/LichKing-2234/ai-efficiency/pull/332), and the
  current [open-Issue query](https://github.com/LichKing-2234/ai-efficiency/issues?q=is%3Aissue%20state%3Aopen).

The categories are migration dispositions, not claims that an entire document
is uniformly correct:

- **Candidate live contract:** extract the still-live, non-obvious contract into
  a neutral contract home after reconciling it with code. Do not move the file
  wholesale merely because it is listed here.
- **Uniquely valuable history:** retain a concise historical record because the
  rationale, failed experiment, cutover sequence, or operational evidence is
  not adequately recoverable from current code alone.
- **Redundant completed history:** completion is already owned by code, merged
  PRs, releases, or a current contract; preserving a second execution narrative
  in HEAD adds little value.
- **Stale/contradicted local state:** the status, target state, or unchecked
  ledger is contradicted or superseded by current code and hosted state. Useful
  fragments, if any, need reconciliation before reuse.
- **Genuinely open work:** the file itself is a current, tracker-backed work
  authority. No file met this test.

## Result

| Disposition | Specs | Plans | Total |
| --- | ---: | ---: | ---: |
| Candidate live contract | 24 | 0 | 24 |
| Uniquely valuable history | 10 | 8 | 18 |
| Redundant completed history | 10 | 45 | 55 |
| Stale/contradicted local state | 18 | 48 | 66 |
| Genuinely open work | 0 | 0 | 0 |
| **Total** | **62** | **101** | **163** |

## Cross-corpus findings

1. **There is no live implementation ledger in the corpus.** The only open
   product Issue whose scope is recorded as a known gap in this corpus is
   [perf(settings): bound quota-reset department search](https://github.com/LichKing-2234/ai-efficiency/issues/348).
   Other open product, Wayfinder, or architecture Issues were created as
   tracker-native work and are not represented by these files. Older proposed
   or unchecked documents without an open tracker owner are stale, not silently
   open.
2. **Recent status text already drifted.** The usage-window spec says it is not
   merged, but [PR #358](https://github.com/LichKing-2234/ai-efficiency/pull/358)
   is merged and current code owns `frontend/src/utils/usageWindowPreference.ts`.
   The list/pagination spec makes the same outdated claim, while
   [PR #373](https://github.com/LichKing-2234/ai-efficiency/pull/373) is merged
   and current code uses `frontend/src/components/CursorPager.vue`. The Replan
   spec was updated correctly after [PR #355](https://github.com/LichKing-2234/ai-efficiency/pull/355),
   with current unavailable-target behavior in
   `backend/internal/relayplanning/service.go` and the frontend workflow.
3. **The July performance ledgers are point-in-time branch reports, not current
   state.** Thirty plans from 2026-07-14 through the pre-experiment 2026-07-19
   integration sequence still say work was not merged to `main`, not released,
   or awaited Issues #136/#137. [PR #160](https://github.com/LichKing-2234/ai-efficiency/pull/160)
   merged that stack to `main`,
   [`v0.1.0-preview.74`](https://github.com/LichKing-2234/ai-efficiency/releases/tag/v0.1.0-preview.74)
   shipped it, and Issues [#136](https://github.com/LichKing-2234/ai-efficiency/issues/136)
   and [#137](https://github.com/LichKing-2234/ai-efficiency/issues/137) are closed.
4. **Attribution cleanup invalidated its own remaining-work ledgers.** Issue
   [#252](https://github.com/LichKing-2234/ai-efficiency/issues/252) is closed;
   [PR #330](https://github.com/LichKing-2234/ai-efficiency/pull/330) removed v1
   and AE OTLP application paths, and
   [PR #332](https://github.com/LichKing-2234/ai-efficiency/pull/332) removed the
   legacy schema. The v2 plan's three unchecked items and the reporting
   onboarding plan's four unchecked items are not open authority.
5. **Duplicate ledgers are structural.** Forty-four specs have an exact
   same-stem implementation plan. Four plans additionally declare an explicit
   `Execution Ledger`, `Integration Ledger`, or `Issue 执行台账`. Across the
   corpus, 18 plans retain **291 unchecked checkboxes**, including documents
   whose status says complete or whose implementation is already merged. This
   makes checkbox state unsuitable as an open-work query.

## Candidate live contracts (24)

These are the documents current `AGENTS.md` or `docs/architecture.md` treats as
active contracts. They are candidates for extraction, not clean renames; the
notes identify known reconciliation needs.

| File | Why it remains a candidate |
| --- | --- |
| `specs/2026-03-24-oauth-cli-login-design.md` | Current OAuth and Relay-provider foundation referenced by both source-of-truth documents. |
| `specs/2026-04-14-llm-settings-runtime-editing-design.md` | Current compatibility boundary for runtime Relay LLM settings. |
| `specs/2026-04-15-oauth-device-login-design.md` | Current cross-process device authorization contract. |
| `specs/2026-05-13-sessionless-local-tool-attribution-design.md` | Surviving sessionless attribution boundary; reconcile its "partially implemented" status against v2 and legacy removal. |
| `specs/2026-05-14-legacy-session-staged-cutover-design.md` | Current negative compatibility boundary proving legacy session/proxy paths are removed. |
| `specs/2026-05-19-ae-cli-deterministic-tool-configuration-design.md` | Current `ae-cli discover` and tool-configuration contract. |
| `specs/2026-05-26-ae-cli-post-commit-async-attribution-sync-design.md` | Current hook/runner durability boundary; reconcile overlapping v2 delivery rules. |
| `specs/2026-06-02-repo-auto-binding-design.md` | Current CLI/backend repository-binding contract. |
| `specs/2026-06-04-admin-sub2api-subscription-assignment-design.md` | Current async subscription-job and Relay mutation contract. |
| `specs/2026-06-10-independent-cli-release-design.md` | Current platform-vs-CLI release-unit and bridge-tag contract. |
| `specs/2026-06-14-user-api-key-first-onboarding-design.md` | Current cross-layer user onboarding contract. |
| `specs/2026-06-22-configurable-directory-sync-design.md` | Current directory DSL, apply, offboarding, and token-invalidation contract. |
| `specs/2026-06-26-team-usage-representative-quota-design.md` | Current representative-scope, Team Usage, and quota-control foundation. |
| `specs/2026-07-07-quota-reset-approval-design.md` | Current quota-reset workflow base contract. |
| `specs/2026-07-10-multi-stage-quota-reset-approval-design.md` | Current department-derived sequential approval extension; remove obsolete browser-verification wording. |
| `specs/2026-07-14-end-to-end-page-loading-performance-design.md` | Current read-model, loading, serving, timeout, and observability foundation after PR #160. |
| `specs/2026-07-25-stateless-team-usage-prewarm-worker-design.md` | Current Team Usage generation/manifest and fallback contract that supersedes the older cold path. |
| `specs/2026-08-11-codex-commit-token-attribution-v2-design.md` | Current production attribution contract after the cutover and v1 removal. |
| `specs/2026-08-14-user-protocol-compatibility-test-design.md` | Current per-protocol user connection-test contract. |
| `specs/2026-08-19-personal-usage-reset-and-oauth-pool-design.md` | Current selected-window reset and OAuth-pool usage contract, implemented by PR #319. |
| `specs/2026-08-19-relay-group-mapping-contract.md` | Current Relay Mapping, Account, fingerprint, retry, and migration contract. |
| `specs/2026-08-24-relay-replan-baseline-roster-design.md` | Current zero-change Replan and unavailable relationship/target extension, implemented by PRs #355/#376/#391. |
| `specs/2026-08-24-usage-window-preference-design.md` | Live browser preference contract, but its unmerged status is contradicted by PR #358. |
| `specs/2026-08-25-list-and-pagination-consistency-design.md` | Live frontend collection-navigation contract, but its unmerged status is contradicted by PR #373. |

## Uniquely valuable history (18)

| File | Unique value to preserve |
| --- | --- |
| `specs/2026-03-17-ai-efficiency-platform-design.md` | Original platform boundary and vocabulary baseline; explicitly historical in current source-of-truth docs. |
| `specs/2026-03-26-session-pr-attribution-design.md` | Explains the pre-sessionless attribution model and why later contracts replaced it. |
| `specs/2026-04-02-local-session-proxy-design.md` | Records the rejected/dormant proxy-first direction; current code must not be described from it. |
| `specs/2026-04-13-frontend-deployment-control-alignment-design.md` | Records why the in-app deployment surface was removed while version visibility survived. |
| `specs/2026-04-13-unified-binary-self-update-design.md` | Preserves rationale for the retired backend-managed self-update direction. |
| `specs/2026-07-19-team-usage-cache-read-retry-and-24-origin-design.md` | Failed 24-origin experiment and measured acceptance boundary. |
| `specs/2026-07-19-team-usage-cold-loading-design.md` | Failed cold-loading hypothesis and measured staging outcome. |
| `specs/2026-07-19-team-usage-shared-trend-cache-design.md` | Intermediate shared-origin hypothesis and evidence that motivated the later prewarmer. |
| `specs/2026-07-20-team-usage-experiment-matrix-design.md` | Comparative experiment rationale; no candidate won every gate. |
| `specs/2026-08-05-codex-token-attribution-ledger-poc-design.md` | Historical v1 POC vocabulary and conservation rationale explicitly separated from formal Activity. |
| `plans/2026-07-19-team-usage-cache-read-retry-and-24-origin.md` | Exact staging failure record for the 24-origin candidate. |
| `plans/2026-07-19-team-usage-cold-loading.md` | Exact cold/warm staging measurements for the failed concurrency experiment. |
| `plans/2026-07-19-team-usage-shared-trend-cache.md` | Exact evidence showing duplicate-load improvement without acceptable cold latency. |
| `plans/2026-07-20-team-usage-experiment-matrix.md` | Cross-candidate matrix and restoration decision. |
| `plans/2026-07-25-stateless-team-usage-prewarm-worker.md` | Detailed staging, restart, expiry-fallback, and telemetry-classification evidence for the accepted direction. |
| `plans/2026-08-12-codex-attribution-v2-cutover-runbook.md` | Ordered production cutover and rollback evidence; its old #252 remaining-work sentence must be marked superseded. |
| `plans/2026-08-12-codex-attribution-v2-qualification-evidence.md` | Canary failure matrix and qualification record not represented by current code. |
| `plans/2026-08-13-attribution-v1-cleanup-preflight.md` | Destructive migration gates, exports, conservation checks, and production closeout evidence. |

## Redundant completed history (55)

The following files describe completed delivery whose enduring behavior is
already owned by current code, a candidate contract above, merged PRs, or
release records. Their historical commits remain available through Git.

| File | Evidence-based disposition |
| --- | --- |
| `specs/2026-04-13-ae-cli-user-install-design.md` | Installer/release implementation owns the completed behavior. |
| `specs/2026-04-13-docker-deploy-ux-and-config-surface-design.md` | Current deploy scripts and templates own this operator-facing behavior. |
| `specs/2026-05-21-user-page-cli-self-serve-design.md` | Current architecture and later onboarding contracts supersede the completed slice. |
| `specs/2026-05-28-ae-cli-cli-update-design.md` | Current CLI updater and independent release contract own the result. |
| `specs/2026-05-28-pr-sync-job-progress-usage-freshness-design.md` | Completed PR-sync behavior is implemented and covered by current code. |
| `specs/2026-06-01-ae-cli-doctor-tool-validation-design.md` | Completed CLI validation behavior is directly owned by `doctorcheck`. |
| `specs/2026-06-03-ae-cli-shared-http-request-handler-design.md` | Completed internal helper boundary is obvious from current packages and tests. |
| `specs/2026-06-03-pr-sync-large-repo-recovery-design.md` | Completed recovery behavior is owned by current PR-sync code. |
| `specs/2026-06-06-repo-webhook-repair-design.md` | Completed repair workflow is owned by current handler/service code and merged evidence. |
| `specs/2026-06-18-agent-group-hermes-openclaw-configuration-design.md` | Completed UI configuration slice is covered by current onboarding architecture. |
| `plans/2026-03-17-phase1-core-framework-mvp.md` | Completed bootstrap ledger. |
| `plans/2026-03-17-phase2-ai-analysis-labeling.md` | Completed early-phase ledger; any gaps require tracker tickets, not plan replay. |
| `plans/2026-03-24-ae-cli-graceful-shutdown.md` | Completed implementation ledger. |
| `plans/2026-03-24-oauth-cli-login.md` | Completed implementation ledger; current OAuth contract survives separately. |
| `plans/2026-03-26-standards-alignment-phase1.md` | Completed repository-alignment ledger. |
| `plans/2026-04-03-session-visibility-and-filters.md` | Completed legacy session UI ledger. |
| `plans/2026-04-07-ae-cli-same-tool-multi-instance.md` | Completed CLI implementation ledger. |
| `plans/2026-04-07-lazy-platform-key-reuse.md` | Completed compatibility implementation ledger. |
| `plans/2026-04-08-github-primary-repo-release-automation.md` | Current workflows and releases own the result. |
| `plans/2026-04-08-production-deployment-packaging.md` | Completed packaging ledger; retired update portions are already marked. |
| `plans/2026-04-09-binary-systemd-install-update.md` | Completed mixed installation ledger; current code/docs own surviving behavior. |
| `plans/2026-04-13-ae-cli-user-install.md` | Completed installer ledger. |
| `plans/2026-04-13-frontend-deployment-control-alignment.md` | Completed then superseded delivery ledger. |
| `plans/2026-04-13-unified-binary-self-update.md` | Completed then retired delivery ledger. |
| `plans/2026-04-15-cli-start-auto-repo-sync.md` | Completed implementation ledger. |
| `plans/2026-05-13-sessionless-local-tool-attribution.md` | Completed first-slice ledger; current contracts own surviving behavior. |
| `plans/2026-05-14-legacy-session-staged-cutover.md` | Completed cutover ledger; negative boundary survives in the candidate contract. |
| `plans/2026-05-14-sessionless-attribution-post-merge-cleanup.md` | Completed post-merge cleanup ledger. |
| `plans/2026-05-19-ae-cli-deterministic-tool-configuration.md` | Completed and repeatedly updated implementation ledger. |
| `plans/2026-05-20-pr-usage-snapshots.md` | Completed pre-v2 attribution slice. |
| `plans/2026-05-24-global-git-hooks-implementation.md` | Completed hook implementation ledger. |
| `plans/2026-05-24-global-hook-reporting-qa.md` | Historical QA checklist with no current work authority. |
| `plans/2026-05-25-events-page-filters-pagination.md` | Completed and verified UI/API ledger. |
| `plans/2026-05-26-admin-users-local-credentials.md` | Completed implementation ledger. |
| `plans/2026-05-26-ae-cli-post-commit-async-attribution-sync.md` | Completed implementation ledger; current/v2 contracts own the behavior. |
| `plans/2026-05-26-user-cli-setup-checklist.md` | Completed UI implementation ledger. |
| `plans/2026-05-28-pr-sync-job-progress-usage-freshness.md` | Completed branch delivery ledger. |
| `plans/2026-05-30-company-wide-usability-hardening.md` | Completed UI hardening ledger. |
| `plans/2026-06-01-ae-cli-doctor-tool-validation.md` | Completed validation ledger. |
| `plans/2026-06-02-ae-cli-reporting-durability-hardening.md` | Completed multi-review hardening ledger. |
| `plans/2026-06-02-repo-auto-binding.md` | Completed implementation ledger; candidate contract survives separately. |
| `plans/2026-06-03-ae-cli-shared-http-request-handler.md` | Completed helper extraction ledger. |
| `plans/2026-06-03-pr-sync-large-repo-recovery.md` | Completed recovery ledger. |
| `plans/2026-06-04-admin-subscription-job-batching.md` | Completed async-job delivery ledger. |
| `plans/2026-06-04-user-setup-manual-config-correction.md` | Completed correction ledger. |
| `plans/2026-06-06-repo-webhook-repair.md` | Completed implementation and live-verification ledger. |
| `plans/2026-06-06-user-usage-trend.md` | Completed usage UI ledger; newer contracts own current semantics. |
| `plans/2026-06-14-user-api-key-first-onboarding.md` | Completed onboarding ledger. |
| `plans/2026-06-18-agent-group-hermes-openclaw-configuration.md` | Completed delivery and release ledger. |
| `plans/2026-06-22-configurable-directory-sync.md` | Completed implementation ledger; candidate contract survives separately. |
| `plans/2026-06-26-team-usage-representative-quota.md` | Completed implementation ledger; candidate contract survives separately. |
| `plans/2026-07-07-quota-reset-approval.md` | Completed workflow ledger. |
| `plans/2026-07-10-quota-reset-actionable-queue-badges.md` | Completed follow-up ledger. |
| `plans/2026-07-27-team-overview-contract-removal.md` | PR #198, release preview.75, and current 404 behavior own the completed removal. |
| `plans/2026-08-18-relay-planning-309-312.md` | Current Relay contracts plus merged PRs/releases own the completed production rollout. |

## Stale or contradicted local state (66)

These files must not be treated as executable plans or current contracts. The
July performance set is listed individually because each file carries its own
obsolete branch/merge/production status even though PR #160 and Issues #136/#137
settled the shared hosted state.

| File | Contradiction or supersession |
| --- | --- |
| `specs/2026-03-24-ae-cli-smart-tool-discovery-design.md` | Proposed LLM discovery is superseded by the deterministic `ae-cli discover` contract and PR #183 follow-ups. |
| `specs/2026-04-08-production-deployment-packaging-design.md` | Mixes surviving packaging with a retired in-app update plane; not suitable as one live contract. |
| `specs/2026-04-09-binary-systemd-install-update-design.md` | Mixes surviving systemd install behavior with removed self-update APIs. |
| `specs/2026-04-14-scm-credentials-provider-binding-design.md` | Proposed credential module is not current code and has no current open tracker owner. |
| `specs/2026-04-15-cli-start-auto-repo-sync-design.md` | Review-requested target was overtaken by later repo binding and attribution contracts. |
| `specs/2026-04-16-session-raw-response-design.md` | Draft depends on the retired local session/proxy surface. |
| `specs/2026-04-16-session-stream-raw-response-design.md` | Draft depends on the retired local session/proxy surface. |
| `specs/2026-05-20-pr-usage-snapshots-design.md` | "Continue correcting" pre-v2 attribution state is superseded by the production v2 contract. |
| `specs/2026-05-21-global-tool-usage-events-page-design.md` | Old Events/session presentation is superseded by current Activity v2 boundaries. |
| `specs/2026-05-23-global-git-hooks-design.md` | Original hook delivery contract is superseded in material parts by v2 claim/delivery semantics. |
| `specs/2026-05-26-admin-users-local-credentials-design.md` | Still says approved for planning although the admin credential surface is implemented and later evolved. |
| `specs/2026-05-26-user-cli-setup-checklist-design.md` | Refers to a current PR branch and checklist UX later replaced by explicit-action onboarding. |
| `specs/2026-05-29-company-wide-user-home-ux-design.md` | Completed home model was later replaced by API-key-first explicit-action onboarding. |
| `specs/2026-05-29-history-pages-task-zone-ui-redesign-design.md` | Point-in-time UI design superseded by later Activity and Element Plus/list contracts. |
| `specs/2026-05-30-company-wide-usability-hardening-design.md` | Point-in-time UI sweep is not a durable current contract. |
| `specs/2026-06-06-user-usage-trend-design.md` | Original usage dashboard semantics are split across newer window, pool/reset, and Team Usage contracts. |
| `specs/2026-06-15-ai-usage-center-lifecycle-home-design.md` | Earlier lifecycle home was superseded by the current group-first explicit-action onboarding flow. |
| `specs/2026-06-16-ai-usage-center-group-quota-design.md` | Original proposed quota source changed during implementation and later contracts own the current behavior. |
| `plans/2026-03-26-ae-cli-smart-tool-discovery-executable.md` | Forty unchecked tasks describe a superseded discovery direction. |
| `plans/2026-03-26-smart-tool-discovery.md` | Explicitly superseded, with 57 unchecked tasks. |
| `plans/2026-03-27-session-pr-attribution.md` | One manual item remains unchecked after the underlying legacy session model was removed. |
| `plans/2026-04-02-local-session-proxy.md` | One rollout item remains unchecked after proxy runtime removal. |
| `plans/2026-04-08-kiro-session-integration.md` | Twenty-four unchecked tasks have no current tracker authority. |
| `plans/2026-04-14-scm-credentials-provider-binding.md` | Two unchecked environment checks do not turn the unimplemented proposal into open work. |
| `plans/2026-04-15-oauth-device-login.md` | Twenty-three unchecked tasks contradict the implemented current device-login contract. |
| `plans/2026-04-16-session-raw-response.md` | Five intentionally pending items depend on a removed session/proxy direction. |
| `plans/2026-04-16-session-stream-raw-response.md` | Two pending items depend on a removed session/proxy direction. |
| `plans/2026-05-21-global-tool-usage-events-page.md` | Four historical commit boxes remain unchecked despite a verified implementation status. |
| `plans/2026-05-21-user-page-cli-self-serve.md` | Twenty-nine unchecked items and older UX conflict with the current onboarding flow. |
| `plans/2026-05-29-history-pages-task-zone-phase1.md` | Explicitly superseded while retaining 57 unchecked items. |
| `plans/2026-06-10-independent-cli-release.md` | Three unchecked items remain despite completed live bridge/release evidence. |
| `plans/2026-06-15-ai-usage-center-lifecycle-home.md` | Three unchecked items remain despite its implemented status and later UX replacement. |
| `plans/2026-06-16-ai-usage-center-group-quota.md` | Thirty-two unchecked design tasks remain despite its implemented runtime-adjustment status. |
| `plans/2026-07-10-multi-stage-quota-reset-approval.md` | One browser item remains unchecked although the current contract and later workflow are implemented. |
| `plans/2026-07-14-work-items-performance.md` | Obsolete pre-PR #160 integration/main/production status. |
| `plans/2026-07-15-admin-users-sql-department-filtering.md` | Obsolete pre-PR #160 integration/main/production status. |
| `plans/2026-07-15-bounded-correlated-http-runtime.md` | Obsolete pre-PR #160 integration/main/production status. |
| `plans/2026-07-15-directory-sync-run-pagination.md` | Obsolete pre-PR #160 integration/main/production status. |
| `plans/2026-07-15-embedded-app-shell-performance.md` | Obsolete pre-PR #160 integration/main/production status. |
| `plans/2026-07-15-events-sql-pagination.md` | Obsolete pre-PR #160 integration/main/production status. |
| `plans/2026-07-15-parallel-route-identity-hydration.md` | Obsolete pre-PR #160 integration/main/production status. |
| `plans/2026-07-15-personal-usage-snapshots.md` | Obsolete pre-PR #160 integration/main/production status. |
| `plans/2026-07-15-pr-list-bulk-freshness.md` | Obsolete pre-PR #160 integration/main/production status. |
| `plans/2026-07-16-observability-performance.md` | Obsolete pre-PR #160 integration/main/production status. |
| `plans/2026-07-16-quota-reset-queues-performance.md` | Obsolete pre-PR #160 integration/main/production status. |
| `plans/2026-07-16-repository-inventory-performance.md` | Obsolete pre-PR #160 integration/main/production status. |
| `plans/2026-07-16-representative-scope-performance.md` | Obsolete pre-PR #160 integration/main/production status. |
| `plans/2026-07-16-settings-provider-performance.md` | Obsolete pre-PR #160 integration/main/production status. |
| `plans/2026-07-16-team-member-metadata-performance.md` | Obsolete pre-PR #160 integration/main/production status. |
| `plans/2026-07-16-team-members-performance.md` | Obsolete pre-PR #160 integration/main/production status. |
| `plans/2026-07-16-team-organization-performance.md` | Obsolete pre-PR #160 integration/main/production status. |
| `plans/2026-07-16-team-summary-performance.md` | Obsolete pre-PR #160 integration/main/production status. |
| `plans/2026-07-16-team-trend-performance.md` | Obsolete pre-PR #160 integration/main/production status. |
| `plans/2026-07-17-directory-effective-hierarchy.md` | Obsolete pre-PR #160 integration/main/production status. |
| `plans/2026-07-17-independent-team-summary-cold-path.md` | Obsolete pre-PR #160 integration/main/production status. |
| `plans/2026-07-18-admin-users-persisted-effective-hierarchy.md` | Says no `main` merge/production verification after PR #160 and Issue #136 closed. |
| `plans/2026-07-18-production-read-cache-observability.md` | Intermediate-branch completion is superseded by PR #160 and production qualification. |
| `plans/2026-07-18-sub2api-team-summary-range-fallback.md` | Staging-only status is superseded by the integrated/released performance stack. |
| `plans/2026-07-18-team-members-independent-origin.md` | Intermediate-branch completion is superseded by PR #160 and production qualification. |
| `plans/2026-07-18-team-trend-independent-origin.md` | Intermediate-branch completion is superseded by PR #160 and production qualification. |
| `plans/2026-07-19-admin-users-hierarchy-cleanup.md` | Says no `main` merge/release/production verification after PR #160 and Issue #136 closed. |
| `plans/2026-07-19-performance-contract-reconciliation.md` | Says no `main` merge/release/deployment after PR #160 and preview.74. |
| `plans/2026-07-19-team-organization-bounded-origin.md` | Says no `main` merge/release/production verification after PR #160 and Issue #136 closed. |
| `plans/2026-07-19-team-overview-split-adapter.md` | Says #136/#137 remain when both are closed and the adapter was removed by PR #198. |
| `plans/2026-08-11-codex-commit-token-attribution-v2.md` | Three unchecked T13/#252 cleanup items remain after Issue #252 and PRs #330/#332 completed. |
| `plans/2026-08-12-v2-reporting-onboarding.md` | Four unchecked Issue-ledger items remain after the implementation merged through PR #235 and later v2 cleanup. |

## Genuinely open work (0)

No corpus file is a current work authority. The known Quota Reset pagination
gap appears inside the live list/pagination contract, but execution is owned by
[Issue #348](https://github.com/LichKing-2234/ai-efficiency/issues/348), not by a
local plan. The documentation-migration and architecture work in open Issues
#393 and #407-#412 was created after this corpus and likewise remains tracker
state rather than a local execution ledger.

## Limitations

- This is a lifecycle inventory, not a line-by-line semantic proof that every
  retained contract clause matches runtime behavior. Candidate contracts need
  focused code-owner review during migration.
- Hosted `origin/main` is the audit boundary. The detached working checkout had
  unrelated dirty files and two additional untracked plans; they were not read
  into, counted in, or classified by this report.
- Production runtime was not queried. Release and production statements are
  accepted only where first-party Issues, PRs, release objects, and current
  repository documentation already record them.
- No migration, rename, deletion, or rewrite of existing files is authorized by
  this research artifact.
