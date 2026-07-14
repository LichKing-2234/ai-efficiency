# Multi-Stage Quota Reset Approval Design

**Date:** 2026-07-10
**Status:** Current implemented contract
**Implementation:** Implemented through commit `42948d5` and fully verified on
2026-07-14. Task 12 browser, Compose, documentation, and final verification
evidence is recorded in the linked live plan.
**Scope:** `backend/ent/schema/`, `backend/internal/quotareset/`, `backend/internal/workitems/`, `backend/internal/handler/`, `backend/internal/directorysync/`, `frontend/src/api/`, `frontend/src/components/settings/`, `frontend/src/components/quota-reset/`, `frontend/src/views/QuotaResetView.vue`
**Related:**

- [2026-07-07-quota-reset-approval-design.md](./2026-07-07-quota-reset-approval-design.md)
- [2026-06-22-configurable-directory-sync-design.md](./2026-06-22-configurable-directory-sync-design.md)
- [2026-06-26-team-usage-representative-quota-design.md](./2026-06-26-team-usage-representative-quota-design.md)
- [docs/architecture.md](../../architecture.md)

## Spec Relationship

The 2026-07-07 quota reset approval spec is the historical predecessor to this
current implemented contract. This design supersedes these parts of the earlier
spec:

1. Single-node approval and immediate reset after the first approval.
2. Upward nearest-configured-department approver resolution.
3. The requirement that a configured department approver must be an
   organization representative for that department.
4. URL-inferred, hardcoded Enterprise WeChat notification formatting.

It does not change the relay boundary or the reset semantics. AI Efficiency
continues to own approval orchestration and calls
`relay.UserSubscriptionQuotaResetter` only after every required node is
satisfied.

`docs/architecture.md` describes this implemented runtime. The 2026-07-07 spec
remains unchanged as a point-in-time record of the earlier single-stage design.

## Data Hygiene

Tests, fixtures, docs, notification examples, screenshots, and command output
must not contain real employees, company email domains, subscription group
names, webhook URLs, credentials, tokens, or passwords.

Use synthetic values such as:

- `alice@example.com`
- `bob@example.org`
- `Department Alpha`
- `Department Beta`
- `Group Alpha`
- `https://hooks.example.com/ai-efficiency`
- `test-token`

Webhook URLs are credentials when they contain a robot key. Logs, API
responses, audit metadata, and test failures must redact query-string secrets.

## Pre-Implementation Findings

Before this contract was implemented, the workflow had one snapshotted
`resolved_approver_user_ids` array. Any one resolved approver or an admin can
approve, and approval immediately starts the quota reset. Approval comments are
optional, while rejection comments are required.

The earlier department approver settings only allowed local users matched to
organization representatives for the selected department. The resolver walks
each requester department path upward and selects the nearest configured
department.

The earlier notifier:

1. Stores one global URL and optional bearer credential.
2. Infers Enterprise WeChat from the URL host and path.
3. Sends a hardcoded `text` body for Enterprise WeChat.
4. Does not enrich the message with requester display identity or team paths.
5. Does not mention current approvers.

The active production Directory Sync mapping was verified during design
exploration to map the stable member external id from the source's Enterprise
WeChat user id field. The generic product contract must still keep
channel-specific recipient resolution behind the notification adapter boundary.

## Problem

Quota resets require more control than one approval followed by immediate
execution:

1. The first review should be performed by someone responsible for one of the
   requester's direct departments.
2. Different subscription groups may require different ordered approval chains.
3. One person's approval should not be requested repeatedly when that person is
   also an approver for later nodes.
4. Every manual decision needs a durable comment for later audit use.
5. Notifications need actionable requester and workflow context and must
   actually mention the people responsible for the active node.
6. Notification channel behavior must be explicitly selected by admins instead
   of inferred from URL shape.

## Goals

1. Build one initial logical node from all of the requester's current direct
   departments.
2. Prefer exact-department configured approvers for the initial node.
3. Fall back per department to synchronized organization representatives only
   when that exact department has no valid configured approver.
4. Never walk to parent departments for initial-node resolution.
5. Support an ordered configured-department approval chain per subscription
   group.
6. Resolve later nodes only from department approver configuration.
7. Let any one eligible approver satisfy a node.
8. Reuse one manual approval for every later node whose approver snapshot also
   contains that actor.
9. Require a comment for every manual approval and rejection.
10. Preserve immutable request, node, approver, decision, and identity snapshots
    for authorization, UI, and future audit tooling.
11. Notify only the currently active node and resolve its delivery recipients
    from current directory and access facts without mutating those snapshots.
12. Provide explicit, preset notification channel adapters, beginning with
    Enterprise WeChat group robot and generic JSON webhook.
13. Preserve admin visibility and current-node fallback without allowing an
    admin action to skip the remaining workflow.
14. Keep existing single-stage requests readable and executable under their
    original contract.

## Non-Goals

1. Do not introduce a general-purpose workflow engine.
2. Do not support arbitrary admin-authored JSON or message templates.
3. Do not support per-request edits to the snapshotted approval chain.
4. Do not change subscription assignment, group limits, rate multipliers, API
   keys, or reset semantics.
