# Configurable Directory Sync Design

**Date:** 2026-06-22
**Status:** Approved design for implementation planning
**Scope:** `backend/internal/directorysync/`, `backend/ent/schema/`, `backend/internal/handler/`, `frontend/src/views/`, `frontend/src/components/settings/`, `frontend/src/api/`, `docs/architecture.md`
**Related:**

- [2026-03-24-oauth-cli-login-design.md](./2026-03-24-oauth-cli-login-design.md)
- [2026-05-26-admin-users-local-credentials-design.md](./2026-05-26-admin-users-local-credentials-design.md)
- [2026-06-04-admin-sub2api-subscription-assignment-design.md](./2026-06-04-admin-sub2api-subscription-assignment-design.md)
- [docs/architecture.md](../../architecture.md)

## Spec Relationship

- This spec adds a configurable organization directory sync capability. It does not replace LDAP login, Relay SSO, relay identity provisioning, `/user` setup, or the existing admin subscription assignment workflow.
- LDAP remains an authentication source. Directory Sync is an organization-facts source used for reporting, filtering, and admin offboarding review.
- AI access assignment and subscription add/extend/remove/reset-quota operations continue to use the existing admin users subscription workflow. Directory Sync must not automatically assign, remove, or reset relay/sub2api subscriptions.
- Offboarding review uses Directory Sync facts to identify local users whose email is missing from the latest full-company directory snapshot. Admins must explicitly confirm any action.
- This spec introduces a reusable local auth-token revocation capability so confirmed offboarding can invalidate existing AI Efficiency access and refresh tokens. The offboarding review page owns the disable action, while the general `/admin/users` table may surface the resulting derived access status for review and filtering.
- Implementation must update `docs/architecture.md` after the module lands because this changes project-level runtime relationships and admin surfaces.

## Data Hygiene

This feature handles organization and employment data. Design docs, tests, fixtures, screenshots, templates, examples, prompts, and command output must not contain real company domains, real employee emails, real department names, real subscription groups, real API keys, real tokens, or real internal URLs.

All examples must use safe placeholder values such as:

- `https://directory.example.com`
- `X-Directory-API-Key`
- `directory_api_key`
- `alice@example.com`
- `bob@example.org`
- `Department Alpha`
- `Department Beta`

The product may help an administrator generate DSL from external API documentation, but built-in templates and tests must stay fully synthetic.

## Problem

The platform currently supports local users, LDAP login, Relay SSO, relay identity provisioning, and admin-managed relay/sub2api subscription jobs. It does not have a first-class organization directory model. Admins therefore cannot:

1. Configure a generic organization source without adding platform-specific code.
2. Sync departments and members for reporting and filtering.
3. Compare local relay-bound users against the latest full-company directory snapshot.
4. Review potential offboarding cases in a dedicated surface.
5. Confirm a controlled action that disables the upstream relay/sub2api user and invalidates the local user's existing AI Efficiency login tokens.

Hard-coding specific vendor directory platforms, LDAP group sync, or a specific private API into the backend would create long-term platform coupling. The system needs a generic, declarative HTTP sync layer.

## Goals

1. Add a configurable directory source model that can express organization APIs through a safe HTTP DSL.
2. Support manually triggered validate, preview, and apply runs.
3. Support scheduled apply runs.
4. Persist current departments and members from the latest complete successful `full_company` apply run.
5. Match directory members to local users by normalized email only.
6. Show organization facts in admin/reporting surfaces after implementation.
7. Add an offboarding review page for local relay-bound users whose email is missing from the latest full-company directory snapshot.
8. Let admins confirm a controlled offboarding action that disables the corresponding relay/sub2api user and revokes local AI Efficiency tokens.
9. Keep subscription assignment and removal under the existing admin subscription workflow. Directory Sync does not automatically mutate subscriptions.
10. Provide safe, synthetic templates and AI prompt helpers so admins can generate DSL without copying real secrets into examples.

## Non-Goals

1. Do not add built-in vendor directory SDK integrations.
2. Do not execute arbitrary JavaScript, shell, jq programs, or plugin code from the DSL.
3. Do not automatically create local users from directory members in the first version.
4. Do not automatically assign, extend, or remove relay/sub2api subscriptions.
5. Do not delete local users based on directory sync results.
6. Do not disable local login from LDAP alone. Confirmed offboarding uses explicit local token revocation.
7. Do not add a visual drag-and-drop workflow builder in the first version.
8. Do not store external API documentation pasted for AI generation.
9. Do not use partial sync results to mark employees as offboarded.
10. Do not implement webhook-driven directory sync in the first version.

