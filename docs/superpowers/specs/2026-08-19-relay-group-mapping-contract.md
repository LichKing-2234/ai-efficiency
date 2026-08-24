# Relay Group Mapping Contract

## Status

This is the current implementation contract for the department x Platform Relay
planning workflow. It supersedes the allocation details in the 2026-08-18
planning plan where they conflict. It does not authorize a staging or
production release.

## Inputs and Preview

- A preview selects one provider, directory department, Platform, Template
  Group, optional Migration Source Group, and per-target planning cost.
- Template and Migration Source are independent IDs. Both, and every existing
  target ID supplied by an administrator, must exist on the selected Platform.
- When a Migration Source is selected, its members with a valid local Relay
  mapping are recommended by default and other members require an explicit
  administrator addition. Without a Migration Source, mapped department users
  may be recommended and every selected user remains target-only unless the
  administrator chooses an explicit same-Platform Source.
- A non-source addition receives only a target subscription. It does not remove
  an old subscription and does not move API Keys. Source members retain the
  source-subscription removal and API-Key migration steps.
- Per-target user search is server-paginated across all local users. Search and
  Preview both revalidate the local-to-Relay identity against the selected
  Provider; a stale or cross-Provider Relay ID cannot be assigned by bypassing
  the frontend.
- Each Preview Target exposes its reviewed Account order. A new Target defaults
  to the Template Group's same-Platform Account IDs and priorities; an existing
  Target defaults to its saved desired state, or its current Relay state when
  Account management has not been initialized. The administrator may search
  all same-Platform Account types and add, remove, or reorder them before
  Confirm. Existing Relay priorities are preserved even when multiple Accounts
  share one priority; an administrator edit rewrites the displayed order to
  consecutive priorities. An explicit empty Account list keeps that Target
  inactive and blocks member migration for only that Target.
- User and Account text search waits for a short typing pause before querying.
  Pagination remains immediate, and a response for an older query cannot
  replace the newest query's results.
- The initial Preview recommends a target count, then treats the administrator's
  reviewed assignment list as authoritative. The administrator may add empty
  proposed Targets or remove proposed Targets while retaining at least one;
  indexes are normalized before the next Preview. Removing a Target leaves its
  users unassigned, and a newly added Target inherits the Template Account
  defaults. Existing-mapping Replan still preserves its fixed target IDs and
  does not expose this resize control.
- A new Target name is suggested from the current Directory department leaf,
  normalized Platform, and a two-digit-or-longer sequence, for example
  `Department Alpha-openai-01`. Surrounding whitespace and control characters
  are removed while ordinary Unicode and internal spaces are preserved. The
  Platform and sequence suffix is retained when the department portion is
  truncated to the Relay 100-character limit. Provider Group names are reserved
  when assigning sequence numbers. Adding or removing an uncreated Target
  recomputes suggestions in current Preview order; an administrator may edit
  every reviewed name before Confirm.
- Empty, over-length, duplicate, and provider-conflicting reviewed names block
  Confirm. Preview edits and suggestion recalculation are local and read-only.
  Confirm revalidates the reviewed names before the first Relay mutation and
  returns the structured stale-plan response if a name has since been claimed.
- Preview is read-only. Group creation, membership changes, source removal,
  API-Key binding, and adoption require the final Confirm action.

## Stable Mapping

The mapping is keyed by provider, department external ID, and Platform. Group
IDs are authoritative; display names are snapshots. Replan preserves target
Group IDs and only produces a new member assignment matrix. It does not create,
deactivate, resize, or automatically reshuffle target Groups.

Opening Replan reconstructs the last confirmed member-to-target assignments as
a zero-change baseline, including saved members outside the mapping's current
department whose local-to-Relay identity remains valid. Other eligible
candidates remain visible with the existing usage and ranking fields, but start
unselected and unassigned and are not recommendations. Remaining target
capacity never places them automatically; only explicit administrator add,
move, remove, or adoption actions change the reviewed member matrix.

