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
- It adds a Team Overview view inside AI Usage Center for department-subtree usage ranking and member comparison. Team Overview is intentionally different from the personal AI Usage view and must not render quota cards or quota controls.
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
5. Add a Team Overview route under AI Usage Center for the represented department subtree.
6. In Team Overview, replace the personal Token Trend chart slot with a top-12 member usage trend chart.
7. In Team Overview, replace the personal Model Distribution chart with a member usage table.
8. Keep quotas and multiplier controls out of Team Overview.
9. Let representatives adjust a selected member's user-specific group rate multiplier for existing subscription groups from the selected-member AI Usage context.
10. Explain that a draft multiplier changes future quota consumption speed; it must not reprice historical Used / Quota values in the selected member's Quotas view before the representative confirms the write.
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
| Product placement | AI Usage Center subject selector plus Team Overview tab |
| AI Usage Center IA | `My Usage` / scoped member selector / Team Overview tab for representatives |
| Team Overview IA | `/usage/team` inside AI Usage Center |
| Team Overview Token Trend replacement | Top-12 member usage trend chart |
| Team Overview Model Distribution replacement | Member usage table |
| Team Overview quotas | Hidden; no quota cards or quota controls |
| Aggregation | Personal usage by selected subject; team overview by member |
| Provider scope | Primary provider only |
| Usage detail | Aggregated summaries only, no raw request log |
| Quota control | User-specific group rate multiplier |
| Quota display | `Used / Quota` displayed in sub2api enforcement dollars |
| Quota preview | Client-side explanation of future consumption speed before confirm |
| Self multiplier edit | Forbidden; only another in-scope ancestor representative can adjust a representative |
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
2. UI can display direct quota amounts instead of explaining multiplier-derived consumption speed.

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
6. Treat self multiplier changes as forbidden, even when the current user is also a representative. A higher-level representative can change that user's multiplier only from the higher-level representative's own session.

## sub2api Contract Facts

The design relies on these current sub2api behaviors:

1. Subscription-mode eligibility checks compare one `UserSubscription` usage record against the group's daily/weekly/monthly limits.
2. User platform quota is skipped in subscription mode.
3. Gateway billing selects multiplier by priority: user-specific group multiplier, then group default multiplier, then system default multiplier.
4. Usage logs and subscription consumption use `ActualCost`, so a higher multiplier consumes subscription quota faster and a lower multiplier consumes it slower.
5. `GET /api/v1/admin/groups/:id/rate-multipliers` returns user-specific group rate entries.
6. `PUT /api/v1/admin/groups/:id/rate-multipliers` replaces the rate-multiplier part for the whole group.
7. Entries omitted from the PUT payload have `rate_multiplier` cleared. Existing `rpm_override` values are not part of this feature and must not be changed by AE.

Important implication: AE must never PUT only the target member's entry. AE must read current entries, merge the target change, preserve every non-target entry that has a non-null `rate_multiplier` or `rpm_override`, preserve the target's existing `rpm_override` when changing only `rate_multiplier`, then PUT the full merged rate list.

## Multiplier Semantics

Selected-member AI Usage must describe the control as `Rate multiplier`, with quota impact shown as derived data.

For a subscription group, `rate_multiplier` changes the speed at which future requests consume subscription quota. It does not rewrite historical usage already accumulated in sub2api.

For display:

```text
effective_multiplier = user_specific_multiplier if present else group_default_multiplier if present else system_default_multiplier
display_daily_quota = raw_group_daily_limit_usd
display_weekly_quota = raw_group_weekly_limit_usd
display_monthly_quota = raw_group_monthly_limit_usd
display_daily_used = raw_daily_usage_usd
display_weekly_used = raw_weekly_usage_usd
display_monthly_used = raw_monthly_usage_usd
```

Selected-member Quotas must display `Used / Quota` in sub2api's enforcement basis:

```text
display_quota = raw_group_period_limit_usd
display_used = raw_period_usage_usd
```

Example: if the group monthly quota is `$500`, the member's effective multiplier is `2x`, and sub2api reports `$80` usage for the period, the selected-member Quotas row displays `$80 / $500`. Future requests consume the remaining quota at `2x` speed.

If a sub2api endpoint already returns display-ready usage or quota values, the relay adapter must mark that basis explicitly and AE must not divide those values by multiplier.

Display rules:

1. Missing group limit means that period is unlimited.
2. Effective multiplier `0` means future consumption does not advance by cost for that group. It must not reset historical used values. Representatives must not be allowed to set `0` in the first version.
3. If a member has no user-specific multiplier, display `Inherited` and show whether the inherited value comes from group default or system default.
4. Reset means clear the user-specific multiplier so the member inherits the group or system default. It does not write the inherited value as an explicit user multiplier.
5. Show clear explanatory copy before submitting: the multiplier affects future quota consumption speed; it is not a quota-limit edit and does not recalculate existing Used / Quota values.
6. While a representative edits a selected member's multiplier, the Quotas view must keep displayed Used / Quota stable. The backend is not called and no audit row is created until the representative confirms.
7. Canceling the edit discards the draft and restores the last persisted multiplier state.

Draft explanation:

```text
preview_multiplier = draft_rate_multiplier for mode=set
preview_multiplier = inherited_default_multiplier for mode=reset
future_consumption_speed = preview_multiplier
display_used = raw_period_usage_usd
display_quota = raw_group_period_limit_usd
```

