# Relay Group Mapping Contract

This contract describes the current administrator-owned department x Platform
Relay Group Mapping, reviewed Preview, Replan Baseline, Account relationships,
member migration, confirmation, persistence, and retry behavior. Read it before
changing Relay Planning Preview/Execute, Replan, mapping maintenance, Group or
Account reconciliation, Managed Mapping Member moves/removals, or mapping
renewal.

Relay identity and Provider access follow
[relay user access](./relay-user-access.md). Department facts follow
[directory sync](./directory-sync.md). Usage/ranking presentation follows
[usage and quota](./usage-and-quota.md), and valid Team Usage prewarm may supply
the request-scoped usage source under
[team usage prewarm](./team-usage-prewarm.md).

## Terms and Authority

| Term | Meaning |
| --- | --- |
| Template Group | Same-Platform Group copied for new Target configuration; it is not the member migration source. |
| Migration Source Group | Optional same-Platform Group whose reviewed members may lose the source subscription and have eligible API Keys moved. |
| Target Group | Stable Relay Group managed by one mapping. Its ID is authoritative; its name is a display snapshot/reviewed mutation. |
| Account relationship | Ordered Account-to-Target binding, including Relay priority, that controls the reviewed Account pool. |
| Managed Mapping Member | Local user explicitly assigned to a Target Group by one mapping. The persisted assignment remains authoritative in Replan when the current Relay identity is unavailable. |
| Replan Baseline | Member-to-Target assignment saved by the last confirmed mapping operation and used as the comparison point for Replan. |
| Replan Roster | Managed Mapping Members assigned to one Target Group by the Replan Baseline; it excludes newly eligible candidates and retains members whose current Relay identity is unavailable. |
| Unavailable Replan Target | Target Group ID saved by the Replan Baseline but absent from current Relay Group facts. |
| Relay-only unmanaged member | User observed in a managed Target but lacking a usable local mapping; visible for capacity and explicit adoption only. |

A mapping is unique by Provider, Directory department external ID, and Platform.
Persisted Target Group IDs, `member_assignments`, member sources, desired Account
relationships, and operation state are its authority. Names and current Relay
relationships are snapshots used for display, drift detection, and execution
validation.

Template Group, optional Migration Source Group, and every existing Target are
independent IDs. Each must exist on the selected Provider and Platform. Template
and Migration Source are never implicitly interchangeable.

## Initial Preview

An initial Preview selects Provider, department, Platform, Template Group,
optional Migration Source Group, and per-Target planning cost. It is read-only.
Group duplication/rename/activation, Account writes, subscription changes,
API-Key moves, member adoption, and mapping persistence begin only after final
Confirm.

Provider, department, and Platform also identify the unique managed mapping.
When that relationship already exists, the browser opens its Replan Baseline
instead of requesting a new allocation. A stale browser mapping list may still
reach the initial Preview endpoint; the backend then returns categorized HTTP
409 `existing_mapping` with the existing Mapping ID, after which the browser
refreshes the mapping list and opens that Replan. Initial Execute performs the
same fresh collision check before any Relay or mapping write, so a stale client
or direct API caller cannot replace the saved Target IDs or Replan Baseline.

Candidate behavior depends on the reviewed source:

- With a Migration Source, source members with valid local Relay mappings may
  start recommended; other candidates require explicit administrator addition.
- Without a Migration Source, mapped department users may be recommended, but
  every selected member remains target-only until an explicit same-Platform
  source is reviewed.
- User search spans all local users and is server-paginated. Search and Preview
  revalidate local-to-Relay identity against the selected Provider.
- A stale, missing, or cross-Provider Relay identity cannot become executable
  through a client-supplied ID.

The initial Preview recommends a Target count, then treats reviewed assignments
as authoritative. The administrator may add empty proposed Targets or remove
uncreated Targets while retaining at least one. Removing a proposed Target
leaves its members unassigned; adding one inherits Template Account defaults.
Indexes are normalized before the next Preview.