5. Do not modify sub2api source code or access its database directly.
6. Do not implement the future cross-feature audit center UI.
7. Do not add approval deadlines, escalation timers, delegation windows, or
   automatic reminder schedules in this iteration.
8. Do not send notifications for queued or automatically satisfied nodes.

## Captured Decisions

| Area | Decision |
| --- | --- |
| First-node department scope | All current direct department memberships |
| First-node config lookup | Exact department only; no ancestor traversal |
| First-node priority | Valid configured approvers first, then representatives for that same department |
| Multiple departments | Merge candidates into one node; any candidate can approve |
| Missing first-node candidate | Skip the first node and continue to the configured chain |
| Configured approver eligibility | Any active directory-matched local user; organization representative status is not required |
| Later-node source | Ordered department nodes configured per subscription group |
| Later-node resolution | Configured department approvers only; no organization representative fallback |
| Empty later node | Current-node admin fallback |
| Empty later chain | After the initial node is approved or skipped, reset immediately |
| Per-node quorum | Any one eligible approver |
| Approval reuse | A manual approval satisfies every later node containing the same actor |
| Decision comment | Required for approve and reject |
| Admin behavior | Admin may act on the current node only and cannot skip the chain |
| Snapshot timing | Entire workflow and notification identities snapshot when the request is created for authorization, UI, and audit |
| Config and directory changes | Approval configuration changes affect new workflow snapshots only; current directory identity and access changes are re-evaluated for node-activation and cancellation delivery |
| Notification templates | Code-owned preset per channel |
| Enterprise WeChat format | Preset `markdown` with `<@userid>` mentions |
| Notification routing | Current active node only; activation and cancellation use currently usable snapshotted users or current admins exclusively; skipped and pre-satisfied nodes produce no delivery |
| Audit | Normalized current state plus append-only request events |

## Approaches Considered

### Option A: Normalized Workflow Nodes and Decisions

Persist configured chains, request-node snapshots, node approvers, and manual
decisions in separate tables.

Pros:

1. Clear concurrency and authorization boundaries.
2. Efficient current-node and Work Items queries.
3. One decision can satisfy multiple later nodes without duplicating decisions.
4. Provides durable facts for the future audit module.

Cons:

1. Requires several new Ent schemas and compatibility handling.

### Option B: One JSON Workflow Field on the Request

Store all nodes, candidates, decisions, and state transitions in one JSON field.

Pros:

1. Fewer tables.

Cons:

1. Harder current-node counting and indexing.
2. Fragile concurrent updates.
3. Poorer referential integrity and audit queries.
4. More application-level schema validation.

### Option C: Event Replay as the Only Source of Current State

Persist only events and derive current state by replay.

Pros:

1. Complete event history.

Cons:

1. Unnecessary workflow-engine complexity.
2. More expensive list and Work Items queries.
3. Harder operational recovery.

## Decision

Use **Option A**. Normalized workflow tables own current state, while
`quota_reset_request_events` remains an append-only event stream.

## Terminology

1. **Direct department**: a department linked directly to the requester by the
   current `directory_member_departments` snapshot.
2. **Configured department approver**: an enabled local approver row explicitly
   assigned by an admin to one department.
3. **Synchronized department representative**: a directory member identified as
   a representative of one exact department by the current representative
   metadata contract.
4. **Initial node**: the first logical approval node built from all requester
   direct departments.
5. **Configured chain node**: one ordered department node from the selected
   subscription group's configured approval chain.
6. **Active node**: the only node that currently accepts a manual decision.
7. **Approval reuse**: satisfying a later node with an earlier manual approval
   by the same eligible actor.
8. **Admin fallback**: admin authorization to decide the current node. It is not
   permission to complete the entire chain in one action.
9. **Usable normal approver**: a local user matched to a member in the current
   successful directory snapshot, excluding the requester for that request. A
   relay mapping and a channel recipient id are not required to approve.

### Directory Member Identity Precedence

When mapping a current directory member to a local approver or notification
candidate, a non-null `matched_user_id` is authoritative. The resolver returns
that exact user only when the id exists in the caller's allowed user map; an
unavailable, deleted, or out-of-scope matched user leaves the member unmatched.
It must not reassign that member by email. Normalized-email fallback is allowed
only when `matched_user_id` is null.

This precedence applies both to workflow creation and to live activation or
cancellation recipient resolution. In particular, notification resolution may
use a user subset containing only snapshotted candidates; a member still
matched to another user cannot contribute its display identity or notification
ids to a candidate that later acquired the same email address. Requester
user-to-member lookup remains the separate algorithm documented below.

## Architecture

`backend/internal/quotareset` remains the workflow owner. It is responsible for:

1. Subscription option validation.
2. Department approver and subscription-chain configuration.
3. Initial-node and configured-chain resolution.
4. Immutable request workflow snapshots.
5. Current-node authorization and state transitions.
6. Approval reuse.
7. Reset execution and retry.
8. Request event writes.
9. Notification context creation and dispatch.

Existing boundaries remain:

1. `backend/internal/directorysync` and `backend/internal/directorytree` provide
   current organization facts.
2. `backend/internal/relay.Provider` and optional relay interfaces own upstream
   subscription reads and reset execution.
3. `backend/internal/workitems` queries the normalized active workflow state.
4. HTTP handlers remain thin and pass authenticated actor facts into the
   service.