If `raw_group_period_limit_usd` is missing, that period remains unlimited in display. If `preview_multiplier` is invalid or outside delegated policy, the UI may show the draft as invalid but must not submit it.

## Delegated Multiplier Policy

Delegated multiplier edits need a policy boundary because lowering a multiplier slows future quota consumption and effectively lets the member make more standard-cost requests before reaching the same sub2api limit.

First-version default policy:

1. Representatives may reset a member to inherit the default multiplier.
2. Representatives may set an explicit multiplier below, equal to, or above the inherited default multiplier.
3. Representatives may not set multiplier `0`.
4. Representatives may not set a negative multiplier.
5. Representatives may not set a multiplier above an AE server-side maximum, default `10`.
6. Representatives may not submit `NaN`, infinite values, or values with more than two decimal places.

This default lets upper-level representatives intentionally slow or accelerate a member's future quota consumption through sub2api's existing user-specific group rate multiplier, while still preserving a positive multiplier and a bounded maximum.

The backend should normalize accepted multiplier values to a stable decimal representation before comparison, audit, and relay write. Do not compare unrounded floating-point values directly when deciding whether a request is a no-op or policy violation.

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

Team Overview view
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
    - team member top-12 trend, ranking, and table
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
4. `frontend` owns AI Usage Center subject selection, the Team Overview route, selected-member subscription rows, and edit dialogs.
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

Team Overview member details must include every directory member in the represented subtree, including the representative themself and members without a matched AE user. Rows without an AE user use `directory_member_external_id` as their stable row identity, return `user_id: 0`, `selectable: false`, and keep the Open action disabled. Team Overview may resolve such rows to sub2api relay users by exact email match for read-only usage aggregation. Selected-member usage and quota management routes remain local-user operations and require a positive scoped `user_id` plus a relay mapping.

Security requirements:

1. Every representative usage endpoint must recompute or load the current representative scope server-side.
2. `target_user_id` must be validated against the resolved allowed local user ids.
3. `group_id` must be validated against the target relay user's active existing subscriptions.
4. The current representative cannot manage themself through delegated multiplier controls, even if they appear in a represented subtree. A representative may still view `My Usage`, but self quota controls are hidden or disabled.
5. An upper-level representative may manage another representative only when `actor_user_id != target_user_id` and every department represented by the target representative is inside a strict descendant subtree of at least one department represented by the actor. A peer representative in the same represented department is not considered upper-level for quota control.
6. Admin role alone is not used to broaden these `/user/team-usage/*` endpoints. Admin-wide views remain separate admin routes.

For requirement 5, determine the target's represented departments using the same resolver inputs as the actor: `department.metadata.representative_external_ids` and `member.metadata.leader_department_ids`. If the target has no represented departments, ordinary subtree membership is enough. If the target has represented departments and any of those roots are equal to or outside the actor's represented roots, the actor is not upper-level for quota control.

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

If the current user appears inside their own represented subtree, the subject list must de-duplicate that person into the synthetic `My Usage` subject. The UI must not render a second editable member row for the current user.

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
4. Selected member quota cards use the target user's active subscription groups and user-specific rate multiplier state. This is a selected-member personal usage view, not the Team Overview route.
5. If `subject.user_id` equals the current actor, quota controls must be read-only even if the same user is a representative. Self multiplier writes are rejected by the update endpoint as well.

