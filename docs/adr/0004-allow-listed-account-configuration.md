---
status: accepted
---

# Let AI Efficiency apply an allow-listed Account configuration

AI Efficiency may read and apply one explicitly allow-listed set of account-level
settings on existing Relay (Sub2API) accounts: `credentials.model_mapping` plus a
named list of feature flags in `extra`. It never reads credential material, never
stores the configuration it applies, and never writes any other account column.

This deliberately extends the platform past two boundaries that were stated
before it existed. `docs/adr/0002-managed-relay-relationship-operations.md`
grants authority only over relationships explicitly managed by a Relay Group
Mapping, and account configuration is not a relationship. The Relay Group
Mapping contract states that `plan_type` is the one field read out of the
credential blob and that every other credential and private-configuration field
stays unrepresented. Both are amended here rather than bypassed. The amendment is
narrow by construction: the allow-list is exhaustive, it is enforced inside the
Relay projection so a future upstream field cannot widen it, and adding to it
requires amending this record rather than only changing code.

The write is a single key-level JSONB merge, never a document replace. That
choice is what makes the operation safe to perform on a live account: the merge
cannot carry credential material, cannot overwrite a concurrent token refresh,
and cannot be overwritten by one. Because the upstream endpoint reports success
for account IDs that do not exist, every application is verified by read-back
rather than trusted from its response.

## Considered Options

- Applying configuration through `apply-oauth-credentials` was rejected because
  it merges Go-side and then replaces the entire `credentials` column, so a
  token refresh committing between the read and the write is clobbered, and the
  reverse is equally possible.
- Keeping AI Efficiency read-only and having administrators copy configuration by
  hand in Sub2API was rejected because re-entering a 13-entry model mapping plus a
  dozen feature flags per seat is the cost being removed, and a manual path has
  no verification step.
- Storing a configuration template in AI Efficiency and applying it to accounts
  was rejected because it duplicates upstream state, requires its own lifecycle
  and persistence, and retains configuration the platform has no reason to hold.
- Broadening the allow-list to all account fields — column-level concurrency,
  priority, notes, proxy, and group relationships — was rejected because group
  relationships are Mapping authority under ADR-0002, and the column-level fields
  are writable only through the document-replacing endpoint this record avoids.
- Background automatic application was rejected for the same reason ADR-0002
  rejects background repair: it would turn an unreviewed change into a production
  traffic mutation without an administrator's confirmation.

## Consequences

- The "`plan_type` is the one field read out of the credential blob" rule becomes
  an allow-list rule. The contract and the code comment that carry that rule must
  name the allow-list, and any later addition to it requires amending this record.
- `relay.Account` keeps its current shape and privacy guarantee. The copied
  configuration travels in a separate, closed projection, so the existing
  whole-DTO assertion that credential material never escapes retains its force.
- Read-back verification after every application is mandatory, not a refinement,
  because the upstream endpoint reports success for nonexistent accounts.
- Read-only access to the allow-list is itself an extension of the read boundary
  and is covered by this decision; the platform still never reads, transports, or
  stores credential material.
- ADR-0002's prohibition on unattended writers continues to apply. This operation
  is administrator-confirmed and synchronous, and there is no background
  synchronizer.
- Implementation, a pull request, and deployment require separate authorization,
  as ADR-0002 also requires.