## Configuration Model

### Department Approvers

Keep `quota_reset_approver_configs` as the single source for configured
department approvers.

Change its validation contract:

1. Department selection still comes from the current Directory Sync source.
2. The approver must be a current directory-matched local user who is eligible
   to log in.
3. The approver does not need to represent, lead, or belong to the selected
   department.
4. The requester is excluded only when resolving a specific request, not when
   saving global configuration.
5. Multiple enabled approvers per department are allowed.
6. An approver without an Enterprise WeChat recipient id may be configured, but
   the settings UI must show a notification-coverage warning.

The admin candidate API must therefore become a paginated searchable
directory-matched local-user lookup instead of a selected-department
representative lookup.

### Subscription Group Approval Chains

Add one optional chain per `provider_id + group_id`.

Each chain:

1. Snapshots the group name for readability.
2. Contains zero or more ordered department nodes.
3. Uses a department at most once.
4. May reference only a current department with at least one enabled department
   approver when saved.
5. Is local AI Efficiency state and is not overwritten by Directory Sync or
   relay refreshes.

A zero-node or absent chain means that the workflow contains only the initial
node. If the initial node is skipped, reset execution begins immediately.

Deleting or disabling the final enabled approver row for a department used by
an enabled chain must be rejected with the referencing subscription groups.
Runtime admin fallback still covers later drift, such as a snapshotted user
becoming unable to log in.

## Data Model

### `quota_reset_approval_chains`

Fields:

| Field | Type | Notes |
| --- | --- | --- |
| `id` | int | Primary key |
| `provider_id` | int | Relay provider |
| `group_id` | string | Subscription group id |
| `group_name` | string | Display snapshot |
| `enabled` | bool | Disabled chains are ignored for new requests |
| `created_by_user_id` | int | Admin actor |
| `updated_by_user_id` | int | Admin actor |
| `created_at` | time | |
| `updated_at` | time | |

Unique index: `(provider_id, group_id)`.

### `quota_reset_approval_chain_nodes`

Fields:

| Field | Type | Notes |
| --- | --- | --- |
| `id` | int | Primary key |
| `chain_id` | int | Parent chain |
| `position` | int | Zero-based order within configured chain |
| `directory_source_id` | int | Source active when saved |
| `department_external_id` | string | Exact configured department |
| `department_display_path` | string | Readable snapshot |
| `created_at` | time | |
| `updated_at` | time | |

Indexes:

1. Unique `(chain_id, position)`.
2. Unique `(chain_id, department_external_id)`.
3. `(directory_source_id, department_external_id)`.

### Changes to `quota_reset_requests`

Add:

| Field | Type | Notes |
| --- | --- | --- |
| `workflow_version` | int | Immutable creation fact; existing rows backfill to `1`; new workflow uses `2` |
| `current_node_id` | nullable int | Active v2 node |
| `workflow_completed_by_decision_id` | nullable int | Decision that completed the final unsatisfied node |
| `requester_display_name_snapshot` | string | Immutable notification and audit identity |
| `requester_email_snapshot` | string | Immutable notification and audit identity |
| `requester_department_paths` | nullable JSON array | Historical v1 `NULL` is allowed; new Ent creates receive a fresh `[]`; immutable after insert |
| `requester_notification_ids` | nullable JSON object | Historical v1 `NULL` is allowed; new Ent creates receive a fresh `{}`; immutable after insert; channel-keyed ids, for example `{"wecom":"alice-id"}` |

Existing v1 resolution and decision fields remain for compatibility. V2 services
must treat request nodes and decisions as authoritative.

All request creation facts are immutable after insert: `requester_user_id`,
`requester_relay_user_id`, `provider_id`, `group_id`, `group_name`,
`group_platform`, `reason`, `workflow_version`, both requester identity snapshots,
both requester JSON snapshots, the existing `resolved_approver_user_ids` and
`matched_department_paths` resolution snapshots, and `created_at`. Workflow state,
the v1 approval/rejection/reset/decision state, and `updated_at` remain mutable.

The four request JSON creation snapshots (`requester_department_paths`,
`requester_notification_ids`, `resolved_approver_user_ids`, and
`matched_department_paths`) remain SQL-nullable because they live on an existing
production table and historical v1 rows may contain SQL `NULL`. Their application
defaults are factories that return a fresh non-nil empty slice or map for each new
Ent create. Explicit nil setters fail schema validation, as do nil element maps in
`matched_department_paths`; empty non-nil containers remain valid.

Ent's `Optional().Immutable()` JSON generation exposes `Clear*` methods on the
public mutation object even though update builders expose no normal setters. A
`QuotaResetRequest` schema hook rejects those four clear flags unconditionally,
including direct `Mutation()` calls, before SQL executes. It does not trust the
mutable reported mutation operation, so `SetOp` cannot relabel an update and
bypass the guard. Thus historical `NULL` remains readable without allowing a
stored snapshot to be cleared after creation.

When the legacy v1 resolver has no approver snapshot, its create builder omits
that setter and lets the schema factory supply `[]`. This is distinct from an
explicit nil setter, which remains a validation error.

### `quota_reset_request_nodes`

Fields:

| Field | Type | Notes |
| --- | --- | --- |
| `id` | int | Primary key |
| `request_id` | int | Parent request |
| `position` | int | Initial node is zero; configured nodes follow |
| `node_type` | enum | `requester_departments` or `configured_department` |
| `label` | string | Readable snapshot |
| `department_snapshots` | JSON array | Non-null, defaults to `[]`; explicit nil containers and nil element maps fail Ent schema validation |
| `status` | enum | See node state model |
| `admin_fallback_required` | bool | Immutable creation-time fact that no normal candidate was usable when the workflow was resolved |
| `satisfied_by_decision_id` | nullable int | Manual decision satisfying this node |
| `activated_at` | nullable time | |
| `completed_at` | nullable time | |
| `created_at` | time | |
| `updated_at` | time | |

`request_id`, `position`, `node_type`, `label`, `department_snapshots`,
`admin_fallback_required`, and `created_at` are immutable after insert. Workflow
state fields `status`, `satisfied_by_decision_id`, `activated_at`, `completed_at`,
and `updated_at` remain mutable.

An omitted `department_snapshots` setter receives a fresh empty default for that
create. An explicit nil container or any nil element map is rejected before
create; empty non-nil arrays remain valid. Business-content validation belongs to
workflow resolution, not this schema validator.

Indexes:

1. Unique `(request_id, position)`.
2. PostgreSQL partial unique `(request_id) WHERE status = 'active'`.
3. `(request_id, status)`.
4. `(status, activated_at)`.

### `quota_reset_request_node_approvers`

Stores immutable normal-approver snapshots.

Fields:

| Field | Type | Notes |
| --- | --- | --- |
| `id` | int | Primary key |
| `request_node_id` | int | Parent node |
| `user_id` | int | Local user id |
| `display_name` | string | Snapshot |
| `email` | string | Snapshot |
| `source` | enum | `configured` or `directory_representative` |
| `source_department_external_ids` | JSON array | Non-null, defaults to `[]`; explicit nil fails Ent schema validation |
| `notification_ids` | JSON object | Non-null, defaults to `{}`; explicit nil fails Ent schema validation |
| `created_at` | time | |

Every field is immutable after insert.

These rows, including their display names, emails, and notification ids, remain
the authorization, UI-summary, and audit snapshots. Notification delivery does
not rewrite them. Node activation and cancellation use their immutable user ids
as the candidate set, then resolve current delivery people separately.

Omitted setters receive fresh empty defaults for each create. Explicit nil
containers are rejected before create, while empty non-nil arrays and objects
remain valid. Request, node, and node-approver JSON snapshots share schema-only
factory and validator helpers, while only the existing request table retains SQL
nullability for legacy compatibility.

Unique index: `(request_node_id, user_id)`.

### `quota_reset_request_decisions`

Stores one row for every manual decision.

Fields:

| Field | Type | Notes |
| --- | --- | --- |
| `id` | int | Primary key |
| `request_id` | int | Parent request |
| `request_node_id` | int | Node active when decision was submitted |
| `actor_user_id` | int | Local user |
| `actor_display_name` | string | Snapshot |
| `decision` | enum | `approve` or `reject` |
| `comment` | string | Required and non-empty |
| `admin_override` | bool | Actor used current admin permission |
| `created_at` | time | |

Every field is immutable after insert.

Indexes:

1. Unique `(request_node_id)` so only one manual decision wins per node.
2. `(request_id, created_at)`.
3. `(actor_user_id, created_at)`.

One approval decision may be referenced by multiple later request nodes through
`satisfied_by_decision_id`.

### Request Events

Extend `quota_reset_request_events.event_type` with:

1. `workflow_snapshotted`
2. `node_activated`
3. `node_approved`
4. `node_satisfied_by_prior_approval`
5. `node_skipped_no_approver`
6. `admin_fallback_activated`

Event metadata may contain ids, positions, counts, channel type, and delivery
recipient coverage. It must not duplicate full comments, reasons, webhook URLs,
or channel recipient ids.

## Initial Node Resolution

Workflow resolution and persistence run in one PostgreSQL repeatable-read
transaction. The transaction begins before the resolver reads the current
directory source, users, approver configuration, approval chain, or chain nodes;
the same Ent transaction persists the request, node and approver snapshots, and
creation events. Consequently, a concurrent atomic directory apply or approval
chain replacement may affect the next request, but cannot produce one immutable
request from facts that never coexisted in a committed database snapshot.
Request creation does not take the admin approval-configuration `SystemSetting`
lock; repeatable-read isolation is the consistency boundary.

Relay subscription preflight occurs before this transaction. Each creation
attempt records the requester relay id used to list active subscriptions and
validate the requested group. After the transaction begins, request creation
re-reads the requester through the transaction client. If the transaction relay
id differs, the attempt rolls back before resolver or persistence work and
retries the complete requester, provider, subscription, group, and active-request
preflight with the new binding. Creation uses a maximum of three attempts; if
the binding changes on every attempt, it returns an explicit wrapped binding
churn error instead of looping. If the new binding lacks the requested group,
the normal `ErrInactiveSubscription` behavior applies with no request. If the
binding disappears, creation returns `ErrNoRelayMapping` with no request. Only
an attempt whose subscription lookup and transaction snapshot use the same
relay id may persist. The immutable requester relay id, display name, and email
snapshots come from that transaction view and its directory resolution, not
from an earlier attempt's user object.