For selected-member dashboards, `subject_subscription_groups` is the authoritative quota/control payload. `group_quotas` is a compatibility projection for existing quota-card components and must be derived from the same enforcement-basis display values. The frontend must not infer editability, multiplier state, or draft state from `group_quotas`.

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
  "system_default_multiplier": 1.0,
  "inherited_default_multiplier": 1.0,
  "user_multiplier": 2.0,
  "effective_multiplier": 2.0,
  "multiplier_source": "user",
  "daily_limit_usd": 10.0,
  "weekly_limit_usd": 50.0,
  "monthly_limit_usd": 200.0,
  "daily_effective_allowance_usd": 5.0,
  "weekly_effective_allowance_usd": 25.0,
  "monthly_effective_allowance_usd": 100.0,
  "daily_display_used_usd": 1.2,
  "weekly_display_used_usd": 5.1,
  "monthly_display_used_usd": 21.25,
  "daily_usage_usd": 2.4,
  "weekly_usage_usd": 10.2,
  "monthly_usage_usd": 42.5,
  "usage_value_basis": "raw_actual_cost",
  "quota_window_basis": "sub2api_enforcement_window",
  "editable": true,
  "editable_reason": null
}
```

The existing `daily_usage_usd`, `weekly_usage_usd`, and `monthly_usage_usd` fields remain raw relay usage values when the relay returns raw subscription usage. The UI must render `*_display_used_usd` and `*_effective_allowance_usd` for `Used / Quota`; those display fields must remain in sub2api enforcement units and must not be divided by the multiplier. If the relay returns display-ready values, `usage_value_basis` should identify that basis and the adapter must set raw and display fields consistently without applying multiplier division.

`quota_window_basis` must identify that daily, weekly, and monthly values are aligned to sub2api's enforcement windows. AE must not reinterpret these windows using the frontend's selected chart timezone.

`editable` must be `false` when the row belongs to the current actor. In that case `editable_reason` should be `self_edit_forbidden`. Other possible non-editable reasons include `no_relay_mapping`, `inactive_subscription`, `unsupported_provider`, and `policy_read_only`.

When `subject_subscription_groups` and `group_quotas` are both present, each `group_quotas.groups[*]` item for a subscription group must use the matching row's enforcement-basis display used/quota values for the selected period. This keeps existing quota cards visually consistent with the editable Quotas rows.

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

Reset to inherit default:

```json
{
  "mode": "reset",
  "reason": "Return to inherited default"
}
```

Request validation:

1. `mode` must be `set` or `reset`.
2. `rate_multiplier` is required for `set` and must be omitted or ignored for `reset`.
3. `reason` is optional, trimmed, and capped at `500` characters. It must be treated as plain text in audit UIs.
4. `rate_multiplier` must pass delegated policy after decimal normalization.

Response:

```json
{
  "status": "succeeded",
  "audit_id": 9001,
  "group_id": "42",
  "old_multiplier": 1.0,
  "old_multiplier_source": "group",
  "new_multiplier": 2.0,
  "new_multiplier_source": "user",
  "changed": true,
  "old_effective_monthly_allowance_usd": 200.0,
  "new_effective_monthly_allowance_usd": 100.0
}
```

If the requested normalized state already matches the current state, AE must not call sub2api. Return `changed=false`, keep the audit status `succeeded`, and include the current before/after values.

Validation failures:

1. `403` if the current user is not a representative for the target user.
2. `403` if `actor_user_id == target_user_id`, regardless of representative metadata.
3. `403` if the target user is a representative but is not in a strict descendant department subtree of the actor's represented departments.
4. `404` if the target user does not exist in the representative scope.
5. `409` if the target user has no relay user mapping.
6. `409` if the target user does not have an active subscription for the group.
7. `422` if the multiplier violates delegated policy.
8. `503` if the primary relay provider does not support the required rate-multiplier APIs.

### Team Overview

```text
GET /api/v1/user/team-usage/overview?start_date=...&end_date=...&granularity=day&timezone=...
```

Returns the independent team page data for the representative scope. Team Overview uses the same range model as the personal AI Usage view: Today, 7 Days, and 30 Days, defaulting to 30 Days. Summary cards, top-12 ranking, trend series, and the member table all follow the selected range. AE computes `range_actual_cost` and `range_total_tokens` from scoped member trend points for the selected window; it must not display sub2api historical `total_actual_cost` as if it were the selected-window value. The `members` table is the scoped directory member roster, not only the relay-backed usage roster: members without a matched AE user or resolvable relay user remain visible with zero selected-window billed usage, nullable usage-only fields, and `selectable=false`.

Some relay-adapter field names remain aligned with sub2api (`actual_cost`, `today_actual_cost`, `total_actual_cost`), but user-facing Team Overview labels use billed usage / 计费用量. Do not label these values as actual cost / 实际成本 in user interfaces because the value is multiplier-adjusted subscription consumption, not a direct finance cost.

First-version response:

```json
{
  "configured": true,
  "is_representative": true,
  "window": {
    "start_date": "2026-05-28",
    "end_date": "2026-06-26",
    "granularity": "day",
    "today": "2026-06-26",
    "rolling_days": 30,
    "timezone": "Asia/Shanghai"
  },
  "summary": {
    "unavailable": false,
	    "unavailable_reason": null,
	    "member_count": 10,
	    "relay_member_count": 8,
	    "range_actual_cost": 123.45,
	    "range_total_tokens": 456789,
	    "today_actual_cost": null,
	    "total_actual_cost": null,
	    "unit_label": "USD"
	  },
  "top_members": [
    {
      "rank": 1,
      "user_id": 101,
      "display_name": "Alice",
	      "email": "alice@example.com",
		      "department_external_id": "department-alpha",
		      "department_display_path": "Department Alpha",
	      "range_actual_cost": 12.3,
	      "today_actual_cost": 1.23,
	      "total_actual_cost": 12.3,
	      "total_tokens": null
    }
  ],
	  "top_member_trend": {
	    "unit_label": "USD",
	    "rank_basis": "range_actual_cost",
	    "unavailable": false,
    "unavailable_reason": null,
    "series": [
      {
        "user_id": 101,
        "display_name": "Alice",
        "rank": 1,
        "unavailable": false,
        "unavailable_reason": null,
        "points": [
          {
            "date": "2026-06-26",
            "actual_cost": 1.23,
            "total_tokens": null
          }
        ]
      }
    ]
  },
  "members": [
    {
      "user_id": 101,
      "display_name": "Alice",
	      "email": "alice@example.com",
		      "department_external_id": "department-alpha",
		      "department_display_path": "Department Alpha",
	      "relay_user_id": 1001,
	      "range_actual_cost": 12.3,
	      "today_actual_cost": 1.23,
	      "total_actual_cost": 12.3,
      "subscription_count": null,
      "selectable": true
    },
    {
      "user_id": 0,
      "directory_member_external_id": "member-bob",
      "display_name": "Bob",
      "email": "bob@example.org",
	      "department_external_id": "department-alpha",
	      "department_display_path": "Department Alpha",
	      "relay_user_id": 1002,
	      "range_actual_cost": 7.8,
	      "today_actual_cost": 0.5,
	      "total_actual_cost": 30.2,
	      "total_tokens": 12345,
	      "subscription_count": null,
	      "selectable": false
	    }
	  ],
	  "member_tree": [
	    {
	      "department_external_id": "department-alpha",
	      "parent_external_id": null,
	      "name": "Department Alpha",
	      "display_path": "Department Alpha",
	      "depth": 0,
	      "child_count": 1,
	      "member_count": 10,
	      "connected_member_count": 8,
	      "range_actual_cost": 123.45,
	      "range_total_tokens": 456789,
	      "members": [],
	      "children": []
	    }
	  ]
	}