## Decisions

| Area | Decision | Reason |
| --- | --- | --- |
| Integration model | Generic declarative HTTP DSL | Avoids platform code while supporting common REST directory APIs |
| Config entry | Advanced JSON/YAML editor with safe templates | Faster and safer than a full visual builder |
| Identity match | Email only | Matches the current LDAP-oriented identity model and keeps local schema impact small |
| Sync execution | Manual validate/preview/apply plus scheduled apply | Keeps directory facts current without platform-specific webhooks |
| Offboarding rule | Missing email from latest complete successful `full_company` apply run | Direct and explainable; failures do not change the risk list |
| Access mutation | Admin-confirmed disable upstream relay/sub2api user plus local token revocation | Stops AI access while preserving manual control |
| Subscription mutation | Existing admin users subscription workflow only | Prevents directory sync from silently changing AI access groups |
| Token invalidation | `users.token_valid_after` | Required because current refresh JWTs can slide forward indefinitely while refreshed; storing the revocation floor on `users` is the smallest current-contract change |

## Approaches Considered

### Option A: Generic HTTP DSL With Synthetic Templates

Admins configure a declarative sync DSL that describes HTTP requests, credentials, iteration, extraction, and field mapping. Built-in templates use only placeholder endpoints and data.

Pros:

1. Supports many organization APIs without vendor-specific backend code.
2. Keeps external secrets and platform details out of the repository.
3. Allows preview and validation before applying changes.
4. Matches the modular-monolith boundary: one `directorysync` module owns this capability.

Cons:

1. Requires careful DSL validation and error messages.
2. Advanced admins must understand JSON/YAML and JSONPath-like mappings.

### Option B: Fixed Departments Plus Members API Shape

Hard-code a simple two-step model: list departments, then list members by department.

Pros:

1. Smaller implementation.
2. Easier UI.

Cons:

1. Reintroduces a hidden platform contract.
2. Does not cover single-list, nested-tree, paginated, or multi-step APIs well.
3. Would likely need a breaking redesign.

### Option C: External Push Snapshot API Only

AI Efficiency exposes a standard snapshot ingestion API. Separate scripts sync external directory APIs and push normalized departments/members.

Pros:

1. Keeps the backend simple.
2. Avoids DSL complexity.

Cons:

1. Requires external operations and scripts.
2. Product cannot offer built-in validation, preview, schedule, or templates.
3. Admin experience is worse.

## Decision

Use **Option A: Generic HTTP DSL With Synthetic Templates**.

## Architecture

Add `backend/internal/directorysync` as the owner of organization source configuration, DSL validation, execution, normalization, snapshots, and risk derivation.

The module boundaries are:

- `directorysync.Service`: validates source configuration, runs preview/apply jobs, persists run state, and updates current directory facts.
- `directorysync.Executor`: executes the safe HTTP DSL with bounded timeouts, response size limits, and credential injection.
- `directorysync.Normalizer`: converts step outputs into canonical departments and members.
- `directorysync.RiskService`: derives offboarding candidates from local users and the latest successful full-company snapshot.
- `directorysync.OffboardingService`: performs admin-confirmed offboarding actions by calling relay/provider disable capability and local auth token revocation.

Existing modules remain responsible for their current domains:

- `auth` owns login, JWT generation, validation, and token revocation checks.
- `relay` owns upstream relay/sub2api user operations.
- `adminsubscription` owns subscription add/extend/remove/reset-quota jobs.
- `handler` owns HTTP API boundaries.
- `frontend` owns settings, preview, run history, and offboarding review UI.

## Data Model

### `directory_sources`

Stores source configuration.

Fields:

- `id`
- `name`
- `description`
- `scope`: enum, first version only accepts `full_company`
- `enabled`
- `dsl`: text or JSON
- `schedule_enabled`
- `schedule_interval`: enum `hourly`, `daily`, `weekly`
- `schedule_timezone`
- `last_successful_run_id`
- `last_run_id`
- `created_at`
- `updated_at`

The DSL stores credential references only. It must not store secret values.
Real source configurations may contain the administrator's real HTTPS endpoint
URLs, header names, query names, and response field paths when those are needed
to call the directory API. Secret values still must stay in the encrypted
credential store and be referenced only by `auth.credential_ref`.

### `directory_sync_runs`

Stores execution history.

Fields:

- `id`
- `source_id`
- `mode`: enum `validate`, `preview`, `apply`
- `trigger`: enum `manual`, `schedule`
- `status`: enum `queued`, `running`, `completed`, `completed_with_warnings`, `failed`
- `phase`: enum `validating`, `executing`, `normalizing`, `applying`, `completed`, `failed`
- `started_at`
- `completed_at`
- `http_request_count`
- `department_count`
- `member_count`
- `invalid_member_count`
- `warning_count`
- `error_message`
- `warnings`: JSON array, redacted
- `summary`: JSON object, redacted
- `preview_diff`: JSON object, redacted and only for preview/apply response summaries
- `created_at`
- `updated_at`

The current company directory snapshot is resolved from the latest successful
`full_company` apply run (`completed` or `completed_with_warnings`), using run
completion time and run id as the ordering source of truth. Editing an older
source must not make it current.

Run logs must not store request headers, credential values, full response bodies, or raw employee data beyond bounded redacted samples.

### `directory_departments`

Stores the current canonical department snapshot for a source.

Fields:

- `id`
- `source_id`
- `external_id`
- `parent_external_id`
- `effective_parent_external_id`: nullable derived parent for the applied hierarchy
- `name`
- `path`
- `metadata`: redacted JSON
- `last_seen_run_id`
- `created_at`
- `updated_at`

Unique index: `(source_id, external_id)`.

`parent_external_id` remains the normalized upstream fact and is never rewritten to
repair hierarchy defects. Each successful apply derives exactly one
`effective_parent_external_id` for every department in that run: null/blank or
missing parents become effective roots, and one deterministic anchor per closed
cycle becomes an effective root. The anchor is the cycle row ordered first by
`LOWER(BTRIM(name)), external_id` under explicit `C` collation semantics: trim
ASCII spaces, fold ASCII `A-Z`, then compare UTF-8 bytes. PostgreSQL readers use
`LOWER(BTRIM(name) COLLATE "C") COLLATE "C", external_id COLLATE "C"` so the
result does not change with the database default collation. Every other valid
edge is preserved. The existing `source_id` and `last_seen_run_id` fields scope
and version the derived relation with the applied snapshot. Existing readers may
continue reconstructing the same relation from upstream facts during staged
migration.

### `directory_members`

Stores the current canonical member snapshot for a source.

Fields:

- `id`
- `source_id`
- `external_id`
- `email_normalized`
- `display_name`
- `department_external_id`
- `status`
- `metadata`: redacted JSON
- `matched_user_id`: nullable local `users.id`, computed by email
- `last_seen_run_id`
- `created_at`
- `updated_at`

Unique index: `(source_id, email_normalized)`.

One normalized email has exactly one canonical member row per source. If the same
email appears in multiple department responses, the executor coalesces those rows
into the same canonical member, keeps `department_external_id` as a compatibility
primary department field, and stores every current department membership in
`directory_member_departments`.

### `directory_member_departments`

Stores the current department membership links for canonical members. This table
is the source of truth for organization membership checks and multi-department
reporting. `directory_members.department_external_id` remains a compatibility
primary department for older rows and row-level display fallback.

Fields:

- `id`
- `source_id`
- `directory_member_id`
- `member_external_id`
- `member_email_normalized`
- `department_external_id`
- `last_seen_run_id`
- `created_at`
- `updated_at`

Unique index: `(source_id, member_email_normalized, department_external_id)`.

### `directory_offboarding_actions`

Stores explicit admin actions from the offboarding review surface.

Fields:

- `id`
- `source_id`
- `user_id`
- `relay_user_id`
- `directory_run_id`
- `action`: enum `disable_relay_user`
- `status`: enum `running`, `succeeded`, `failed`, `partial_failed`
- `reason`
- `error_message`
- `performed_by_user_id`
- `created_at`
- `updated_at`

Unique index: `(source_id, user_id, action)` for the current action contract, so repeated clicks retry or update the same action record instead of creating duplicate successful actions.

### Local Auth State

Add `users.token_valid_after` as a nullable timestamp.

All access and refresh tokens already carry `iat`. Token validation and refresh must reject tokens with `iat < token_valid_after`.

## DSL Contract

The DSL is declarative and safe. It supports JSON or YAML input, parsed into the same internal structure.

Example template:

```yaml
version: 1
scope: full_company
auth:
  type: header
  header: X-Directory-API-Key
  credential_ref: directory_api_key
limits:
  timeout_seconds: 30
  max_response_bytes: 1048576
  max_items: 50000
steps:
  - id: departments
    request:
      method: GET
      url: https://directory.example.com/api/v1/departments
    extract:
      items: $.data.departments
    map:
      department:
        external_id: $.id
        parent_external_id: $.parent_id
        name: $.name
        path: $.path
        metadata:
          representative_external_ids: $.leader_ids

  - id: members
    foreach: departments.items
    request:
      method: GET
      url: https://directory.example.com/api/v1/users
      query:
        department_id: "{{ item.external_id }}"
    extract:
      items: $.data.users
    map:
      member:
        external_id: $.id
        email: $.email
        display_name: $.name
        department_external_id: "{{ source.external_id }}"
        status: $.status
        metadata:
          leader_department_ids: $.leader_department_ids
```