Within that transaction, the resolver uses the current successful full-company
directory snapshot.

Algorithm:

1. Match the requester to the current directory member by local user id, with
   normalized-email fallback.
2. Read every direct membership from `directory_member_departments`. Use
   `directory_members.department_external_id` only as the compatibility fallback
   when membership rows are absent.
3. For each exact direct department, query enabled configured approvers for that
   same `directory_source_id + department_external_id`.
4. Remove the requester and users that are not currently usable.
5. If valid configured candidates remain for that department, add them with
   source `configured` and do not add organization representatives for that
   department.
6. Otherwise resolve synchronized representatives for that exact department
   from `department.metadata.representative_external_ids` and
   `member.metadata.leader_department_ids`.
7. Remove the requester and unusable representatives.
8. Merge candidates from all direct departments into one node, deduplicating by
   local user id while preserving all source-department evidence.
9. Do not inspect parent departments.
10. If the merged candidate set is empty, persist the initial node as
    `skipped_no_approver` and continue to the first configured chain node.

If there is no current successful directory snapshot, request creation returns
`503 directory_unavailable`. It must not silently bypass the initial node because
the organization service is temporarily unavailable.

If a current directory snapshot exists but the requester has no matched member
or no memberships, persist a skipped initial node with explicit resolution
evidence and continue to the configured chain.

## Configured Chain Resolution

For each enabled chain node in position order:

1. Preserve the configured department identity and display path.
2. Query enabled configured approvers for the current source and exact
   department.
3. Remove the requester and unusable users.
4. Do not use synchronized organization representatives.
5. Snapshot the remaining candidates.
6. If no candidate remains, persist the node with
   `admin_fallback_required=true`.

The full chain and all normal candidate identities are immutable after request
creation. Later admin configuration changes affect only new requests. Directory
Sync and local access changes do not alter existing workflow authorization or
UI/audit snapshots, but they are re-evaluated when resolving activation and
cancellation delivery recipients.

Admin authorization is evaluated at decision time because admin roles may
legitimately change. Admins are not inserted into every node's normal approver
snapshot.

## Node State Model

Node statuses:

1. `queued`
2. `active`
3. `approved`
4. `satisfied_by_prior_approval`
5. `skipped_no_approver`
6. `rejected`

Invariants:

1. At most one node per request is `active`.
2. Only `active` accepts a manual decision.
3. `satisfied_by_prior_approval` references an earlier approve decision by a
   user present in that node's normal approver snapshot.
4. The initial node may be `skipped_no_approver`. Configured later nodes are
   never skipped for missing candidates; they require admin fallback.
5. Rejection is terminal for the overall request.
6. Reset starts only when no `queued` or `active` node remains.

Request status remains:

1. `pending` while approval nodes remain.
2. `approved_resetting` during execution.
3. `approved_reset_succeeded` on success.
4. `approved_reset_failed` on failure.
5. `rejected` after any node rejection.
6. `cancelled` after requester cancellation.

## Decision Processing

Decision requests include `request_id`, `request_node_id`, and a non-empty
`comment`.

Approval transaction:

1. Lock the request and current node.
2. Require overall status `pending` and the supplied node to equal
   `current_node_id` with status `active`.
3. Reject requester self-approval.
4. Authorize either a normal snapshotted node approver or a current admin.
5. Insert one approve decision and mark the current node `approved`.
6. Scan every later `queued` node.
7. If the actor appears in a later node's normal approver snapshot, mark that
   node `satisfied_by_prior_approval` and reference the same decision.
8. Do not treat admin role alone as membership in later nodes.
9. Activate the first remaining `queued` node, or atomically move the request to
   `approved_resetting` when none remains.
10. Commit before notification or relay I/O.

Rejection transaction:

1. Apply the same current-node and authorization checks.
2. Insert one reject decision.
3. Mark the current node and overall request `rejected`.
4. Leave queued nodes unchanged as immutable evidence that they were never
   reached.

Concurrent or stale decisions return `409 workflow_advanced` with the latest
request summary. They must not act on a newly activated node.

The requester may cancel while the request is `pending`. Existing decisions
remain immutable. Cancellation is not allowed after reset execution starts.

## Reset Execution and Retry

After the approval transaction commits:

1. Execute the existing provider-scoped quota reset once.
2. Preserve daily, weekly, and monthly reset semantics.
3. Write existing reset-started, succeeded, or failed events.
4. Do not reopen approval nodes after a reset failure.

For v2 requests, reset retry is allowed to:

1. The actor of `workflow_completed_by_decision_id`.
2. A current admin.

Legacy v1 requests retain their existing retry authorization.

## Admin Fallback

Admins can view every request and decide the current active node at any time.

Rules:

1. One admin decision completes only the current node.
2. It does not skip later nodes because the actor is an admin.
3. Approval reuse still applies when the same admin user id is explicitly in a
   later node's normal approver snapshot.
4. `admin_override=true` is recorded when the actor was not a normal candidate
   for the current node.
