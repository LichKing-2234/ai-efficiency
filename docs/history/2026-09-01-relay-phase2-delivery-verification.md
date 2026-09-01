# Relay Planning Phase 2 Delivery Verification

## Scope

This record closes the implementation verification for GitHub issue #450 and
the Phase 2 range beginning with PR #456. It uses only synthetic users, Groups,
Accounts, API-Key IDs, and fake Relay Provider behavior. It does not require or
perform a destructive production relationship test.

## Deterministic Proof Matrix

The delivery suite restarts a newly constructed service after each managed
boundary: Operation persistence, subscription assignment, API-Key binding,
Source removal, Account update, existing-Group rename, new-Group creation,
complete Relay readback, and Mapping baseline promotion. Each case must finish
with verified Target readback and the expected terminal Operation lifecycle.

Additional focused cases prove:

- a dropped API-Key bind response is resolved by readback without replaying the
  already-applied write;
- Restore ignores prior Resume step evidence, re-reads baseline facts, and
  records a separate attempt without overwriting the failed initial attempt;
- two opposite cross-Mapping ownership requests cannot deadlock or partially
  acquire ownership, while an unrelated Mapping remains writable;
- a PostgreSQL trigger-induced failure rolls back destination and source
  baseline promotion together, and restart later promotes both together;
- missing reviewed external resources block before any recovery write and do
  not create a replacement;
- the existing migration, handler, and frontend suites retain exact legacy
  classification, authorization, stale-confirm, and recovery-dialog behavior.

## Verification State

Local non-database gates completed as follows:

```text
cd backend && go test ./... -run '^$' && go vet ./... && go build ./...
PASS

cd frontend && npm test
PASS: 62 files, 874 tests

cd frontend && npm run build
PASS

AE_E2E_BASE_URL=http://127.0.0.1:5187 npm run test:e2e:role
PASS: 126/126 across 390, 768, 1024, 1280, and 1440 pixel viewports

git diff --check
PASS
```

The first unchanged role-E2E run reported the same transient `/usage` 390-pixel
wait timeout previously recorded for Phase 1 while the other 125 cases passed.
An immediate unchanged rerun against the same Vite process passed 126/126.

The local focused database and race run is environment-blocked because the
local PostgreSQL service reports `could not write init file`; no local database
or container data was deleted to bypass that condition. The Hosted PR checks
are the authoritative PostgreSQL-backed execution evidence and will be recorded
here before merge.

## Remaining Resume-only Effects

New Group creation cannot restore a previously nonexistent Group without an
unsafe delete, so Operations containing that effect advertise Resume only.
Ambiguous legacy state likewise exposes no recovery direction and remains a
manual-review external blocker. These are explicit contract limits, not missing
retry paths.
