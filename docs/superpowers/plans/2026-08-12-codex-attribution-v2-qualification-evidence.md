# Codex Attribution v2 Qualification Evidence

**Date:** 2026-08-12  
**Status:** Local non-canary qualification complete; hosted CI, final review, and separately authorized real canary remain  
**Ticket:** [#250](https://github.com/LichKing-2234/ai-efficiency/issues/250)

This record maps the #250 acceptance criteria to executable evidence. Synthetic
fixtures use only example identities and fake Relay providers. No production
endpoint, release, deployment, cutover, or data reset was used.

## Contract And Failure Matrix

| Requirement | Authoritative evidence |
| --- | --- |
| Shadow epoch cannot affect v1 or formal Activity/readiness | `TestSyntheticRequestToActivityKeepsShadowEpochIsolated`, `TestV2ClaimHTTPReplayAuthorizationAndEpochIsolation`, `TestV2OverviewCountsPoolsOnceAndClampsRatioToUsageAsOf` |
| Request-to-commit-to-official-Token-to-pool-to-Activity chain | `TestSyntheticRequestToActivityKeepsShadowEpochIsolated` exercises claim ingest, fake `relay.RequestUsageReader`, reconciliation, pool materialization, formal exclusion, and shadow readback in one database |
| Multi-Request turn and late Request | `TestScanCodexV2ClaimsMultiRequestStableArchiveRecoveryAndPrivacy`, `TestIngestReplayAndLateRequest`, `TestApplyV2ClaimAcknowledgementsRetainsDigestOnlyAuditAndAcceptsLateRequest` |
| Provider switch and account isolation | `TestScanCodexV2ClaimsFailsClosedOnCommitMismatchAndProviderSwitch`, `TestRunOnceRejectsClaimGroupProviderMismatch`, `TestRunOnceRevalidatesOwnerAfterLookup`, `TestRunOnceRevalidatesProviderAfterLookup` |
| Duplicate, conflict, and response loss | `TestIngestConflictRollsBackIndependentGroup`, `TestIngestReplayAndLateRequest`, `TestRunV2ClaimSyncPreservesLocalStateOnResponseLoss`, `TestApplyV2ClaimAcknowledgementsPreservesUnknownResponse` |
| Partial, outage, expiry, and exact 24-hour final lookup boundary | `TestRunOnceFinalizesPartialGroupAtExactSafetyBoundary`, `TestFinalizationWaitsUntilSafetyBoundaryAndForActiveLease`, `TestRetryIsCappedAtFinalAttemptDeadline`, `TestFinalizationWaitsForEveryBatchedFinalAttempt`, `TestLateRequestInNearExpiryGroupGetsImmediateFinalAttempt`, `TestProviderOutageAtFinalBoundaryBecomesCoverageGap` |
| Multi-replica one-flight and idempotent materialization | `TestRunOnceLeaseCollapsesConcurrentReplicas`, `TestLateWorkerCannotOverwriteRenewedLease`, `TestConcurrentMaterializationCountsEachClaimOnce`, `TestReconciliationSerializesWithAllocationRematerialization` |
| Direct-to-shared conservation | `TestMaterializeGroupMigratesDirectToSharedWithoutRecounting`, `TestPRProjectionsKeepOneDistinctPoolScopeTotal`, `TestV2RepositoryAndPRPagesKeepSharedValuesNonAdditive` |
| Rewrite, orphan, and cherry-pick conservation | `TestApplyRewriteMigratesPostRetentionPoolWithoutRequestRows`, `TestApplyRewriteResolvesOutOfOrderChainToTerminalCommit`, `TestMarkCommitOrphanedPreservesTokenAndRequiresAuthority`, `TestApplyCherryPickAddsInheritedRelationWithoutRecounting` |
| UTC, Shanghai, DST, and quarter-hour local days | `TestV2OverviewCountsPoolsOnceAndClampsRatioToUsageAsOf`, `TestPreviousV2WindowUsesLocalCalendarDays`, `TestV2RepositoryAndPRPagesKeepSharedValuesNonAdditive` (Asia/Kathmandu) |
| Scale and query plans | `TestV2RepositoryPageUsesPoolRangeAndCommitLookupIndexes` forbids sequential pool/commit scans at 2,000 rows; `TestV2ReadPathsStayWithinScaleLatencyBudget` captures and `EXPLAIN ANALYZE`s the production summary, trend, Repository/PR ranking, search/name-sort, and cursor-page SQL at 2,500 pools, enforcing at most 30,000 scanned rows and 2 seconds per read |
| Denominator exactness and provider-set completeness | `TestActivityDenominatorUsesExactFreshPersonalUsage`, `TestActivityDenominatorRejectsStaleOrScopeMismatchedUsage`, `TestActivityDenominatorFailsClosedForUncoveredProviderSet`, `TestV2MemberDenominatorCacheIsAuthorizationAndProviderIsolated`; `TestActivityDenominatorResolverStaysWithinScaleBudgets` exercises the real resolver, provider/binding queries, Usage reader boundary, and member cache twice against 2,501 providers with ceilings of 2 seconds, 4 SQL queries, and 2,501 scanned rows |
| Exact/lower-bound/unavailable/no-Usage/zero and comparison omission | `TestV2RatioStates`, `TestV2OverviewKeepsActivityWhenDenominatorErrors`, `TestV2OverviewReturnsExactAdjacentPercentagePointChange`, Activity view ratio-state table tests |
| Personal/member/team/admin/denied authorization | `TestV2ScopeAuthorizationIsRevalidatedForMemberAndTeam`, rendered Activity team/member tests, and the 126-case role E2E matrix |
| 7/30/90/custom, URL recovery, races, local lane failures | Rendered Activity page preset tests verify URL and API orchestration; custom validation plus Activity view tests cover member URL state, Repo/PR filters, PR commit expansion, superseded responses, independent ratio/trend lanes, refresh failure, search/sort/cursor, and tab restoration |
| Desktop/mobile/accessibility and no overflow | role E2E at 390/768/1024/1280/1440, native button/ARIA state tests, table/card boundary tests, and no-horizontal-overflow assertions |
| No Request/claim/calibration/operational detail in product reads | `TestV2ProductDTOContainsNoRequestDetail`, Activity rendered text assertions, and role E2E Activity omission check |

## Local Candidate Gates

All commands below passed in the isolated #250 worktree on 2026-08-12:

```text
backend: go test ./...
backend: go test -race ./internal/activity ./internal/attributionclaim ./internal/attributionpool ./internal/attributionreconcile ./internal/handler
backend: go test -count=5 ./internal/activity ./internal/attributionclaim ./internal/attributionpool ./internal/attributionreconcile
backend: go vet ./...
ae-cli:  go test ./...
frontend: npm test                           56 files, 759 tests
frontend: npm run build:measure              structural assertions PASS
frontend: Node 20 build measurement          initial shell 72,695 / 72,800 bytes
frontend: npm run test:e2e:role              126/126
```

The production-query scale gate completed locally with 2,500 pools under its
30,000-row scan and 2-second latency budgets for every named read path. The
real denominator resolver completed two member resolutions against 2,501
providers under its 2,501-row, 4-query, and 2-second budgets, and the second
resolution reused the authorization/provider-isolated cache.

## Cutover Artifacts

The companion
[Codex Attribution v2 Cutover Runbook](./2026-08-12-codex-attribution-v2-cutover-runbook.md)
contains:

- hard go/no-go gates;
- Prometheus dashboard queries and alert conditions;
- exact v1 bucket/revision export plus SHA-256 manifest procedure;
- dependency-ordered, transactional v1 reset SQL with post-delete assertion;
- ordered cutover readbacks;
- forward-only rollback rules.

## Remaining Gates

- [ ] Hosted CI is green at the final PR head.
- [ ] Standards and spec reviews have no P0/P1 findings.
- [ ] A separately authorized real Request-to-commit-to-Activity canary passes
  without entering the formal epoch.

Until all three are complete, #251 remains blocked and this document is not
cutover authorization.
