# Relay Group Mapping Contract

## Status

This is the current implementation contract for the department x Platform Relay
planning workflow. It supersedes the allocation details in the 2026-08-18
planning plan where they conflict. It does not authorize a staging or
production release.

## Inputs and Preview

- A preview selects one provider, directory department, Platform, Template
  Group, Migration Source Group, and per-target planning cost.
- Template and Migration Source are independent IDs. Both, and every existing
  target ID supplied by an administrator, must exist on the selected Platform.
- Source-group members with a valid local Relay mapping are recommended by
  default. Members outside the source group are never used to create a default
  target; an administrator may explicitly add them after a target exists.
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

## Execution and Retry

Each mapping stores `operation_state` as a JSON object. It records the last
operation key, each target-group step, each local member step, and each
explicitly adopted Relay-only member, including status and error text. A
partial execution is returned as `needs_retry`; a later Confirm/replan replaces
successful step entries while retaining unresolved failed entries. No audit
event stream is implied by this field.

Relay members that exist in a managed target Group but have no local mapping are
shown as unmanaged. Their observed 30-day cost contributes to remaining target
capacity. An administrator may explicitly adopt one; adoption only ensures the
target subscription and never performs source removal or API-Key migration.
