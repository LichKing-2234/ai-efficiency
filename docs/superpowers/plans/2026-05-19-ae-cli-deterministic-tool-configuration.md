# ae-cli Deterministic Tool Configuration Plan

**Status:** Completed in this rollout

**Goal:** Add a real `ae-cli` command that can configure supported local AI tools (`codex`, `claude`, `gemini`) from provider-delivered credentials, and fix the backend `/api/v1/providers` handler bug that blocked live CLI usage.

## Steps

- [x] Add failing tests for provider fetch, tool detection, tool config writers, and the discover command flow.
- [x] Implement `ae-cli discover` plus the `ae-cli/internal/toolconfig` package.
- [x] Add `GET /api/v1/providers` client parsing in `ae-cli/internal/client`.
- [x] Fix `backend/internal/handler/provider.go` to read the authenticated user from `auth.GetUserContext`.
- [x] Update CLI docs and add a current-contract spec for deterministic tool configuration.
- [x] Run `cd ae-cli && go test ./...`.
- [x] Run `cd backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./internal/handler -run TestListProvidersForUserWithValidToken -v`.
- [x] Run a mock end-to-end `ae-cli discover` execution against a local stub `/api/v1/providers` server and verify the generated files under a temporary `HOME`.

## Known Remaining Gaps

- The existing live process behind `http://localhost:18081` was still serving the pre-fix backend binary during this rollout, so a real-machine `ae-cli discover --dry-run` against that endpoint continued to return the old `/api/v1/providers` `500` until that backend is restarted with the new code.
