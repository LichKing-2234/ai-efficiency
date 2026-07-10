# Quota Reset Approval Design

**Date:** 2026-07-07
**Status:** Current implemented contract
**Scope:** `backend/ent/schema/`, `backend/internal/quotareset/`, `backend/internal/handler/`, `backend/internal/relay/`, `backend/internal/directorytree/`, `frontend/src/views/`, `frontend/src/components/`, `frontend/src/api/`, `frontend/src/types/`, `frontend/src/i18n.ts`
**Related:**

- [2026-06-26-team-usage-representative-quota-design.md](./2026-06-26-team-usage-representative-quota-design.md)
- [2026-06-22-configurable-directory-sync-design.md](./2026-06-22-configurable-directory-sync-design.md)
- [2026-06-04-admin-sub2api-subscription-assignment-design.md](./2026-06-04-admin-sub2api-subscription-assignment-design.md)
- [docs/architecture.md](../../architecture.md)

## Spec Relationship

This spec describes the current implemented quota reset approval workflow. It was originally drafted before implementation and is now the contract for the shipped request, approval, reset, audit-event, approver-configuration, and notification behavior.

It extends the current Directory Sync and relay/sub2api contracts:

1. Directory Sync remains the source for current department membership and parent-child hierarchy.
2. Admin-authored quota reset approver configuration is local AI Efficiency state and is not overwritten by Directory Sync.
3. Quota reset still uses the relay provider boundary, specifically `relay.UserSubscriptionQuotaResetter`, rather than direct sub2api database access.
4. The workflow resets a selected active subscription group's daily, weekly, and monthly usage windows. It does not edit group limits, subscriptions, API keys, or delegated rate multipliers.

`docs/architecture.md` has been updated to describe the current runtime surface and boundaries.

## Data Hygiene

Tests, fixtures, docs, screenshots, examples, notification payload examples, and command output for this feature must not contain real employee data, real company domains, real department names, real subscription group names, real webhook URLs, real API keys, real tokens, or real passwords.

Use synthetic values such as:

- `alice@example.com`
- `bob@example.org`
- `Department Alpha`
- `Department Beta`
- `Group Alpha`
- `Group Beta`
- `https://hooks.example.com/ai-efficiency`
- `test-token`

## Problem

Users can exhaust a subscription group's quota usage window and need a controlled way to request a reset. Admins can already reset quota from admin subscription jobs, but that workflow is admin-centric and does not provide:

1. A user-facing request flow with a required reason.
2. Department-scoped approval by configured group representatives.
3. An auditable approval and reset event history.
4. Notification hooks for external workflow surfaces.
5. A fallback path when directory-derived approvers are missing or stale.

The existing representative quota control is rate-multiplier based and affects future consumption speed. This feature is different: it resets already accumulated daily, weekly, and monthly usage for one selected user subscription group through sub2api's reset-quota API.

## Goals

1. Let a user request a quota reset for one of their current active subscription groups.
2. Require an application reason when the user submits the request.
3. Resolve approvers from admin-configured department approver settings.
4. Support users with multiple current department memberships.
5. Use nearest configured approver department per membership path, then merge approvers across paths.
6. Let any resolved approver approve or reject the request.
7. Let admins view and process all requests as a fallback.
8. Execute the reset automatically after approval.
9. Persist an audit-ready event stream for request creation, approver resolution, notifications, decisions, reset execution, failures, cancellation, and retry.
10. Let admins configure outbound webhook notification for quota reset workflow events.
11. Keep sub2api integration behind `backend/internal/relay.Provider` and optional provider extension interfaces.

## Non-Goals