```

Team Overview must not include `group_quotas`, subscription quota rows, or multiplier edit actions.

Top-12 trend rules:

1. `top_members` is ranked by selected-window billed usage (`range_actual_cost`) from the complete scoped relay-user set, where relay users can come from local `users.relay_user_id` reconciliation or exact sub2api email lookup for directory-only rows.
2. `top_member_trend.series` contains the same top members, in the same rank order, and renders as the Team Overview replacement for the personal Token Trend chart.
3. Each trend point uses billed usage in the requested range and granularity. Token totals are optional and may be `null` when the relay cannot provide them without raw log scans.
4. The trend series must be scoped before rendering. AE must not pass through a global sub2api user trend result that includes users outside the representative's department subtree.
5. If one top member's trend fetch fails, the response may include that member with `points: []` and an unavailable flag rather than failing the entire Team Overview page. Authorization failures still fail closed.
6. Top 12 must be computed from the complete scoped relay-user set. If the subtree is too large for the configured full-scope usage scan, AE must return empty `top_members`, `top_member_trend.unavailable=true`, and `top_member_trend.unavailable_reason=scope_too_large`; it must not compute top 12 from a truncated subset.
7. If the complete selected-window trend scan exceeds AE's backend budget or the relay provider returns a non-authorization transient error, AE returns the page with partial data instead of waiting for the frontend timeout. In that state `summary.unavailable=true`, `summary.unavailable_reason=provider_error`, selected-window aggregate totals are `null`, `top_members=[]`, and `top_member_trend.unavailable=true`. Authorization and scope errors still fail closed.

`member_tree` follows the current Directory Sync hierarchy. When a representative has multiple represented roots, the backend returns the largest non-overlapping roots first: if one represented root contains another represented root, only the ancestor appears as a top-level tree root and the child appears nested under it. Each department node aggregates direct members plus descendants for member count, connected member count, selected-window billed usage, and selected-window tokens. `members` remains as a compatibility flat list.

`subscription_count` is optional in the API and should be `null` unless the relay provider can return it without per-member subscription fan-out. The representative Team Overview table should not render a subscription-count column until the backend returns reliable batched subscription counts.

If the representative scope exceeds the configured full-scope summary cap, `summary.unavailable` must be `true`, `summary.unavailable_reason` must be `scope_too_large`, aggregate numeric totals should be `null`, and paginated `members` may still be returned. Do not return partial aggregate totals without an unavailable marker.

If the current user has no representative scope, return `is_representative: false` with empty rows. This lets the frontend hide the Team Overview entry point or render a compact empty direct-route state without turning ordinary users into error states.

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

Rejected multiplier write attempts must be audited locally when the request reached the write endpoint and the actor was authenticated. For out-of-scope targets, the actor-facing audit response must not include target details that the actor is not allowed to know; the admin-facing audit response may include redacted request metadata for investigation.

## Relay Provider Extensions

Keep `relay.Provider` stable for core capabilities where possible. Add optional extension interfaces for representative subject usage, Team Overview, and multiplier management.

```go
type SubjectUsageDashboardProvider interface {
    GetUsageDashboardForUser(ctx context.Context, relayUserID int64, params UserUsageDashboardParams) (*UserUsageDashboardResponse, error)
}

type TeamUsageSummaryProvider interface {
    GetBatchUserUsageStats(ctx context.Context, userIDs []int64, params TeamUsageSummaryParams) (map[int64]TeamUserUsageStats, error)
}

type TeamMemberTrendProvider interface {
    GetUsageTrendForUsers(ctx context.Context, relayUserIDs []int64, params TeamMemberTrendParams) (map[int64][]UsageTrendPoint, error)
}

