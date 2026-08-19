# Personal Usage Reset And OAuth Pool Contract

Status: current implementation contract for issues #316 and #317.

## Scope

The authenticated personal `/usage` page exposes two separate reset concepts:

1. The selected user's subscription reset, shown on the matching group quota row.
2. The next reset for the active OAuth account pool behind that access group.

The page never exposes account identifiers, names, emails, raw credentials, or
raw provider snapshots. API-key quota rows do not claim a subscription reset.

## Selected Subscription Window

The dashboard range selects the subscription window as follows:

| Dashboard selection | Subscription window |
| --- | --- |
| `today` or hourly view | daily |
| `7d` | weekly |
| `30d` | monthly |

The Relay adapter reads `/api/v1/subscriptions/progress` with the current user's
session and maps `progress.daily/weekly/monthly.resets_at` to the quota row. If
the progress endpoint fails, the adapter falls back to the existing admin
subscription list so current `Used / Quota` remains available without a reset
timestamp. The browser shows the reset timestamp in the user's local timezone
and a relative countdown; an absent or invalid timestamp hides only the reset
row.

## OAuth Pool Projection

`GET /api/v1/user/usage/group-pool-usage` is served by the AI Efficiency backend
through the optional `relay.GroupOAuthPoolUsageReader` capability. The adapter:

- resolves the current user's effective access groups;
- requests `type=oauth` and the group filter from Sub2API, then applies the
  local `status == active` check so temporarily rate-limited active accounts
  are retained;
- requests one cached batch usage snapshot with `force=false`;
- keeps only valid `seven_day` snapshots and averages their utilization;
- reports valid OAuth accounts, active OAuth account total, latest snapshot time,
  and the nearest future `resets_at`.

The displayed value is **7-day rolling-window average utilization**. It is not
the current user's `Used / Quota`, and it is not asserted to equal
`sum(used) / sum(quota)`. A partial snapshot reports coverage. A group with no
valid snapshot is omitted. Pool errors return an `unavailable` section and never
change the personal usage or quota response.

## Frontend Boundary

Usage, group quota, and OAuth pool requests run independently and are protected
by generation-safe abort handling. Pool data is joined to visible quota cards by
`group_id`; member-scoped usage never calls the personal pool endpoint.
