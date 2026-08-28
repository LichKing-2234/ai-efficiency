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

## Frontend Reviewed-Plan Ownership

`frontend/src/composables/useRelayPlanningWorkflow.ts` owns the active reviewed
plan from the first Preview or Replan response through explicit Target, member,
source, Account, rename, removal, cross-mapping, and unmanaged-adoption edits.
It also owns plan-scoped user and Account search lifecycles, one canonical
reviewed-state request projection, relationship-fingerprint and operation-key
lifecycle, stale-plan replacement, persisted retry-intent restoration, and the
Confirm/Execute handoff.

`RelayPlanningView.vue` keeps provider/department/Platform planning inputs,
mapping-list pagination, Mapping Renewal, Rebind, saved Account administration,
rendering, feedback, and explicit administrator intent. HTTP transport remains
in `frontend/src/api/relayPlanning.ts`. Preview and local edits remain read-only;
only the explicit final Confirm invokes Execute. A categorized stale response
closes the old confirmation and makes the refreshed plan visible without
automatically replaying the previous execution. A `needs_retry` mapping status
takes precedence over relationship warnings, and member readback errors remain
visible in the execution result.

## Stable Mapping

The mapping is keyed by provider, department external ID, and Platform. Group
IDs are authoritative; display names are snapshots. Replan preserves target
Group IDs and only produces a new member assignment matrix. It does not create,
deactivate, resize, or automatically reshuffle target Groups.

Opening Replan reconstructs the last confirmed member-to-target assignments as
a zero-change baseline, including saved members outside the mapping's current
department and saved members whose current local-to-Relay identity is
unavailable. An unavailable saved member remains visible in the saved Target
with a safe warning and blocks the complete Confirm before any Relay write;
otherwise valid edits in the same plan are not partially executed. Other
eligible candidates remain visible with the existing usage and ranking fields,
but start unselected and unassigned and are not recommendations. Remaining
target capacity never places them automatically; only explicit administrator
add, move, remove, or adoption actions change an executable reviewed member
matrix.

If a saved Target Group is absent from current Relay Group facts, Replan keeps
that stable Target ID in its original order and retains its saved member roster.
It shows a safe unavailable-Target warning and proposes no automatic
replacement, member relocation, removal, resize, or deactivation. The complete
reviewed plan is non-executable, including otherwise valid edits for available
Targets, and Confirm returns the categorized stale-plan response before any
Relay write. When the same Target relationship becomes available again, the
Group fingerprint changes; the administrator must review a fresh Preview before
Confirm can proceed normally.

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

The mapping list starts its Provider Group, complete user/subscription, and
same-Platform Account reads concurrently. It loads the current Directory
snapshot once, resolves all mapped departments in one bounded query, and
reuses those facts across every mapping returned by the HTTP request. Relay
pagination and the number of Providers or Platforms determine the upstream
read count; the number of managed mappings does not.

Relay Planning's provider-wide relationship snapshot contains exact user
identity and every subscription's ID, Group, status, and expiry. Renewal and
Replan reuse one such snapshot throughout a Preview request for candidate
validation, managed and unmanaged membership, drift, effects, and fingerprint
construction. API Keys are read at most once for each relevant Relay user, and
Group and Account collections are loaded once. These request-scoped facts are
discarded with the request and are never stored in a cross-request cache.

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
restored, or reused without another assignment when its subscription is already
active. Eligible active API Keys still bound to the target are moved back, and
the target subscription is removed. If a legacy managed member has no saved
per-member Source, removal remains non-executable until the administrator
explicitly selects a same-Platform Source or `Target only`. Template and managed
Target Groups cannot be selected as the removal Source. Explicit `Target only`
removes only the Target subscription and moves no API Key. `Move Here`
transfers one member from one explicit same-Provider, same-Platform mapping.
`Add Additionally` preserves the old
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

Renewal requires a final explicit Confirm and obtains one fresh relationship
snapshot before its first write. Stale validation uses that snapshot, and an
`extend` or `renew` writes the reviewed subscription ID directly without
rediscovering it by user and Group. Selected mutations run with bounded
concurrency while results retain deterministic member order. Readback uses one
new bounded snapshot after all mutations complete. Execution is synchronous
and reports per-member `succeeded`, `skipped`, or `failed` results in the
current dialog. It does not create a background job or renewal-history entity.
One stable operation key is retained for the dialog, propagated as
deterministic per-member idempotency keys, and reused only when retrying failed
members. Successful and skipped members are never submitted again by that
retry. Closing or refreshing the result dialog ends that result view; a later
renewal starts a new explicit operation.

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
The reviewed removal destination is carried as per-member Source intent through
Preview, Confirm, relationship fingerprinting, execution, readback, and retry.
Missing provenance never defaults silently to `Target only`; an explicit zero is
the only Target-only instruction. A pending removal retry keeps that reviewed
destination locked; changing Source or switching to `Target only` requires a new
removal operation after the pending retry is resolved. A provenance-free legacy
retry may load a read-only plan that shows the removal intent without inventing
subscription or API-Key effects; execution remains blocked until the
administrator reviews a destination.
Only Keys active when reviewed enter the removal operation. Their IDs remain in
the retry fingerprint and final readback even if a later Relay change moves or
deactivates them; Keys already inactive at review time remain outside the
operation and are never moved.
After all explicit-removal writes, execution obtains one fresh provider-wide
subscription relationship snapshot and reads the affected users' API Keys. A
removal succeeds only when the saved Source subscription is active, the Target
subscription is absent, and every reviewed eligible active Key has the expected
final binding. Missing or unavailable readback remains `needs_retry`; it never
restores the deleted desired assignment or repeats a step already proven successful.
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
Completed member steps are reusable only when the saved action and Target match
the current retry. A completed forward migration never satisfies a later
removal merely because both operations reference the same Target.
Destination and source mapping changes are committed in one local transaction.
The execution response reports one persistence result per affected mapping; a
failure rolls back every local mapping change and returns structured retryable
`failed`, `rolled_back`, and `skipped` results instead of leaving a half-saved
transfer.

Relay members that exist in a managed target Group but have no local mapping are
shown as unmanaged. Their observed 30-day cost contributes to remaining target
capacity. An administrator may explicitly adopt one; adoption only ensures the
target subscription and never performs source removal or API-Key migration.