1. Do not edit sub2api group daily, weekly, or monthly quota limits.
2. Do not assign, extend, remove, or create subscriptions as part of this workflow.
3. Do not create, revoke, or edit API keys.
4. Do not change delegated rate multiplier behavior.
5. Do not infer approvers from sub2api group ownership.
6. Do not introduce a new first-class `group_representative` user role.
7. Do not implement a full audit center UI in this feature. Persist audit events now so a later audit module can consume them.
8. Do not modify sub2api source code.
9. Do not build custom notification templates in the first version.
10. Do not support multi-step approval in the first version. Any one resolved approver or any admin can make the decision.

## Captured Decisions

| Area | Decision |
| --- | --- |
| Reset semantics | Clear daily, weekly, and monthly usage windows for the selected user subscription group |
| Subscription group selection | Requester must choose one current active subscription group |
| Provider scope | Primary relay provider only |
| Approver source | Admin-authored local department approver configuration |
| Directory source | Current successful Directory Sync snapshot |
| Approver config eligibility | Admins select approvers from local users matched to group representatives for the selected department, derived from `department.metadata.representative_external_ids` and `member.metadata.leader_department_ids`; candidate resolution may fall back to current local-user email matching when `directory_members.matched_user_id` is stale |
| Approver resolution | For each requester department membership, walk upward until the nearest configured approver department is found |
| Multiple memberships | Resolve each membership path independently, merge approvers, and allow any approver to approve |
| Admin fallback | Admins can view and handle all requests |
| Self approval | Requesters cannot approve their own requests; admin fallback remains available |
| Missing approver | Request remains pending with no resolved approvers and is handled by admins |
| Audit | Persist request events now; defer full audit module UI |
| Notification | Admin-configured outbound webhook with optional credential-backed auth |

## Approaches Considered

### Option A: AI Efficiency Local Approval Facade

AI Efficiency owns requests, approval routing, status, audit events, notification, and UI. Approved resets call sub2api through the relay provider reset-quota capability.

Pros:

1. Matches current ownership: organization facts and representative UX live in AI Efficiency.
2. Avoids direct sub2api database coupling.
3. Reuses the existing admin reset-quota provider capability.
4. Gives a complete user and approver workflow with local audit facts.

Cons:

1. Adds local schema and approval state to AI Efficiency.
2. Requires a small outbound notification subsystem.

### Option B: Notification-Only Request Tracker

AI Efficiency records the request and sends a notification, but admins manually reset quota from the existing admin user subscription workflow.

Pros:

1. Shorter first implementation.

Cons:

1. No closed-loop automatic reset.
2. Weak audit trail because the actual reset happens elsewhere.
3. Higher operational risk from missed manual steps.

### Option C: Move Approval Into sub2api

sub2api owns the approval workflow and reset execution.

Pros:

1. Keeps quota operations in the upstream quota system.

Cons:

1. Forces sub2api to understand AI Efficiency Directory Sync and department approver rules.
2. Requires cross-repo product and deployment work.
3. Makes the first version much larger than needed.

## Decision

Use **Option A**.

AI Efficiency will provide the local approval facade, while sub2api remains the enforcement and reset system behind the relay provider boundary.

## Terminology

Use precise terms in code and UI:

1. **Subscription group** means the sub2api group selected by the requester for quota reset.
2. **Department** means an organization unit from Directory Sync.
3. **Approver configuration** means local AI Efficiency admin configuration that maps a department to one or more approver users.
4. **Resolved approver** means a user selected by the resolver for a specific request and snapshotted on that request.

Do not call approver-configured departments "subscription groups" in UI or API names.

## Data Model

### `quota_reset_approver_configs`

Stores local admin configuration for department approvers.

Fields:

| Field | Type | Notes |
| --- | --- | --- |
| `id` | int | Primary key |
| `directory_source_id` | int | Current Directory Sync source when configured |
| `department_external_id` | string | Department external id |
| `department_display_path` | string | Snapshot for admin readability |
| `approver_user_id` | int | Local user id |
| `enabled` | bool | Disabled rows are ignored by resolution |
| `created_by_user_id` | int | Admin actor |
| `updated_by_user_id` | int | Admin actor |
| `created_at` | time |  |
| `updated_at` | time |  |

