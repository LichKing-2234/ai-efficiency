# AI Efficiency Platform

AI Efficiency Platform measures and improves AI-assisted development while providing users with a guided path to connect their permitted AI tools.

## Language

**Access Group**:
A user-selectable access scope that determines which AI service category and personal credential the user may use.
_Avoid_: Service source, AI service

**Usage Window Preference**:
The last preset window explicitly selected within the AI Usage task zone and shared by personal, team, and member usage views. It excludes Activity date ranges.
_Avoid_: Date filter, Activity range

**Current Step**:
The onboarding panel the user has explicitly chosen to view. It is independent of facts such as whether a credential already exists or a connection test has succeeded.
_Avoid_: Active state, completion state

**Connection Test**:
A platform-originated request that verifies sub2api accepts the selected access group, personal credential, model, and protocol. It does not prove that a local AI client has been configured.
_Avoid_: Client verification, account health test

**Client Identity Profile**:
The request characteristics that sub2api recognizes as belonging to a supported official client family.
_Avoid_: Client verification, client login state

**Managed Mapping Member**:
A local user explicitly assigned to a managed Relay target group by one Relay Group Mapping. A Relay-only user observed in that target group is not a managed mapping member.
_Avoid_: Group member, Relay member

**Replan Baseline**:
The member-to-target assignment saved by the last confirmed Relay Group Mapping operation and used as the comparison point for a new replan. It remains authoritative for roster reconstruction when live Relay relationships drift; unavailable facts do not silently delete or relocate its assignments.
_Avoid_: Original members, current members

**Replan Roster**:
The managed mapping members in one target group at the Replan Baseline. It excludes newly eligible source or department users, is not expanded by recommendations, and retains members whose current Relay identity or saved target is unavailable.
_Avoid_: Candidate pool, suggested members

**Mapping Renewal**:
An explicit subscription-renewal operation for one Relay Group Mapping. It selects all managed mapping members by default, excludes Relay-only unmanaged members, and adds the requested renewal term from an active subscription's current expiry or from the renewal time when the subscription has expired. It creates a missing expected subscription but never resumes a suspended subscription, removes an unrelated subscription, or moves traffic.
_Avoid_: Append subscription, Group renewal

**Renewal Term**:
The number of days added by one Mapping Renewal. Each operation defaults to 365 days, may be changed for that operation, and does not alter the Relay Group Mapping.
_Avoid_: Mapping validity, Subscription default