Supported first-version features:

- HTTP methods: `GET`
- Auth: static header credential injection through `credential_ref`
- Headers: literal non-sensitive values plus credential injection through the
  top-level `auth.header` / `auth.credential_ref` contract
- Query parameters: literal non-sensitive values and simple template expressions
- Iteration: `foreach` over a prior step's extracted items
- Extraction and mappings: JSONPath-like paths, including `$` when the response root itself is the item array and numeric array indexes such as `$.departmentIds[0]` for fields inside item objects
- Mapping targets: `department`, `member`
- Explicit metadata mappings: optional `department.metadata.*` and
  `member.metadata.*` entries are allowlisted JSONPath/template mappings for
  non-sensitive ids or role flags that the platform needs. Known organization
  representative fields are `department.metadata.representative_external_ids`
  and `member.metadata.leader_department_ids`. The known notification identity
  field is `member.metadata.wecom_userid`; WeCom-backed sources must map it
  explicitly when quota-reset robot notifications should mention approvers.
- Templates: `{{ item.field }}` and `{{ source.field }}` only. In a `foreach` member step, `item` refers to the member row and `source` refers to the outer iteration item, such as the department row.
- Limits: timeout, response size, and total item caps

Unsupported first-version features:

- Arbitrary JavaScript
- Arbitrary jq programs
- Shell commands
- Request body templating
- OAuth flows
- Browser automation
- Webhooks
- Writes to external systems
- Dynamic secret interpolation outside the auth/header contract

Validation rules:

1. `version` must be `1`.
2. `scope` must be `full_company`.
3. Every step must have a unique `id`.
4. Every request URL must use `https://` unless an explicit admin-only unsafe-local toggle is added for local testing.
5. Credential references must resolve to existing credentials. Source create and
   update must run static DSL validation before persisting `directory_sources.dsl`,
   so literal credentials cannot be saved and later re-exposed by source APIs.
6. JSONPath expressions must parse before execution. `extract.items: $` is valid only for root-array responses. Wildcards and filters are unsupported; a single numeric array index segment is supported for mapping scalar values from arrays.
7. Member mapping must include `email`.
8. Department mapping must include `external_id` and `name`.
9. Invalid or missing email rows become warnings and are excluded from `directory_members`.
10. Member mapping may provide either `department_external_id` for one
    membership or `department_external_ids` for all memberships returned on the
    member row. When both are present, the executor stores one canonical member
    and deduplicates the union of all mapped department ids.
11. Duplicate emails across different departments are normal multi-department
    membership evidence. Duplicate email plus department pairs are ignored after
    the first normalized membership.
12. `request.url`, `request.headers`, and `request.query` must not contain
    literal credentials. Sensitive keys such as authorization, cookie, token,
    secret, password, credential, and API-key variants are rejected in request
    headers/query parameters. Secret-looking values such as bearer/basic/token
    credentials, JWT-shaped strings, and common API-token prefixes are rejected.

## Backend API

All endpoints are admin-only.

### Source CRUD

```text
GET /api/v1/admin/directory/sources
POST /api/v1/admin/directory/sources
GET /api/v1/admin/directory/sources/:id
PUT /api/v1/admin/directory/sources/:id
DELETE /api/v1/admin/directory/sources/:id
```

Delete is a soft delete. A deleted or disabled source must not run scheduled sync. Historical runs remain queryable for admin audit.

Source update and soft delete can change which Directory facts contribute to Work Items. Each successful update/delete therefore commits the source mutation and the shared PostgreSQL work-item counts revision in one transaction. Validation failure or another rejected mutation changes neither source state nor revision.

### Validation

```text
POST /api/v1/admin/directory/sources/:id/validate
```

Runs static validation only. It does not call external APIs.

### Preview

```text
POST /api/v1/admin/directory/sources/:id/preview
```

Starts an asynchronous preview run and returns the persisted run row. The frontend polls the run detail endpoint for progress and redacted diff. Preview does not update current directory facts and cannot update offboarding risks.

### Apply

```text
POST /api/v1/admin/directory/sources/:id/runs
```

Body:

```json
{
  "mode": "apply"
}
```