Replan shows the current Relay name and department-based suggestion for every
managed Target. Suggestions are assigned in stable ascending Target Group ID
order, so member moves and usage changes do not renumber them. A department
rename or explicit rebind recalculates suggestions but performs no Relay write.
Existing Target rename is unselected by default; an administrator may select
and edit Targets individually or explicitly apply all suggestions in the
mapping. Template and Migration Source Groups are never part of that rename
set. Rename-only plans use the same final Confirm gate, which summarizes each
selected `current name -> reviewed name` transition.

Rebind changes only the saved relationship. The UI requires a final Confirm,
and the backend validates the department input and all Template, Source, and
target IDs against the selected Platform before persisting it. When a stored
department disappears from the current directory snapshot, mapping reads keep
the relationship, mark it unavailable, and return same-Platform departments
that are not already mapped as suggestions.

The mapping list reads the Provider's user directory together with active
subscription Group IDs in one paginated operation per Provider and reuses one
Account relationship read per Provider and Platform for the lifetime of the
HTTP request. Replan uses the same batch directory contract when detecting
Relay-only members in managed target Groups. Neither path fans out one
subscription request per Provider user; relationship freshness remains
request-bound and is not stored in a cross-request cache.

## Account and Member Maintenance

Existing mappings start with Account management uninitialized. They display
the current same-Platform Account IDs, safe display fields, and per-Group
priorities without treating an absent desired state as an instruction to remove
Accounts. `Adopt Current` saves those IDs and priorities locally and performs no
Relay write. Account search covers every Account type on the selected Platform;
status and schedulability are warnings rather than filters.
Mappings also warn when one reviewed target uses multiple Accounts or one
Account is reused across reviewed targets. Group-configuration compatibility
is shown only when the Provider exposes a privacy-safe compatibility field;
the current sub2api Account summary does not, so AI Efficiency does not infer
compatibility from credentials or provider-private configuration.

After initialization, the saved desired Account order is applied only through
Confirm. A Preview edit becomes the reviewed desired state for that Confirm;
it is not written to Relay or persisted separately. Reconciliation adds,
removes, or changes only the selected target Group relationship while
preserving every unrelated Account-to-Group binding. A newly duplicated target
uses the reviewed Account order, which defaults to the Template Account IDs and
priorities, verifies those relationships, and becomes active before member
migration. An Account failure blocks only its target; other targets continue.
Account reconciliation re-reads the latest Account relationships for each
target before mutation so one Account can be reused safely across targets. An
existing target whose reviewed desired Account pool is empty has its remaining
Account bindings removed, is made inactive, and receives no member migration.

An administrator may explicitly remove a managed member. A saved Source is
restored, eligible API Keys still bound to the target are moved back, and the
target subscription is removed. Without a saved Source, only the target
subscription is removed. `Move Here` transfers one member from one explicit
same-Provider, same-Platform mapping. `Add Additionally` preserves the old
mapping, subscription, and API-Key bindings and adds only the new target
subscription. The UI warns that this leaves the user in multiple managed
Account pools. Department changes never trigger either action automatically.

## Mapping Renewal

Every new subscription assigned by Relay Planning uses 365 validity days. This
applies to initial execution, Replan additions, `Add Additionally`, and adoption
of a Relay-only member. The new default affects only future assignments. A
release never backfills, extends, or otherwise mutates an existing subscription.

Each managed mapping exposes an explicit `Renew Subscriptions` action. The
renewal Preview selects all saved managed mapping members by default and allows
the administrator to deselect individual members. Relay-only users observed in
a target Group remain outside the renewal scope until they are explicitly
adopted. The renewal term defaults to 365 days, can be changed for the current
operation, and is not persisted as mapping configuration.

The read-only Preview shows each selected member, expected target Group,
subscription status, current expiry, planned action, and resulting expiry. For
an active subscription, the term is added to its current expiry. For an expired
subscription, the term starts at execution time and the subscription becomes
active. A missing expected subscription is created for the requested term. A
suspended subscription is skipped and remains suspended. Subscriptions in an
unexpected or additional Group are shown as drift but are never renewed,
removed, or used as a reason to move API Keys by this operation.

