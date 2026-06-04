# Admin Sub2API Subscription Management Design

**Date:** 2026-06-04
**Status:** Implemented current contract
**Scope:** `backend/internal/handler/`, `backend/internal/relay/`, `backend/internal/usersetup/`, `frontend/src/views/admin/`, `frontend/src/views/UserView.vue`, `frontend/src/api/`, `frontend/src/types/`, `docs/architecture.md`
**Related:**
- [2026-05-26-admin-users-local-credentials-design.md](./2026-05-26-admin-users-local-credentials-design.md)
- [2026-05-21-user-page-cli-self-serve-design.md](./2026-05-21-user-page-cli-self-serve-design.md)
- [2026-03-24-oauth-cli-login-design.md](./2026-03-24-oauth-cli-login-design.md)
- [docs/architecture.md](../../architecture.md)

## Spec Relationship

The 2026-05-26 admin users spec defined `/admin/users` as local-user inspection plus explicit relay password reveal. This spec extends that admin surface with centralized sub2api subscription management.

The initial 2026-06-04 implementation added row-level single-user assignment. The current contract supersedes that row-level UX with one selected-scope workflow that supports one user, multiple selected users, the current filter result, and all relay-mapped users. The old single-user assignment API remains for compatibility, but the frontend uses the batch management API.

This does not change LDAP bind password handling, relay password reveal rules, provider CRUD, `/user` self-serve group visibility, or direct sub2api database coupling rules. All sub2api mutations still go through `backend/internal/relay.Provider` plus optional relay adapter interfaces.

## Goals

1. Let admins manage sub2api subscriptions from `/admin/users` for one user, multiple selected users, the current filter result, or all relay-mapped users.
2. Support three operations: add a subscription group, extend an existing subscription, and remove an existing subscription.
3. Keep the local users list backed by the local `users` table.
4. List assignable subscription groups from enabled DB-backed relay providers through the relay adapter.
5. Treat unmapped local users as per-user skipped results in batch operations instead of blocking the whole batch.
6. Keep subscription management separate from plaintext relay password reveal.
7. Prevent quick repeated `/user` create-key clicks from creating duplicate sub2api API keys.

## Non-Goals

1. No local user role, auth source, relay binding, or password edit workflow.
2. No relay API key inventory or usage display on `/admin/users`.
3. No sub2api source-code change or direct DB coupling.
4. No attempt to make cross-process API key creation globally serialized.
5. No async job table for subscription batches; the implemented batch API is synchronous, capped at 500 target users per request, and returns per-user results.

## Backend Contract

### List Subscription Options

```text
GET /api/v1/admin/users/subscription-options
```

Access:

- Requires authenticated admin.

Response:

```json
{
  "providers": [
    {
      "id": 1,
      "name": "sub2api",
      "display_name": "Sub2API",
      "groups": [
        {
          "group_id": "42",
          "group_name": "Group Alpha",
          "platform": "openai",
          "subscription_type": "subscription"
        }
      ]
    }
  ]
}
```

Rules:

1. Only enabled relay providers are listed.
2. Groups come from the provider's admin group list via the relay adapter.
3. Groups with `subscription_type=subscription` are assignable. Empty `subscription_type` is treated as assignable for older relay payloads.
4. Groups with no id or platform are ignored.

### Batch Manage Subscriptions

```text
POST /api/v1/admin/users/subscriptions/batch
```

Request examples:

```json
{
  "scope": "selected",
  "user_ids": [7, 8],
  "operation": "add",
  "provider_id": 1,
  "group_id": "42",
  "validity_days": 30
}
```

```json
{
  "scope": "current_filter",
  "filters": { "q": "alice" },
  "operation": "extend",
  "provider_id": 1,
  "group_id": "42",
  "days": 14
}
```

```json
{
  "scope": "all_mapped",
  "operation": "remove",
  "provider_id": 1,
  "group_id": "42"
}
```

Allowed scopes:

1. `selected`: process the provided local `user_ids`, deduplicated in first-seen order. Positive IDs that no longer exist remain in the result as per-user `failed` rows.
2. `current_filter`: process every local user matching the same search predicate as `/admin/users?q=...`, across all pages.
3. `all_mapped`: process all local users with a positive `relay_user_id`.

Allowed operations:

1. `add`: requires positive `validity_days`; calls sub2api `POST /api/v1/admin/subscriptions/assign`.
2. `extend`: requires positive `days`; the relay adapter first calls sub2api `GET /api/v1/admin/users/:id/subscriptions`, finds the matching `group_id`, then calls `POST /api/v1/admin/subscriptions/:subscription_id/extend`.
3. `remove`: the relay adapter first calls sub2api `GET /api/v1/admin/users/:id/subscriptions`, finds the matching `group_id`, then calls `DELETE /api/v1/admin/subscriptions/:subscription_id`.

