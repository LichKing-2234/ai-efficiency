# Account Quota Sweep

The Account Quota Sweep is the administrator-only 「额度巡检」 tab of the existing
`/admin/relay-accounts` page, whose navigation label is 「Relay 账号」. It
refreshes the quota facts of the selected Relay provider's OpenAI OAuth accounts
from Sub2API in one explicit, bounded asynchronous fan-out and shows each
account's last successful observation when one exists:

- 额度 — the 5h and 7d usage windows.
- 点数 — the Codex spendable credit balance.
- 重置券 — the banked ChatGPT/Codex reset credits and their expiries.

The tab is a display refresh, not an account mutation. It has no reset-credit
action, no automatic or scheduled trigger, and no history or trend view; an
administrator starts every sweep manually. Spending a reset credit remains the
gated [Owner Account Console](./owner-account-console.md) mutation and is not
reachable from this surface.

Read this contract before changing the `/admin/account-quotas` routes, the
`backend/internal/accountquota` service or worker, the `account_quota_sweeps`
and `account_quota_observations` tables, or the Relay accounts quota tab.
Cached read-model semantics and request deadlines follow
[Platform Loading](./platform-loading.md); the shared Relay provider boundary
follows [Relay User Access](./relay-user-access.md).

## Terms and authority

| Term | Meaning |
| --- | --- |
| Account Quota Sweep | One administrator-triggered asynchronous refresh of the quota facts of many accounts, visible as a durable job with counters, per-account results and a terminal status. It is never one of the two Owner Operations. |
| Account Quota Observation | The last successful quota facts recorded for one upstream account by a sweep, together with `observed_at`. It survives later sweeps and is never an authority for spending a reset credit. |
| Working set | The selected Relay provider's eligible OpenAI accounts, each joined for display with its cached directory snapshot and its last observation. |
| Directory snapshot | The relay runtime's cached owner-account projection for one provider, platform and configuration version. It supplies the fallback credits/reset-credit facts before an account has an observation. |
| Auto-reset account | An account whose Sub2API auto reset-credit worker switch (`auto_reset_credit_enabled`) is enabled. Refreshing it can make Sub2API's own worker spend a banked credit asynchronously. |

Sub2API remains the authority for every quota value. The platform adds only
sweep metadata (status, counters, timestamps, the closed sanitized reason code,
`observed_at`) and stores the returned facts without recomputation: it computes
no usage percentage, credit balance or window boundary of its own. The two usage
windows are identified by `window_seconds` (18000 for 5h, 604800 for 7d), not by
their position in the upstream payload.

The module adds no Relay capability. It reuses the existing
`relay.OwnerAccountDirectoryReader` directory and the
`relay.OpenAIQuotaManager.RefreshOpenAIQuota` quota refresh through
`relayruntime.Manager`, and it never calls the reset-credit consumption method of
that same capability.

## Admin surface

- `/admin/relay-accounts` is administrator-only. The page has two tabs:
  「账号配置」 (the existing comparison-and-copy working set) and 「额度巡检」
  (this contract).
- The quota tab issues no request while the selected platform is not `openai`;
  it renders a platform hint instead.
- The page keeps the existing Provider switcher. The quota tab is scoped to one
  Relay provider and one platform; there is no cross-provider or cross-platform
  sweep or aggregate. Changing the Provider resets the page to the 「账号配置」
  tab.
- The working set has no pagination. It is the provider's whole eligible OpenAI
  account pool, sorted by account name and then account id.
- The tab filters the loaded working set client-side: name/email search, account
  status, group, reset-credit bucket (`0`, `1`, `2`, `3+`, `unknown`), failed-only,
  and unobserved-only.
- Starting a sweep requires an explicit confirmation dialog. It shows the target
  count, the number of upstream quota refresh calls, and how many accounts in
  scope are auto-reset accounts that are skipped by default; including them is an
  explicit opt-in.
- While the latest sweep is `queued` or `running`, the tab polls
  `GET /admin/account-quotas/sweeps/latest` every two seconds, shows the
  `processed_count / total_count` progress, and disables starting another sweep.
  When the sweep reaches a terminal status the tab reloads the working set and
  shows a summary message.