Indexes:

1. `(directory_source_id, department_external_id, enabled)`
2. `(approver_user_id, enabled)`
3. Unique active row guard on `(directory_source_id, department_external_id, approver_user_id)` if supported by the target dialect; otherwise enforce in service code.

Rules:

1. A department can have multiple approvers.
2. An approver can be configured on multiple departments.
3. Configs are local and must not be overwritten by Directory Sync apply runs.
4. New or updated configs must use a local user matched to a Directory Sync member that is a representative for the selected department. Representative facts come from `directory_departments.metadata.representative_external_ids` and `directory_members.metadata.leader_department_ids`.
5. If a configured department disappears from the current directory tree, the config is stale and no longer resolves for new requests, but the row remains visible to admins for cleanup.

### `quota_reset_requests`

Stores the request state and immutable resolution snapshots.

Fields:

| Field | Type | Notes |
| --- | --- | --- |
| `id` | int | Primary key |
| `requester_user_id` | int | Local user id |
| `requester_relay_user_id` | int64 | Snapshotted relay user id used for reset |
| `provider_id` | int | Primary relay provider id at creation |
| `group_id` | string | Selected subscription group id |
| `group_name` | string | Selected subscription group name snapshot |
| `group_platform` | string | Optional platform snapshot |
| `reason` | string | Required requester reason |
| `status` | enum | See status model |
| `resolved_approver_user_ids` | JSON int array | Snapshotted approver ids |
| `matched_department_paths` | JSON array | Resolution evidence per membership path |
| `approved_by_user_id` | nullable int | Decision actor |
| `rejected_by_user_id` | nullable int | Decision actor |
| `decision_reason` | string | Required for rejection, optional for approval |
| `decided_at` | nullable time |  |
| `reset_error` | string | Last reset error |
| `reset_started_at` | nullable time |  |
| `reset_completed_at` | nullable time |  |
| `created_at` | time |  |
| `updated_at` | time |  |

Status values:

1. `pending`
2. `approved_resetting`
3. `approved_reset_succeeded`
4. `approved_reset_failed`
5. `rejected`
6. `cancelled`

Indexes:

1. `(requester_user_id, created_at)`
2. `(status, created_at)`
3. `(provider_id, group_id, status)`
4. Partial unique guard on `(requester_user_id, provider_id, group_id)` for active statuses.
5. `(updated_at)`

Duplicate guard:

1. A requester cannot have more than one active request for the same `provider_id + group_id`.
2. Active statuses are `pending`, `approved_resetting`, and `approved_reset_failed`.
3. The service checks before create and the database enforces the guard to cover concurrent submissions.
4. After `approved_reset_succeeded`, `rejected`, or `cancelled`, the user may file a new request for the same group.

### `quota_reset_request_events`

Stores audit-ready event facts. This table is append-only except for mechanical retention work in future audit tooling.

Fields:

| Field | Type | Notes |
| --- | --- | --- |
| `id` | int | Primary key |
| `request_id` | int | Quota reset request id |
| `actor_user_id` | nullable int | User who caused the event, if applicable |
| `event_type` | enum | See event types |
| `metadata` | JSON object | Redacted structured facts |
| `error_message` | string | Redacted error summary |
| `created_at` | time |  |

Event types:

1. `created`
2. `approver_resolved`
3. `notification_sent`
4. `notification_failed`
5. `approved`
6. `reset_started`
7. `reset_succeeded`
8. `reset_failed`
9. `rejected`
10. `cancelled`
11. `reset_retried`

Rules:

1. Events must not contain API keys, relay passwords, webhook tokens, or webhook signatures.
2. User-entered `reason` and `decision_reason` may be referenced by id or truncated summary in notification metadata, but the full value lives on the request row.
3. Reset execution must write both status updates and event rows.
4. Notification failure writes an event and does not change request status.

