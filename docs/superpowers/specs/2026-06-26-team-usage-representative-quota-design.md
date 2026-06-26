# Representative AI Usage, Team Overview, and Quota Control Design

**Date:** 2026-06-26
**Status:** Draft for user review
**Scope:** `backend/internal/handler/`, `backend/internal/relay/`, `backend/internal/directorytree/`, `backend/ent/schema/`, `frontend/src/components/user/usage/`, `frontend/src/api/`, `frontend/src/types/`, `frontend/src/i18n.ts`, `docs/architecture.md`
**Related:**

- [2026-06-06-user-usage-trend-design.md](./2026-06-06-user-usage-trend-design.md)
- [2026-06-16-ai-usage-center-group-quota-design.md](./2026-06-16-ai-usage-center-group-quota-design.md)
- [2026-06-22-configurable-directory-sync-design.md](./2026-06-22-configurable-directory-sync-design.md)
- [docs/architecture.md](../../architecture.md)

## Spec Relationship

- This spec extends the AI Usage Center from a personal usage surface into a subject-switchable usage surface. Representatives can view `My Usage` or switch to a scoped member and see that member's AI usage.
- It adds a separate Team Overview page for department-subtree usage ranking and member comparison. Team Overview is intentionally different from the personal AI Usage view and must not render quota cards or quota controls.
- It inherits the personal quota-card conventions from the 2026-06-16 group quota design for per-subject AI Usage only. Quota cards and multiplier controls belong to the selected member's personal usage context, not to the Team Overview page.
- It inherits representative metadata from the 2026-06-22 Directory Sync design. Team membership and representative scope come from local directory facts, not from sub2api group ownership and not from a new local admin role.
- It does not change historical specs in place. This spec is the current contract for representative team usage and delegated quota control.
- Implementation must update `docs/architecture.md` because this adds a new delegated access boundary and a new audit surface.

## Data Hygiene

Tests, fixtures, docs, screenshots, and examples for this feature must not contain real employee data, real company domains, real subscription group names, real API keys, real tokens, or real internal URLs.

Use synthetic values such as:

- `alice@example.com`
- `bob@example.org`
- `Department Alpha`
- `Department Beta`
- `Group Alpha`
- `Group Beta`

## Problem

AI Efficiency currently exposes personal AI usage through the AI Usage Center. Admin users can also manage relay/sub2api subscriptions for other users through admin-only flows. There is no delegated manager surface for an organization representative to answer:

1. Which members in my represented department subtree are using AI.
2. How to switch from my own AI Usage to a specific member's AI Usage without becoming an admin.
3. How the represented department subtree ranks by member usage.
4. Which members are driving most of the team usage.
5. Which subscription groups a selected member has.
6. Whether a selected member is close to the effective quota for a subscription group.
7. How to restrict a selected member's effective subscription allowance without granting the representative full admin access.

The original "set member quota" request cannot be implemented by editing group quota:

1. Group daily/weekly/monthly limits are configured on sub2api groups and apply per user subscription at enforcement time.
2. Editing a group limit would affect every user in that group, not only one represented member.
3. Per-user API key quota is not the right first-version control because the target deployment uses sub2api subscription mode, where user platform quota and API key quota are not the primary subscription limit.

The practical hard-enforcement lever available today is sub2api's user-specific group rate multiplier. That multiplier changes `ActualCost`, and subscription usage is accumulated from `ActualCost`, so it changes how quickly a member consumes the group daily/weekly/monthly subscription limits.

## Goals

1. Add a subject selector to AI Usage Center: `My Usage` plus scoped members.
2. Let representatives switch to a scoped member and view that member's AI Usage.
3. Keep representatives as normal users with delegated scope, not admins.
4. Scope the first version to the primary relay provider only.
5. Add a separate Team Overview page for the represented department subtree.
6. In Team Overview, replace the personal Token Trend chart slot with a top-12 member usage ranking.
7. In Team Overview, replace the personal Model Distribution chart with a member usage table.
8. Keep quotas and multiplier controls out of Team Overview.
9. Let representatives adjust a selected member's user-specific group rate multiplier for existing subscription groups from the selected-member AI Usage context.
10. Show how the selected multiplier changes effective daily, weekly, and monthly allowance amounts.
11. Persist a local audit trail for every delegated multiplier write attempt and expose it to both the acting representative and admins.
12. Keep sub2api integration behind `backend/internal/relay.Provider` and optional provider extension interfaces.

