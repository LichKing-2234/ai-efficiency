# AI Efficiency Platform

AI Efficiency Platform measures and improves AI-assisted development while providing users with a guided path to connect their permitted AI tools.

## Language

**Managed Mapping Member**:
A local user explicitly assigned to a managed Relay target group by one Relay Group Mapping. A Relay-only user observed in that target group is not a managed mapping member.
_Avoid_: Group member, Relay member

**Replan Baseline**:
The member-to-target assignment saved by the last confirmed Relay Group Mapping operation and used as the comparison point for a new replan. It remains authoritative when a saved member's current Relay identity becomes unavailable; that drift does not silently delete or relocate the saved assignment.
_Avoid_: Original members, current members

**Replan Roster**:
The managed mapping members in one target group at the Replan Baseline. It excludes newly eligible source or department users, is not expanded by recommendations, and retains members whose current Relay identity is unavailable.
_Avoid_: Candidate pool, suggested members

**Unavailable Replan Target**:
A target Group ID saved by the Replan Baseline that is absent from the current Relay Group facts. Replan keeps the saved Target and its roster visible as non-executable drift until a later fresh Preview observes the Target again.
_Avoid_: Deleted target, replacement target
