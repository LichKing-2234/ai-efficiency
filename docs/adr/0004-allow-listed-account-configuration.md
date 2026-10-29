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

## Amendment 2026-10-26: reset-credit expiry keys

Sub2API `0.2.18-ccws2` adds two account-level `extra` keys for using OpenAI/Codex
rate-limit reset credits before they expire:
`auto_reset_credit_expiry_enabled` and
`auto_reset_credit_expiry_lead_minutes`. The allow-list grows by those two keys
(now thirteen) so the existing copy surface carries the expiry setting; the same
narrowed projection and key-level JSONB merge are used, with no new surface.

Sub2API reads a missing pair as disabled with a ten-minute lead, so those are the
recorded defaults. #6950 also changed the 5h threshold default from 100% to 0
(window disabled); the recorded default moves with it, while stored values keep
their meaning. This is the record the allow-list rule requires for an addition.

## Amendment 2026-10-29: display-only reset-credit count on the copy page

The account-configuration copy page shows each account's remaining reset-credit
count and filters the working set by it, because an administrator choosing copy
targets needs to see which accounts still hold banked ChatGPT/Codex reset credits.
Reading that value is an extension of the read boundary recorded above, so it is
recorded here.

The value is a display-only observation, not account configuration. It is read
from Sub2API's persisted `extra.codex_reset_credit_snapshot` through the same
narrowed account-list decode, projected onto its own
`relay.AccountConfigurationRecord.ResetCreditsAvailable` field, and clamped to the
credits that have not expired — the same projection the owner-account console
uses, so both surfaces report the same count. The key never joins the copy
allow-list (which remains thirteen keys), is never compared against a baseline, is
never written to any account, and is never persisted by AI Efficiency. Copying a
snapshot between accounts would fabricate observed upstream state; that is why the
copy allow-list and the display-only key list are separate and guarded by tests.

## Amendment 2026-10-29: inert auto-reset window thresholds

Sub2API evaluates `auto_reset_credit_5h_threshold` and
`auto_reset_credit_7d_threshold` only while the account-level
`auto_reset_credit_enabled` switch is on. The 5h default moved from 100% to 0 in
`0.2.19` (#6950), while accounts materialised by older builds still store 100%.
Comparing raw values therefore reports every account that never carried the keys
as deviating, over a threshold that has no effect for either side; the same shape
mismatch is reported as missing or additional keys. Copying those values would
also materialise a meaningless 100% threshold on accounts that may later turn the
switch on.

Comparison now reads the two thresholds only when both the baseline and the
target store the switch as enabled, and reports the switch itself when it
differs. A copy from a baseline whose switch is off writes the provider
thresholds (5h 0, 7d 1) instead of the baseline's stored values. The allow-list is
unchanged and stored values keep their meaning for an account whose switch is on:
this narrows when two allow-listed keys are compared and what a copy writes, not
what may be read. The contract
`docs/contracts/relay-group-mapping.md` states the resulting comparison and copy
behavior.