## Non-Goals

1. Do not edit sub2api group daily/weekly/monthly quota.
2. Do not assign, extend, or remove subscriptions from the representative surfaces.
3. Do not create, revoke, or edit member API keys from the representative surfaces.
4. Do not expose raw request logs to representatives in the first version.
5. Do not support bulk multiplier edits in the first version.
6. Do not support multiple relay providers in the first version.
7. Do not introduce a new first-class `group_representative` user role.
8. Do not modify sub2api source code as part of this feature.
9. Do not let representatives configure RPM overrides.
10. Do not infer representative scope from sub2api group membership.
11. Do not show quota cards, subscription quota rows, or multiplier controls on Team Overview.

## Captured Decisions

| Area | Decision |
| --- | --- |
| Representative source | Directory Sync metadata |
| Representative scope | Entire represented department subtree |
| User role | Delegated normal user, not admin |
| Product placement | AI Usage Center subject selector plus separate Team Overview page |
| AI Usage Center IA | `My Usage` / scoped member selector |
| Team Overview IA | Independent team page |
| Team Overview Token Trend replacement | Top-12 member usage ranking |
| Team Overview Model Distribution replacement | Member usage table |
| Team Overview quotas | Hidden; no quota cards or quota controls |
| Aggregation | Personal usage by selected subject; team overview by member |
| Provider scope | Primary provider only |
| Usage detail | Aggregated summaries only, no raw request log |
| Quota control | User-specific group rate multiplier |
| Subscription mutation | Existing subscriptions only, no assign/remove |
| Audit | Required locally in AE |

## Approaches Considered

### Option A: AE Delegated Facade Over sub2api User Group Rate Multipliers

AE owns representative scope, UI, validation, and audit. sub2api remains the hard-enforcement system through existing admin APIs for user group rate multipliers.

Pros:

1. Matches the current subscription-mode deployment.
2. Avoids editing group quota shared by other users.
3. Avoids relying on API key quota semantics that are not the main subscription-mode enforcement path.
4. Keeps organization-tree authorization in AE, where Directory Sync already lives.
5. Does not require sub2api schema or enforcement changes.

Cons:

1. The UI must explain that this is multiplier control, not direct quota editing.
2. AE must use sub2api's whole-group rate-multiplier write API carefully.
3. Concurrent direct edits in sub2api admin UI cannot be fully version-protected without a new sub2api patch/version API.

### Option B: Add Representative-Aware APIs to sub2api

sub2api would own representative scope and expose a direct delegated management API.

Pros:

1. Cleaner upstream write semantics if sub2api also adds scoped patch endpoints.
2. Less adapter logic in AE.

Cons:

1. Couples sub2api to AE Directory Sync semantics.
2. Requires cross-repo product and auth design.
3. Makes this feature much larger than the requested AI Usage Center extension.

### Option C: Add True Per-User Subscription Quotas to sub2api

sub2api would add explicit user subscription quota override fields and enforce them directly.

Pros:

1. Best long-term domain semantics for "set quota".
2. UI can display direct quota amounts instead of multiplier-derived effective allowances.

Cons:

1. Requires sub2api schema, enforcement, cache, admin UI, and migration work.
2. Does not reuse the current hard-enforcement lever.
3. Larger than the first version.

## Decision

Use **Option A** for the first version.

AE will provide a delegated management facade:

1. Resolve which local users the current representative may see and manage.
2. Fetch usage and subscription facts through the primary relay provider.
3. Display selected-subject usage with the same high-level AI Usage Center shape where upstream APIs support it.
4. Write user-specific group rate multipliers through sub2api admin APIs via relay provider extension methods.
5. Persist an AE audit record for every attempted change.

## sub2api Contract Facts

The design relies on these current sub2api behaviors:

1. Subscription-mode eligibility checks compare one `UserSubscription` usage record against the group's daily/weekly/monthly limits.
2. User platform quota is skipped in subscription mode.
3. Gateway billing selects multiplier by priority: user-specific group multiplier, then group default multiplier, then system default multiplier.
4. Usage logs and subscription consumption use `ActualCost`, so a higher multiplier consumes subscription quota faster and a lower multiplier consumes it slower.
5. `GET /api/v1/admin/groups/:id/rate-multipliers` returns user-specific group rate entries.
6. `PUT /api/v1/admin/groups/:id/rate-multipliers` replaces the rate-multiplier part for the whole group.
7. Entries omitted from the PUT payload have `rate_multiplier` cleared. Existing `rpm_override` values are not part of this feature and must not be edited by AE.

