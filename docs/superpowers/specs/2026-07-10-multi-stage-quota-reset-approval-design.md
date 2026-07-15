# Lean Multi-Stage Quota Reset Approval Design

**Status:** Approved for reimplementation

**Date:** 2026-07-15

## Relationship To Existing Design

This spec extends the current quota reset approval contract in
`2026-07-07-quota-reset-approval-design.md`. It replaces the abandoned
normalized workflow-node design previously drafted under this file path.

Existing request, event, department approver, webhook, Work Items, and relay
reset behavior remains authoritative unless this spec explicitly changes it.

## Goal

Add a small, quota-reset-specific approval sequence:

1. First ask an approver for the requester's exact department.
2. Then process the subscription group's configured department chain in order.
3. Reuse an earlier approval when the same person appears in a later step.
4. Notify the active approvers with useful requester context and WeCom mentions.
5. Preserve decision comments and workflow history for a future audit module.

This is an incremental extension of the existing quota reset module, not a
general workflow engine.

## Confirmed Product Rules

### Initial step

For every exact department directly linked to the requester:

1. Use enabled `quota_reset_approver_configs` rows for that exact department.
2. If that department has no configured approver, use its synced department
   representatives that map to active local users.
3. Do not walk to parent departments.
4. Combine candidates from all requester departments into one initial step.
5. One approval from any candidate approves the whole initial step.
6. Exclude the requester from candidates.
7. If no department produces a candidate, omit the initial step and continue.

Configured approvers may be any active directory-matched local user. They do
not have to be a synced department representative. Representative membership
is used only by the fallback rule.

### Configured chain

Admins configure one ordered department list per relay provider subscription
group.

For each configured department:

1. Resolve candidates only from enabled exact-department approver configs.
2. Do not use representatives and do not walk to parent departments.
3. One candidate approval completes the step.
4. If no configured candidate is usable, the step remains actionable by admins
   as fallback.

If no chain exists, the request contains only the initial step. If neither an
initial step nor a configured chain step can be created, create one admin
fallback step so the request is never permanently unapprovable.

### Reused approvals

After an approval, inspect later steps in order. A later step is automatically
satisfied when its resolved candidate set contains any person who has already
approved an earlier step. Continue until the next unsatisfied step.

Automatically satisfied steps:

- do not create another manual decision;
- do not send an activation notification;
- record the satisfying actor and source decision in workflow state and events.

### Decisions and reset

- Approval and rejection comments are required and trimmed.
- Only the active step can be decided.
- A normal user must be an active step candidate and cannot approve their own
  request.
- Admins may decide the active step as a fallback/override, but cannot skip
  directly to a later step.
- Rejection ends the request immediately.
- Completing the final step enters the existing reset execution flow.
- Relay reset success/failure and retry semantics remain unchanged.

## Scope Control

### In scope

- One compact subscription-group chain configuration model.
- One request-owned JSON workflow document and integer revision.
- Exact-department resolution and representative fallback.
- Sequential decisions, prior-approver reuse, and admin fallback.
- Current-step Work Items counts and approval lists.
- Requester/team-focused notifications with WeCom mentions.
- Admin UI for department approvers, group chains, and notification channel.
- Request timeline and mandatory decision comments.
- Compatibility for existing single-stage requests.

### Explicitly out of scope

- A reusable workflow framework.
- Separate request-node, node-approver, or decision tables.
- Notification outbox, retries, delivery status dashboard, or idempotency API.
- Admin-authored message templates or arbitrary template expressions.
- Editing an in-flight workflow after request creation.
- General audit browsing UI.
- Reworking directory synchronization or relay APIs.

## Data Model

### New `quota_reset_approval_chains` table

One row represents one subscription group's configured chain.

| Field | Type | Contract |
| --- | --- | --- |
| `provider_id` | int | Relay provider id |
| `group_id` | string | Relay subscription group id |
| `group_name` | string | Display snapshot |
| `department_chain` | JSON | Ordered array of department references |
| `enabled` | bool | Disabled rows are ignored for new requests |
| `created_by_user_id` | int | Last known creator |
| `updated_by_user_id` | int | Last editor |
| `created_at` | time | Creation time |
| `updated_at` | time | Update time |