- The tab exposes no reset-credit action, no account configuration write, no
  credential material, and no copy operation.

## Read model

`GET /api/v1/admin/account-quotas` returns the working set. It performs no quota
refresh call; the account directory comes from the relay runtime's owner-account
cache, which is loaded upstream on a miss or after invalidation.

Each item contains:

- `account` — the privacy-safe account identity: id, name, email, status,
  schedulable, shadow, `auto_reset_credit_enabled`, and bound groups
  (id/name/priority). Shadow accounts are excluded from the working set, so
  `shadow` is always false for a listed account. The projection carries no
  credential material.
- `snapshot` — the cached directory facts, or null when the directory reports
  neither credits, reset credits nor a usage timestamp. `observed_at` is the
  credit snapshot time when one is reported and the usage snapshot time
  otherwise, and is null when neither exists; `usage_updated_at` is the separate
  usage freshness marker; `credits` and `reset_credits` are the cached Codex
  balance and banked reset credits.
- `observation` — the last successful sweep observation, or null until a sweep
  observes the account. It carries `sweep_id`, `observed_at`, `plan_type`, the
  returned usage windows, credits and reset credits.

`latest_sweep` is the provider's newest sweep row, whatever its status or scope,
or null when the provider has never been swept. `total` is the number of items.

The observation is the durable display fact: one row exists per
`(provider_id, account_id)`, a later sweep overwrites only the accounts it
observes, and a single-account sweep therefore never hides the other accounts'
observations. A failed account outcome writes no observation, so the last
successful one remains visible. The cached directory snapshot is the fallback
before the first observation and may lag behind an observation; the tab prefers
`observation.credits` / `observation.reset_credits` over the snapshot values and
shows usage windows only from an observation.

## Eligibility and scope

An account is eligible when the directory reports it on `platform=openai` and its
`type` is `oauth` (case-insensitive, trimmed) and it is not a shadow account.
Every sweep target must be eligible.

- Empty or omitted `account_ids` starts an `all`-scope sweep over every eligible
  account. Accounts with `auto_reset_credit_enabled=true` are not called and are
  pre-recorded as `skipped` rows with `error_code = "auto-reset-enabled"`; they
  still count toward `total_count` and `processed_count`.
- `include_auto_reset=true` opts those accounts into the all-scope sweep and
  also permits them in an explicit selection.
- A non-empty `account_ids` starts a `selected`-scope sweep over the listed
  accounts only. Duplicate ids are collapsed, non-positive ids are rejected, and
  every id must resolve to an eligible account. An ineligible id, or an
  auto-reset account without `include_auto_reset`, rejects the whole request with
  `400 target-not-eligible`; a selection is never partially refreshed.
- A sweep has at most 200 target accounts. More targets are rejected with
  `400 invalid-request`.
- When the resolved target set is empty — for example every eligible account is
  an auto-reset account and `include_auto_reset` is not set — the sweep is
  rejected with `400 no-targets`.
- A provider has at most one `queued` or `running` sweep, enforced by a database
  partial unique index over `provider_id`; a competing start returns
  `409 sweep-in-flight`. This single-flight rule applies to both scopes.
- Two all-scope sweeps for one provider are separated by at least two minutes.
  The interval is measured from `created_at` of the provider's newest sweep row,
  whatever that row's scope was. A start inside the window returns
  `409 sweep-too-soon` with `retry_after_seconds`. Selected-scope sweeps skip the
  interval check.

## Sweep lifecycle

`POST /api/v1/admin/account-quotas/sweeps` validates the request, settles stale
sweeps, checks the single-flight and interval rules, resolves the target set, and
inserts one `queued` row with the pre-decided skip rows and counters. It returns
`202` with that sweep immediately; the fan-out runs detached from the request
context. The start path itself may load the owner-account directory upstream when
its cache is cold; only the per-account quota fan-out is detached.

The detached worker:

1. Marks the sweep `running` and records `started_at`.
2. Resolves the provider's long-running Relay client and requires the
   `relay.OpenAIQuotaManager` capability. A provider row load, resolution or
   capability failure ends the sweep as `failed` with a fixed sanitized
   `last_error` before any target is called; a worker panic also ends it as
   `failed`, without writing target outcomes.