Important implication: AE must never PUT only the target member's entry. AE must read current entries, merge the target change, preserve all other users' non-null `rate_multiplier` entries, then PUT the full merged rate list.

## Multiplier Semantics

Selected-member AI Usage must describe the control as `Rate multiplier`, with quota impact shown as derived data.

For a subscription group:

```text
effective_multiplier = user_specific_multiplier if present else group_default_multiplier
effective_daily_allowance = group_daily_limit_usd / effective_multiplier
effective_weekly_allowance = group_weekly_limit_usd / effective_multiplier
effective_monthly_allowance = group_monthly_limit_usd / effective_multiplier
```

The "effective allowance" means the approximate pre-multiplier standard-cost amount the member can consume before the subscription window reaches the group limit. The actual enforced sub2api limit remains the group daily/weekly/monthly `ActualCost` limit.

Display rules:

1. Missing group limit means that period is unlimited.
2. Effective multiplier `0` means consumption does not advance by cost for that group and period calculations are infinite. Representatives must not be allowed to set `0` in the first version.
3. If a member has no user-specific multiplier, display `Inherited`.
4. Reset means clear the user-specific multiplier so the member inherits the group default. It does not write the group default value as an explicit user multiplier.
5. Show before and after values for every configured period before submitting.

## Delegated Multiplier Policy

Delegated multiplier edits need a policy boundary because lowering a multiplier increases effective allowance.

First-version default policy:

1. Representatives may reset a member to inherit the group default.
2. Representatives may set an explicit multiplier greater than or equal to the group default multiplier.
3. Representatives may not set a multiplier below the group default multiplier.
4. Representatives may not set multiplier `0`.
5. Representatives may not set a negative multiplier.
6. Representatives may not set a multiplier above an AE server-side maximum, default `10`.

This default lets representatives restrict usage while preventing them from granting extra quota beyond the group's default economics. If the product later needs representatives to grant extra allowance, that must be a separate admin-configured policy extension.

If an existing user-specific multiplier was set by an admin below the group default, selected-member AI Usage may display it but must not let the representative create or reapply that value unless a future policy explicitly allows it.

## Architecture

```text
AI Usage Center
  |
  |-- Subject selector
        - My Usage
        - scoped members
  |
  |-- My Usage
        GET /api/v1/user/usage/dashboard
  |
  |-- Selected member usage
        GET /api/v1/user/team-usage/subjects
        GET /api/v1/user/team-usage/subjects/:user_id/usage/dashboard
        PUT /api/v1/user/team-usage/subjects/:user_id/groups/:group_id/rate-multiplier
        GET /api/v1/user/team-usage/audit

Team Overview page
  |
  v
  GET /api/v1/user/team-usage/overview
      |
      v
  AE representative scope resolver
    - current user -> current directory member
    - representative departments -> subtree department ids
    - subtree members -> local users
      |
      v
  primary relay provider
    - selected-subject usage summary/trend/models
    - team member ranking and table
    - selected-subject subscriptions
    - group rate multipliers
    - merged rate-multiplier write

Admin audit
  |
  v
  GET /api/v1/admin/team-usage/audit
```

Module boundaries:

1. `handler` owns HTTP contracts, request validation, and auth failures.
2. A small representative scope service owns directory metadata parsing and subtree resolution. This should be reusable outside representative usage features.
3. `relay.Provider` owns sub2api integration. Representative usage and overview features must use optional extension interfaces for capabilities that not every relay implementation supports.
4. `frontend` owns AI Usage Center subject selection, the separate Team Overview page, selected-member subscription rows, and edit dialogs.
5. Ent schema owns local audit records only. It must not copy sub2api usage logs into AE.

## Representative Scope Resolution

The resolver must fail closed.

Inputs:

1. Current AE user id and normalized email.
2. Current Directory Sync source id from the latest successful full-company apply run.
3. `directory_members` from that source.
4. `directory_departments` from that source.

Representative departments are the union of:

1. Departments where `department.metadata.representative_external_ids` contains the current member's `external_id`.
2. Departments whose `external_id` appears in the current member's `metadata.leader_department_ids`.

For each represented department, compute the subtree using `directorytree.Tree.SubtreeIDs`.

Allowed members are directory members whose `department_external_id` is in any represented subtree. Resolve each member to an AE user by:

1. `directory_members.matched_user_id`, when present and positive.
2. normalized email match against `users.email`, as fallback.

Rows without an AE user may appear in non-actionable counts only if the UI needs them for organization visibility. First-version member management rows should include only matched local users. Rows without `relay_user_id` are visible as unavailable and cannot be managed.

Security requirements:

1. Every representative usage endpoint must recompute or load the current representative scope server-side.
2. `target_user_id` must be validated against the resolved allowed local user ids.
3. `group_id` must be validated against the target relay user's active existing subscriptions.
4. The current representative cannot manage themself through delegated member controls unless they are also in the represented subtree; even then the same policy applies.
5. Admin role alone is not used to broaden these `/user/team-usage/*` endpoints. Admin-wide views remain separate admin routes.

## Backend API

Unless explicitly marked as admin audit, endpoints below are protected normal-user routes. Representative-facing routes are not admin routes.

### Scope Summary

```text
GET /api/v1/user/team-usage/scope
```

Returns whether the current user is a representative and which departments are in scope.

Representative with scope:

```json
{
  "is_representative": true,
  "departments": [
    {
      "external_id": "department-alpha",
      "name": "Department Alpha",
      "display_path": "Department Alpha",
      "subtree_member_count": 12,
      "matched_user_count": 10
    }
  ]
}
```

No scope:

```json
{
  "is_representative": false,
  "departments": []
}
```

### Usage Subjects

```text
GET /api/v1/user/team-usage/subjects?q=alice&page=1&page_size=20
```

Returns the selectable subjects for AI Usage Center.

The response always includes `My Usage` as a synthetic subject. If the current
user is a representative, it also includes matched users from the represented
department subtree.

Example:

```json
{
  "subjects": [
    {
      "subject_type": "self",
      "user_id": 100,
      "display_name": "Me",
      "email": "me@example.com",
      "department_display_path": "Department Alpha",
      "relay_user_id": 1000,
      "selectable": true
    },
    {
      "subject_type": "member",
      "user_id": 101,
      "display_name": "Alice",
      "email": "alice@example.com",
      "department_display_path": "Department Alpha",
      "relay_user_id": 1001,
      "selectable": true
    }
  ]
}
```

Rows without a relay user mapping may be returned with `selectable: false` if the UI needs to explain why a member cannot be viewed.

### Selected Subject Usage Dashboard

```text
GET /api/v1/user/team-usage/subjects/:user_id/usage/dashboard?start_date=...&end_date=...&granularity=...&timezone=...
```

Returns a selected member's AI Usage Center snapshot. The shape should stay as close as possible to `GET /api/v1/user/usage/dashboard` so the frontend can reuse personal dashboard components.

Response additions:

```json
{
  "subject": {
    "subject_type": "member",
    "user_id": 101,
    "display_name": "Alice",
    "email": "alice@example.com",
    "department_display_path": "Department Alpha",
    "relay_user_id": 1001
  },
  "configured": true,
  "range": {
    "start_date": "2026-06-01",
    "end_date": "2026-06-26",
    "granularity": "day",
    "timezone": "Asia/Shanghai"
  },
  "stats": {},
  "trend": [],
  "models": [],
  "group_quotas": {
    "status": "ok",
    "unit_label": "USD",
    "groups": []
  },
  "subject_subscription_groups": []
}
```

Implementation notes:

1. `self` continues to use the existing `/api/v1/user/usage/dashboard` path.
2. Selected member usage must not use the member's relay password or login as that member.
3. `sub2apiRelay` should use admin aggregate APIs filtered by `user_id`:
   - `GET /api/v1/admin/usage/stats`
   - `GET /api/v1/admin/dashboard/trend`
   - `GET /api/v1/admin/dashboard/models`
4. Selected member quota cards use the target user's active subscription groups and user-specific rate multiplier state. This is a selected-member personal usage view, not the Team Overview page.

### Selected Subject Subscription Rows

These rows are returned in the selected subject dashboard as
`subject_subscription_groups`. They are separate from the existing personal
`group_quotas` presentation so multiplier-control fields do not overload the
homepage quota-card contract.

Subscription group row:

```json
{
  "group_id": "42",
  "group_name": "Group Alpha",
  "platform": "openai",
  "subscription_status": "active",
  "group_default_multiplier": 1.0,
  "user_multiplier": 2.0,
  "effective_multiplier": 2.0,
  "multiplier_source": "user",
  "daily_limit_usd": 10.0,
  "weekly_limit_usd": 50.0,
  "monthly_limit_usd": 200.0,
  "daily_effective_allowance_usd": 5.0,
  "weekly_effective_allowance_usd": 25.0,
  "monthly_effective_allowance_usd": 100.0,
  "daily_usage_usd": 2.4,
  "weekly_usage_usd": 10.2,
  "monthly_usage_usd": 42.5,
  "editable": true
}
```

### Update Member Group Multiplier

```text
PUT /api/v1/user/team-usage/subjects/:user_id/groups/:group_id/rate-multiplier
```

Set explicit multiplier:

```json
{
  "mode": "set",
  "rate_multiplier": 2.0,
  "reason": "Reduce member allowance for this month"
}
```

Reset to inherit group default:

```json
{
  "mode": "reset",
  "reason": "Return to group default"
}
```

Response:

```json
{
  "status": "succeeded",
  "audit_id": 9001,
  "group_id": "42",
  "old_multiplier": 1.0,
  "old_multiplier_source": "inherited",
  "new_multiplier": 2.0,
  "new_multiplier_source": "user",
  "old_effective_monthly_allowance_usd": 200.0,
  "new_effective_monthly_allowance_usd": 100.0
}
```

Validation failures:

1. `403` if the current user is not a representative for the target user.
2. `404` if the target user does not exist in the representative scope.
3. `409` if the target user has no relay user mapping.
4. `409` if the target user does not have an active subscription for the group.
5. `422` if the multiplier violates delegated policy.
6. `503` if the primary relay provider does not support the required rate-multiplier APIs.

### Team Overview

```text
GET /api/v1/user/team-usage/overview?timezone=...
```

Returns the independent team page data for the representative scope. The first
version exposes today and rolling 30-day usage because the current sub2api batch
user usage endpoint returns those windows. It does not expose arbitrary team
date ranges.

First-version response:

```json
{
  "configured": true,
  "is_representative": true,
  "window": {
    "today": "2026-06-26",
    "rolling_days": 30,
    "timezone": "Asia/Shanghai"
  },
  "summary": {
    "member_count": 10,
    "relay_member_count": 8,
    "today_actual_cost": 12.34,
    "last_30d_actual_cost": 123.45,
    "unit_label": "USD"
  },
  "top_members": [
    {
      "rank": 1,
      "user_id": 101,
      "display_name": "Alice",
      "email": "alice@example.com",
      "department_display_path": "Department Alpha",
      "today_actual_cost": 1.23,
      "last_30d_actual_cost": 12.3,
      "total_tokens": null
    }
  ],
  "members": [
    {
      "user_id": 101,
      "display_name": "Alice",
      "email": "alice@example.com",
      "department_display_path": "Department Alpha",
      "relay_user_id": 1001,
      "today_actual_cost": 1.23,
      "last_30d_actual_cost": 12.3,
      "subscription_count": 2,
      "selectable": true
    }
  ]
}
```

Team Overview must not include `group_quotas`, subscription quota rows, or multiplier edit actions.

If the current user has no representative scope, return `is_representative: false` with empty rows. This lets the frontend hide or empty the Team Overview page without turning ordinary users into error states.

### Audit

```text
GET /api/v1/user/team-usage/audit?page=1&page_size=20&target_user_id=101
```

Representatives see only audit records for actions they performed.

Admin-wide audit:

```text
GET /api/v1/admin/team-usage/audit?page=1&page_size=50&actor_user_id=100&target_user_id=101&status=succeeded
```

Admins can see all delegated multiplier audit records. The admin response should include actor, target, provider, group, action, status, before/after multipliers, before/after effective limits, reason, redacted error message, and timestamps.

## Relay Provider Extensions

Keep `relay.Provider` stable for core capabilities where possible. Add optional extension interfaces for representative subject usage, Team Overview, and multiplier management.

```go
type SubjectUsageDashboardProvider interface {
    GetUsageDashboardForUser(ctx context.Context, relayUserID int64, params UserUsageDashboardParams) (*UserUsageDashboardResponse, error)
}

type TeamUsageSummaryProvider interface {
    GetBatchUserUsageStats(ctx context.Context, userIDs []int64, params TeamUsageSummaryParams) (map[int64]TeamUserUsageStats, error)
}

type UserSubscriptionLister interface {
    ListUserSubscriptions(ctx context.Context, relayUserID int64) ([]UserSubscription, error)
}

type GroupRateMultiplierManager interface {
    ListGroupRateMultipliers(ctx context.Context, groupID int64) ([]UserGroupRateEntry, error)
    ReplaceGroupRateMultipliers(ctx context.Context, groupID int64, entries []GroupRateMultiplierInput) error
}
```