New Target names are suggested from the current department leaf, normalized
Platform, and a stable sequence. Suggestions preserve the Platform/sequence
suffix under the Relay length limit and avoid existing Provider Group names.
Every name remains editable. Empty, over-length, duplicate, control-character,
or Provider-conflicting reviewed names fail validation before the first write.

Each proposed Target exposes one reviewed Account order. A new Target defaults
to the Template Group's same-Platform Account IDs and existing priorities.
Account search covers all same-Platform Account types. Status/schedulability and
reuse/multiple-Account conditions are warnings, not hidden filters.

Plan-scoped user and Account searches debounce text changes, paginate
immediately, and ignore superseded responses. An older Preview/search/Execute
response cannot replace state created by a newer request or explicit edit.

## Reviewed-Plan Ownership

The Relay Planning workflow composable owns the active Preview/Replan and every
reviewed member, source, removal, cross-mapping action, unmanaged adoption,
Target name, Account order, relationship fingerprint, operation key, stale
replacement, retry restoration, and canonical request projection.

The route view owns planning inputs, mapping-list navigation including
existing-Mapping Preview routing and stale-list recovery, renewal, Rebind,
saved Account administration, rendering, and explicit administrator intent.
HTTP transport remains in the Relay Planning API module. A searched-user source
or cross-mapping intent enters reviewed state only after the latest Preview
succeeds, so hidden request builders cannot drift from the visible plan.

A categorized stale response closes the old confirmation and may replace the
visible plan with a refreshed read-only plan. It never replays the rejected
execution or silently reapplies prior Confirm intent. A `needs_retry` mapping
status takes precedence over relationship warnings, and member readback errors
remain visible in the execution result.

## Stable Mapping and Replan

Replan keeps every existing Target's stable ID and original order. It may
change the reviewed member matrix and selected Target names/Accounts, and it may
append empty proposed Targets without Relay Group IDs. It does not remove,
replace, deactivate, or automatically reshuffle existing Target Groups.

Opening Replan reconstructs the last confirmed `member_assignments` as the
zero-change Replan Baseline:

- Every Managed Mapping Member in the Replan Baseline starts selected in its
  assigned Target Group.
- Managed Mapping Members outside the current department remain in the baseline.
- Other eligible candidates remain visible with existing usage/ranking fields
  but start unselected, unassigned, and not recommended.
- Remaining Target capacity never selects or places another candidate.
- Relay-only unmanaged members start unselected; adoption remains explicit.

A saved Managed Mapping Member is evaluated against the expected Target before
new-candidate Migration Source eligibility. When the expected Target
subscription is active, leaving the Source is the healthy result of the prior
migration and produces neither the per-member Source warning nor the generic
no-eligible-member warning. This exception applies only to the saved roster;
newly reviewed migrations and removals that restore a saved Source still
require their reviewed Source relationship.

Replan also compares saved members with request-bound managed relationship
facts when that member has no unresolved legacy operation. A missing expected
Target subscription is managed relationship drift.
When legacy operation state identifies an exact API Key whose completed move
established the saved Target, that Key being absent from the expected Target is
also managed relationship drift. Replan keeps the saved member visible and
selected, reports the categorized drift, and blocks Confirm before any Relay
write. It does not infer ownership of unrelated API Keys when no exact reviewed
Key identity is available.

An unresolved legacy member operation remains under the existing retry
contract instead of being reclassified as static baseline drift. Phase 1 still
removes the inapplicable Source-candidate warning for that saved member; exact
directional retry containment is handled separately from baseline health.

Only explicit add, move, remove, target-only/source selection, or unmanaged
adoption changes the executable roster. Initial-mapping auto-placement does not
carry into Replan.

`Add group` appends a read-only proposed Target to Replan using the Template
Group's reviewed Account defaults and the same deterministic naming and
validation rules as initial Preview. Removing that unconfirmed proposed Target
deletes only the draft. Confirm duplicates the proposed Target inactive,
renames it, reconciles reviewed Accounts, activates it, applies explicitly
reviewed members, and appends the returned stable Group ID to the Mapping.
Creation state retains that ID across rename, Account, or activation failure so
retry never duplicates another Group. Existing Target retirement remains
outside this behavior.