### `quota_reset_notification_settings`

Stores outbound webhook configuration. A single global setting row is enough for the first version.

Fields:

| Field | Type | Notes |
| --- | --- | --- |
| `id` | int | Primary key |
| `enabled` | bool |  |
| `url` | string | HTTP or HTTPS endpoint configured by admin |
| `auth_type` | enum | `none` or `bearer_token` |
| `credential_id` | nullable int | Secret text credential for bearer token |
| `created_by_user_id` | int | Admin actor |
| `updated_by_user_id` | int | Admin actor |
| `created_at` | time |  |
| `updated_at` | time |  |

Rules:

1. `credential_id` must reference a `secret_text` credential when `auth_type=bearer_token`.
2. Credential delete checks should count notification settings as references so an in-use token cannot be deleted silently.
3. First version does not store custom templates.

## Approver Resolution

The resolver runs when a request is created and stores a snapshot on `quota_reset_requests`.

Inputs:

1. Current successful Directory Sync source.
2. Requester local user.
3. Current directory member matched to requester by local user id or normalized email.
4. Current member-department memberships from `directory_member_departments`, with `directory_members.department_external_id` as fallback.
5. Current `directory_departments` tree.
6. Enabled `quota_reset_approver_configs` for the current source.

Algorithm:

1. Resolve all current requester department ids.
2. For each department id, build a path from that department to the root using `parent_external_id`.
3. Walk the path from leaf to root.
4. At the first department that has enabled approver configs, add all configured approvers for that department and stop walking that path.
5. Repeat for every membership path.
6. Merge approver user ids in deterministic order and remove duplicates.
7. Remove the requester from the approver list to prevent self approval.
8. Store `resolved_approver_user_ids` and a `matched_department_paths` evidence snapshot.

If no approvers remain after resolution, the request is still created as `pending` with an empty approver list. Admins can process it.

Path evidence should include:

```json
{
  "start_department_external_id": "department-alpha-team",
  "path": [
    {"external_id": "department-alpha-team", "display_path": "Department Alpha / Team One"},
    {"external_id": "department-alpha", "display_path": "Department Alpha"}
  ],
  "matched_department_external_id": "department-alpha",
  "matched_approver_user_ids": [7, 8],
  "resolution": "matched"
}
```

For no-match paths, use `resolution: "no_config_found"`.

## State Transitions

Allowed transitions:

| From | Action | To |
| --- | --- | --- |
| none | requester creates request | `pending` |
| `pending` | requester cancels own request | `cancelled` |
| `pending` | resolved approver approves | `approved_resetting` |
| `pending` | admin approves | `approved_resetting` |
| `pending` | resolved approver rejects | `rejected` |
| `pending` | admin rejects | `rejected` |
| `approved_resetting` | reset succeeds | `approved_reset_succeeded` |
| `approved_resetting` | reset fails | `approved_reset_failed` |
| `approved_reset_failed` | resolved approver retries reset | `approved_resetting` |
| `approved_reset_failed` | admin retries reset | `approved_resetting` |

Rules:

1. Only `pending` requests can be approved, rejected, or cancelled.
2. Only `approved_reset_failed` requests can be retried.
3. Approval and retry execute reset through a backend service method, not from the browser.
4. State changes must be protected by a transaction or row-level status predicate so concurrent approvals cannot both execute reset.
5. If a reset succeeds but a later local event write fails, the service must preserve the request as `approved_reset_succeeded` and log the local write failure.

## Backend API

### User Request APIs

```text
GET /api/v1/user/quota-reset/options
```

Returns the current user's active subscription groups from the primary relay provider.

```json
{
  "provider_id": 1,
  "groups": [
    {
      "group_id": "42",
      "group_name": "Group Alpha",
      "platform": "openai",
      "daily_usage_usd": 12.3,
      "weekly_usage_usd": 45.6,
      "monthly_usage_usd": 78.9,
      "daily_limit_usd": 20,
      "weekly_limit_usd": 100,
      "monthly_limit_usd": 300
    }
  ]
}
```

