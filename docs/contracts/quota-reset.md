# Quota Reset Contract

This contract describes the current user-requested subscription quota reset,
department-derived multi-stage approval workflow, Relay reset execution, audit
events, notifications, Work Items counts, and compatibility with historical
single-stage requests. Read it before changing quota-reset routing, decisions,
state transitions, notifications, or reset execution.

Quota reset clears accumulated daily, weekly, and monthly usage for one active
subscription group. It does not change group limits, subscriptions, API keys,
or delegated rate multipliers.

## Request Creation

An authenticated user selects one active subscription group from the enabled
primary Relay provider and supplies a non-empty reason. The requester must have
a current Relay binding. One active request may exist for the same requester,
provider, and group; concurrent creation is protected by the database.

New requests use workflow version 2. Approval routing is resolved from one
repeatable-read view of the current Directory snapshot and is stored on the
request. Later Directory or approver-config changes do not rewrite an in-flight
workflow.

The request, bounded workflow document, initial events, active approver IDs,
path evidence, and Work Items revision commit atomically. Notification runs
after commit and cannot roll back request creation.

## Department-Derived Workflow

The selected subscription group identifies only what will be reset. It has no
effect on approval routing.

The first step merges all exact current requester departments:

1. A department with enabled configuration uses its active configured local
   users who still belong to that department.
2. A department without configuration falls back to active synced
   representatives of that exact department.
3. The requester, inactive Directory members, and locally disabled users are
   excluded.
4. If configuration exists but yields no usable user, the step remains as
   admin fallback.

Later steps walk immediate parents one round at a time. Departments reached in
the same round are deduplicated and merged. Only configured approvers apply in
parent rounds; there is no representative fallback after the exact step. A
configured but unusable round remains admin fallback; an unconfigured round is
skipped. If no normal step exists, one admin-fallback step is created.

One candidate decision completes the active merged step. A person who approved
an earlier step automatically satisfies every later step containing that
person, with durable skip evidence and no duplicate notification.

The workflow is bounded to 21 steps and 100 unique approvers. Unknown versions,
invalid stored workflow shape, or an invalid cursor fail authorization closed.

## Decision Rules

- Every manual approval and rejection requires a non-empty comment.
- A non-admin actor must be a current stored candidate for the active step and
  cannot decide their own request.
- An administrator may decide only the active step; admin fallback does not
  permit jumping ahead.
- Rejection terminates immediately.
- Only the final winning approval starts Relay reset.
- Historical version-1 requests remain operable through their single-stage
  compatibility path.

Each version-2 decision compares stored status and workflow revision. The
workflow, next active approvers, revision, decision/events, terminal status,
and any Work Items revision change commit in one transaction. A concurrent
loser receives invalid status and performs no Relay mutation.

## Request and Reset State

New version-2 requests use an internal workflow-pending status that is exposed
publicly as pending. Pending requests may be cancelled by their requester,
approved/rejected by the active step, or decided by admin fallback.

The reset lifecycle is:

```text
workflow pending -> approved/resetting -> reset succeeded
                                  `-----> reset failed -> retry -> approved/resetting
workflow pending -> rejected
workflow pending -> cancelled
```

Approval/retry leaves the actionable set in a committed local transaction
before Relay is called. Reset execution runs synchronously under a service-owned
30-second context derived without caller cancellation. It uses the snapshotted
provider, requester Relay user, and subscription group through the optional
Relay quota-reset capability.

Success and failure each finalize under a fresh independent five-second
context. Status and the required event are atomic. Failure re-enters the
actionable set and advances the Work Items revision in the same transaction;
success is already outside the actionable set and does not advance it again.
If local success finalization fails after Relay succeeded, the request remains
resetting for explicit reconciliation and Relay is not replayed automatically.

Only a reset-failed request may retry. Retry uses the same request and records
the actor plus retry/reset-started events.

## Approver Configuration

Approver configuration is local administrator state tied to the current
Directory source. It is not overwritten by Directory apply.

Configured users must be active Directory-matched local users in the selected
department. A department may have multiple approvers; one user may appear in
multiple departments. Missing/stale departments remain visible for cleanup but
do not resolve new requests.

The settings API supports replacing selected departments or the complete
current-source configuration.

## Workbench and Work Items

The quota-reset workbench has independent requester, assigned-approval, and
administrator queues. Route entry loads requester history only; hidden queues
load on first selection. Each queue owns its request/error generation, and an
authoritative read failure clears stale history for that queue.

Mutations refresh the source queue, invalidate other affected queues, and force
a fresh shared Work Items count. Approval/admin badges count only actionable
pending or reset-failed work; requester history is not an actionable badge.
Admin totals use the admin fallback count without double-counting a separate
assigned-approver count.

Work Items revision changes atomically with every transition entering or
leaving the actionable set. Redis is a reconstructible count optimization and
is not required for a decision or reset mutation.

## Notifications and Audit

Request events are append-only audit facts covering creation, workflow
resolution, step activation/decision/satisfaction, rejection, cancellation,
reset start/success/failure/retry, and notification result. They contain no
Relay passwords, API keys, webhook tokens, or signatures.

Notification settings explicitly choose generic webhook or WeCom group robot.
Bearer auth references an encrypted secret-text credential and is never
returned in plaintext. WeCom mentions use only the snapshotted allowlisted
`wecom_userid`; email, local ID, and member external ID are never guessed as a
WeCom identity.

Notifications are short-timeout, non-retrying side effects after authoritative
state commits. Failure writes a sanitized event and does not change request
status. Automatically satisfied steps send no activation notification.

## Safety Boundaries

- User lists show only own requests; approver lists use current or historical
  stored workflow participation; admins use separate fallback routes.
- Current active subscription membership is revalidated before request/reset.
- Directory and Relay capability absence fails closed without changing another
  subscription or provider.
- Reset state transitions and required events use status/revision predicates;
  concurrent decisions cannot trigger two resets.
- User-entered comments are stored/rendered as plain text and remain bounded.
- Quota reset never changes limits, membership, API keys, multiplier state, or
  Relay source code.
- New routing inputs, approval tables/routes, or automatic replay require a
  current GitHub spec/ticket.