Starts an asynchronous apply run and returns the persisted run row. The frontend polls the run detail endpoint for progress.

### Runs

```text
GET /api/v1/admin/directory/sources/:id/runs?limit=20&offset=0
GET /api/v1/admin/directory/runs/:id
```

The source run-history endpoint returns one bounded summary page. `limit`
defaults to 20 when absent, invalid, zero, or negative and is capped at 100.
`offset` defaults to 0 when absent, invalid, or negative; a positive unaligned
offset is preserved. The response has this shape:

```json
{
  "items": [
    {
      "id": 42,
      "source_id": 7,
      "mode": "apply",
      "trigger": "manual",
      "status": "completed",
      "phase": "completed",
      "started_at": "2026-06-22T01:00:00Z",
      "completed_at": "2026-06-22T01:02:00Z",
      "http_request_count": 3,
      "department_count": 12,
      "member_count": 240,
      "invalid_member_count": 0,
      "warning_count": 0
    }
  ],
  "total": 2400,
  "page": 0,
  "page_size": 20,
  "latest_active_run": null
}
```

`page` is zero-based `floor(normalized_offset / normalized_limit)`, `page_size`
is the normalized limit, and `total` counts all runs for the source. Summary
rows use stable `started_at DESC NULLS FIRST, id DESC` ordering; queued rows with
no start time therefore sort before started rows, with descending run ID as the
tie-breaker. Each item is a lightweight projection containing only the fields
shown above. It does not select or return `warnings`, `summary`, `preview_diff`,
`error_message`, timestamps unrelated to progress, or other diagnostic/result
blobs.

`latest_active_run` is either null or the same lightweight summary shape. It is
selected independently of the requested history page using the same ordering
and is restricted to the newest `preview` or `apply` run whose status is
`queued` or `running`. `GET /api/v1/admin/directory/runs/:id` remains the
complete selected-run contract and returns the persisted diagnostic fields,
including warnings, summary, preview diff, and error message when present.

The settings UI treats run state as backend-owned. The current Vue consumer was
migrated in the same platform release to request summary pages and fetch
complete detail on selection; repository and organization consumer searches
found no non-frontend caller that requires an unpaginated full-entity
compatibility response. On page open, source selection, or conflict recovery,
the UI uses only page-independent `latest_active_run` for active recovery. It
polls that run, or a just-created active preview/apply run, until terminal.
Selecting a terminal or older history row performs one detail fetch and never
starts, replaces, or cancels the independent active-run polling loop.

### Directory Facts

```text
GET /api/v1/admin/directory/departments?source_id=1&q=alpha
GET /api/v1/admin/directory/members?source_id=1&q=alice&department_id=dept-alpha
```

Responses must not include raw unredacted metadata by default.

Department rows include the source `path` plus a backend-computed, name-based
`display_path`. Search must match `name`, `external_id`, source `path`, and
`display_path`. UI surfaces should render `display_path` first and treat source
`path` as a secondary technical field because some directory APIs use numeric
ID chains for `path`.

The product UI treats organization data as one current company snapshot. `source_id`
is a persistence detail for the directory sync module and must not be exposed as a
normal selector in `/admin/users`. Admin-facing user management APIs that need
department context must resolve the current directory snapshot server-side from
the latest successful apply run, not from `directory_sources.updated_at`.

### Admin Users Department Filters

`GET /api/v1/admin/users` supports an optional `department_id` query parameter.
When present, pagination and totals are computed after filtering local users by
directory members whose current `directory_member_departments.department_external_id`
is in the selected department's subtree, including the selected department itself,
and whose normalized email matches the local user email. Older rows without
membership links fall back to `directory_members.department_external_id`.
Returned user rows may include a `department`
object:

```json
{
  "department": {
    "external_id": "dept-alpha",
    "name": "Department Alpha",
    "path": "Department Alpha"
  }
}
```

Users with no matching current directory member are still listed in the default
user view and show an unmatched organization state. They are excluded when a
specific department filter is active.

Current-filter subscription jobs must use the same department and access-status
filters as the visible `/admin/users` list. A department-filtered or
status-filtered list and a current-filter bulk action must therefore target the
same user set, subject to the existing relay-mapped and target-count rules.

