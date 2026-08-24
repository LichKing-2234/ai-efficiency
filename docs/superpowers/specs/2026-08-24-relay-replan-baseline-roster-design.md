# Relay Replan Baseline Roster Design

## Status

Implemented on branch `codex/issue-354-replan-baseline-roster` for #354 on
2026-08-24. The implementation is not merged or released by this document.
This design supersedes only the existing-mapping Replan candidate
auto-placement behavior in the
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

- Each saved managed member whose local-to-Relay identity remains valid stays
  assigned to the target Group recorded by the Replan Baseline.
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
assignments contain only valid members restored from the saved mapping and
their saved target Group IDs. Remaining target capacity never pulls in eligible
source or department candidates.

Candidate loading still includes saved members outside the mapping's current
department so the complete Replan Baseline can be reconstructed. Other
candidates, live subscription facts, usage facts, unmanaged Relay members,
relationship fingerprints, and retry state remain available through their
existing contracts. A later request containing explicit administrator edits is
validated and summarized through the existing Preview and Confirm path.
Existing identity revalidation remains authoritative: a stale or cross-Provider
Relay identity is warned and cannot enter executable assignments.

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