`(provider_id, group_id)` is unique.

Each `department_chain` item contains only:

```json
{
  "directory_source_id": 1,
  "department_external_id": "dept-alpha",
  "department_display_path": "Company / Group Alpha"
}
```

The service validates that items belong to the current successful directory
snapshot, rejects duplicate departments, and limits a chain to 20 items.

### Existing `quota_reset_requests` changes

Add three fields:

| Field | Type | Contract |
| --- | --- | --- |
| `workflow_version` | int | `1` for historical rows, `2` for this design |
| `workflow` | nullable JSON | Request-time workflow snapshot and progress |
| `workflow_revision` | int | Compare-and-swap revision for decisions |

No requester identity columns are added. Version 2 stores requester identity,
department paths, step candidates, and notification ids inside `workflow`.

`resolved_approver_user_ids` remains the indexed current-action field:

- version 1 keeps its existing meaning;
- version 2 always contains the active step's normal candidate user ids;
- admin-only fallback uses an empty array.

This preserves current approval-list and Work Items JSON containment queries.

### Workflow JSON

The application owns a versioned typed document:

```json
{
  "version": 2,
  "current_step": 0,
  "requester": {
    "user_id": 10,
    "display_name": "alice",
    "email": "alice@example.com",
    "department_paths": ["Company / Group Alpha"],
    "notification_ids": {"wecom": "alice"}
  },
  "steps": [
    {
      "kind": "requester_departments",
      "label": "Company / Group Alpha",
      "department_external_ids": ["dept-alpha"],
      "approvers": [
        {
          "user_id": 20,
          "display_name": "bob",
          "email": "bob@example.org",
          "source": "configured",
          "notification_ids": {"wecom": "bob"}
        }
      ],
      "admin_fallback": false,
      "status": "active"
    }
  ]
}
```

Step status is one of `queued`, `active`, `approved`,
`satisfied_by_prior_approval`, or `rejected`. A completed step stores one
decision object containing actor id/display snapshot, comment, admin flag, and
timestamp. A reused step stores the earlier approving actor and source step.

The workflow is bounded to 21 steps and 100 unique approvers. Decode or
validation failure returns an internal consistency error and never falls back
to permissive authorization.

### Existing request events

Keep `quota_reset_request_events` as the durable append-only audit source. Add
event types only for:

- `workflow_created`
- `step_approved`
- `step_satisfied`
- `step_activated`
- `admin_fallback_activated`

Approval/rejection event metadata contains step index, label, actor display
snapshot, comment, admin flag, and resulting revision. Existing reset and
notification events remain unchanged.

No separate decision table is introduced.

## Transaction And Concurrency Rules

Request creation writes the request plus `created`, `approver_resolved`, and
`workflow_created` events in one database transaction. Notification delivery
happens after commit.

Decision processing:

1. Load and validate the version 2 workflow.
2. Authorize against the active step.
3. Compute the next workflow state in memory.
4. Update the request only where `status = pending` and
   `workflow_revision = <loaded revision>`.
5. Increment the revision and append decision/transition events in the same
   transaction.
6. Treat a zero-row update as `ErrInvalidStatus`; the caller reloads instead of
   accepting a second decision.
7. Commit before notification or relay calls.

The final approval atomically changes the request to
`approved_resetting`. Existing reset execution then handles the external relay
call and terminal status.

## Resolution And Identity

The resolver reads one current successful directory snapshot and builds:

- exact requester department memberships;
- enabled exact-department approver configs;
- representative metadata;
- active directory members mapped to active local users;
- optional `metadata.wecom_userid` values.

Candidate identity matching prefers `directory_members.matched_user_id`, then
normalized email. Disabled directory members, relay-disabled local users, and
the requester are excluded.

`metadata.wecom_userid` is the only WeCom mention id. Missing values remain
missing; local ids and emails are not guessed as WeCom ids.

## Notification Contract

`quota_reset_notification_settings` adds an explicit `channel` enum:

- `generic_webhook`: stable structured JSON;
- `wecom_group_robot`: code-owned WeCom markdown preset.

