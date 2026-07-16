# Department-Derived Quota Reset Approval Design

**Status:** Implemented on the feature branch; browser verification remains
blocked because the required browser runtime is unavailable

**Date:** 2026-07-16

## Relationship To Existing Design

This spec extends
`docs/superpowers/specs/2026-07-07-quota-reset-approval-design.md`. The older
spec remains authoritative for request submission, existing department approver
configuration, request/events, Work Items, relay quota reset, and notification
settings unless this document changes the behavior explicitly.

This document replaces the subscription-group approval-chain design previously
written at this path. That design incorrectly coupled approval routing to the
selected subscription group and introduced unnecessary configuration, API, UI,
and persistence. It must not be implemented or replayed.

## Goal

Resolve a quota reset approval sequence directly from the requester's current
department hierarchy:

```text
request submitted
  -> exact-department approval
  -> configured parent-department approvals, nearest to root
  -> one relay quota reset
```

The selected subscription group identifies only the quota to reset. It has no
effect on approval routing.

## Product Rules

### Request

- The requester selects one active subscription group and provides a reason.
- Only one active request may exist for the same requester, provider, and group.
- Approval routing is resolved and snapshotted when the request is created.
- Later directory or approver-config changes do not rewrite an in-flight request.

### Step 1: exact departments

Read every exact department directly linked to the requester in the current
successful Directory Sync snapshot.

For each exact department:

1. If enabled department approver config exists, use its active configured users.
2. If no config exists, fall back to active synced representatives of that exact
   department.
3. Never walk to a parent while resolving this first step.
4. Exclude the requester, inactive directory members, and disabled local users.

Merge candidates from all exact requester departments into one step. One
candidate approval completes the whole step.

Configured approvers may be any active directory-matched member of the selected
department; they do not have to be representatives.

If config exists but none of its users is currently usable, that department
contributes no normal candidate. If no exact department contributes any normal
candidate, retain an admin-fallback first step when at least one exact department
had config; otherwise skip the first step and continue upward.

### Later steps: walk parent departments

Start from the immediate parent of each exact requester department. Walk all
paths upward one parent edge per round until every path reaches the organization
root.

For each round:

1. Deduplicate departments when multiple paths converge on the same ancestor.
2. Read enabled approver configs for the departments in that round.
3. Ignore departments with no config; representatives are not a fallback after
   the first step.
4. Merge active configured users from all configured departments into one step.
5. One merged candidate approval completes the round.
6. If the round contains config but no configured user is usable, retain an
   admin-fallback step.
7. If the round contains no config at all, create no step and continue upward.

This defines "same level" as the same upward traversal round from the
requester's exact departments, not a globally stored organization depth.

### Reused approvals

After an approval, automatically satisfy every later step whose candidate set
contains a person who already approved an earlier step.

An automatically satisfied step records the person and source step in workflow
state and request events, requires no second comment, and sends no activation
notification.

### Admin fallback

- Admins may decide the active step whether or not it has normal candidates,
  but cannot jump ahead.
- Admin fallback does not grant admin authority; it keeps a step actionable
  when no normal candidate exists.
- If resolution produces no first or parent step, create one admin-fallback step
  so the request cannot become permanently unapprovable.

### Decisions and reset

- Every manual approval or rejection requires a non-empty trimmed comment.
- A normal user must be an active candidate and cannot approve their own request.
- Rejection terminates the request immediately.
- The final winning approval starts exactly one relay reset transition.
- Relay execution occurs after the approval transaction commits, survives HTTP
  caller cancellation, and has a bounded timeout.
- Reset failure remains retryable under the existing final-approver/admin rules.

## Persistence

### No new business table

Do not add `quota_reset_approval_chains` or any other approval-routing table.
Do not add request-node, node-approver, decision, outbox, or template tables.

The current feature branch must remove the chain Ent schema and all generated
chain entity code before merge.

### Existing request row

Persist the bounded request-time workflow on `quota_reset_requests`:

| Field | Purpose |
| --- | --- |
| `workflow_version` | Distinguishes historical single-stage and new workflows |
| `workflow` | Requester, ordered steps, candidates, decisions, and skip evidence |
| `workflow_revision` | Compare-and-swap decision revision |

`resolved_approver_user_ids` continues to contain only normal candidates for the
current active step. Existing approval lists and Work Items queries continue to
use this indexed field. Admin-only fallback uses an empty list.

The workflow JSON is bounded to 21 steps and 100 unique approvers. Decode,
validation, or unknown-version failure is an authorization error and fails
closed.

Version 2 requests may retain the internal `workflow_pending` status, mapped to
public `pending`, to prevent an old Pod from treating them as historical
single-stage requests during a rolling deployment.