Renewal requires a final explicit Confirm and revalidates the relevant
subscription facts before its first write. Execution is synchronous and reports
per-member `succeeded`, `skipped`, or `failed` results in the current dialog. It
does not create a background job or renewal-history entity. One stable operation
key is retained for the dialog, propagated as deterministic per-member
idempotency keys, and reused only when retrying failed members. Successful and
skipped members are never submitted again by that retry. Closing or refreshing
the result dialog ends that result view; a later renewal starts a new explicit
operation.

## Relationship-Bound Confirmation

Every Preview returns an opaque versioned SHA-256 relationship fingerprint.
Version 2 carries separate canonical hashes for Group, Account, mapping and
retry state, local-to-Relay identity, subscription, and API-Key relationships
so a stale response can name the safe relationship category that changed.
Its canonical input contains the selected Provider and Platform, relevant Group
IDs and Platforms, saved and actual target Account IDs and priorities, affected
local-to-Relay user IDs, relevant subscription Group/status facts, and eligible
API-Key ID/Group bindings. It never contains credentials, API-Key values,
Account private configuration, or raw provider payloads.

Thirty-day cost, Token totals, rank, and usage freshness are advisory and are
excluded from the fingerprint. Refreshing only those fields does not invalidate
Confirm. Confirm must send the fingerprint from the exact Preview being
reviewed. Before the first Relay mutation, the backend rebuilds the complete
relationship snapshot. A missing fingerprint is rejected; a changed or no
longer valid relationship returns HTTP `409` with `stale_relay_plan`, a safe
difference category, and a refreshed plan when one can still be produced. No
Relay mutation occurs on that path.

The confirmation view groups the planned Account add/remove/priority actions,
member add/move/remove actions, subscription effects, and API-Key move counts by
target Group. A stale response closes the old confirmation, replaces the plan
with the refreshed plan when available, and requires another explicit Confirm.

## Execution and Retry

Each mapping stores `operation_state` as a JSON object. It records the last
operation key, each target-group step, each local member step, and each
explicitly adopted Relay-only member, including status and error text. A
partial execution is returned as `needs_retry`; a later Confirm/replan replaces
successful step entries while retaining unresolved failed entries. No audit
event stream is implied by this field.
New Target execution records whether creation is still pending or completed.
It duplicates the inactive Template Group, applies the reviewed name,
reconciles Accounts, activates the Group, and only then migrates members. A
rename or activation failure blocks later work only for that new Target.
Replan restores a pending creation by Target Group ID, retries it without
duplicating another Group, and marks creation complete only after activation.
For an existing Target, a rename failure remains an independent retryable step
and does not block reviewed Account or member operations.
An explicit removal updates the desired member state immediately. If an
upstream step fails, the operation state retains its Target and saved Source so
the same removal can be Previewed and retried without restoring the deleted
desired assignment.
Failed `Move Here` operation state likewise retains the source mapping and
actual previous target. Reopening Replan restores that action in the UI and the
backend independently restores it when a client omits the retry action.
When a Replan moves a member between managed target Groups, the retry state also
retains the actual previous target Group ID so a failed API-Key move can be
retried from that Group instead of falling back to the original source Group.
Once a matching relationship preflight has completed, later upstream failures
are recorded as normal per-step retry state rather than relabeled as a stale
Preview. A retry obtains a fresh Preview fingerprint while retaining the saved
operation state and actual previous target Group needed by unfinished steps.
Completed subscription steps for the same reviewed Target are not submitted
again during that retry; choosing a different Target requires a new step.
Destination and source mapping changes are committed in one local transaction.
The execution response reports one persistence result per affected mapping; a
failure rolls back every local mapping change and returns structured retryable
`failed`, `rolled_back`, and `skipped` results instead of leaving a half-saved
transfer.

Relay members that exist in a managed target Group but have no local mapping are
shown as unmanaged. Their observed 30-day cost contributes to remaining target
capacity. An administrator may explicitly adopt one; adoption only ensures the
target subscription and never performs source removal or API-Key migration.
