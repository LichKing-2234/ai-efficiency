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

**Relationship Operation**:
A durable, admin-reviewed Relay Planning change with immutable directional steps, affected-Mapping ownership, independent execution attempts, and verified terminal readback. Resume converges to the reviewed Target and promotes the Replan Baseline; Restore converges to the captured baseline without changing it.
_Avoid_: Retry state, Relay transaction

**Sub2API Group**:
The Sub2API routing group whose per-group health the Ops Monitoring surface displays. It is an upstream routing target, not a user-selectable Access Group and not a Relay Group Mapping target.
_Avoid_: Access Group, Target Group, department group

**Group Health Snapshot**:
The verbatim Sub2API ops overview for one Sub2API Group over one explicit time window, shown as returned. The platform never recomputes it.
_Avoid_: computed health score, derived metric

**Ops Monitoring**:
The admin-only read-only surface that presents Group Health Snapshots for every Sub2API Group at once, with no alerting and no relationship or account mutations.
_Avoid_: Relay Planning, dashboard, alerting

**Account Custodian (号主)**:
The local user whose normalized email equals an upstream Sub2API OpenAI account's `credentials.email` or its email-shaped `name`, and therefore may read that account's facts or act on its upstream-scoped capabilities. Either field is sufficient; ownership is derived on every request from `users.email` and is not stored as a registry.
_Avoid_: Account owner registry, assigned owner

**Owner Account**:
An OpenAI OAuth account on the primary Relay provider whose normalized `credentials.email` or email-shaped `name` (exactly one `@` with non-empty local and domain parts) equals the current local user's email. Shadow accounts are excluded. Only Owner Accounts appear in 「我的账号」.
_Avoid_: Custody record, claimed account

**Owner Operation**:
One asynchronous, audited, non-retried upstream mutation started by an Account Custodian (`reset_credit` or `send_invitation`). It is visible as an operation id and may end `unknown` when the upstream outcome cannot be determined. A quota or referral refresh is recorded as a terminal operation row for audit and freshness but is not an asynchronous mutation.
_Avoid_: Retry job, quota reset request

**Unknown Owner Operation**:
An Owner Operation whose upstream call may have executed but whose terminal outcome could not be observed. It is never retried automatically and blocks a repeat until a newer owner-requested observation exists; there is no administrator override.
_Avoid_: Failed operation, pending retry

**Cached Reset Credit Count**:
The number of unexpired banked ChatGPT/Codex reset credits recorded by the last cached upstream snapshot for one account. The account-configuration copy page displays it and filters the working set by it; it is not account configuration, is never compared against a baseline, and is never copied.
_Avoid_: Recharge coupon, account balance, live reset count