The current code already uses a local optional `ListUserSubscriptions` shape in the personal usage quota path. Implementation should formalize that optional interface in `backend/internal/relay/provider.go` instead of duplicating anonymous interfaces across handlers.

`relay.Group` must include the fields selected-member quota controls need:

1. `rate_multiplier`
2. `daily_limit_usd`
3. `weekly_limit_usd`
4. `monthly_limit_usd`
5. `subscription_type`

`sub2apiRelay` implementation should use:

1. `GET /api/v1/admin/usage/stats?user_id=...` for selected-member stats.
2. `GET /api/v1/admin/dashboard/trend?user_id=...` for selected-member token trend.
3. `GET /api/v1/admin/dashboard/models?user_id=...` for selected-member model distribution.
4. `POST /api/v1/admin/dashboard/users-usage` for Team Overview today and rolling 30-day member summary.
5. `GET /api/v1/admin/users/:id/subscriptions` for selected-member subscription groups.
6. `GET /api/v1/admin/groups/:id/rate-multipliers` for current user-specific group multipliers.
7. `PUT /api/v1/admin/groups/:id/rate-multipliers` for merged whole-group writes.

Team Overview intentionally does not use team aggregate trend or team model distribution in the first version. The personal Token Trend slot is replaced by a top-12 member ranking, and the personal Model Distribution slot is replaced by a member usage table. A future version can add sub2api multi-user aggregate endpoints if true team-level charts become required.

## Rate Multiplier Write Flow

The write flow must preserve other users' multipliers as much as the current sub2api API allows.

```text
1. Authenticate current AE user.
2. Resolve representative scope.
3. Verify target AE user is in scope.
4. Verify target user has relay_user_id.
5. Resolve primary provider.
6. Verify target relay user has active existing subscription for group_id.
7. Fetch group metadata and current rate multipliers.
8. Compute old effective multiplier and old effective allowances.
9. Validate requested set/reset against delegated policy.
10. Insert AE audit row with status=running.
11. Acquire AE-side provider/group write lock.
12. Re-fetch current group rate multipliers.
13. Build merged rate list:
    - include every non-target entry with non-null rate_multiplier
    - for mode=set, include target entry with requested multiplier
    - for mode=reset, omit target entry
14. PUT merged entries to sub2api.
15. Re-read group rate multipliers.
16. Verify target entry matches requested state.
17. Update audit row to succeeded with before/after values.
18. Return updated state.
```

The AE-side write lock should use a database-backed advisory lock or equivalent process-safe mechanism keyed by `(provider_id, group_id)`. This serializes AE-originated representative writes. It cannot prevent direct concurrent writes from sub2api admin UI. If direct sub2api concurrent writes become a real operational issue, sub2api needs a versioned patch endpoint; AE must not pretend the current whole-group PUT API provides compare-and-swap semantics.

If the relay write fails after audit insert, update audit status to `failed` with a redacted error. If the relay write succeeds but readback verification fails, update audit status to `partial_failed` and return `502` with a generic message.

## Audit Data Model

Add `team_usage_rate_multiplier_audits`.

Fields:

- `id`
- `actor_user_id`
- `target_user_id`
- `provider_id`
- `relay_user_id`
- `group_id`
- `group_name`
- `action`: enum `set_rate_multiplier`, `reset_rate_multiplier`
- `status`: enum `running`, `succeeded`, `failed`, `partial_failed`
- `old_multiplier`
- `old_multiplier_source`: enum `user`, `group`, `system`, `unknown`
- `new_multiplier`
- `new_multiplier_source`: enum `user`, `group`, `system`, `unknown`
- `old_effective_limits`: JSON object with daily/weekly/monthly values
- `new_effective_limits`: JSON object with daily/weekly/monthly values
- `scope_evidence`: JSON object containing represented department ids and target member department id
- `reason`
- `error_message`
- `created_at`
- `updated_at`

Audit records must not store API keys, relay auth passwords, bearer tokens, raw request logs, or full directory member metadata.

Indexes:

- `(actor_user_id, created_at)`
- `(target_user_id, created_at)`
- `(provider_id, group_id, created_at)`
- `(status, created_at)`

## Frontend UX

Use the existing AI Usage Center shell and components for per-subject usage. Add a separate Team Overview page for team-level comparison.

Top-level structure:

```text
AI Usage Center
  Subject selector
    - My Usage
    - Alice
    - Bob
  Personal usage cards
  Token Trend
  Model Distribution
  Selected-subject subscription quota / multiplier controls

Team Overview
  Summary cards
  Top 12 member usage ranking
  Member usage table
  Link/action to open member in AI Usage Center

Audit History
  Representative's multiplier actions
```

AI Usage Center subject selector:

1. Always includes `My Usage`.
2. For representatives, includes scoped members from the represented department subtree.
3. Selecting a member updates the entire AI Usage Center snapshot to that member's usage.
4. Selected member snapshots reuse the existing personal layout where possible:
   - stats cards
   - Token Trend
   - Model Distribution
   - subscription quota cards or rows
5. Rate multiplier controls appear only in selected-member subscription quota context.

Team Overview first screen:

1. Summary cards:
   - scoped members
   - relay-enabled members
   - today actual cost
   - rolling 30-day actual cost
2. Top 12 member usage ranking:
   - replaces the personal Token Trend slot
   - ranks scoped members by rolling 30-day actual cost
   - shows today actual cost as secondary context
   - shows token totals when the relay can provide them without per-member log scans
3. Member usage table:
   - replaces the personal Model Distribution slot
   - name
   - email
   - department
   - today actual cost
   - rolling 30-day actual cost
   - subscription count
   - status
   - action to open that member in AI Usage Center

Team Overview must not render:

1. quota cards
2. subscription quota rows
3. multiplier controls
4. raw request logs

Rate multiplier modal:

1. Shows current state and group default.
2. Offers `Set explicit multiplier` and `Reset to inherited default`.
3. Shows before/after effective daily, weekly, monthly allowance.
4. Rejects values outside delegated policy before submit.
5. Requires an optional short reason field. Empty reason is allowed but still audited.

Empty states:

1. No representative scope: hide member subjects and Team Overview, or show a compact "No delegated team scope" state.
2. Scope exists but no matched relay users: Team Overview shows unavailable member states; AI Usage Center subject selector keeps `My Usage` only.
3. Provider unsupported: Team Overview shows "Team Overview is temporarily unavailable"; selected-member usage shows a scoped unavailable state.
4. Member has no active subscriptions: selected-member AI Usage can show usage charts but no quota controls.

## Error Handling

1. Scope resolution errors fail closed and do not return partial unauthorized rows.
2. Directory Sync missing current source returns no representative scope.
3. Relay provider missing or unsupported returns `configured=false` or `unavailable` state instead of exposing internal details.
4. Target user mismatches return generic scoped errors to avoid leaking whether an out-of-scope user exists.
5. Rate write failures create or update audit records.
6. UI refreshes member detail after a successful write to avoid stale effective allowances.

## Performance and Limits

1. Default member page size is `20`; maximum `100`.
2. Representative scope resolution may cache department subtree ids per request, not globally.
3. Batch usage lookup should cap one request to `100` relay user ids. Larger scopes page through members.
4. The dashboard summary must compute totals for the full representative scope, not only the current member page. The backend may chunk relay calls by `100` relay user ids and should cap one full-scope summary at `500` relay users. If the scope is larger, return a summary-unavailable state while still allowing paginated member browsing.
5. Avoid per-member trend requests on Team Overview. Member trend is fetched only after a representative selects a specific member in AI Usage Center.

## Backend Implementation Notes

Suggested module split:

1. `backend/internal/representativescope`
   - Resolve current user's represented departments and allowed users.
   - Own metadata parsing for `representative_external_ids` and `leader_department_ids`.
2. `backend/internal/handler/team_usage.go`
   - Own HTTP endpoints.
   - Keep handlers thin; delegate scope and relay operations.
3. `backend/internal/teamusage`
   - Build subject usage snapshots, Team Overview summaries, subscription rows, effective allowance calculations, and write orchestration.
4. `backend/internal/relay`
   - Add optional interfaces and sub2api implementations.

The representative metadata parsing currently exists inside admin users handler helpers. Implementation should extract reusable logic instead of copying private handler helpers into representative usage handlers.

## Frontend Implementation Notes

Suggested components:

1. `UserUsageDashboard.vue`
   - Add subject-aware rendering while preserving current `My Usage` behavior.
   - Keep personal dashboard behavior unchanged.
2. `UserUsageSubjectSelector.vue`
   - Lists `My Usage` and scoped members.
3. `TeamOverviewPage.vue`
   - Owns independent team summary, top-12 ranking, and member usage table.
4. `TeamOverviewMemberTable.vue`
   - Owns sorting, pagination, and open-member action.
5. `SelectedSubjectSubscriptionRows.vue`
   - Render selected member group rows and effective allowance cells in AI Usage Center.
6. `TeamRateMultiplierModal.vue`
   - Own set/reset workflow and before/after calculation preview.
7. `TeamUsageAuditList.vue`
   - Shows representative's own actions.
8. Admin audit table
   - Shows all delegated multiplier actions for admins, either as a focused admin route or as a tab in the existing admin users area.

Team Overview is an independent page. It may be linked from AI Usage Center for representatives, but it must not replace the personal usage dashboard.

## Testing

Backend unit tests:

1. Representative scope resolution:
   - representative via department metadata
   - representative via member metadata
   - subtree includes child departments
   - unmatched user is excluded from manageable rows
   - no current directory source fails closed
2. Representative usage handlers:
   - non-representative gets empty scope
   - representative sees only subtree members
   - subject list includes `My Usage`
   - selected-member dashboard rejects out-of-scope users
   - selected-member dashboard uses relay admin aggregate APIs, not target member credentials
   - Team Overview returns top-12 member ranking
   - Team Overview response never includes `group_quotas`
   - out-of-scope target update is rejected
   - target without relay mapping is rejected
   - target without active subscription is rejected
   - unsupported provider returns unavailable state
3. Effective allowance:
   - inherited multiplier
   - explicit user multiplier
   - missing period quota
   - multiplier policy bounds
   - reset clears explicit multiplier
4. sub2api relay adapter:
   - list group rate multipliers decodes nullable fields
   - merged write preserves non-target rate entries
   - reset omits target rate entry
   - PUT errors are surfaced without logging secrets
5. Audit:
   - running row created before relay write
   - success row stores before/after values
   - failure row stores redacted error

Frontend tests:

1. AI Usage Center renders a subject selector with `My Usage`.
2. Representative subject selector includes scoped members.
3. Selecting a member reloads the AI Usage Center snapshot for that member.
4. Team Overview is reachable as an independent page for representatives.
5. Team Overview renders top-12 member usage ranking instead of personal Token Trend.
6. Team Overview renders member usage table instead of personal Model Distribution.
7. Team Overview does not render quota cards or multiplier controls.
8. Selected-member AI Usage renders subscription controls when the member has active subscriptions.
9. Non-representative users do not see member subjects or Team Overview entry points.
10. Member table open action switches to that member in AI Usage Center.
11. Effective allowance preview updates immediately when multiplier changes.
12. Invalid multiplier disables submit.
13. Reset mode displays inherited group default result.
14. Successful write refreshes selected-member usage and audit list.

Manual verification:

1. Confirm personal My Usage remains unchanged.
2. Confirm representative can switch to a scoped member and see stats, trend, and model distribution.
3. Confirm Team Overview shows ranking and member table, with no quota UI.
4. Confirm representative cannot access an out-of-scope user by URL.
5. Confirm changing multiplier in AE is visible in sub2api group rate multiplier admin view.
6. Confirm sub2api subscription usage consumption changes according to `ActualCost` after multiplier change.

## Rollout

1. Land backend schema migration and handler contracts behind the existing authenticated user route group.
2. Add relay optional interfaces and sub2api implementation.
3. Add AI Usage Center subject selector and selected-member read-only usage.
4. Add independent Team Overview page with ranking and member table.
5. Add multiplier edit workflow and audit list in selected-member usage context.
6. Update `docs/architecture.md`.
7. Validate in staging with synthetic directory members and groups.

The feature should fail closed if Directory Sync has no current source or if the primary provider does not support the required sub2api admin APIs.

## Open Follow-Ups Outside First Version

1. sub2api versioned patch API for single user group rate multiplier updates.
2. True per-user subscription quota overrides in sub2api.
3. Multi-provider representative usage and Team Overview.
4. True team-wide trend and model distribution backed by multi-user aggregate upstream APIs.
5. Admin-configurable delegated multiplier policies in UI.
6. Batch edits with approval workflow.