If a Managed Mapping Member in the Replan Baseline has no current local-to-Relay
identity, the member remains visible in its Replan Roster with a safe warning.
The complete plan is non-executable, including otherwise valid edits. Explicit
removal cannot hide that blocker or silently delete the saved relationship while
identity remains unavailable.

When a Target Group in the Replan Baseline is absent from current Relay Group
facts, it is an Unavailable Replan Target. Replan preserves its stable ID,
original order, and Replan Roster. It supplies no replacement, relocation,
removal, resize, deactivation, or synthetic name. Rename controls are disabled
for that Target, and the complete plan is non-executable. When the Target
reappears, the Group relationship fingerprint changes and a fresh Preview is
required before normal Confirm.

Replan presents current and department-based names for every available managed
Target. Existing Target rename is opt-in per Target or through explicit Apply
All. Template and Migration Source Groups are outside the rename set. Rename-
only work uses the same Preview/fingerprint/Confirm/retry gate as member changes.

## Rebind

Rebind changes only the persisted mapping relationship. It requires explicit
confirmation and revalidates the department plus Template, Migration Source,
and Target IDs against the selected Platform before persistence. It performs no
background member move or Relay relationship rewrite.

If a saved department is absent from the current Directory snapshot, mapping
reads preserve the relationship, mark it unavailable, and offer unmapped same-
Platform departments as explicit Rebind choices.

## Account Relationships

An existing mapping with uninitialized Account management displays current
same-Platform relationships without treating missing desired state as a removal
instruction. Explicit `Adopt Current` persists the current Account IDs and
priorities as desired local state and performs no Relay write.

After initialization, saved desired Account order is applied only through
Confirm. Preview edits are reviewed intent, not an immediate Relay or local
write. Reconciliation changes only the selected Target relationship and
preserves every unrelated Account-to-Group binding. It re-reads current Account
relationships before mutation so the same Account may be safely reused by
multiple Targets.

Existing equal priorities are preserved until an administrator edits the order;
an edited order becomes consecutive priorities. An explicit empty Account list
removes that Target's remaining Account bindings, makes it inactive, and blocks
member migration only for that Target. A Target-local Account failure is kept
as retry state while other Targets may continue after the complete preflight
has passed.

Group-configuration compatibility is shown only when the Provider exposes a
privacy-safe fact. AI Efficiency does not infer compatibility from credentials,
private Account configuration, status, or schedulability.

## Member Relationship Operations

A source-backed addition ensures the Target subscription, moves eligible
AI Efficiency-managed API Keys from the reviewed Source to the Target, and
removes the Source subscription. A target-only addition ensures only the Target
subscription. Every new subscription assigned by this workflow uses 365 days.

An explicit Managed Mapping Member removal deletes the desired assignment
immediately in reviewed state:

- With a saved Source, execution reuses an already-active Source subscription
  or restores it, moves eligible Keys still bound to the reviewed Target back
  to the Source, and removes the Target subscription.
- With an explicitly reviewed Target-only destination, execution removes only
  the Target subscription and invents no Source or Key move.
- A legacy Managed Mapping Member with no saved per-member Source provenance
  remains non-executable until the administrator selects a same-Platform Source
  or explicitly selects Target only. Template and managed Target Groups are not
  valid removal Sources.
- Unrelated subscriptions, API Keys, Groups, Accounts, and members remain
  unchanged.

The reviewed removal destination travels through Preview, fingerprint,
Confirm, execution, readback, and retry. Missing provenance never silently
defaults to Target only; only an explicit zero value means Target only. A
pending removal retry locks its reviewed destination. Changing Source or
switching to Target only requires a new removal operation after that retry is
resolved. A provenance-free legacy retry may show its removal intent in a
read-only plan, but cannot invent subscription/Key effects or execute before
destination review.