5. For node activation and cancellation, re-evaluate the immutable snapshotted
   normal user ids against the current successful directory snapshot and local
   access state. If any remain usable, target only those current people. If none
   remain usable, target current admins exclusively, regardless of the node's
   immutable creation-time `admin_fallback_required` value. Exclude the
   request's requester from this fallback recipient set even when that user is
   currently an admin; retain every other current admin under the existing
   admin policy.
6. Normal nodes with usable candidates do not proactively mention admins.

## Notification Architecture

Replace URL-shape inference with an explicit channel adapter registry.

Initial channel types:

1. `wecom_group_robot`
2. `generic_webhook`

Each adapter owns:

1. Type-specific setting validation.
2. Preset rendering.
3. Headers and authentication.
4. Response business-error validation.
5. Payload size enforcement and safe truncation.

The notifier receives a channel-neutral `NotificationContext` containing:

1. Event and occurrence time.
2. Request id and status.
3. Requester display name, email, direct department paths, and notification ids.
4. Subscription group name and platform.
5. Full request reason for rendering, bounded by adapter limits.
6. Current node label, position, total node count, and approvers.
7. Completed decision history with actor names and comments.
8. Action URL.

No adapter receives relay credentials, webhook credentials, API keys, or reset
provider secrets in the rendering context.

### Notification Settings

Extend the single global notification setting with:

| Field | Type | Notes |
| --- | --- | --- |
| `channel_type` | enum | `wecom_group_robot` or `generic_webhook` |
| `template_version` | int | Code-owned preset version |

Retain enabled state, URL, auth type, and optional credential reference.

Rules:

1. Admins explicitly select `channel_type`.
2. Runtime delivery never infers channel type from URL.
3. `wecom_group_robot` validates the Enterprise WeChat group robot host and
   endpoint path and uses no bearer-token auth.
4. `generic_webhook` supports existing none or bearer-token auth.
5. Settings reads return a redacted URL preview and `url_configured` rather than
   exposing a robot key.
6. An update may omit the URL to retain the existing secret value.
7. Delivery and error messages redact URL query strings.
8. A one-time migration may classify an existing saved setting by URL so the
   currently configured channel keeps working. After backfill, channel type is
   explicit.

### Enterprise WeChat Preset

Use `msgtype=markdown`, not `markdown_v2`. Enterprise WeChat's current group
robot contract supports `<@userid>` mentions in `text` and `markdown` content,
while `markdown_v2` does not support member mentions.

Preset example with synthetic data:

```text
# 额度重置待审批
<font color="warning">Alice 提交了额度重置申请</font>

> 申请人：Alice
> 所属团队：Department Alpha / Team One
> 订阅组：Group Alpha
> 申请原因：Complete a time-sensitive build investigation.
> 当前节点：2/3 · Department Beta
> 审批进度：1/3

待审批：<@bob-wecom-id>
[进入待处理](https://ai-efficiency.example.com/usage/quota-reset?request_id=123)
```

Rendering rules:

1. Put action state, requester, team, subscription group, and reason before
   secondary metadata.
2. Include the latest completed decision actor and comment when activating a
   later node.
3. Mention every unique, mentionable current-node approver.
4. For admin fallback, mention current admins with resolvable Enterprise WeChat
   ids.
5. If an approver lacks a recipient id, include their display name followed by
   an unavailable-mention marker and record coverage in event metadata.
6. Missing recipient ids do not fail the whole delivery.
7. Truncate low-priority fields before requester, group, action, current node,
   and action URL.
8. Never send a separate mention-only message.

For node activation and cancellation, the recipient resolver starts from the
immutable current-node approver user ids but derives delivery people from the
current successful directory snapshot and current local access state. It uses
the same usability predicate as workflow creation: the user must have an active
current directory member, must not be the requester, and must have neither
`relay_disabled_at` nor `token_valid_after` set. A usable approver remains the
normal recipient even when no channel recipient id is available; missing ids
produce coverage output rather than admin fallback. If no snapshotted user is
currently usable, delivery switches to current admins only and never combines
stale normal users with admins. The requester is removed from this node-fallback
admin set even when currently an admin, preventing self-notification without
changing global admin eligibility or other event routing. Duplicate user ids
are removed.

Live delivery people receive their current display name, email, and
channel-specific notification identities. The immutable approver rows continue
to provide authorization, UI, and audit summaries. The Enterprise WeChat
adapter supports the current deployment's member external-id mapping and may
prefer an explicit allowlisted `member.metadata.wecom_userid` mapping when
present. Future channel adapters add their own resolver without changing
workflow state.

### Generic Webhook Preset

Send a stable versioned JSON object:

```json
{
  "schema_version": 2,
  "event": "quota_reset_approval_node_activated",
  "request": {
    "id": 123,
    "status": "pending",
    "requester": {
      "display_name": "Alice",
      "email": "alice@example.com",
      "departments": ["Department Alpha / Team One"]
    },
    "subscription_group": {
      "id": "42",
      "name": "Group Alpha",
      "platform": "openai"
    },
    "reason": "Complete a time-sensitive build investigation."
  },
  "current_node": {
    "id": 456,
    "position": 2,
    "total": 3,
    "label": "Department Beta",
    "approvers": [
      {"user_id": 7, "display_name": "Bob"}
    ]
  },
  "approval_history": [],
  "action_url": "https://ai-efficiency.example.com/usage/quota-reset?request_id=123",
  "occurred_at": "2026-07-10T10:00:00Z"
}
```