### Events

`quota_reset_request_events` remains the append-only audit source. Record
workflow creation, manual decisions, step activation, prior-approval
satisfaction, rejection, reset, retry, cancellation, and notification results.
No separate decision table is required.

## Resolution And Concurrency

- Resolve directory source, exact memberships, ancestor paths, approver configs,
  representatives, and local-user matches from one repeatable-read snapshot.
- Deduplicate departments, users, and converged ancestor paths deterministically.
- Create the request and initial events in one transaction; notify after commit.
- Decide a request only when stored status and `workflow_revision` still match.
- Update workflow, revision, current approver ids, terminal status, and events in
  one transaction.
- A concurrent loser receives `ErrInvalidStatus` and performs no relay reset.
- Invalid terminal cursors return an error rather than indexing outside the step
  list.

## Notification Contract

Keep the existing explicit channel setting:

- `generic_webhook`: structured JSON;
- `wecom_group_robot`: code-owned WeCom markdown preset.

Active-step notifications include requester name/email, exact department paths,
subscription group, application reason, step progress, previous comment, action
URL, and active approvers. WeCom mentions use only snapshotted
`metadata.wecom_userid`; local ids and emails are never guessed as WeCom ids.

Automatically satisfied steps send no notification. Notification failures are
non-blocking and persist only sanitized status/error-code information.

## API And UI

### Reuse existing routes

Keep existing request, list, approve, reject, retry, cancel, approver-config,
candidate, and notification-setting routes. Do not add approval-chain routes.

Request summaries may expose workflow version, current step, and ordered step
snapshots through the existing list responses.

### Settings

Organization & Login keeps only:

1. Department approval representatives: department dropdown and searchable
   department-member approver dropdown.
2. Notification channel and existing URL/auth controls.

Remove the standalone subscription-group approval-chain section and component.
Search remains inside opened dropdowns; raw internal ids are not editable.

### Approval workbench

Keep the existing quota reset route and queue structure. Display current
step/progress, comments, automatically satisfied steps, and a required-comment
decision dialog. Show actions only when the backend marks the current viewer as
eligible for the active step. Processed history remains readable across API
pages.

## Compatibility

- Historical workflow-version-1 requests remain operable.
- Existing department approver and notification settings remain valid.
- Existing chain configuration is not migrated because the branch has not been
  released; remove the branch-only table/API/UI instead.
- No `sub2api` source or direct database integration changes are allowed.
- The PR must be squash merged so abandoned intermediate chain implementation
  does not enter `main` history.

## Complexity Guardrail

The implementation must stay within these boundaries:

- `0` new business tables.
- `0` new approval-chain API routes.
- `0` new approval-chain settings components or stores.
- At most `2` new backend production files: a pure workflow model and focused
  workflow transaction service.
- At most `2` new frontend production components: decision dialog and workflow
  timeline.
- Target no more than `1,500` added hand-written production lines relative to
  `origin/main`, excluding Ent-generated code and tests.

If the target is exceeded, stop and simplify before continuing. Generated Ent
changes must be reported separately from hand-written changes.

## Testing

Backend tests cover:

- exact config priority and representative fallback;
- multiple exact departments merged into one first step;
- parent traversal rounds, converged ancestors, missing config, and admin
  fallback for unusable config;
- prior-approver reuse, rejection, CAS conflicts, final reset, retry, and version
  1 compatibility;
- current-step approval lists and Work Items counts;
- notification context, WeCom mentions, public status, and bounded relay timeout;
- absence of approval-chain routes and schema.

Frontend tests cover department-member settings, removal of chain settings,
required comments, step timeline, current-action visibility, complete history
pagination, and explicit notification channel saves. Final verification includes
full backend/frontend/CLI tests, frontend build, role E2E, and one browser-driven
multi-step reset workflow.

## Acceptance Criteria

1. Subscription group selection never changes approval routing.
2. The first step uses exact-department config first and exact-department
   representatives only when config is absent.
3. Multiple exact departments merge into one first step; any candidate can pass.
4. Later steps are derived from configured ancestors in nearest-to-root traversal
   rounds, with same-round departments merged.
5. Missing config is skipped; unusable config retains an admin-fallback step.
6. Any earlier approving person automatically satisfies matching later steps
   without another notification.
7. Every manual approval/rejection preserves a required comment.
8. Concurrent final decisions trigger at most one relay reset transition.
9. Historical requests remain operable and internal workflow status is not
   exposed publicly.
10. Settings contain no subscription-group approval-chain section.
11. The final diff adds no business table or approval-chain API.
12. The hand-written production diff remains within the complexity guardrail.