`GET /api/v1/admin/users/departments` returns departments from the current
single directory snapshot in tree preorder. Each row includes `external_id`,
optional `parent_external_id`, `name`, source `path`, name-based `display_path`,
`depth`, `child_count`, direct `member_count` / `matched_user_count`, and subtree
`subtree_member_count` / `subtree_matched_user_count`. These counts use current
member-department memberships and deduplicate the same canonical member inside a
single subtree aggregate. When representative
metadata is mapped, each row also includes `representative_count` and
`matched_representative_count`, derived from
`department.metadata.representative_external_ids` plus cross-checked
`member.metadata.leader_department_ids`. The existing direct count fields remain
direct-only for compatibility; UI copy must distinguish direct counts from
totals that include child departments. UI labels, department filters, and
user-row department text must use `display_path` or `name`, not the source
`path`, because source paths may be numeric ID chains.

### Offboarding Review

```text
GET /api/v1/admin/directory/offboarding-candidates?q=alice&page=1&page_size=20
POST /api/v1/admin/directory/offboarding-candidates/:user_id/disable-relay-user
```

`source_id` may be accepted as an internal compatibility parameter, but the
normal product flow omits it and resolves the current snapshot server-side.
Candidate listing defaults to 20 rows per page, caps `page_size` at 100, orders
by username and then local user id, and returns `items`, `page`, `page_size`,
and `total`. The badge count uses the same database anti-join without loading
candidate rows or per-user action records.
Disable actions must also resolve and recheck against the current snapshot
server-side; a supplied older `source_id` must not be trusted for the final
missing-email decision.

Candidate rows include:

- local user id
- username
- email
- auth source
- relay user id
- reason: `missing_from_latest_full_company_directory`
- last successful directory run id and timestamp
- current local token revocation timestamp, if any
- upstream disable status, if previously attempted

The disable endpoint requires explicit confirmation:

```json
{
  "confirm_email": "alice@example.com",
  "reason": "missing_from_latest_full_company_directory"
}
```

It performs:

1. Reload local user.
2. Confirm the user's normalized email is still missing from the latest successful full-company directory snapshot.
3. Confirm the user has `relay_user_id`.
4. Resolve the relevant relay provider.
5. Disable the upstream relay/sub2api user through a new relay capability.
6. Set local `token_valid_after` to now.
7. Record an audit result.

It must not remove subscriptions automatically.

The upstream Relay disable remains outside the local database transaction. After Relay reports success, the backend synchronously derives `context.WithTimeout(context.WithoutCancel(requestContext), 5*time.Second)` and commits `users.token_valid_after`, the succeeded offboarding action, and the shared PostgreSQL work-item counts revision in one transaction. This finalization is independent from client cancellation but remains bounded.

If finalization fails, the backend rolls that transaction back before recording `partial_failed`. The failure record uses a second independent `context.WithTimeout(context.WithoutCancel(requestContext), 5*time.Second)` so an expired finalization deadline cannot leave an already-disabled Relay user with a permanently `running` local action. Exact-email confirmation, current snapshot resolution, and current-membership recheck still occur before Relay disable and never consult cached work-item counts.

## Relay Capability

Add an optional relay interface, for example:

```go
type UserDisabler interface {
    DisableUser(ctx context.Context, userID int64) error
}
```

The sub2api adapter implements it using the upstream user-status API available in the deployed sub2api version. If upstream only supports generic user update, the adapter must still expose `DisableUser` so handlers do not depend on sub2api request details.

If a relay provider does not implement the capability, the offboarding action returns `422`.

## Token Revocation Contract

Current auth behavior issues stateless JWT access and refresh tokens. Access tokens default to a short TTL, refresh tokens default to a longer TTL, and refresh currently issues a new refresh token. That creates sliding refresh behavior.

To make offboarding effective:

1. Token generation continues to include `iat`.
2. Access-token validation loads the user's `token_valid_after` and rejects tokens issued before it.
3. Refresh-token validation also loads `token_valid_after` before issuing a new pair.
4. Successful offboarding finalization sets `token_valid_after = now` in the same transaction as its succeeded action and work-item revision change.
5. Existing clients receive `401` on the next API call or refresh attempt.

This is not a full session-management feature. It is a per-user revocation floor.

## Frontend Design

### Settings: Directory Sync

Add a `Directory Sync` block under `/settings` -> `Organization & Login`.

It shows:

- source list
- enabled state
- schedule
- latest successful sync
- latest run status
- member count
- offboarding candidate count

The edit drawer includes:

- source name
- description
- enabled toggle
- schedule toggle and interval
- timezone
- credential selector
- DSL editor
- validate button
- preview button
- save button
- run now button

### Templates And AI Prompt Helper

The settings UI includes a template panel with safe synthetic templates:

1. `Departments then members`
2. `Single members endpoint`
3. `Paged members endpoint`

Each template uses only placeholder values.

The UI also includes `Copy AI Prompt`, which copies a prompt explaining:

- the DSL schema
- required fields
- supported features
- safety rules
- expected output format

The prompt must distinguish configuration evidence from sensitive data. It can
ask for real API documentation, real endpoint paths, real query/header names,
and real response field paths when those are necessary to produce a working DSL.
It must tell the admin not to paste secret values, API keys, bearer tokens,
cookies, passwords, or raw employee rows into AI tools. Credential values must
be represented as a `credential_ref` name that the admin will resolve inside AI
Efficiency.

The product does not store external API docs pasted into AI tools.

### Preview UI

Preview shows:

- departments discovered
- members discovered
- invalid member count
- warnings
- sample redacted rows
- planned creates/updates/missing rows

Email samples must be masked by default, with a toggle only if needed for admin debugging. Tests must use only `example.com` addresses.

### Offboarding Review

Add an admin page or settings subpage for offboarding candidates.

The page shows:

- candidate user
- email
- auth source
- relay user id
- reason
- latest directory run timestamp
- action status

Action:

- `Disable relay user`
- requires confirmation with the candidate email
- explains that it disables upstream AI access and revokes local AI Efficiency tokens
- states that subscriptions are not removed automatically

### Admin Users Organization Views

`/admin/users` remains the single admin surface for local users and AI access
support. It adds an internal view switch:

1. User view: the existing user/access table plus a department column,
   department filter, derived access-status column, and access-status filter.
   The access-status column is derived from local user state: `disabled` takes
   precedence when `users.token_valid_after` or `users.relay_disabled_at` is set,
   or when a successful `directory_offboarding_actions` disable action exists;
   otherwise users with a stored relay credential are `configured`, and users
   without one are `missing_credential`.
2. Department view: a directory-backed collapsible tree list inside the same
   route, showing department name/display path, hierarchy indentation, direct
   member count, direct matched local-user count, subtree total member count,
   subtree total matched local-user count, and a drill-in action that switches
   back to user view with the selected department subtree filter.

Post-implementation extension: `/admin/users` also supports direct relay-user
disablement through `POST /api/v1/admin/users/:id/disable-access`. That action
requires exact email confirmation, calls the same optional `relay.UserDisabler`
capability, and records `users.relay_disabled_at`. Unlike confirmed directory
offboarding, this direct user-management action intentionally does not set
`users.token_valid_after` and does not revoke existing local AI Efficiency
tokens.

The route may preserve the selected view and department filter in query
parameters, for example `view=departments` or `department_id=dept-alpha`, but it
must not add a separate department page or expose a directory source selector.

## Sync Execution

### Validate

Static-only. No external HTTP calls.

### Preview

Runs the DSL asynchronously against the external API and records a preview run. It never updates current directory facts.

### Apply

Runs the DSL and normalizes the complete result. Only after all required steps
succeed does it update current directory facts, run completion fields,
`last_successful_run_id`, and the shared PostgreSQL work-item counts revision in
one transaction.

Apply behavior:

1. Refuse to queue a new apply when the same source already has a queued or
   running apply run.
2. Mark run `running`.
3. Execute steps with limits.
4. Normalize departments/members.
5. Validate required member emails.
6. Compute diff.
7. Resolve the effective department hierarchy in bounded, deterministic,
   non-recursive application work before mutating current facts.
8. In a transaction, replace current facts for the source, including each
   department's effective parent, mark the run
   `completed` or `completed_with_warnings`, and set source `last_run_id` /
   `last_successful_run_id`, then advance the work-item counts revision before
   commit.

Failed apply runs do not change current directory facts, offboarding candidates,
or the revision. Validate and preview runs also leave the revision unchanged.

### Schedule

A background scheduler finds enabled sources whose next run is due and starts
apply runs. The scheduler must prevent concurrent runs for the same source, and
manual apply creation must use the same queued/running apply guard.

First implementation can use an in-process scheduler because the app is a modular monolith. If multiple backend replicas become supported later, scheduling must add a DB lease.

## Offboarding Candidate Rule

A local user is an offboarding candidate when all are true:

1. There is a latest complete successful `full_company` apply run. `completed`
   and `completed_with_warnings` both qualify only when all required HTTP steps
   finished and the run produced a complete normalized snapshot. The latest run,
   not the newest edited source, determines the current source.
2. The local user's normalized email does not exist in that source's current `directory_members`.
3. The local user has a `relay_user_id`.
4. The local user has not already been successfully disabled through the offboarding action, or the UI is showing historical/completed cases.

Only complete successful apply runs can change the candidate set. Validate, preview, failed apply, and partial apply attempts cannot mark users as missing.

## Error Handling