Rules:

1. Requires authenticated user.
2. Requires local relay mapping.
3. Lists active subscriptions only.
4. Uses primary provider only in the first version.

```text
POST /api/v1/user/quota-reset/requests
```

Request:

```json
{
  "group_id": "42",
  "reason": "Need to finish a time-sensitive build investigation."
}
```

Rules:

1. `reason` is required after trimming.
2. `group_id` must match one of the current active subscription groups.
3. Creates a request and resolution snapshot in one transaction.
4. Sends notification after commit if webhook is enabled.

```text
GET /api/v1/user/quota-reset/requests?page=1&page_size=20&status=pending
POST /api/v1/user/quota-reset/requests/:id/cancel
```

Rules:

1. Users can list only their own requests.
2. Users can cancel only their own `pending` requests.

### Approver APIs

```text
GET /api/v1/user/quota-reset/approvals?page=1&page_size=20&status=pending
POST /api/v1/user/quota-reset/approvals/:id/approve
POST /api/v1/user/quota-reset/approvals/:id/reject
POST /api/v1/user/quota-reset/approvals/:id/retry-reset
```

Reject request:

```json
{
  "decision_reason": "Please reduce non-critical usage and reapply if the issue remains blocked."
}
```

Rules:

1. Requires authenticated user.
2. Non-admin users can access only requests whose snapshotted `resolved_approver_user_ids` include their user id.
3. Requesters cannot approve, reject, or retry their own requests through approver APIs even if they appear in stale approver snapshots.
4. Admin APIs are the fallback path and can process every request, including a request submitted by the same admin account.
5. Reject requires `decision_reason`.
6. Approval may include optional `decision_reason`.

### Admin APIs

```text
GET /api/v1/admin/quota-reset/approver-candidates?source_id=1&department_external_id=department-alpha
GET /api/v1/admin/quota-reset/approver-configs
PUT /api/v1/admin/quota-reset/approver-configs
```

The approver candidates endpoint returns only local users matched to representatives for the selected department. It also returns unmatched directory representatives for admin diagnostics when a representative exists in the directory snapshot but has no local login user match yet. The PUT endpoint accepts flattened config rows. By default it replaces configured approvers only for departments present in `items`. If `mode` is `replace_all`, it replaces the full current-source approver config set. The service rejects rows whose `approver_user_id` is not a matched representative for the row's department. The Organization & Login settings UI is a full-list editor and therefore sends `mode: "replace_all"` deliberately.

```json
{
  "mode": "replace_departments",
  "items": [
    {
      "department_external_id": "department-alpha",
      "department_display_path": "Department Alpha",
      "approver_user_id": 7,
      "enabled": true
    }
  ]
}
```

```text
GET /api/v1/admin/quota-reset/requests?page=1&page_size=20&status=pending
POST /api/v1/admin/quota-reset/requests/:id/approve
POST /api/v1/admin/quota-reset/requests/:id/reject
POST /api/v1/admin/quota-reset/requests/:id/retry-reset
```

Admins can process any request. Admin approvals still use the same reset execution path and event model.

```text
GET /api/v1/admin/quota-reset/notification-settings
PUT /api/v1/admin/quota-reset/notification-settings
POST /api/v1/admin/quota-reset/notification-settings/test
```

Rules:

1. Test sends a synthetic payload and writes no request event.
2. Settings never return the bearer token plaintext, only credential id and masked credential summary.

## Frontend

### User Quota Reset Entry

Add a request entry point to the personal AI Usage quota area. The action opens a modal with:

1. Subscription group select.
2. Current usage and limit summary for the selected group.
3. Required reason textarea.
4. Submit button.