type UserDirectoryProvider interface {
    ListUsers(ctx context.Context) ([]User, error)
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

`relay.UserGroupRateEntry` and `relay.GroupRateMultiplierInput` must model both `rate_multiplier` and `rpm_override` as nullable values. AE only changes `rate_multiplier`, but the relay adapter must preserve `rpm_override` values during whole-group replacement writes.

`sub2apiRelay` implementation should use:

1. `GET /api/v1/admin/usage/stats?user_id=...` for selected-member stats.
2. `GET /api/v1/admin/dashboard/trend?user_id=...` for selected-member token trend.
3. `GET /api/v1/admin/dashboard/models?user_id=...` for selected-member model distribution.
4. `POST /api/v1/admin/dashboard/users-usage` for Team Overview today and total member summary (`data.stats` map keyed by user id).
5. `GET /api/v1/admin/users?page=...&page_size=200` once per Team Overview request to build a relay user directory for cached `relay_user_id` validation and email-based directory-only member resolution. Team Overview must not validate cached relay bindings with a per-member `GET /api/v1/admin/users/:id` fan-out.
6. `GET /api/v1/admin/dashboard/trend?user_id=...` bounded fan-out for Team Overview selected-window ranking and trend data, capped by the full-scope relay-user cap.
7. `GET /api/v1/admin/users/:id/subscriptions` for selected-member subscription groups.
8. `GET /api/v1/admin/groups/:id/rate-multipliers` for current user-specific group multipliers.
9. `PUT /api/v1/admin/groups/:id/rate-multipliers` for merged whole-group writes.

Team Overview intentionally does not use a global unscoped sub2api user-trend response in the first version. AE fetches selected-window trend points only for relay users inside the representative's allowed scope, then computes `range_actual_cost`, token totals, ranking, summary, and the top-12 series from that scoped set. Before usage aggregation, AE may fetch the relay user directory through `UserDirectoryProvider` to validate cached bindings and repair stale local `relay_user_id` values without an N+1 lookup. The personal Token Trend slot is replaced by this top-12 billing trend chart, and the personal Model Distribution slot is replaced by a member details table. A future version can add a scoped multi-user upstream endpoint to remove the bounded fan-out.

## Rate Multiplier Write Flow

The write flow must preserve other users' multipliers as much as the current sub2api API allows.

```text
1. Authenticate current AE user.
2. Insert AE audit row with status=running and request metadata that is safe for the actor's current authorization state.
3. Resolve representative scope.
4. Reject and mark audit status=rejected if `actor_user_id == target_user_id`.
5. Verify target AE user is in scope; reject and mark audit status=rejected with redacted target evidence if not.
6. If target is also a representative, verify the actor is an upper-level ancestor representative; reject and mark audit status=rejected otherwise.
7. Verify target user has relay_user_id.
8. Resolve primary provider.
9. Verify target relay user has active existing subscription for group_id.
10. Fetch group metadata and current rate multipliers.
11. Compute old effective multiplier and existing quota display values.
12. Validate requested set/reset against delegated policy; reject and mark audit status=rejected on policy denial.
13. If the requested normalized state equals the current state, update audit status=succeeded with `changed=false` and return without calling sub2api.
14. Acquire AE-side provider/group write lock.
15. Re-fetch current group rate multipliers.
16. Re-check no-op and policy against the freshly fetched multiplier state.
17. Build merged rate list:
    - include every non-target entry with non-null rate_multiplier or non-null rpm_override
    - for mode=set, include target entry with requested multiplier and existing rpm_override if any
    - for mode=reset, omit target entry only if it has no rpm_override; otherwise include target entry with rate_multiplier cleared and rpm_override preserved
18. PUT merged entries to sub2api.
19. Re-read group rate multipliers.
20. Verify target entry matches requested state.
21. Update audit row to succeeded with before/after values and `changed=true`.
22. Return updated state.
```

The AE-side write lock should use a database-backed advisory lock or equivalent process-safe mechanism keyed by `(provider_id, group_id)`. This serializes AE-originated representative writes. It cannot prevent direct concurrent writes from sub2api admin UI. If direct sub2api concurrent writes become a real operational issue, sub2api needs a versioned patch endpoint; AE must not pretend the current whole-group PUT API provides compare-and-swap semantics.

If validation fails after audit insert, update audit status to `rejected` with a safe `rejection_reason`. If the relay write fails, update audit status to `failed` with a redacted error. If the relay write succeeds but readback verification fails, update audit status to `partial_failed` and return `502` with a generic message.

## Audit Data Model

Add `team_usage_rate_multiplier_audits`.

Fields:

- `id`
- `actor_user_id`
- `target_user_id`: nullable for rejected requests where storing actor-visible target details would leak scope information
- `provider_id`
- `relay_user_id`
- `group_id`
- `group_name`
- `action`: enum `set_rate_multiplier`, `reset_rate_multiplier`
- `status`: enum `running`, `succeeded`, `failed`, `partial_failed`, `rejected`
- `old_multiplier`
- `old_multiplier_source`: enum `user`, `group`, `system`, `unknown`
- `new_multiplier`
- `new_multiplier_source`: enum `user`, `group`, `system`, `unknown`
- `changed`
- `old_effective_limits`: JSON object with daily/weekly/monthly values
- `new_effective_limits`: JSON object with daily/weekly/monthly values
- `scope_evidence`: JSON object containing represented department ids and target member department id
- `rejection_reason`: enum `not_representative`, `self_edit_forbidden`, `not_upper_level_representative`, `out_of_scope`, `no_relay_mapping`, `inactive_subscription`, `policy_denied`, `provider_unsupported`
- `request_metadata`: JSON object for admin-only diagnostics, with redacted requested target/group ids when the actor is not allowed to see the target
- `reason`
- `error_message`
- `created_at`
- `updated_at`

Audit records must not store API keys, relay auth passwords, bearer tokens, raw request logs, or full directory member metadata.

Representative-facing audit responses must redact `request_metadata`, `target_user_id`, target display name, and target email for `rejection_reason=out_of_scope`. Admin audit responses may include those fields when needed for investigation.

Indexes:

- `(actor_user_id, created_at)`
- `(target_user_id, created_at)`
- `(provider_id, group_id, created_at)`
- `(status, created_at)`

## Frontend UX

Use the existing AI Usage Center shell and components for per-subject usage. Add Team Overview as a separate route under AI Usage Center for team-level comparison.

Canonical frontend routes:

1. `/usage`: AI Usage Center, defaulting to `My Usage`.
2. `/usage/members/:user_id`: independent selected-member usage detail for one scoped member.
3. `/usage/team`: Team Overview inside AI Usage Center.
4. `/` redirects to `/usage`.
5. Do not support `/?subject_user_id=...` as a canonical or compatibility route.

Top-level structure:

```text
AI Usage Center
  Top-level tabs
    - My Usage
    - Team Overview, visible only for representatives
  Subject selector
    - My Usage
    - Alice
    - Bob
  Personal usage cards
  Token Trend
  Model Distribution
  Selected-subject subscription quota / multiplier controls

Selected Member Usage Detail
  Explicit back action to Team Overview
  Member identity header
  Personal usage cards for that member
  Selected-subject subscription quota / multiplier controls
  Quotas
  Token Trend
  Model Distribution

AI Usage Center / Team Overview
  Summary cards
  Top 12 billing trend chart
  Member details table
  Link/action to open member in AI Usage Center
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
6. For `My Usage`, quota cards remain read-only. If the current user is also a representative, the UI still must not show self multiplier edit controls.

Selected Member Usage Detail:

1. `/usage/members/:user_id` is a focused detail page, not another copy of the full AI Usage Center switcher.
2. It must not render the `My Usage` / `Team Overview` top-level tabs.
3. It must not render the member subject selector.
4. It must load the route `user_id` directly through `GET /api/v1/user/team-usage/subjects/:user_id/usage/dashboard`; the frontend must not fall back to `My Usage` when the target is missing, out of scope, or not selectable.
5. The backend remains the authorization boundary for the route `user_id`. Out-of-scope targets return a generic not-found style error and must not leak whether the user exists.
6. The top of the page must show an explicit `Back to Team Overview` action linking to `/usage/team`. Do not place the Team Overview return action in the same control group as time range filters.
7. Do not render an AI Usage Center breadcrumb on this focused detail page.
8. The header must align with the personal usage header structure: one main `Member Usage` title, no repeated small eyebrow label, and a subtitle carrying member email plus department metadata.
9. `subject_subscription_groups` must render above the selected-member quota cards. Representatives should see the member's subscription groups and multiplier state before the derived quota projection.

Team Overview first screen:

1. Summary cards:
   - scoped members
   - relay-enabled members
   - billed usage in the currently selected range
   - token usage in the currently selected range
2. Range selector:
   - Today
   - 7 Days
   - 30 Days
   - the selector controls summary cards, top-12 ranking, trend points, and member table values
   - selecting a new range updates the selected button immediately, keeps the previous data visible, shows an updating indicator, disables range buttons during the request, and marks the content region busy
3. Top 12 billing trend chart:
   - replaces the personal Token Trend slot
   - selects the top 12 scoped members by selected-window billed usage
   - renders one trend series per selected member over the chosen date range
   - keeps legend/order in rank order
   - shows selected-window billed usage as hover or side-list context
   - shows token totals when the relay can provide them without raw log scans
4. Member details table:
   - replaces the personal Model Distribution slot
   - renders as a collapsible organization tree using the same current Directory Sync hierarchy pattern as the admin users department view
   - supports multiple represented teams by showing the largest non-overlapping represented department roots and nesting child teams underneath them
   - includes every scoped directory member returned by representative scope, even when that member has no local AE user, no relay binding, or no selected-window usage
   - department rows show department name/display path, subtree member count, connected member count, selected-window billed usage, and selected-window token usage
   - name
   - email
   - selected-window billed usage
   - selected-window token usage
   - red not-connected status for members without a resolved relay user
   - action to open `/usage/members/:user_id` only when `selectable=true` and `user_id > 0`; otherwise the action is disabled

Team Overview must not render:

1. quota cards
2. subscription quota rows
3. multiplier controls
4. raw request logs

Rate multiplier modal:

1. Shows current state and inherited default, including whether the inherited source is group or system.
2. Offers `Set explicit multiplier` and `Reset to inherited default`.
3. Shows before/after effective daily, weekly, monthly allowance.
4. Rejects values outside delegated policy before submit.
5. Requires an optional short reason field. Empty reason is allowed but still audited.

Selected-member Quotas preview:

1. Opening the multiplier editor creates a client-side draft keyed by `(subjectUserId, groupId)`.
2. Changing the draft multiplier leaves that group's displayed daily, weekly, and monthly `Used / Quota` values stable in the selected-member Quotas area.
3. The editor explains the derived effect instead of repricing persisted values: future requests consume the same quota at the draft multiplier speed.
4. The editor must say that this is not changing the member's quota limit and does not recalculate existing Used / Quota values.
5. Confirm submits the write endpoint, clears the draft, refreshes selected-member usage, and refreshes audit history.
6. Cancel or closing the editor clears the draft and restores persisted values.

Empty states:

1. No representative scope: hide member subjects and the Team Overview tab; direct `/usage/team` may show a compact "No delegated team scope" state.
2. Scope exists but no matched relay users: Team Overview shows unavailable member states; AI Usage Center subject selector keeps `My Usage` only.
3. Provider unsupported: Team Overview shows "Team Overview is temporarily unavailable"; selected-member usage shows a scoped unavailable state.
4. Member has no active subscriptions: selected-member AI Usage can show usage charts but no quota controls.
5. Direct member route target not in scope: show the normal usage unavailable state from the target dashboard request; do not show the actor's own usage as a fallback.

## Error Handling

1. Scope resolution errors fail closed and do not return partial unauthorized rows.
2. Directory Sync missing current source returns no representative scope.
3. Relay provider missing or unsupported returns `configured=false` or `unavailable` state instead of exposing internal details.
4. Target user mismatches return generic scoped errors to avoid leaking whether an out-of-scope user exists.
5. Rate write failures and rejected write attempts create or update audit records.
6. UI refreshes member detail after a successful write to avoid stale multiplier state.
7. Self multiplier write attempts return `403`, create a local audit row with `status=rejected` and `rejection_reason=self_edit_forbidden`, and must not call the relay provider.
8. Quota period windows and reset boundaries must follow sub2api enforcement semantics. AE may display the user's selected timezone for chart ranges, but daily/weekly/monthly subscription quota windows must not be recalculated from frontend timezone if sub2api uses a different reset window.

## Performance and Limits

1. Default member page size is `20`; maximum `100`.
2. Representative scope resolution may cache department subtree ids per request, not globally.
3. Batch usage lookup should cap one request to `100` relay user ids. Larger scopes page through members.
4. The dashboard summary must compute totals for the full representative scope, not only the current member page. The backend may chunk relay calls by `100` relay user ids and should cap one full-scope summary at `500` relay users. If the scope is larger, return a summary-unavailable state while still allowing paginated member browsing.
5. Team Overview selected-window ranking uses trend fan-out for the full scoped relay-user set, capped by the configured full-scope limit. Do not rank from a truncated subset. The sub2api relay adapter may fetch per-user trend rows with bounded concurrency, but it must preserve the complete-scope ranking contract.
6. Team Overview top-12 ranking and trend must be based on a complete selected-window full-scope usage scan. If the scoped relay-user count exceeds the configured full-scope cap, return unavailable state for top members and top-member trend instead of ranking a truncated subset. If the complete scan times out within AE's backend trend budget, return partial Team Overview data with explicit unavailable markers rather than letting the browser request time out.
7. Team Overview supports Today, 7 Days, and 30 Days. It defaults to 30 Days and should cap requested ranges to the same maximum as the existing personal AI Usage trend unless a later implementation plan explicitly raises that limit.

## Backend Implementation Notes

Suggested module split:

1. `backend/internal/representativescope`
   - Resolve current user's represented departments and allowed users.
   - Own metadata parsing for `representative_external_ids` and `leader_department_ids`.
2. `backend/internal/handler/team_usage.go`
   - Own HTTP endpoints.
   - Keep handlers thin; delegate scope and relay operations.
3. `backend/internal/teamusage`
   - Build subject usage snapshots, Team Overview summaries, subscription rows, quota display values, and write orchestration.
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
   - Owns independent team summary, top-12 member trend chart, and member usage table.
4. `TeamOverviewMemberTrendChart.vue`
   - Renders top-12 member trend series in rank order and handles empty or partially unavailable series.
5. `TeamOverviewMemberTable.vue`
   - Owns sorting, pagination, and open-member action.
6. `SelectedSubjectSubscriptionRows.vue`
   - Render selected member group rows, enforcement-basis Used / Quota cells, and multiplier explanation copy in AI Usage Center.
7. `TeamRateMultiplierModal.vue`
   - Own set/reset workflow, local draft state, and confirm/cancel behavior.
8. Admin audit table (follow-up)
   - Shows all delegated multiplier actions for admins, either as a focused admin route or as a tab in the existing admin users area.
   - Current representative AI Usage and Team Overview surfaces do not render audit history.

Team Overview is an independent route under AI Usage Center. It is linked from the AI Usage Center tab bar for representatives, but it must not replace the personal usage dashboard.

The selected-member route is intentionally more independent than `/usage`: it may reuse `UserUsageDashboard.vue` internals, but it should pass an explicit member-route mode that hides AI Usage Center tabs and subject switching while directly requesting the route target from the backend.

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
   - selected-member `group_quotas` projection uses the same normalized values as `subject_subscription_groups`
   - Team Overview returns top-12 member trend chart data
   - Team Overview top-member trend contains only scoped users
   - Team Overview top-member trend series order matches selected-window ranking order
   - Team Overview summary cards, member table, and top-12 ranking follow Today / 7 Days / 30 Days range selection
   - Team Overview member table includes scoped directory members without local user or relay usage and disables their open action
   - Team Overview summary and member tree include selected-window token totals
   - Team Overview member tree returns largest non-overlapping represented roots and nested departments
   - Team Overview returns top-member trend unavailable when full-scope ranking would require truncation
   - Team Overview response never includes `group_quotas`
   - out-of-scope target update is rejected
   - self target update is rejected with `403`
   - peer representative cannot update another representative in the same represented department
   - ancestor representative can update another representative when all target represented departments are in strict descendant subtrees
   - ancestor representative cannot update a target representative whose represented departments are only partially under the actor
   - target without relay mapping is rejected
   - target without active subscription is rejected
   - unsupported provider returns unavailable state
3. Quota display and multiplier semantics:
   - inherited multiplier
   - inheritance falls back from user-specific to group default to system default
   - explicit user multiplier
   - displayed `Used / Quota` stays in sub2api enforcement units
   - draft multiplier edits do not reprice historical Used / Quota values
   - multiplier help copy explains future quota consumption speed
   - quota windows follow sub2api enforcement windows
   - missing period quota
   - multiplier policy bounds
   - multiplier decimal precision and non-finite values are rejected
   - reset clears explicit multiplier
4. sub2api relay adapter:
   - list group rate multipliers decodes nullable fields
   - top-12 trend fan-out is capped and maps returned trend points to requested relay users
   - merged write preserves non-target rate and RPM entries
   - set preserves target RPM override while changing only target rate multiplier
   - reset omits target rate entry only when the target has no RPM override
   - PUT errors are surfaced without logging secrets
5. Audit:
   - running row created before relay write
   - success row stores before/after values
   - no-op writes store `changed=false` and do not call sub2api
   - failure row stores redacted error
   - rejected self-edit stores `status=rejected` and `rejection_reason=self_edit_forbidden`
   - rejected out-of-scope update does not leak unauthorized target details to the acting representative

Frontend tests:

1. AI Usage Center renders a subject selector with `My Usage`.
2. Representative subject selector includes scoped members.
3. Selecting a member reloads the AI Usage Center snapshot for that member.
4. Team Overview is reachable at `/usage/team` for representatives.
5. Team Overview renders top-12 billing trend chart instead of personal Token Trend.
6. Team Overview renders member details table instead of personal Model Distribution.
7. Team Overview does not render quota cards or multiplier controls.
8. Team Overview Today / 7 Days / 30 Days selection updates summary, ranking, chart, and table using the selected window.
9. Team Overview range switching shows a visible updating state while preserving previous data until the new response arrives.
10. Team Overview summary shows selected-window token usage.
11. Team Overview member details render as an expandable organization tree with department aggregate counts, billed usage, and token usage.
12. Team Overview marks members without a resolved relay user as not connected in red, with localized English and Chinese copy.
13. Selected-member AI Usage renders subscription controls when the member has active subscriptions.
14. Non-representative users do not see member subjects or Team Overview entry points.
15. Member table open action switches to `/usage/members/:user_id`.
16. Selected-member Quotas keep `Used / Quota` stable when draft multiplier changes.
17. Selected-member multiplier modal explains that the multiplier affects future quota consumption speed; it is not changing the member's quota limit and does not recalculate existing Used / Quota values.
18. Invalid multiplier disables submit.
19. Reset mode displays inherited default result and source.
20. Successful write refreshes selected-member usage. Audit history is written locally but not rendered in the representative UI.
21. Self rows do not show multiplier edit controls.
22. `/usage/members/:user_id` renders independently without top-level AI Usage Center tabs or the member subject selector.
23. `/usage/members/:user_id` calls the selected-member dashboard endpoint directly and does not fall back to personal usage when the target cannot be loaded.
24. `/usage/members/:user_id` renders an explicit `Back to Team Overview` link outside the range-control group.
25. Selected-member subscription groups render before selected-member quota cards.

Manual verification:

1. Confirm personal My Usage remains unchanged.
2. Confirm representative can switch to a scoped member and see stats, trend, and model distribution.
3. Confirm Team Overview shows top-12 billing trend chart and member details table, with no quota UI.
4. Confirm Team Overview Today / 7 Days / 30 Days updates summary, ranking, chart, and member table to the selected range.
5. Confirm representative cannot access an out-of-scope user by URL.
6. Confirm editing a multiplier leaves selected-member Used / Quota unchanged before confirmation and on cancel.
7. Confirm a `2x` multiplier shows explanatory copy that future requests consume quota at `2x` speed.
8. Confirm the representative cannot adjust their own multiplier.
9. Confirm a peer representative cannot adjust another representative in the same represented department.
10. Confirm an ancestor representative can adjust a lower-level representative's multiplier when the target is inside a strict descendant subtree.
11. Confirm changing multiplier in AE is visible in sub2api group rate multiplier admin view.
12. Confirm sub2api subscription usage consumption changes according to `ActualCost` after multiplier change.

## Rollout

1. Land backend schema migration and handler contracts behind the existing authenticated user route group.
2. Add relay optional interfaces and sub2api implementation.
3. Add AI Usage Center subject selector and selected-member read-only usage.
4. Add `/usage/team` Team Overview route with top-12 member trend chart and member table.
5. Add multiplier edit workflow in selected-member usage context; keep audit display out of representative UI.
6. Update `docs/architecture.md`.
7. Validate in staging with synthetic directory members and groups.
8. Follow up with a dedicated administrator audit UI that consumes `/api/v1/admin/team-usage/audit`.

The feature should fail closed if Directory Sync has no current source or if the primary provider does not support the required sub2api admin APIs.

## Open Follow-Ups Outside First Version

1. sub2api versioned patch API for single user group rate multiplier updates.
2. True per-user subscription quota overrides in sub2api.
3. Multi-provider representative usage and Team Overview.
4. Optimized scoped multi-user trend and team model distribution backed by upstream aggregate APIs.
5. Admin-configurable delegated multiplier policies in UI.
6. Batch edits with approval workflow.