The generic payload excludes channel recipient ids by default.

### Notification Events and Routing

Send:

1. `quota_reset_approval_node_activated` when a real node becomes active.
2. `quota_reset_request_rejected` to mention the requester when possible.
3. `quota_reset_request_cancelled` to inform the active approvers.
4. `quota_reset_request_reset_succeeded` to mention the requester.
5. `quota_reset_request_reset_failed` to mention the requester, completion
   decision actor, and current admins when possible.
6. `quota_reset_notification_test` for explicit admin testing.

Do not send:

1. A separate request-created message in addition to the first node activation.
2. Notifications for `skipped_no_approver`.
3. Notifications for `satisfied_by_prior_approval`.
4. Notifications for future queued nodes.

Notification failure remains non-blocking. It writes a redacted
`notification_failed` event. HTTP non-2xx and Enterprise WeChat non-zero
`errcode` responses are failures. Keep the existing short timeout and no
automatic retry in this iteration.

The test action uses synthetic request data. For Enterprise WeChat, it mentions
the triggering admin only when a resolvable recipient id exists and returns a
coverage warning otherwise.

## API Contract

### Admin Configuration

Add:

1. `GET /api/v1/admin/quota-reset/approver-candidates` with search and
   pagination over active directory-matched local users.
2. `GET /api/v1/admin/quota-reset/approval-chains`.
3. `PUT /api/v1/admin/quota-reset/approval-chains` using explicit full-list
   replace semantics.
4. `GET /api/v1/admin/quota-reset/approval-chain-options` for current
   subscription groups and selectable configured departments.

Keep existing approver and notification settings routes, with their request and
response contracts extended as described above.

### Decisions

Keep the current approve and reject route shape, but v2 payloads require:

```json
{
  "request_node_id": 456,
  "decision_reason": "Approved for the current release investigation."
}
```

For legacy v1 requests, `request_node_id` remains optional and existing behavior
continues.

### Request Responses

Add a versioned workflow summary:

1. Current node.
2. Ordered node snapshots and status.
3. Named approver snapshots appropriate for the viewer.
4. Manual decisions and comments.
5. Whether the viewer can approve, reject, cancel, or retry.
6. Admin fallback state.

The backend is the authorization source. The frontend must not infer action
permission by comparing raw user-id arrays.

## Frontend Design

Split the current large `QuotaResetApprovalSettings.vue` surface into focused
components:

1. `DepartmentApproverSettings.vue`
2. `SubscriptionGroupApprovalChains.vue`
3. `QuotaResetNotificationSettings.vue`

The parent settings section coordinates loading and shared success/error
feedback only.

### Department Approvers

1. Keep direct department dropdown selection with in-dropdown filtering.
2. Replace representative-only candidates with a paginated user search.
3. Show display name, email, current department paths, and Enterprise WeChat
   mention coverage.
4. Preserve readable rows, enable/disable, and delete.
5. Show chain-reference errors before destructive changes.

### Subscription Group Chains

1. Select one current subscription group.
2. Show its ordered department nodes.
3. Add nodes from departments with enabled approver configuration.
4. Support add, delete, move up, and move down.
5. Prevent duplicate departments.
6. Show stale group, source, department, and approver configuration warnings.
7. Save all chains atomically.

### Notification Settings

1. Explicit channel type selector.
2. Type-specific URL and auth controls.
3. Read-only preset-format description and synthetic preview.
4. Mention-coverage status for Enterprise WeChat.
5. Test button with returned success, warning, or business-error detail.
6. No arbitrary JSON or template editor.

### Approval Workbench

Keep:

1. `My Requests`.
2. `Approvals`.
3. `All Requests` for admins.

Request detail shows:

1. Requester identity and all direct team paths.
2. Subscription group and reason.
3. Ordered node timeline.
4. Current node and named candidates.
5. Manual decision comments.
6. `Satisfied by an earlier approval from <name>` for reused decisions.
7. Admin override and fallback markers.
8. Reset result or failure.

`Approvals` defaults to requests whose current node is assigned to the current
user. Historical filters may show nodes the user manually decided. Future queued
assignments remain hidden until activated.

## Work Items

For v2 requests:

1. `quota_reset_approval_count` counts `pending` requests whose active node has
   the current user as a normal candidate, excluding the user's own requests.
2. It also counts `approved_reset_failed` when the user is the workflow
   completion decision actor and may retry.
3. `quota_reset_admin_count` counts all `pending` and
   `approved_reset_failed` requests because admins can act on the current node or
   retry.
4. Admin `total_count` keeps the current deduplication rule and uses the admin
   quota count rather than adding personal assignment count again.
5. Automatically satisfied, skipped, queued, completed, rejected, and cancelled
   nodes do not create separate work-item counts.

Legacy v1 requests retain current count semantics.

## Error Handling

