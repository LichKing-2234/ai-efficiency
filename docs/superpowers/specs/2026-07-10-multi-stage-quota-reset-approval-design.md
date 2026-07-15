# Quota Reset Sequential Approval Design

**Status:** Implemented on the feature branch; final regression verification is pending

**Date:** 2026-07-15

## Relationship To Existing Design

This spec extends
`docs/superpowers/specs/2026-07-07-quota-reset-approval-design.md`. The older
spec remains the source for the request form, request/event records, Work Items,
relay reset behavior, and the original department approver settings.

This document replaces the abandoned generic workflow-node proposal previously
drafted for this follow-up. The implemented design is quota-reset-specific and
adds one business table only.

## Goal

For a user's subscription-group quota reset request:

1. Obtain one approval from the user's exact department approvers.
2. Continue through the selected subscription group's ordered department chain.
3. Reuse an earlier person's approval when the same person appears later.
4. Notify only the currently actionable approvers with requester context.
5. Reset the quota once after the final approval and preserve all decisions for
   future audit UI work.

## Product Contract

### Request

- The requester selects one active subscription group.
- The application reason is required.
- Only one active request may exist for the same requester, provider, and group.
- The workflow is resolved and snapshotted when the request is created. Later
  directory or admin configuration changes do not rewrite an in-flight request.

### Initial step

The initial step uses only departments directly linked to the requester in the
current successful Directory Sync snapshot.

For each exact requester department:

1. Use enabled `quota_reset_approver_configs` rows for that exact department.
2. If no config exists for that department, fall back to synced representatives
   for that exact department.
3. Do not walk to parent departments.
4. Exclude the requester and inactive or unmatched users.

Candidates from all exact requester departments are merged into one step. One
candidate approval completes the step. Configured approvers may be any active,
directory-matched member of the selected department; only fallback approvers
must be representatives.

If no exact department yields a candidate, omit the initial step and continue to
the configured chain.

### Subscription-group chain

Admins configure at most one ordered department list for each relay provider and
subscription group.

For each department in order:

- resolve enabled approver configs for that exact department only;
- do not fall back to representatives and do not walk to parents;
- allow any one resolved candidate to approve the step;
- allow admins to decide the active step as fallback when normal candidates are
  absent or unavailable.

If neither an initial step nor a configured chain step exists, create one admin
fallback step so the request remains actionable.

### Reused approvals

After each approval, scan later steps in order. A later step is automatically
satisfied when its candidate list contains anyone who approved an earlier step.

An automatically satisfied step:

- records the satisfying person and source step;
- creates a durable transition event;
- requires no second decision comment;
- sends no activation notification.

The scan continues until the next unsatisfied step or workflow completion.

### Decisions and reset

- Approval and rejection both require a non-empty trimmed comment.
- Only the active step may be decided.
- A normal user must be an active candidate and cannot approve their own request.
- An admin may decide the active step but cannot jump to a later step.
- Rejection ends the request immediately.
- The winning final approval changes the request to `approved_resetting` in the
  same compare-and-swap transaction as the final decision.
- Relay reset runs after commit, survives caller cancellation, and has a
  30-second execution deadline.
- Reset success becomes `approved_reset_succeeded`; reset or persistence failure
  becomes `approved_reset_failed` and remains retryable by the final approver or
  an admin under the existing retry contract.

## Persistence

### One new business table

`quota_reset_approval_chains` stores one row per `(provider_id, group_id)`:

| Field | Purpose |
| --- | --- |
| `provider_id`, `group_id` | Unique relay subscription-group identity |
| `group_name` | Display snapshot |
| `department_chain` | Ordered JSON department references |
| `enabled` | Whether new requests use the chain |
| creator/editor/timestamps | Configuration history |

Each chain item stores `directory_source_id`, `department_external_id`, and a
display-path snapshot. Saves reject stale directory sources, duplicate groups,
duplicate departments, non-subscription groups, and chains longer than 20
departments.

### Existing request row

`quota_reset_requests` adds:

| Field | Purpose |
| --- | --- |
| `workflow_version` | `1` for historical requests, `2` for this flow |
| `workflow` | Bounded request-time JSON snapshot and progress |
| `workflow_revision` | Compare-and-swap revision |

The JSON stores requester display context, ordered steps, candidate snapshots,
decision comments, prior-approval satisfaction, and notification ids. It is
limited to 21 steps and 100 unique approvers. Invalid or unknown versions fail
closed.

`resolved_approver_user_ids` remains the indexed list of normal candidates for
the current active step. Existing approval-list and Work Items queries continue
to use it; admin-only fallback uses an empty list.

### Rolling-deployment status and indexes

Version 2 requests awaiting approval are stored as `workflow_pending` but are
returned to clients and generic webhooks as public `pending`. This prevents old
Pods from interpreting a version 2 row as a version 1 request during a rolling
deployment.

