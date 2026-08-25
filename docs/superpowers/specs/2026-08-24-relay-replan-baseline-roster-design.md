# Relay Replan Baseline Roster Design

## Status

The zero-change Replan Baseline was implemented for #354 and merged through PR
#355 on 2026-08-24. Request-scoped relationship reuse was merged through PR
#363 on 2026-08-25. The unavailable-member follow-up for #374 merged through PR
#376 on 2026-08-25. This design also defines the unavailable-saved-Target
behavior tracked by #375. It supersedes only the existing-mapping Replan
candidate auto-placement and unavailable-relationship behavior in the
[Relay Group Mapping Contract](./2026-08-19-relay-group-mapping-contract.md).

## Problem

Replan restored saved member assignments and then automatically placed other
eligible source members into remaining target capacity. The UI rendered the
combined result, so an administrator could not reliably distinguish the last
confirmed roster from newly placed members.

The saved member-to-target state is the **Replan Baseline**. The saved managed
members in one target Group are its **Replan Roster**.

## Decision

Replan opens as a zero-change view of the last confirmed mapping:

- Each saved managed member stays assigned to the target Group recorded by the
  Replan Baseline. A member whose current local-to-Relay identity is unavailable
  remains visible in that saved Target with a safe warning.
- Each saved Target Group ID stays in its original position. If that Target is
  absent from current Relay Group facts, its saved roster remains visible with
  a safe warning and no replacement, move, removal, resize, or deactivation is
  proposed.
- Replan does not automatically select, assign, or recommend any other member.
- Each target Group shows its Replan Roster using the same member usage and
  ranking presentation available when creating a mapping. No new ranking
  metric or ordering rule is introduced.
- Other available local candidates use the existing candidate presentation,
  but start unselected and unassigned and are not labeled as recommendations.
- Administrators retain the existing explicit member operations: search and
  add, move, remove, and choose the current source or target-only behavior.
- Relay-only unmanaged members retain the existing unmanaged-member behavior
  and start unselected. Adoption remains an explicit administrator action.

Replan remains read-only until the administrator confirms reviewed changes.
Opening Replan alone produces no member mutation and no proposed member delta.

## Backend Contract

For an existing-mapping Replan request without reviewed assignments, returned
assignments contain every saved managed member and the saved target Group ID.
Members with an unavailable current identity remain visible but make the plan
non-executable. An unavailable saved Target likewise remains in the assignment
list by stable ID with its saved roster and makes the complete plan
non-executable. Remaining target capacity never pulls in eligible source or
department candidates.

Candidate loading still includes saved members outside the mapping's current
department so the complete Replan Baseline can be reconstructed. Other
candidates, live subscription facts, usage facts, unmanaged Relay members,
relationship fingerprints, and retry state remain available through their
existing contracts. A later request containing explicit administrator edits is
validated and summarized through the existing Preview and Confirm path.
Existing identity revalidation remains authoritative: a stale or cross-Provider
Relay identity is warned and cannot enter an executable plan. Replan keeps the
saved assignment visible, but Confirm rejects the complete reviewed plan before
any Relay write, including otherwise valid edits in the same plan. Explicit
add, move, or remove edits cannot silently hide that saved member while the
identity remains unavailable.

Current Relay Group facts determine whether each saved Target is available.
The deterministic roster calculation receives those already-loaded facts and
adds an unavailable-Target blocker without changing target order, member
assignments, reviewed edits, or cost totals. Preview skips name validation only
for that blocked Target because no current Relay name exists. Confirm returns
the categorized stale-plan response before any Relay write. If the Target later
reappears, its Group fingerprint changes, so the missing-state Preview is stale
and a fresh Preview is required before normal Confirm can proceed.

One request-scoped provider relationship snapshot supplies identity and
complete subscription facts for current-department candidates, saved external
members, and Relay-only unmanaged-member detection. Replan loads Group and
same-Platform Account collections once and reads each relevant user's API Keys
at most once, then reuses those same facts for eligibility, migration effects,
the Replan Baseline and Roster, and the relationship fingerprint. A valid Team
Usage prewarm generation remains the only provider-wide 30-day usage source on
a full hit. No relationship fact is cached across HTTP requests.

## Unchanged Behavior

This design does not change:

- initial mapping creation or its existing ranking and selection workflow;
- target Group IDs, fixed Replan Group count, naming, or rename review;
- Account adoption, desired Account reconciliation, or drift warnings;
- Rebind behavior;
- manual member add, move, remove, or Relay-only adoption semantics;
- relationship fingerprints, stale-plan handling, Confirm, persistence, or
  retry behavior.

## Acceptance Scenarios

1. A mapping last confirmed Alice in Target A and Bob in Target B. Replan opens
   with Alice in A and Bob in B, with no member changes proposed.
2. Carol is now eligible through the source Group. Replan displays Carol in the
   existing candidate surface, but Carol is unselected and unassigned.
3. The administrator explicitly adds Carol to Target A. Only then does the plan
   contain an add effect for Carol.
4. A saved managed member no longer belongs to the mapped department. Replan
   still restores that member to the saved target.
5. An unmanaged Relay-only member remains in the existing unmanaged-member
   surface, starts unselected, and is adopted only through an explicit action.
6. A saved managed member loses the current local-to-Relay identity. Replan
   keeps the member in the saved Target with an unavailable-identity warning.
   If the administrator also reviews a valid edit for another member, Confirm
   rejects the complete plan before any Relay write and returns the refreshed
   roster through the existing stale-plan response.
7. A saved Target Group is absent from current Relay Group facts. Replan keeps
   the Target ID and its saved members in their original positions, warns that
   the Target is unavailable, and blocks a mixed plan before any Relay write.
   After the Target reappears, the old fingerprint is stale and a fresh Preview
   restores normal execution.