Add idempotency:

- Only unambiguous already-existing assignment responses, such as `SUBSCRIPTION_ALREADY_EXISTS`, are treated as successful idempotent assignments.
- Semantic assignment conflicts such as `SUBSCRIPTION_ASSIGN_CONFLICT` with `metadata.conflict_reason` are returned as per-user failures so admins can see mismatched validity or notes instead of a false success.

Response:

```json
{
  "status": "completed",
  "scope": "selected",
  "operation": "add",
  "provider_id": 1,
  "group_id": "42",
  "total_count": 2,
  "success_count": 1,
  "skipped_count": 1,
  "failed_count": 0,
  "results": [
    {
      "user_id": 7,
      "username": "alice",
      "email": "alice@example.com",
      "relay_user_id": 42,
      "status": "success"
    },
    {
      "user_id": 8,
      "username": "bob",
      "email": "bob@example.org",
      "status": "skipped",
      "message": "user is not linked to a relay user"
    }
  ]
}
```

Rules:

1. The relay provider must exist, be enabled, support group listing, and expose the selected group as assignable.
2. Invalid provider/group/operation/scope input fails the whole request.
3. Per-user relay mutation errors become `failed` result rows and do not stop later users in the same batch.
4. Unmapped local users become `skipped` result rows unless the scope is `all_mapped`, which excludes them before execution.
5. `selected` scope reports stale or unknown positive local user IDs as `failed` result rows instead of silently dropping them.
6. Requests with more than 500 target users fail with 422; admins must narrow the filter or selected set.
7. Subscription mutations edit relay/sub2api state only; local user identity fields are not edited.

### Compatibility: Single-User Add

```text
POST /api/v1/admin/users/:id/subscriptions
```

The old single-user assignment endpoint remains available for compatibility. It validates the same enabled provider and assignable group constraints, requires the local user to have a positive `relay_user_id`, and calls the same relay add operation. The current frontend does not use this endpoint.

## Duplicate Create-Key Guard

`usersetup.Service.CreateGroupCredential` is idempotent per local user, relay provider, and group inside the backend process:

1. Acquire a process-local lock keyed by `user_id:provider_id:group_id`.
2. Ensure relay user binding and write credentials as before.
3. Re-list current user API keys while holding the keyed lock.
4. If the managed key already exists for the selected group, return it as the mutation result.
5. Otherwise create one key with the current user's relay write credentials.

The frontend also disables create/regenerate buttons while the request is in flight. The backend guard is still required because browser UI state does not protect real concurrent HTTP requests.

## Frontend Contract

`/admin/users` uses one subscription management panel above the local user list:

1. Admins select table rows for single-user or multi-user management.
2. Scope can be `Selected`, `Current filter`, or `All mapped`.
3. Operation can be `Add`, `Extend`, or `Remove`.
4. Provider and subscription group are selected once for the operation.
5. Add uses validity days; extend uses extension days; remove requires explicit confirmation.
6. The result summary shows success, skipped, and failed counts plus per-user result rows.

The local user table no longer contains repeated row-level subscription forms. Mobile uses selectable user cards with the same centralized operation panel.

## Testing

Backend tests cover:

1. Subscription option listing filters to assignable groups.
2. Single-user compatibility assignment calls the relay adapter with the selected provider, relay user, group, and validity.
3. Batch selected scope adds subscriptions, skips unmapped users, and reports stale selected IDs as failed rows.
4. Oversized synchronous batches are rejected before subscription mutations run.
5. Batch current-filter scope extends only matching users.
6. Batch all-mapped scope removes subscriptions for every mapped user.
7. sub2api adapter posts add bodies, resolves existing subscription IDs for extend/remove, returns a not-found error when no matching group subscription exists, and keeps semantic assignment conflicts fatal.
8. Concurrent create-key calls for the same local user, provider, and group create only one relay key and return the same result.

Frontend tests cover:

1. Admin users view renders the centralized subscription management panel.
2. Selecting one user and adding a subscription calls the batch API with `scope=selected` and one `user_id`.
3. Selecting multiple users and extending subscriptions calls the batch API with both `user_ids`.
4. Removing subscriptions for all mapped users requires explicit confirmation before calling the batch API.
5. User setup disables create key while the request is in flight and does not fire a second create request.
