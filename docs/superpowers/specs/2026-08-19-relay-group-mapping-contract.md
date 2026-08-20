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
- Preview is read-only. Group creation, membership changes, source removal,
  API-Key binding, and adoption require the final Confirm action.

## Stable Mapping

The mapping is keyed by provider, department external ID, and Platform. Group
IDs are authoritative; display names are snapshots. Replan preserves target
Group IDs and only produces a new member assignment matrix. It does not create,
deactivate, resize, or automatically reshuffle target Groups.

Rebind changes only the saved relationship. The UI requires a final Confirm,
and the backend validates the department input and all Template, Source, and
target IDs against the selected Platform before persisting it. When a stored
department disappears from the current directory snapshot, mapping reads keep
the relationship, mark it unavailable, and return same-Platform departments
that are not already mapped as suggestions.

## Account and Member Maintenance

Existing mappings start with Account management uninitialized. They display
the current same-Platform Account IDs, safe display fields, and per-Group
priorities without treating an absent desired state as an instruction to remove
Accounts. `Adopt Current` saves those IDs and priorities locally and performs no
Relay write. Account search covers every Account type on the selected Platform;
status and schedulability are warnings rather than filters.

After initialization, the saved desired Account order is applied only through
Confirm. Reconciliation adds, removes, or changes only the selected target
Group relationship while preserving every unrelated Account-to-Group binding.
A newly duplicated target inherits the Template Account IDs and priorities,
verifies those relationships, and becomes active before member migration. An
Account failure blocks only its target; other targets continue.

An administrator may explicitly remove a managed member. A saved Source is
restored, eligible API Keys still bound to the target are moved back, and the
target subscription is removed. Without a saved Source, only the target
subscription is removed. `Move Here` transfers one member from one explicit
same-Provider, same-Platform mapping. `Add Additionally` preserves the old
mapping, subscription, and API-Key bindings and adds only the new target
subscription. Department changes never trigger either action automatically.

## Relationship-Bound Confirmation

Every Preview returns an opaque versioned SHA-256 relationship fingerprint.
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
When a Replan moves a member between managed target Groups, the retry state also
retains the actual previous target Group ID so a failed API-Key move can be
retried from that Group instead of falling back to the original source Group.
Once a matching relationship preflight has completed, later upstream failures
are recorded as normal per-step retry state rather than relabeled as a stale
Preview. A retry obtains a fresh Preview fingerprint while retaining the saved
operation state and actual previous target Group needed by unfinished steps.

Relay members that exist in a managed target Group but have no local mapping are
shown as unmanaged. Their observed 30-day cost contributes to remaining target
capacity. An administrator may explicitly adopt one; adoption only ensures the
target subscription and never performs source removal or API-Key migration.