The original partial unique active-request index remains unchanged for rollback
compatibility. A second explicitly named partial unique index includes both
`pending` and `workflow_pending`, preventing an older binary from creating a
version 1 duplicate while a version 2 request is active. Ent's generated files
are expected to change mechanically for the new table, fields, enum, and index.

### Events

`quota_reset_request_events` remains the append-only audit source. Version 2 adds
only workflow creation, step approval, step activation, prior-approval
satisfaction, and admin-fallback activation event types. Existing rejection,
notification, reset, cancel, and retry events remain authoritative.

No request-node, node-approver, or decision table is introduced.

## Consistency And Concurrency

- Request-time directory, membership, approver-config, and chain reads use one
  repeatable-read snapshot.
- Request creation writes the request and initial events in one transaction;
  notification happens after commit.
- A decision update requires stored status `workflow_pending` and the loaded
  `workflow_revision`.
- The update, revision increment, active-candidate replacement, and transition
  events commit together.
- A zero-row update loses the race and returns `ErrInvalidStatus`.
- Terminal or malformed workflow cursors return an error and never index outside
  the step list.
- External relay calls run after the decision commit. Timeout or relay failure is
  persisted with a detached database context so the request does not remain
  indefinitely in `approved_resetting`.

## Notification Contract

Admins select one explicit channel:

- `generic_webhook`: stable structured JSON;
- `wecom_group_robot`: code-owned WeCom markdown.

The existing URL and optional bearer-token credential settings remain. Message
fields are not individually templated. New channel presets may be added later
without introducing a template DSL.

An active-step notification includes:

- requester name, email, and exact department display paths;
- subscription group and platform;
- application reason, bounded by the channel renderer;
- current step and total step count;
- previous approval comment when present;
- active approvers and an action URL.

WeCom mentions use only request-time `metadata.wecom_userid` snapshots. Missing
ids are displayed as unmentionable; local ids and emails are never guessed as
WeCom userids.

Notification failure never rolls back a request. Transport errors expose no URL
or credential, and third-party business responses persist only the HTTP status
or numeric webhook `errcode`, not an untrusted echoed message.

## API And UI

Existing request, approve, reject, cancel, retry, and list routes remain. Approve
and reject bodies require `decision_reason`.

The admin API adds:

- `GET /api/v1/admin/quota-reset/approval-chains`
- `PUT /api/v1/admin/quota-reset/approval-chains`

Approver candidates are active directory-matched members of the selected exact
department. Responses include representative and WeCom-id availability flags.
The approver settings response includes `current_directory_source_id`, resolved
by the backend from the current successful snapshot.

The settings UI extends the existing quota reset section with:

1. exact-department approver configuration;
2. ordered subscription-group department chains;
3. explicit notification channel configuration.

Department, group, and approver values use dropdowns. Search appears inside an
opened dropdown; raw internal id fields are not editable.

The existing quota reset workbench remains the data owner. Version 2 request
details show step progress, comments, and automatically satisfied steps.
Decision actions appear only for the backend-authorized current step. No quota
reset Pinia store or generic settings framework is added.

## Compatibility

- Existing rows default to workflow version 1 and continue through the original
  single-stage path.
- Existing approver configs and notification rows remain valid.
- Legacy notification rows infer their initial explicit channel from the URL on
  first read: WeCom robot URLs become `wecom_group_robot`; others become
  `generic_webhook`.
- Public API status remains `pending` for both workflow versions.
- Unknown workflow versions fail closed for approve, reject, and retry.
- No `sub2api` source or database access is added.

## Out Of Scope

- Generic workflow or approval-engine abstractions.
- Separate workflow-node or decision tables.
- Notification outbox, delivery retries, or delivery dashboard.
- Admin-authored templates or expression language.
- Editing an in-flight workflow.
- General audit browsing UI.
- Provider-level exactly-once reset guarantees after a process crash.

## Acceptance Criteria

1. The initial step uses exact-department config first and exact-department
   representatives only as fallback, with no parent walk.
2. Any candidate from any exact requester department can complete the merged
   initial step.
3. Group-chain departments execute in order using configured approvers only.
4. Any prior approving person automatically satisfies all matching later steps
   without another notification.
5. Every manual approval or rejection preserves a required comment.
6. Concurrent decisions have one winner, and the final winner starts one relay
   reset.
7. Relay reset cannot hang indefinitely after the approval commit.
8. Pending lists and badges reflect only the current active step.
9. WeCom notifications identify the requester, team, group, reason, and current
   approvers, mentioning valid WeCom userids.
10. Admin configuration uses directory-backed dropdowns rather than raw ids.
11. Historical version 1 requests remain operable and version 2 state is hidden
    from public clients.
12. The implementation adds exactly one business table and no generic workflow
    framework.