Only eligible Keys that are active at review time enter a removal operation.
Their IDs remain part of retry fingerprint/readback even if a later Relay
change moves or deactivates them. Keys already inactive when reviewed stay
outside the operation and are never moved.

After removal writes, one fresh provider-wide subscription snapshot plus
affected-user API-Key readback must prove: saved Source active when applicable,
Target subscription absent, and each reviewed eligible Key on its expected
Group. Missing or mismatched readback keeps the removal `needs_retry`; it does
not restore the deleted desired assignment or repeat a step already proven
successful.

`Move Here` transfers a member from one explicit same-Provider,
same-Platform mapping. `Add Additionally` preserves the old mapping,
subscription, and API-Key bindings and adds only the new Target subscription.
Department drift never triggers either action automatically.

Relay-only unmanaged members contribute observed usage to Target capacity but
are absent from renewal and Replan Rosters. Explicit adoption ensures only the
Target subscription, creates the persisted Managed Mapping Member relationship,
and never removes a source subscription or moves an API Key.

## Relationship-Bound Confirm

Every Preview returns an opaque versioned SHA-256 relationship fingerprint.
The current fingerprint carries separate canonical hashes for:

- Group identity, Platform, and reviewed names;
- current/saved Account IDs and priorities;
- mapping and persisted retry state;
- local-to-Relay user identity;
- subscription Group/status relationships;
- eligible API-Key ID/Group bindings.

Credentials, API-Key values, private Account configuration, and raw Provider
payloads are excluded. Usage, rank, cost, and freshness are advisory and also
excluded; refreshing only advisory values does not invalidate Confirm.

Confirm must send the fingerprint from the exact plan under review. Before the
first Relay or mapping write, the backend rebuilds the complete relationship
snapshot and revalidates Provider, Platform, Groups, Targets, members, Accounts,
subscriptions, API Keys, names, and unavailable blockers.

A missing fingerprint is rejected. Any changed/invalid relationship returns
HTTP 409 `stale_relay_plan` with safe difference categories and, when possible,
a refreshed plan. This path performs no Relay, mapping, or retry-state write.
The administrator must review and explicitly Confirm again.

## Execution, Persistence, and Retry

After a complete preflight passes, execution may record per-Target/member
upstream failures as `needs_retry`; those failures are not relabeled as stale.

A new Target from either initial Preview or Replan is duplicated inactive from
the Template, renamed, reconciled to the reviewed Account pool, activated, and
only then receives members. Creation state records the stable new Group ID so
retry cannot duplicate another Group.
A rename/activation failure blocks later work for that new Target. Existing-
Target rename failure is an independent retryable step and does not suppress
reviewed Account/member steps.

Each mapping stores bounded `operation_state` for the operation key and
Target/member/adopted-user steps, including status, source/previous Target, and
safe error text. Replan restores unfinished explicit removal/move intent and the
actual previous Target needed for API-Key retry. Successful steps are not
submitted again; unresolved steps use a fresh Preview fingerprint and explicit
Confirm.

While a legacy operation is unresolved, its versioned intent binds the Mapping,
Provider and Platform, Target identity and reviewed name, Account IDs and
priorities, member action, local and Relay identities, Source and Target IDs,
the frozen reviewed API-Key ID set, and the expected relationship result. The
API-Key set is frozen before dispatch and preserves an explicit empty set, so a
retry never discovers and mutates a newly created Key. A retry may continue
only when that complete reviewed direction matches. Reverse or edited intent is
rejected as non-retryable `legacy_operation_conflict` before any Relay write;
older `needs_retry` state without complete identity is likewise blocked for
manual intervention rather than guessed.

