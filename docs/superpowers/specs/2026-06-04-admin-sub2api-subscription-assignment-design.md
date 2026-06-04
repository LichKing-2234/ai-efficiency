# Admin Sub2API Subscription Assignment Design

**Date:** 2026-06-04  
**Status:** Implemented current contract  
**Scope:** `backend/internal/handler/`, `backend/internal/relay/`, `backend/internal/usersetup/`, `frontend/src/views/admin/`, `frontend/src/views/UserView.vue`, `frontend/src/api/`, `frontend/src/types/`, `docs/architecture.md`  
**Related:**  
- [2026-05-26-admin-users-local-credentials-design.md](./2026-05-26-admin-users-local-credentials-design.md)  
- [2026-05-21-user-page-cli-self-serve-design.md](./2026-05-21-user-page-cli-self-serve-design.md)  
- [2026-03-24-oauth-cli-login-design.md](./2026-03-24-oauth-cli-login-design.md)  
- [docs/architecture.md](../../architecture.md)

## Spec Relationship

The 2026-05-26 admin users spec defined the first `/admin/users` surface as local-user inspection plus explicit relay password reveal. This spec extends that surface with a narrow admin mutation: assigning an existing sub2api subscription group to an already mapped relay user.

This does not change LDAP bind password handling, relay password reveal rules, provider CRUD, `/user` self-serve group visibility, or direct sub2api database coupling rules. All sub2api changes still go through `backend/internal/relay.Provider` or optional relay adapter interfaces.

## Goals

1. Let admins centrally assign sub2api subscription groups from `/admin/users`.
2. Keep the local users list backed by the local `users` table.
3. List assignable subscription groups from enabled DB-backed relay providers through the relay adapter.
4. Assign subscriptions only for local users that already have `relay_user_id`.
5. Keep assignment separate from plaintext relay password reveal.
6. Prevent quick repeated `/user` create-key clicks from creating duplicate sub2api API keys.

## Non-Goals

1. No bulk assignment workflow in this iteration.
2. No local user role, auth source, relay binding, or password edit workflow.
3. No relay API key inventory or usage display on `/admin/users`.
4. No sub2api source-code change or direct DB coupling.
5. No attempt to make cross-process API key creation globally serialized.

## Backend Contract

### List Assignment Options

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

### Assign Subscription

```text
POST /api/v1/admin/users/:id/subscriptions
```

Request:

```json
{
  "provider_id": 1,
  "group_id": "42",
  "validity_days": 30
}
```

Rules:

1. The local user must exist.
2. The local user must already have a positive `relay_user_id`.
3. The relay provider must support explicit subscription assignment.
4. The backend calls sub2api `POST /api/v1/admin/subscriptions/assign` through `backend/internal/relay`.
5. Existing subscription conflicts from sub2api remain idempotent success when the conflict reason is `SUBSCRIPTION_ASSIGN_CONFLICT` or equivalent.
6. Assignment mutates relay subscription state only; the local user row is not edited.

## Duplicate Create-Key Guard

`usersetup.Service.CreateGroupCredential` is now idempotent per local user, relay provider, and group inside the backend process:

1. Acquire a process-local lock keyed by `user_id:provider_id:group_id`.
2. Ensure relay user binding and write credentials as before.
3. Re-list current user API keys.
4. If the managed key already exists for the selected group, return it as the mutation result.
5. Otherwise create one key with the current user's relay write credentials.

The frontend also disables the create button while the create request is in flight. The backend guard is still required because browser UI state does not protect real concurrent HTTP requests.

## Frontend Contract

`/admin/users` adds a compact row-level assignment control:

1. Select relay provider.
2. Select subscription group.
3. Enter positive validity days, defaulting to 30.
4. Click Assign.
5. Show row-level success or error text.

The control is disabled when the user has no `relay_user_id` or when assignment options are unavailable.

## Testing

Backend tests cover:

1. Subscription option listing filters to assignable groups.
2. Assignment calls the relay adapter with the selected provider, relay user, group, and validity.
3. sub2api adapter posts the selected assignment body.
4. Concurrent create-key calls for the same local user, provider, and group create only one relay key and return the same result.

Frontend tests cover:

1. Admin users view renders assignment controls and posts selected values.
2. User setup disables create key while the request is in flight and does not fire a second create request.
