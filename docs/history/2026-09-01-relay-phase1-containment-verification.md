# Relay Planning Phase 1 Containment Verification

## Scope

This record verifies the independently releasable Phase 1 production-safety
containment for GitHub issue #440. It covers the merged range from
`224a1cab873c830440147edc5c1fee9214205850` through
`4bde40ffcae9ab341b9186858d041cafc34ec469`:

- `2b327c85` / PR #451: distinguish a healthy migrated Replan roster from
  managed Target relationship drift;
- `0e034d12` / PR #453: bind legacy retry reuse to exact directional identity
  and fresh Relay readback;
- `4bde40ff` / PR #454: expose exact continuation versus manual intervention in
  the current UI and lock unrelated mutation surfaces.

The verification used only synthetic users, Groups, Accounts, API-Key IDs, and
fake Relay Provider behavior. No production relationship was changed or
intentionally broken.

## Incident Reproduction

The focused handler and service suites reproduced both sides of the incident:

- a saved member with the expected Target subscription and reviewed Target Key,
  but no Migration Source subscription, remains selected without Source or
  generic no-eligible warnings;
- missing managed Target subscription or reviewed Target Key is categorized and
  blocks Confirm before a Relay write;
- a completed forward direction cannot satisfy a reviewed reverse direction;
- an exact same-direction retry reuses a successful step only when request-bound
  readback still proves its expected subscription, Source, and Key result;
- incomplete legacy identity, changed intent, and reviewed-Key readback outside
  Source/Target fail closed before Relay writes;
- Account adoption/edit, Rebind, and Mapping Renewal Confirm remain blocked
  while the legacy operation is unresolved.

Commands and results:

```text
go test ./internal/relayplanning -run TestExecuteReplanRetriesFailedAPIKeyMoveFromPreviousTarget -count=1
PASS

go test ./internal/handler -run 'TestRelayPlanning(ReplanDistinguishesMigratedBaselineFromTargetDrift|ExplicitRemovalRetryDoesNotRepeatCompletedWrites|UnresolvedLegacyOperationBlocksUnrelatedMappingMutations)' -count=1
PASS

go test -race ./internal/relayplanning -run TestExecuteReplanRetriesFailedAPIKeyMoveFromPreviousTarget -count=1
PASS

go test -race ./internal/handler -run 'TestRelayPlanning(ReplanDistinguishesMigratedBaselineFromTargetDrift|ExplicitRemovalRetryDoesNotRepeatCompletedWrites)' -count=1
PASS
```

## Release Gates

```text
cd backend && go test -p 1 ./... -count=1
PASS

cd backend && go vet ./...
PASS

cd backend && go build ./...
PASS

cd frontend && npm test
PASS: 62 files, 872 tests

cd frontend && npx vue-tsc -b --pretty false
PASS

cd frontend && npm run build:measure
PASS: structural assertions and existing bundle ceilings

AE_E2E_BASE_URL=http://127.0.0.1:5173 npm run test:e2e:role
PASS: 126/126 across 390, 768, 1024, 1280, and 1440 pixel viewports

git diff --check 224a1cab873c830440147edc5c1fee9214205850..4bde40ffcae9ab341b9186858d041cafc34ec469
PASS
```

The first role-E2E attempt reported one transient timeout at `/usage` on the
390-pixel viewport while the other 125 cases passed. An unchanged rerun against
the same Vite server passed 126/126. This is recorded separately from the Relay
Planning focused evidence.

The Phase 1 range contains no changes under `backend/ent/schema`, Ent migration
assets, or deploy assets.

Measured gzip aggregates remained within the enforced ceilings:

| Aggregate | Result | Ceiling |
| --- | ---: | ---: |
| Initial shell | 72,756 bytes | 76,800 bytes |
| Default English `/usage` | 158,596 bytes | 163,840 bytes |
| Default English `/admin/users` | 255,221 bytes | 262,144 bytes |

## Boundary

Phase 1 contains the current production failure modes. It does not implement or
claim any of the following Phase 2 guarantees:

- independent durable Relationship Operation entities or attempt history;
- delayed atomic Replan Baseline promotion;
- Restore Replan Baseline;
- durable Resume/Restore recovery APIs or lifecycle UI;
- restart recovery at every managed write boundary;
- affected-Mapping ownership rows or distributed concurrency ownership.

Those guarantees remain tracked by #445 through #450 and must not be inferred
from this verification record.