The modal must make clear that approval resets the selected group's daily, weekly, and monthly used amounts; it does not change future quota limits.

### Quota Reset Workbench

Add `/usage/quota-reset` under the AI Usage task zone.

Tabs:

1. `My Requests`: visible to all users.
2. `Approvals`: visible when the user has pending or historical approval assignments.
3. `All Requests`: visible to admins.

First-version filters:

1. Pending.
2. Processed.
3. Reset failed.

The request detail panel shows:

1. Requester.
2. Selected subscription group.
3. Reason.
4. Status.
5. Resolved approvers.
6. Matched department path evidence.
7. Decision reason.
8. Reset error when present.
9. A compact event timeline for the request only.

This compact timeline is not the future full audit module.

### Work Items Integration

The shared `/work-items` entry uses `/api/v1/work-items/counts`.

Quota reset contributes:

1. `quota_reset_approval_count`: actionable requests assigned to the current approver, excluding the approver's own requests.
2. `quota_reset_admin_count`: all actionable requests for admin fallback processing.

The shared counts contract also includes `ai_access_setup_count` for the current user's missing reusable AI access and `offboarding_count` for admin-only directory offboarding candidates. `total_count` is the number displayed in the sidebar badge. For admins, `total_count` uses personal AI access setup plus admin fallback quota-reset count plus offboarding count; it does not add the separate assigned-approver count again.

### Admin Settings

Add quota reset approval settings to `/settings` under Organization & Login because the feature depends on the current department tree.

Controls:

1. Full-list approver config table with readable department and approver identity, enable/disable, and delete.
2. Add-row form with a Directory Sync department dropdown that opens directly and filters departments inside the dropdown panel, followed by an approver dropdown loaded from matched representatives for that department. If the directory has representatives but none are matched to local login users, the UI shows the unmatched representative details as an admin diagnostic instead of a generic empty state.
3. Webhook enabled toggle.
4. Webhook URL with `http`/`https` validation when enabled.
5. Auth type select: none or bearer token.
6. Credential select for bearer token; bearer credentials must reference `secret_text` credentials.
7. Test webhook button.

Department tree selection and user multi-select are desirable polish once the settings surface needs stronger discoverability, but they are not required for the first release contract.

## Notification Contract

Outbound webhook sends `POST` with `Content-Type: application/json`.

If `auth_type=bearer_token`, the sender loads the configured `secret_text` credential and sends:

```text
Authorization: Bearer <secret>
```

Generic webhook payload shape:

```json
{
  "event": "quota_reset_request_created",
  "request_id": 123,
  "status": "pending",
  "requester_user_id": 10,
  "provider_id": 1,
  "group_id": "42",
  "group_name": "Group Alpha",
  "group_platform": "openai",
  "reason_preview": "Need to finish a time-sensitive build investigation.",
  "resolved_approver_user_ids": [7, 8],
  "action_url": "https://ai-efficiency.example.com/usage/quota-reset?request_id=123",
  "occurred_at": "2026-07-07T10:00:00Z"
}
```

When the configured URL targets an Enterprise WeChat group robot endpoint (`qyapi.weixin.qq.com/cgi-bin/webhook/send`), the sender adapts the request body to WeCom's text-message format:

```json
{
  "msgtype": "text",
  "text": {
    "content": "AI Efficiency 额度重置审批通知\n事件：新申请待审批\n申请ID：123\n订阅组：Group Alpha\n状态：pending\n原因：Need to finish a time-sensitive build investigation.\n处理入口：https://ai-efficiency.example.com/usage/quota-reset?request_id=123"
  }
}
```

Events that send notifications:

1. `quota_reset_request_created`: notify resolved approvers; if no approver is resolved, notify admin-facing webhook as fallback.
2. `quota_reset_request_cancelled`: notify that the request was cancelled.
3. `quota_reset_request_rejected`: notify requester.
4. `quota_reset_request_reset_succeeded`: notify that the reset succeeded.
5. `quota_reset_request_reset_failed`: notify resolved approvers and admins.
6. `quota_reset_notification_test`: sent only by the admin settings test action.