The admin configures channel, enabled state, URL, auth type, and credential.
Message fields are not individually configurable. New channels can add another
adapter later without changing workflow state.

Active-step notification highlights:

- requester display name and email;
- requester department paths;
- subscription group and platform;
- full application reason within channel limits;
- current step and progress;
- previous approval comment when present;
- action URL;
- active approvers.

The WeCom preset renders `<@userid>` for active approvers with a valid
`wecom_userid`. It explicitly labels approvers without a mention id. Skipped
steps do not notify. Test notification uses synthetic example data only.

Notification failures remain non-blocking and append the existing failure
event without storing webhook secrets or full URLs in errors.

## API Changes

Keep existing request and decision routes. Decision bodies require a non-empty
`decision_reason` for both approve and reject.

Add admin routes:

- `GET /api/v1/admin/quota-reset/approval-chains`
- `PUT /api/v1/admin/quota-reset/approval-chains`

The list response includes enabled relay subscription group options. Department
dropdowns reuse the existing searchable Directory Sync department API, so this
feature does not add another full-directory transport or cache.

Existing approver candidate lookup changes from representative-only results to
active directory-matched members of the selected department. Results retain a
representative flag and WeCom-mention availability for display.

Request summaries add:

- `workflow_version`
- `current_step`
- `workflow_steps`

Historical version 1 summaries keep their existing shape and behavior.

## Frontend

Extend existing quota reset surfaces rather than adding a new store or settings
framework.

### Settings

The existing quota reset settings section contains three plain subsections:

1. Department approvers: department dropdown plus searchable member dropdown.
2. Subscription group chains: group dropdown plus reorderable department rows.
3. Notification: explicit channel dropdown and existing URL/auth controls.

Search input appears inside an opened dropdown. Controls use backend-returned
display paths/names and never expose editable internal id inputs.

### Approval workbench

The current quota reset page keeps Mine, Pending, and Admin views. Version 2
rows show current step/progress. The details view shows the compact step
timeline and prior comments. Approve and reject open one small dialog requiring
a comment.

Only backend-authorized active requests expose decision actions. Processed
history remains readable but is not counted as pending work.

## Compatibility And Migration

- Existing rows backfill to `workflow_version = 1`, empty workflow, revision 0.
- Version 1 requests continue through the existing single-stage service path.
- New requests use version 2 after the schema migration.
- Existing approver configs and notification settings remain in place.
- Existing notification rows default `channel` by URL during read/migration:
  WeCom robot URL becomes `wecom_group_robot`; all others become
  `generic_webhook`.
- No data migration creates request-node or decision rows.

## Testing Boundaries

Backend tests cover:

- exact-department config, representative fallback, multiple departments, and
  no-initial-step behavior;
- ordered group chain resolution and admin fallback;
- one approval per step, rejection, prior-actor reuse, and final reset;
- compare-and-swap conflict behavior;
- version 1 compatibility;
- current-step list and Work Items counts;
- requester context, channel selection, WeCom mentions, and synthetic tests;
- chain replacement validation and API authorization.

Frontend tests cover dropdown filtering, chain editing, explicit channel save,
mandatory comments, step progress, and action visibility. One browser test
covers request creation, initial approval, one configured chain approval, and
terminal success.

## Acceptance Criteria

1. A new request first targets exact-department configured approvers, then
   exact-department representatives only when config is absent.
2. Any candidate from any requester department can complete the initial step.
3. Subscription-group departments execute in configured order using only exact
   department approver configs.
4. A prior approving actor automatically satisfies every later step containing
   that actor without duplicate notification.
5. Every manual approval or rejection requires and preserves a comment.
6. Completing the final step invokes the existing relay quota reset exactly
   once for the winning transition.
7. Pending lists and badges reflect only the current active step.
8. WeCom notifications identify the requester/team/group/reason and mention
   active approvers when `metadata.wecom_userid` is available.
9. Admins configure departments, members, groups, chains, and channels through
   dropdown controls.
10. Historical version 1 requests remain operable.
11. The implementation adds no request-node, node-approver, or decision table.