DSL validation errors return `400` with field paths.

External HTTP errors:

- non-2xx status marks the run failed
- timeout marks the run failed
- response too large marks the run failed
- invalid JSON marks the run failed

Mapping warnings:

- missing optional department parent is allowed
- a parent missing from the same applied source becomes an effective root while
  the upstream parent fact remains stored
- a closed department cycle is accepted after removing only its deterministic
  anchor edge from the effective hierarchy
- missing member email excludes that row and records a warning
- invalid email excludes that row and records a warning
- duplicate member email keeps one deterministic row and records a warning

Offboarding errors:

- candidate no longer missing: `409`
- local user missing relay user id: `422`
- relay provider lacks disable capability: `422`
- upstream disable fails: `502`
- local token revocation fails after upstream disable: `500` plus audit row marked partial failure
- bounded local finalization expires after upstream disable: roll back token/action/revision state, then record `partial_failed` under a new independent bounded context

Partial offboarding failure must be visible in the UI so admins can retry or investigate.

## Security

1. All directory APIs are admin-only.
2. Credentials are referenced, not embedded.
3. Logs and run summaries redact secrets and avoid raw payload storage.
4. External HTTP execution enforces timeouts, response size limits, item caps, and HTTPS-only URLs by default.
5. Templates and tests use only synthetic data.
6. Preview output masks sensitive member fields by default.
7. Offboarding action requires explicit email confirmation.
8. Token revocation is local and does not depend on LDAP availability.
9. Directory sync must not write to external organization systems.
10. Directory sync must not mutate relay subscriptions.

## Testing

Backend tests:

- DSL schema validation accepts valid templates and rejects unsupported features.
- Credential references resolve without exposing secret values in run summaries.
- Executor handles simple GET, header auth, query templates, foreach, JSONPath extraction including root-array responses, and limits.
- Preview run does not update `directory_members`.
- Failed apply run does not change current facts or offboarding candidates.
- Successful full-company apply updates departments, members, and `last_successful_run_id`.
- Successful apply stores one deterministic effective parent per department,
  preserves upstream parent facts, and versions every relation with the applied
  run id.
- Acyclic, missing-parent, self-cycle, multi-row cycle, input-order, non-ASCII
  collation parity, rollback, and large non-recursive hierarchy fixtures pass.
- Source update/delete and successful apply advance the shared work-item revision atomically with their local state; validation, preview, conflict, and failed apply paths do not.
- Email matching is case-insensitive and trims whitespace.
- Invalid emails are warnings and excluded.
- Duplicate emails are deterministic and warning-backed.
- Offboarding candidate query uses latest successful full-company apply only.
- Confirmed offboarding calls relay disable and sets token revocation floor.
- Successful offboarding finalization commits token revocation, succeeded action, and revision atomically under an independent five-second context; deadline/finalization failure rolls back and records `partial_failed` under a new independent bounded context.
- Access and refresh tokens issued before the revocation floor are rejected.
- Tokens issued after the revocation floor are accepted.
- Providers without disable capability return `422`.

Frontend tests:

- Directory Sync settings block renders inside Organization & Login.
- Template panel uses only synthetic placeholder values.
- Copy AI Prompt includes safety guidance and no real data.
- Validate, Preview, Save, and Run Now call the expected APIs.
- Preview displays counts and warnings without requiring raw sensitive values.
- Offboarding page lists candidates and requires email confirmation.
- Disable action copy clearly says subscriptions are not removed automatically.

Manual/environment-sensitive verification:

- Run a local fake directory API server with synthetic data.
- Configure a source using placeholder credentials.
- Validate, preview, apply, and scheduled apply against the fake server.
- Confirm failed fake API responses do not alter the current directory.
- Confirm disabling a synthetic candidate invalidates existing tokens.

Default commands:

```text
cd backend && go test ./...
cd frontend && pnpm test
```

## Documentation Updates Required During Implementation

Implementation must update:

- `docs/architecture.md` with the new `directorysync` module, settings surface, scheduler, offboarding flow, and auth token revocation floor.
- This spec if implementation changes the public DSL or API contract.
- `AGENTS.md` only if new agent collaboration rules are needed.
- `CLAUDE.md` only if navigation or contributor guidance changes.

## Rollout Notes

1. Ship schema and backend APIs behind admin-only access.
2. Ship settings UI templates and validation before encouraging scheduled apply.
3. Keep sources disabled by default until an admin validates and previews them.
4. Use synthetic fixtures in all tests.
5. Add offboarding disable action only after token revocation checks are covered by tests.
6. Do not enable any built-in real-company templates.