Rules:

1. Notification failures write `notification_failed` events and do not block or change request state.
2. Notification success writes `notification_sent` events.
3. Use short HTTP timeouts. Five seconds is the default.
4. Do not retry automatically in the first version to avoid duplicate external messages.
5. The webhook URL is admin-configured and must use `http` or `https`.
6. HTTP non-2xx responses are failures.
7. HTTP 2xx responses with JSON `errcode` present and non-zero are failures; the test action should surface the returned error text to admins.

## Error Handling

| Case | Behavior |
| --- | --- |
| Requester has no relay mapping | Reject create request with `403 no_relay_mapping` |
| Provider lacks subscription listing | Return `422 provider_unsupported` |
| Provider lacks quota reset | Allow option display only if resetter is supported; approve returns `422 provider_unsupported` if support disappears |
| Selected group is not active for requester | Reject create request with `400 inactive_subscription` |
| No approver config found | Create `pending` request with empty approvers; admin fallback only |
| All resolved approvers are the requester | Create `pending` request with empty approvers; admin fallback only |
| Request is already active for same user/group | Reject create request with `409 active_request_exists` |
| Concurrent approvals | First status transition wins; later request returns current state |
| Reset API fails or times out | Mark `approved_reset_failed`, preserve error, allow retry |
| Notification fails | Write event only; do not alter request status |

## Service Boundaries

Add `backend/internal/quotareset` as the workflow owner:

1. Request creation.
2. Subscription option loading.
3. Approver config management.
4. Approver resolution.
5. State transitions.
6. Reset execution.
7. Event writes.
8. Notification dispatch.

Reuse existing modules:

1. `backend/internal/directorytree` for parent traversal and display paths.
2. `backend/internal/directorysync.CurrentSourceID` for current directory snapshot resolution.
3. `backend/internal/relay.Provider` plus `relay.UserSubscriptionLister` and `relay.UserSubscriptionQuotaResetter`.
4. Existing credential encryption for webhook bearer token storage.

Do not add direct dependencies from `quotareset` into representative team usage internals. If common directory matching helpers are needed, extract a small reusable helper or keep the resolver local and tested.

## Testing

Backend tests:

1. User can list only active subscription groups.
2. Create request requires a reason and a valid active subscription group.
3. Single-department requester resolves nearest configured approver.
4. Multi-department requester resolves each path independently and merges approvers.
5. Child department approver overrides parent for that path.
6. Parent department approver applies when no child config exists.
7. Requester is excluded from approvers; admin fallback remains.
8. No approver config creates a pending admin-fallback request.
9. Any resolved approver can approve.
10. Non-approver cannot approve.
11. Admin can approve any request.
12. Approval calls `ResetSubscriptionQuotaForUser` with requester relay user id and selected group id.
13. Reset failure stores `approved_reset_failed` and writes `reset_failed`.
14. Retry from failed state reuses the same request and writes `reset_retried` with actor metadata.
15. Rejection requires a decision reason.
16. Cancellation is allowed only for requester-owned pending requests.
17. Duplicate active request guard rejects same user/provider/group.
18. Notification success and failure both write events without changing core status.
19. Credential-backed webhook settings do not return secret plaintext.

Frontend tests:

1. Request modal validates group and reason.
2. My Requests list renders statuses and cancel action.
3. Approvals tab appears for approvers and supports approve/reject.
4. Reset failed rows show retry action when allowed.
5. Admin settings can configure department approvers and webhook settings.
6. Notification credential select uses existing credential summaries without showing secret values.

Verification commands:

```text
cd backend && go test ./...
cd frontend && npm test
```

Run environment-sensitive browser checks separately if the implementation changes route layout or modal behavior.
