# Codex 0.149.1 Commit Allocation Failure

**Date:** 2026-08-26

**Question:** Why do recent Codex 0.149.1 patch turns produce no commit allocation even when trusted HTTP Request IDs are present?

**Direction:** Artifact-first. Extend the exact generated-wrapper contract; LoongSuite is not needed for this failure.

## Answer

The current generated `custom_tool_call` shape binds both the patch literal and the patch result, then emits the result directly:

```js
const patch = "<ONE_JSON_STRING_PATCH>";
const result = await tools.apply_patch(patch);
text(result);
```

The scanner accepts two other exact shapes: the older `const patch = ...; text(await tools.apply_patch(patch));` wrapper and an inline result wrapper that ends in `text(JSON.stringify(result))`. Its anchored inline regular expression does not accept `text(result)` ([parser patterns](https://github.com/LichKing-2234/ai-efficiency/blob/e6a73f41f5096dbf860cd1df61fe0d67d00f2865/ae-cli/internal/attributionlocal/claims_v2.go#L40-L45), [extraction](https://github.com/LichKing-2234/ai-efficiency/blob/e6a73f41f5096dbf860cd1df61fe0d67d00f2865/ae-cli/internal/attributionlocal/claims_v2.go#L762-L785)).

That mismatch occurs before path canonicalization, patch replay, or Git comparison. `v2StructuredPatchInput` returns an empty patch, so the response item contributes no mutation ([call site](https://github.com/LichKing-2234/ai-efficiency/blob/e6a73f41f5096dbf860cd1df61fe0d67d00f2865/ae-cli/internal/attributionlocal/claims_v2.go#L525-L539)). Candidate construction then records `missing_structured_mutation`; allocation creation is reachable only after a non-empty, valid mutation survives deterministic commit comparison ([candidate construction](https://github.com/LichKing-2234/ai-efficiency/blob/e6a73f41f5096dbf860cd1df61fe0d67d00f2865/ae-cli/internal/attributionlocal/claims_v2.go#L650-L679)). Request correlation can therefore succeed while `commit_allocations` stays empty.

This is the same class of generated-wrapper drift repaired in [PR #325](https://github.com/LichKing-2234/ai-efficiency/pull/325), but with a new exact output expression. That earlier repair added `text(JSON.stringify(result))` and bumped scanner progress; it did not cover direct `text(result)`.

## Tight Reproduction

A temporary sanitized test used the actual scanner seam, a real temporary Git repository and commit, a synthetic successful HTTP Request row, and this real-shape wrapper. It contained no real user data.

```text
go test ./internal/attributionlocal \
  -run '^TestResearchCodex01491CurrentApplyPatchWrapper$' -count=3

FAIL (3/3)
RequestIDs: [client:request-current-wrapper]
CommitAllocations: []
GapReason: missing_structured_mutation
```

The loop is deterministic, agent-runnable, and takes about four seconds for three repetitions. It asserts the exact symptom: trusted Request evidence exists but no uploadable commit allocation is produced.

Replacing only the result-handling statements with the older accepted `text(await tools.apply_patch(patch))` form made the same fixture pass 3/3. The repository, commit, Request evidence, patch binding, patch literal, and expected file content were unchanged.

## First-Party Artifact Check

The installed runtime was `codex-cli 0.149.1`. A read-only query over operator-local Codex JSONL dated 2026-08-24 through 2026-08-26 selected only sessions whose metadata belonged to the AI Efficiency workspace. Before aggregation it classified only wrapper grammar; it did not emit or retain patch content, prompts, code, paths, thread IDs, turn IDs, or Request IDs.

| Sanitized grammar class | Tool-call rows |
| --- | ---: |
| older direct-await wrapper | 378 |
| patch variable, then bound result passed directly to `text` | 156 |
| result property passed to `text` | 12 |
| bound result passed through `JSON.stringify` | 4 |
| inline argument, then bound result passed directly to `text` | 2 |
| other exact-unclassified wrappers | 4 |

These are tool-call rows, not unique turns or commits. The count is evidence that direct result emission is a normal first-party artifact shape, not evidence that every row should be accepted. Only the exact, single-call shape described below should graduate into the contract.

## Hypotheses Tested

| Hypothesis | Probe | Result |
| --- | --- | --- |
| Wrapper recognition rejects the current shape | Run the current direct-result wrapper through `ScanCodexV2ClaimsFromHome` | Confirmed: `missing_structured_mutation`, with Request ID present and no allocation, 3/3 |
| Absolute or canonical paths are the failure | Use the accepted wrapper and change only the patch header to a repository-internal absolute path | Rejected as cause: relative and absolute/canonical variants both produced one uploadable allocation, 3/3 |
| Patch replay is the failure | Run parent-to-commit update replay and two ordered updates in one commit | Rejected as cause: both existing regressions passed 3/3 ([replay tests](https://github.com/LichKing-2234/ai-efficiency/blob/e6a73f41f5096dbf860cd1df61fe0d67d00f2865/ae-cli/internal/attributionlocal/claims_v2_test.go#L1239-L1269)) |
| Multiple commits in one turn are unsupported | Run separate-file and same-file sequential allocation regressions | Rejected as cause: both passed 3/3; the state retained two ordered allocations ([same-file sequence](https://github.com/LichKing-2234/ai-efficiency/blob/e6a73f41f5096dbf860cd1df61fe0d67d00f2865/ae-cli/internal/attributionlocal/claims_v2_test.go#L1341-L1384)) |
| Scanner progress causes the initial miss | Run the failing fixture directly through the scanner, bypassing hook progress | Rejected as the initial cause: the direct parser seam still failed 3/3 |

Path handling is deterministic: an absolute path is accepted only after it resolves inside the repository and becomes repository-relative; an outside or parent traversal path remains invalid ([canonical path](https://github.com/LichKing-2234/ai-efficiency/blob/e6a73f41f5096dbf860cd1df61fe0d67d00f2865/ae-cli/internal/attributionlocal/claims_v2.go#L957-L979)). Patch replay remains content-based against the commit parent/current tree, not time or path proximity ([mutation replay](https://github.com/LichKing-2234/ai-efficiency/blob/e6a73f41f5096dbf860cd1df61fe0d67d00f2865/ae-cli/internal/attributionlocal/claims_v2.go#L788-L940)).

## Smallest Contract Direction

Amend the active structured-mutation contract to accept exactly this additional generated shape:

```js
const <patch_identifier> = "<ONE_JSON_STRING_PATCH>";
const <result_identifier> = await tools.apply_patch(<same_patch_identifier>);
text(<same_result_identifier>);
```

Keep all existing fail-closed constraints:

- anchor the whole wrapper;
- require exactly one `tools.apply_patch` call and one JSON string literal;
- require the same patch identifier in the literal binding and `apply_patch(...)` call;
- require the same result identifier in the awaited binding and `text(...)` call;
- continue rejecting comments, template literals, multiple calls, mismatched identifiers, property access such as `text(result.output)`, and trailing control flow;
- require the decoded value to have exact `*** Begin Patch` / `*** End Patch` framing;
- retain repository path containment, patch replay, and deterministic parent/current Git-content proof before allocation.

This is a narrow parser-contract extension, not a heuristic. It preserves the active spec's principle that a recognized wrapper never substitutes for deterministic Git proof ([current contract](https://github.com/LichKing-2234/ai-efficiency/blob/e6a73f41f5096dbf860cd1df61fe0d67d00f2865/docs/superpowers/specs/2026-08-11-codex-commit-token-attribution-v2-design.md#L170-L203)).

The implementation must also increment scanner progress from v5 to v6. Current completed `source x trigger` units are skipped when trusted Request evidence is unchanged ([runner skip](https://github.com/LichKing-2234/ai-efficiency/blob/e6a73f41f5096dbf860cd1df61fe0d67d00f2865/ae-cli/internal/hooks/background_runner.go#L535-L545)); an older progress version is what clears completed units and causes one exact rescan ([migration](https://github.com/LichKing-2234/ai-efficiency/blob/e6a73f41f5096dbf860cd1df61fe0d67d00f2865/ae-cli/internal/hooks/v2_scan_progress.go#L78-L97)). The active spec already requires a version bump whenever scanner semantics can alter claim classification ([progress contract](https://github.com/LichKing-2234/ai-efficiency/blob/e6a73f41f5096dbf860cd1df61fe0d67d00f2865/docs/superpowers/specs/2026-08-11-codex-commit-token-attribution-v2-design.md#L300-L325)).

## Required Regression Boundary

The follow-up implementation should cover:

1. patch-variable/direct-result wrapper acceptance with relative and repository-internal absolute patch paths;
2. add, update, delete, ordered same-file updates, and a turn allocated across multiple commits;
3. rejection of mismatched result identifiers, property access, comments, template literals, malformed framing, multiple patch calls, and trailing statements;
4. unchanged deterministic Git mismatch rejection;
5. v5-to-v6 one-time rescan and idempotent replay after acceptance;
6. an installed-runner HTTP Request-to-checkpoint-to-backend ACK readback.

## Scope Boundary

No backend schema, Activity API, Token authority, Request correlation, or LoongSuite integration is needed for this failure. This direction can recover retained HTTP turns that already have trusted Request IDs and deterministic commit content. It cannot synthesize expired evidence and does not make the unsupported remote-control WebSocket transport trustworthy; that transport still requires a separate exact transport, success, and turn-identity contract ([explicit non-goal](https://github.com/LichKing-2234/ai-efficiency/blob/e6a73f41f5096dbf860cd1df61fe0d67d00f2865/docs/superpowers/specs/2026-08-11-codex-commit-token-attribution-v2-design.md#L925-L941)).