Each completed member step also stores its exact identity. Reuse requires both
that identity and fresh request-bound Relay readback proving the expected Target
subscription, Source removal, and reviewed API-Key bindings. State that merely
says `succeeded` cannot override contradictory readback. A reviewed Key that is
missing or observed on neither the reviewed Source nor Target stops recovery as
`readback_mismatch` before another write. Adopt Accounts, saved
Account edits, Rebind, and Mapping Renewal Confirm remain blocked while the
legacy operation is unresolved. This is Phase 1 containment only: it does not
provide Restore, an event history, or the durable Relationship Operation model.

The current UI presents unresolved legacy state as either `Continue exact
operation` when complete intent and step identities exist, or `Manual
intervention required` when they do not. Exact continuation opens the saved
review read-only: member, Account, rename, Rebind, renewal, and search/edit
controls are disabled while explicit Confirm remains available for the unchanged
direction. Manual-intervention state exposes no Confirm path. Neither state
renders a Restore command or claims the Phase 2 lifecycle model.

The PostgreSQL schema now provides independent `relationship_operations`,
affected-Mapping ownership, immutable directional steps, and attempt records.
Each Mapping also has a monotonic `baseline_revision`, defaulting existing and
new rows to revision 1. Active ownership is partial-unique by Mapping, so the
storage layer can represent at most one non-terminal owner while retaining
released historical ownership. Snapshots, fingerprints, directional identity,
reviewed resources, supported directions, and attempt direction are stored
independently from legacy `operation_state`.

This is a storage boundary only. Current Confirm/Retry execution does not yet
create or orchestrate these entities, and Mapping baseline promotion still
follows the legacy runtime described above. Persist-before-dispatch, lifecycle
orchestration, Resume/Restore, alignment APIs, migration, and restart/concurrency
guarantees remain pending their dependent delivery issues.

Destination and source mapping changes commit in one local transaction. A local
persistence failure rolls back every affected mapping and returns structured
`failed`, `rolled_back`, and `skipped` results rather than a half-saved transfer.
`operation_state` is current retry state, not an audit event stream.

## Mapping Renewal

`Renew Subscriptions` is an explicit read-only Preview plus final Confirm. It
selects the Replan Baseline's Managed Mapping Members by default; Relay-only
members remain outside scope until adopted. The term defaults to 365 days, is
editable for the current operation, and is not persisted as mapping
configuration.

The reviewed action is deterministic:

- active subscription: extend from current expiry;
- expired subscription: restart active from execution time;
- missing expected subscription: create for the reviewed term;
- suspended subscription: skip and preserve suspension;
- unexpected/additional Group: report drift without renewal, removal, or Key
  movement.

Confirm obtains a fresh relationship snapshot before its first write and uses
the reviewed subscription ID directly. Mutations run with bounded concurrency;
results retain deterministic member order. One fresh readback follows all
writes. Retry reuses deterministic per-member idempotency keys and sends only
failed members; succeeded/skipped members are not submitted again.

Renewal is synchronous and returns per-member `succeeded`, `skipped`, or
`failed`. It creates no background job or renewal-history entity. Closing the
result view ends that operation; a later renewal starts from a new Preview.

## Read and Privacy Boundaries

Preview/Replan use one request-scoped provider-wide identity/subscription
snapshot for candidate validation, Managed Mapping Member and Relay-only
unmanaged membership, effects, retry, and fingerprint construction. Group and
Account collections load once; each relevant user's API Keys load at most once.
Directory facts are resolved once per mapping-list request. These facts are
discarded after the request and are not cross-request authorization or
relationship caches.

The mapping list begins Provider Group, relationship, Account, and Directory
reads concurrently; upstream read count scales with Relay pagination and
Provider/Platform count, not managed-mapping count. Usage remains advisory and
may use a valid Team Usage prewarm full hit without changing relationship
authority or the fingerprint.

All Relay reads/writes remain behind `relay.Provider` capabilities and HTTP.
There is no direct sub2api database coupling. Fingerprints, persisted state,
logs, errors, and UI exclude credentials, API-Key values, private Account
configuration, and raw upstream payloads.

Current callers retain their implemented Provider capability grouping and
fallback ownership. `relay.Provider` remains capability-level rather than a
workflow-level facade.