| Case | Behavior |
| --- | --- |
| No current successful directory snapshot | Reject create with `503 directory_unavailable` |
| Requester unmatched in a valid directory snapshot | Persist skipped initial node and continue |
| No direct memberships | Persist skipped initial node and continue |
| First-node configured users unusable | Fall back to same-department representatives |
| First-node config and representative both unavailable | Skip initial node |
| Later configured node has no usable configured approver | Activate with admin fallback |
| Requester is the only candidate | Exclude requester; apply normal empty-node behavior |
| Chain configuration changes after create | No effect on existing request |
| Actor already approved an active earlier node | Later matching nodes are pre-satisfied |
| Stale node decision | Return `409 workflow_advanced` with latest summary |
| Concurrent decisions | First committed decision wins |
| Some snapshotted node approvers become unusable before activation/cancellation delivery | Notify only the snapshotted approvers that remain currently usable |
| Every snapshotted node approver becomes unusable before activation/cancellation delivery | Notify current admins exclusively without changing workflow snapshots or authorization |
| Directory member has a non-null matched user outside the allowed candidate set, but its email matches a candidate | Treat the member as unmatched; do not email-fallback or expose its notification identity |
| Notification recipient id missing | Send without that mention and return/log coverage warning |
| Notification delivery fails | Record event; do not change workflow |
| Reset fails | Preserve completed approvals and allow authorized retry |

## Compatibility and Migration

1. Add `workflow_version` with existing rows backfilled to `1`.
2. New requests use version `2`.
3. Do not synthesize node rows for historical or active v1 requests.
4. Route v1 decisions, retries, list responses, and Work Items through the
   existing behavior.
5. Keep legacy fields until a later, separately designed data migration.
6. Keep all existing department approver rows.
7. Backfill the existing notification channel type once, preserving the current
   delivery behavior.
8. Do not auto-create subscription group chains. Their absence has explicit
   zero-node semantics.
9. Update `docs/architecture.md` only when v2 code becomes the current runtime.

## Testing

### Backend

Cover:

1. Exact-department config wins without ancestor traversal.
2. A configured non-representative is accepted and selected.
3. Same-department representative fallback.
4. Multiple direct departments merge into one initial node.
5. Per-department priority when one department has config and another requires
   representative fallback.
6. Empty initial node skips to the configured chain.
7. Later nodes never use organization representative fallback.
8. Empty later node requires admin.
9. Empty chain resets after the initial node.
10. Requester self-approval exclusion.
11. Snapshot immutability after config and directory changes.
12. Any one candidate satisfies a node.
13. Non-contiguous later nodes are satisfied by one earlier actor.
14. Admin override completes only the current node.
15. Approve and reject comments are required.
16. Rejection, cancellation, reset success, failure, and retry.
17. Row-lock and stale-node conflict behavior.
18. Active-node Work Items counts.
19. Legacy v1 behavior.
20. Explicit channel selection with no runtime URL inference.
21. Enterprise WeChat Markdown rendering, requester/team context, approver
    mentions, safe truncation, and missing-recipient coverage.
22. Enterprise WeChat business-error responses.
23. Generic versioned JSON rendering.
24. URL and error redaction.

### Frontend

Cover:

1. Searchable non-representative approver selection.
2. Enterprise WeChat mention-coverage warnings.
3. Subscription group chain add, delete, reorder, duplicate prevention, and
   reference validation.
4. Explicit notification channel controls and preset previews.
5. Required approve and reject comments.
6. Node timeline, reused approval, fallback, and admin override rendering.
7. Backend-provided action permissions.
8. Current-node-only approval queues and badges.
9. Legacy v1 request rendering.

### Browser Verification

Use distinct synthetic authenticated roles:

1. Requester with multiple direct departments.
2. First-node configured approver.
3. First-node representative fallback approver.
4. Later configured approver.
5. Actor appearing in multiple non-adjacent nodes.
6. Admin fallback.

Verify:

1. Settings save and reload.
2. Request creation snapshots the intended nodes.
3. Only the active node appears in pending work.
4. Approval reuse skips later matching nodes and produces no duplicate
   notification.
5. Final approval triggers one reset.
6. Enterprise WeChat payload contains requester/team details and the intended
   `<@userid>` values.
7. Desktop and mobile layouts have no overlap or clipped decision controls.

### Verification Commands

```text
cd backend && go test ./...
cd frontend && npm test
cd frontend && npm run build
cd frontend && npm run test:e2e:role
git diff --check
```

Environment-sensitive browser and real webhook checks must be reported
separately from deterministic unit tests.

## Acceptance Criteria

1. A requester's first node uses only exact direct departments, with
   per-department config priority and representative fallback.
2. Admins can configure non-representative department approvers.
3. Subscription groups can have ordered configured-department chains.
4. A node needs only one approval.
5. One approval automatically satisfies every later node containing that actor.
6. Every manual decision has a non-empty durable comment.
7. Admins can handle only the current node and cannot bypass remaining nodes.
8. Existing requests keep their original behavior.
9. Notifications identify requester, teams, subscription group, reason,
   current node, progress, and action URL.
10. Enterprise WeChat activation and cancellation notifications revalidate
    snapshotted approvers and mention currently usable recipients using current
    synchronized ids, falling back exclusively to current admins only when no
    normal approver remains usable.
11. Automatically satisfied nodes do not produce duplicate notifications.
12. Admins explicitly choose a preset notification channel type.
13. Work Items counts represent only actionable current work.
14. Request nodes, approvers, decisions, reset events, and notification events
    provide sufficient durable facts for the future audit module.