3. Runs at most four account refreshes concurrently. Each target waits for a
   concurrency slot, then waits 0-400 ms of jitter before its call. Exactly one
   `POST /api/v1/admin/openai/accounts/:id/quota/refresh` upstream call is issued
   per target with a 35-second per-account deadline; there is no retry.
4. Records every target outcome and updates the counters after each outcome. One
   account's failure never aborts the sweep.
5. Writes the terminal `completed` status with the full result list. A sweep
   with per-account failures is still `completed`; `failed` means the worker never
   reached the end of the fan-out.
6. Invalidates the provider's cached `openai` owner-account directory, so the
   next read re-reads the directory that Sub2API updated during the refresh.

The whole fan-out runs under a ten-minute sweep deadline. A target that has not
started by then is recorded as `failed` with `timeout` ("the sweep timed out
before this account was refreshed"). A `queued` or `running` sweep whose
`created_at` is older than the twenty-minute stale threshold is settled as
`abandoned` with `last_error` "the sweep did not finish before the stale
threshold". Settlement is lazy: it runs before a list read, a sweep start, a
latest-sweep read, or a single-sweep read for that provider. A backend restart
before the worker finishes leaves the row `queued` or `running` until that rule
settles it; no sweep is resumed.

Counters are `total_count` (targets plus pre-decided skips), `processed_count`
(skips plus finished targets), `success_count`, `failed_count` and
`skipped_count`; `success_count + failed_count + skipped_count = total_count`
once the sweep is `completed`. A `completed` sweep carries one terminal
`results` entry for every target and skipped account; a `failed` sweep records
only the pre-seeded skip rows because the worker does not write partial target
outcomes. Results are written in completion order rather than account order, and
a non-terminal sweep carries only the pre-seeded skip rows.

## Reason codes

Per-account outcomes use the closed status set `succeeded`, `failed` and
`skipped`. A `failed` result carries one sanitized `error_code` and a fixed
message; no upstream error text is stored or returned.

| Outcome | Code | Meaning |
| --- | --- | --- |
| failed | `reauth-required` | The upstream rejected the account authorization (401/403) after the Relay refreshed the account token, so the account needs re-authorization. A wrong Relay admin key surfaces the same way for every account. |
| failed | `rate-limited` | The upstream rate limited the request (429). |
| failed | `upstream-unavailable` | Any other upstream HTTP status or transport failure. |
| failed | `timeout` | The 35-second per-account deadline, the sweep deadline before the target started, or a context cancellation. |
| failed | `capability-unsupported` | The Relay provider does not expose the OpenAI quota-refresh capability. |
| failed | `compliance-ack-required` | The Relay provider requires administrator compliance acknowledgement. |
| failed | `upstream-outcome-unknown` | The Relay provider could not determine whether the upstream call took effect. |
| skipped | `auto-reset-enabled` | The account's Sub2API auto reset-credit worker is enabled and the sweep did not opt in. |
| succeeded | empty | The observation was returned and stored. |

Sweep-level API failures use the platform envelope's `details.error_code`:

| HTTP | `error_code` | Meaning |
| --- | --- | --- |
| 400 | `invalid-request` | Missing/invalid `provider_id`, invalid JSON body, non-`openai` platform, non-positive account id, or more than 200 targets. |
| 400 | `no-targets` | No eligible account would be refreshed. |
| 400 | `target-not-eligible` | A selected id is not eligible, or an auto-reset account was selected without opt-in. |
| 404 | `provider-not-found` | The Relay provider row does not exist. |
| 404 | `sweep-not-found` | The sweep id does not exist. |
| 409 | `sweep-in-flight` | The provider already has a queued or running sweep. |
| 409 | `sweep-too-soon` | An all-scope sweep was requested inside the two-minute interval; `retry_after_seconds` carries the remaining whole seconds. |
| 423 | `compliance-ack-required` | The Relay provider requires compliance acknowledgement. |
| 501 | `capability-unsupported` | The Relay provider does not expose the owner-account directory capability. |
| 502 | `upstream-unavailable` | The Relay directory read failed or the provider is unavailable. |
| 500 | `internal-error` | Storage or an unexpected failure; the response carries no upstream or database text. |

A provider that exposes the directory capability but not the quota-refresh
capability accepts the sweep and records it as `failed`; the API-level
`501 capability-unsupported` is specific to the directory read.

## HTTP API

All four routes are administrator-gated under `/api/v1` and return the standard
envelope (`{"code": ..., "data": ...}` on success,
`{"code": ..., "message": ..., "details": {"error_code": ...}}` on failure).

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/v1/admin/account-quotas` | Working set plus latest sweep |
| POST | `/api/v1/admin/account-quotas/sweeps` | Start one async sweep, `202` |
| GET | `/api/v1/admin/account-quotas/sweeps/latest` | Poll the provider's newest sweep |
| GET | `/api/v1/admin/account-quotas/sweeps/:id` | Poll one sweep by id |

### `GET /admin/account-quotas`

Query: `provider_id` (required, positive integer), `platform` (optional;
defaults to `openai`; only `openai` is accepted). A missing or invalid
`provider_id`, or another platform, returns `400 invalid-request`. The provider
row is resolved; a missing provider returns `404 provider-not-found`.

```json
{
  "code": 200,
  "data": {
    "provider_id": 3,
    "platform": "openai",
    "total": 1,
    "items": [
      {
        "account": {
          "id": 101,
          "name": "alice@example.com",
          "email": "alice@example.com",
          "status": "active",
          "schedulable": true,
          "shadow": false,
          "auto_reset_credit_enabled": false,
          "groups": [{ "id": 20, "name": "Group Alpha", "priority": 1 }]
        },
        "snapshot": {
          "observed_at": "2026-10-14T08:55:00Z",
          "usage_updated_at": "2026-10-14T08:55:00Z",
          "credits": { "has_credits": true, "unlimited": false, "balance": "12.5" },
          "reset_credits": { "available": 2, "expires_at": ["2026-11-01T00:00:00Z"] }
        },
        "observation": {
          "sweep_id": 41,
          "observed_at": "2026-10-14T09:20:05Z",
          "plan_type": "plus",
          "windows": [
            { "used_percent": 42, "window_seconds": 18000, "resets_at": "2026-10-14T12:00:00Z" },
            { "used_percent": 18, "window_seconds": 604800, "resets_at": "2026-10-19T09:00:00Z" }
          ],
          "credits": { "has_credits": true, "unlimited": false, "balance": "12.5" },
          "reset_credits": { "available": 2, "expires_at": ["2026-11-01T00:00:00Z"] }
        }
      }
    ],
    "latest_sweep": null
  }
}
```

`windows` holds whichever of the upstream primary/secondary windows were
returned, each identified by `window_seconds`; `resets_at` is null when the
upstream reports no reset time. `credits.balance` keeps the upstream nullable
decimal string. `reset_credits.available` is the expiry-clamped banked count and
`expires_at` lists the unexpired expiries.

### `POST /admin/account-quotas/sweeps`

Body:

```json
{
  "provider_id": 3,
  "platform": "openai",
  "account_ids": [101, 102],
  "include_auto_reset": false
}
```

`provider_id` is required; `platform` defaults to `openai`; omitted/empty
`account_ids` means all eligible accounts; `include_auto_reset` defaults to
false. The response is `202` with the queued sweep. An in-flight or
interval conflict returns `409` and starts nothing.

### `GET /admin/account-quotas/sweeps/latest`

Query: `provider_id` (required). Returns the provider's newest sweep, or an
envelope whose `data` member is omitted when the provider has never been swept.
The route does not verify that the provider row exists; it settles stale sweeps
for the given provider first.

### `GET /admin/account-quotas/sweeps/:id`

Returns one sweep. A non-positive or non-integer id returns
`400 invalid-request`; an unknown id returns `404 sweep-not-found`. Reading one
sweep settles that sweep as `abandoned` when it is stale and still
`queued`/`running`.

Sweep body shared by the POST and both GET routes:

```json
{
  "id": 41,
  "status": "running",
  "provider_id": 3,
  "platform": "openai",
  "scope": "all",
  "include_auto_reset": false,
  "total_count": 3,
  "processed_count": 2,
  "success_count": 1,
  "skipped_count": 1,
  "failed_count": 0,
  "created_at": "2026-10-14T09:20:00Z",
  "started_at": "2026-10-14T09:20:00Z",
  "finished_at": null,
  "last_error": "",
  "results": [
    {
      "account_id": 103,
      "status": "skipped",
      "error_code": "auto-reset-enabled",
      "message": "skipped because the account has the auto reset-credit worker enabled",
      "observation": null
    }
  ]
}
```

A terminal `results` entry is `succeeded` (with `observation`),
`failed` (with a sanitized `error_code` and message) or `skipped` (with
`auto-reset-enabled`). `last_error` is a fixed sweep-level message, empty unless
the sweep is `failed` or `abandoned`.

## Storage and freshness

- `account_quota_sweeps` is the durable job row: status
  (`queued`/`running`/`completed`/`failed`/`abandoned`), provider and platform,
  scope (`all`/`selected`), `include_auto_reset`, the five counters, the result
  list as JSON, `last_error`, and created/started/finished timestamps. The partial
  unique index `accountquotasweep_running_unique` covers `provider_id` where the
  status is `queued` or `running`.
- `account_quota_observations` is the durable display fact: one row per
  `(provider_id, account_id)` (unique index), holding the producing `sweep_id`,
  `plan_type`, window list, credits, reset credits and `observed_at`. It is
  upserted only for successful outcomes, and a failed or skipped outcome never
  writes or erases a row.
- `observed_at` is the upstream `fetched_at` when reported and the platform's
  receive time otherwise. It is the observation's own freshness marker; the
  read model has no TTL for observations and adds no history.
- The directory snapshot is not persisted by this module. It is the relay
  runtime's process-local `openai` owner-account cache, keyed by provider id,
  persisted configuration version and platform, fresh for five minutes with a
  ten-second load deadline and last-good fallback on a failed reload. A completed
  sweep invalidates that entry.
- No row stores account credentials, OAuth tokens, authorization blobs or raw
  upstream error text. Result messages and sweep-level errors are fixed strings,
  and error logs use fixed classes rather than upstream text.

## Failure and empty states

| State | Behavior |
| --- | --- |
| No sweep yet | `latest_sweep` is null; the tab shows "No quota sweep yet" and the last-successful observations (or snapshots) still render. |
| No observation for an account | The row shows the cached directory `snapshot` with the snapshot marker; credits/reset credits fall back to it while the usage windows stay unknown until the next sweep. |
| One account fails | Only that account's result is `failed` with its reason code; the other targets are unaffected and successful observations are stored. |
| Sweep is `failed` | `last_error` carries the fixed sweep-level message, no target outcome is fabricated, and the previous observations remain. |
| Sweep is `abandoned` | The stale sweep is terminal with the stale-threshold message; the tab can start a new sweep because no queued/running row remains. |
| Start rejected | The tab shows the envelope message and starts nothing; the selection, filters and last observations are kept. |
| List read fails | The tab drops the stale rows, renders the load error with an explicit refresh action, and never presents the failed read as an empty-but-successful working set. |
| Poll read fails | The two-second poll keeps its interval and retries; a single failed poll does not clear the displayed sweep or account data. |
| Non-OpenAI platform selected | The tab renders the platform hint and issues no account-quota request. |

## Explicit boundaries

- No scheduled, cron or automatic sweep. Every sweep is an explicit
  administrator action.
- No reset-credit consumption, no admin bulk reset, and no reset-credit action in
  this tab. Spending a banked credit stays on the gated Owner Account Console
  mutation path.
- No cross-provider or cross-platform sweep, aggregate or comparison; the
  working set is one provider's OpenAI accounts.
- No sweep history page, trend, delta or previous-sweep comparison. The API
  exposes the newest sweep and one sweep by id; the read model keeps one
  observation per account and no time series.
- No account, configuration or credential mutation from this surface. The only
  local writes are the sweep row and successful observation rows.
- No pagination, server-side filtering or server-side sorting of the working
  set; the tab filters and renders the loaded collection.
