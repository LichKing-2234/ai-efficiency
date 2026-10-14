# Ops Monitoring Contract

This contract describes the current admin-only, read-only Ops Monitoring
surface: the per-group upstream ops facts it presents for every Sub2API Group at
once, the API and Relay read paths that serve them, the display-only metric
alignment rule, and the fetch, cache, and failure behavior. Read it before
changing `/admin/ops-monitoring`, `backend/internal/opsmonitoring`, the Relay
group ops-health/account/proxy-test capability, or the upstream ops field
mapping.

Relay identity, Provider access, and Group/Account planning facts follow
[relay user access](./relay-user-access.md) and
[relay group mapping](./relay-group-mapping.md). Read-model freshness, request
deadlines, and frontend loading follow
[platform loading](./platform-loading.md). Collection presentation follows
[collection navigation](./collection-navigation.md).

## Terms and Authority

| Term | Meaning |
| --- | --- |
| Ops Monitoring | The administrator-only, read-only surface at `/admin/ops-monitoring` that presents every Sub2API Group's upstream ops facts at once. |
| Sub2API Group | The upstream routing group whose ops facts are displayed. It is not a user-selectable Access Group and not a Relay Group Mapping Target. |
| Group Health Snapshot | The verbatim Sub2API ops overview for one Sub2API Group over one explicit UTC window. |
| Metric alignment | The rule that every number on this surface is a field returned by sub2api, displayed without recomputation. |
| Fetch failure | The platform outcome for one group whose overview read did not complete. It is not an upstream health fact. |

sub2api remains the authority for every metric, account, proxy, and egress fact
on this surface. The platform adds only fetch metadata: row `status` and
`error`, `as_of`, `refreshing`, `failed_group_count`, the resolved window, and
the probe `checked_at` / `cached` markers. The surface persists no ops history
and owns no local ops table.

## Admin Surface

- `/admin/ops-monitoring` is an administrator-only route with the navigation
  label 「运维监控」. Its scope is global: it has no department, subject, or
  role scoping.
- The surface is read-only. It exposes no alert, threshold, notification, or
  paging rule; no action button; no relationship, account, or proxy mutation;
  and no history, trend, or previous-window comparison.
- The page lists the configured Relay Providers through the existing provider
  list, starts on the primary Provider, and shows a Provider switcher when more
  than one Provider exists.
- Ops Monitoring shares no endpoint, service, component, store, or navigation
  state with Relay Planning. It reuses the configured sub2api admin credential
  and the existing Relay provider HTTP client behind the `relay.Provider`
  boundary.
- The route and module are named for the surface rather than the provider so a
  second ops source can be added without a URL change. Only sub2api implements
  the capability; the service type-asserts the Relay provider's
  `relay.GroupOpsHealthReader` capability (`ReadGroupOpsHealth`,
  `ListGroupAccounts`, `TestProxy`) and defines no provider-neutral ops
  abstraction.
- No ops endpoint exists upstream for all groups at once. The backend fans out
  one overview read per group and joins the result with directory and account
  facts sub2api already returns.

## Upstream Sources

All four calls go through the configured Relay provider's admin HTTP client and
credential. There is no direct sub2api database access.

| Upstream call | Supplies | Behavior |
| --- | --- | --- |
| `GET /api/v1/admin/groups?page=&page_size=200` | Group identity, name, platform, `account_count`, `active_account_count` | The directory read that enumerates the fan-out. It is the authority for group identity, name, platform, and both account counts. |
| `GET /api/v1/admin/ops/dashboard/overview?group_id=&start_time=&end_time=` | Every per-group metric on the row and in the group detail | One call per selected group. sub2api ignores the upstream `window=` parameter, so the window is always sent as explicit RFC3339 UTC `start_time` / `end_time`. A group-scoped call returns `data.platform` empty, and this layer reads no overview field by name, so platform is never sourced from this response. |
| `GET /api/v1/admin/accounts?page=&page_size=200&group=<id>` | Expanded account list and each account's embedded `proxy` object | Paginated until `pages` is exhausted. Credentials are never decoded. |
| `POST /api/v1/admin/proxies/<id>/test` | Measured egress IP and geo for one proxy | An active upstream probe issued only on explicit operator demand. It measures; it mutates no platform or upstream state. |

The overview `data` object carries 19 scalar keys and 6 sub-objects.
`system_metrics` and `job_heartbeats` are platform-global rather than
group-scoped and are not displayed. The overview's own `start_time`, `end_time`,
and `platform` are also not displayed; the snapshot window and directory
platform are the displayed authorities.

## API Surface

All three routes are administrator-gated under `/api/v1`, require the
`provider_id` query parameter, and return the standard `{"code": 200, "data":
...}` envelope. Invalid query input returns `400`; a provider-level failure
(provider resolution, missing ops capability, or the group directory read)
returns `422` and no snapshot.

### `GET /admin/ops-monitoring/groups`

Query: `provider_id` (required), `window` = `1h` | `6h` | `24h`, `force`
(optional, explicit refresh only).

An omitted or empty `window` selects `1h`; any other value is rejected rather
than silently substituted. There is no server-side platform filter: the page
applies the platform filter client-side over every loaded group, so its options
are derived from the loaded snapshot.

`force=true` (or `force=1`) skips the snapshot cache read and performs the
fan-out immediately; the resulting snapshot is still written to cache. Any other
value keeps the cached read path. The page sends `force` only for the manual
refresh action, never for the automatic 60-second tick.

The response is a group snapshot:

```json
{
  "provider_id": 1,
  "window": "1h",
  "start_time": "2026-10-14T08:20:00Z",
  "end_time": "2026-10-14T09:20:00Z",
  "as_of": "2026-10-14T09:20:00Z",
  "refreshing": false,
  "failed_group_count": 1,
  "groups": [
    {
      "group_id": 20,
      "group_name": "…-openai-01",
      "platform": "openai",
      "account_count": 4,
      "active_account_count": 4,
      "status": "ok",
      "error": "",
      "overview": { "health_score": 60, "request_count_total": 1525 }
    }
  ]
}
```

`overview` is the upstream `data` object passed through verbatim. It is absent
only when that group's fetch failed. `status` is `ok` or `failed` and describes
the fetch outcome, never upstream health.

### `GET /admin/ops-monitoring/groups/:group_id/accounts`

Query: `provider_id` (required). Returns `{group_id, as_of, accounts[]}` with
the privacy-safe account projection and its `proxy` object when one is bound.
This layer loads on demand when a row expands and is never cache-warmed with the
group table.

### `POST /admin/ops-monitoring/proxies/:proxy_id/test`

Query: `provider_id` (required), `force` (optional). Returns the upstream probe
result plus platform metadata `proxy_id`, `checked_at`, and `cached`. A call
inside the cache window returns the stored measurement with `cached = true` and
does not re-probe; `force=true` (or `force=1`) re-probes and still stores the new
measurement. The page sends `force` for the row's explicit probe action, so an
operator re-checking a repaired proxy is never answered from the stored result.

## Metric Alignment

The platform displays; it never computes. Every number shown is a field
returned by sub2api.

Allowed:

- Rendering an upstream field directly, including sub2api's own rates, SLA,
  error counts, account counts, and health score.
- Joining upstream fields for display: directory group facts, the group-scoped
  ops overview, the account list, the account's embedded proxy object, and the
  measured egress probe result. A join is not derivation.
- Presentation-only transforms: fraction-to-percentage formatting, UTC to
  Beijing time conversion, the relative window label, row ordering, and the
  no-traffic classification.

Forbidden:

- Any derived rate, ratio, percentage, or average computed by the platform.
- Any aggregation, including a sum of upstream error rows, a cross-group total,
  and a group-level rollup of account-level values.
- A platform-computed composite health score. sub2api's per-group `health_score`
  is displayed as-is and labelled as sub2api's value; the platform deliberately
  defines no competing health definition.
- A per-group 503/502 status-code split. sub2api exposes no per-group
  status-code split, and `error-distribution` is global only. The displayed
  `429` and `529` values are upstream per-group count fields, not a split the
  platform constructs.
- A dominant-proxy, dominant-error, or most-affected-account rollup.
- Trends, history, deltas, or comparison against a previous window.

`upstream_error_rate` is already the excluding-429/529 figure and is displayed
directly. A production group-scoped read returned
`upstream_error_count_excl_429_529` = 1535 over `request_count_sla` = 21221,
i.e. the returned `upstream_error_rate` = 0.0723, matching the sub2api panel.
Recomputing or re-denominating that rate would violate this contract.

`upstream_error_count_excl_429_529` counts upstream failover attempts rather
than failed user requests, so it is displayed as returned and is never
reconciled against `request_count_total`.

## Field Mapping

Every displayed value maps to an exact upstream field. `data.<key>` means a key
on the `data` object of the group-scoped overview response, passed through
untouched.

### Group row and table columns

| Displayed | Upstream field | Source |
| --- | --- | --- |
| 分组 | directory item `name`, with `id` shown as the group number | `/admin/groups` |
| 平台 | directory item `platform` (the group-scoped overview returns `data.platform` empty) | `/admin/groups` |
| 请求数 | `data.request_count_total` | overview |
| SLA | `data.sla` | overview |
| 请求错误率 | `data.error_rate` | overview |
| 上游错误率 | `data.upstream_error_rate` | overview |
| 上游错误数（排除 429/529） | `data.upstream_error_count_excl_429_529` | overview |
| 429 | `data.upstream_429_count` | overview |
| 529 | `data.upstream_529_count` | overview |
| 健康分 | `data.health_score` | overview |
| 活跃账号数 | directory `active_account_count` / `account_count`, shown as `active / total` | `/admin/groups` |

### Group detail

| Displayed | Upstream field |
| --- | --- |
| 消耗 Token | `data.token_consumed` |
| 成功请求数 | `data.success_count` |
| 错误总数 | `data.error_count_total` |
| SLA 错误数 | `data.error_count_sla` |
| 业务限流数 | `data.business_limited_count` |
| SLA 请求数 | `data.request_count_sla` |
| QPS (avg · current · peak) | `data.qps.avg`, `data.qps.current`, `data.qps.peak` |
| TPS (avg · current · peak) | `data.tps.avg`, `data.tps.current`, `data.tps.peak` |
| 输出 TPS (avg · p50 · p10 · p5 · samples) | `data.output_tps.avg`, `data.output_tps.p50`, `data.output_tps.p10`, `data.output_tps.p5`, `data.output_tps.sample_count` |
| 耗时 (avg · max · p50 · p90 · p95 · p99) | `data.duration.avg_ms`, `data.duration.max_ms`, `data.duration.p50_ms`, `data.duration.p90_ms`, `data.duration.p95_ms`, `data.duration.p99_ms` |
| 首字延迟 (avg · max · p50 · p90 · p95 · p99) | `data.ttft.avg_ms`, `data.ttft.max_ms`, `data.ttft.p50_ms`, `data.ttft.p90_ms`, `data.ttft.p95_ms`, `data.ttft.p99_ms` |

`output_tps` is percentile-shaped upstream — `avg`, `p5`, `p10`, `p50`,
`sample_count` — and carries no `current` or `peak`; the detail formatter renders
the keys the live payload actually returns. `qps` and `tps` are the two
sub-objects that do carry `avg` / `current` / `peak`.

### Account expansion

| Displayed | Upstream field | Source |
| --- | --- | --- |
| Account identity | `id`, projected as `account_id` | `/admin/accounts?group=<id>` |
| Account name | `name` | accounts |
| Platform | `platform` | accounts |
| Type | `type` | accounts |
| Status | `status` | accounts |
| Schedulability | `schedulable` | accounts |
| Proxy binding | `proxy.name`, `proxy.protocol`, `proxy.host`, `proxy.port` | accounts |
| Direct (no proxy) | absent `proxy` / `proxy_id` | accounts |

The projection also carries `proxy_id`, which is the egress-probe target
identity rather than a separate displayed column.

The account credential blob is never decoded and no credential material enters
the projection.

### Measured egress probe

| Displayed | Upstream field |
| --- | --- |
| 出口 IP | `ip_address` |
| Location | `city`, `region`, `country` |
| 延迟 | `latency_ms` |
| Probe outcome | `success` |
| Probe message | `message` |

The probe payload also carries `country_code`; the current detail view does not
render it.

Platform-produced probe metadata that is not an upstream metric: `proxy_id`
(the requested probe target, echoed in the record), `checked_at` (when the
platform stored the measurement), and `cached` (whether a stored measurement was
returned). A probe returns the measured egress identity — for example
`success` = true, `ip_address` = 198.51.100.7, `city` = Example City,
`country` = Example Country, `latency_ms` = 561 — so the displayed egress
identity is measured rather than inferred from configuration.

## Window, Fan-Out, and Cache

The supported windows are `1h` (default), `6h`, and `24h`. The window resolves
to `end = now` UTC and `start = end - window`, and is sent upstream as explicit
RFC3339 UTC values. The displayed window label is relative and in Beijing time.

The group read performs one paginated directory read, then one overview call per
selected group with concurrency 8, a 5-second per-group timeout, and a
20-second overall budget. A group that cannot start before the budget is
exhausted fails its own row; budget exhaustion never fails the read. The budget
has to stay clear of the real cost of the fan-out: one wave is 8 groups, so a
directory of several dozen groups costs several waves, and a budget close to a
single wave's measured latency would report healthy groups as failed after one
slow upstream call.

Group snapshots are reconstructible Redis read models keyed by namespace, kind,
schema version, provider, and window. They are not authority.

- Fresh for 60 seconds: returned immediately.
- Stale but readable up to a 10-minute TTL: the stale snapshot is returned
  immediately with `refreshing = true`, and one background refresh runs behind
  the response without extending it.
- Cold: one single-flight load blocks the first caller within the 20-second
  budget, typically 5-7 seconds. Concurrent callers for the same key join it.
- A failed refresh leaves the previous snapshot readable.
- `force=true` skips the cache read and runs this load path unchanged.

A snapshot that contains failed rows is never written to cache. A failure is not
a measurement: caching one would keep serving a transient upstream error as if it
were data for the whole freshness window, and the next read would never call
upstream again. A previously cached successful snapshot stays readable under its
own TTL, so a later failure still has something to fall back on.

Measured egress results are cached for 6 hours per provider and proxy, matching
the static nature of a fixed egress address; a stored result returns
`cached = true` without re-probing, and `force=true` re-probes. The group
snapshot is the only cache-warmed read: account expansion and the egress probe
are per-row requests outside the fan-out. No ops read persists history or
trends.

Auto-refresh is off by default and re-reads the same window every 60 seconds
when enabled, through the cached read path. A failed group is never stored as
fresh data, so the next read of any kind — an explicit refresh, a window or
provider change, or a later auto-refresh tick — retries it.

## Failure and Empty States

| State | Behavior |
| --- | --- |
| One group's fetch fails | The row's `status` is `failed` with the upstream error text, its metric cells render `—`, and the 分组 cell carries the row-level 取数失败 / Fetch failed tag. It is pinned to the top and counted by `failed_group_count`. Other rows are unaffected. |
| Provider-level failure | Provider resolution, missing ops capability, or the directory read fails the whole read with `422` and no snapshot; the page renders a whole-page error state with manual refresh and no automatic retry. |
| Zero traffic | When every upstream traffic field in the group's overview (`request_count_total`, `request_count_sla`, `success_count`, `error_count_total`, `error_count_sla`, `token_consumed`) is zero or absent, the four traffic-derived table cells (`request_count_total`, `sla`, `error_rate`, `upstream_error_rate`) render the muted 无数据 / No data state. The group detail panel prints every returned value verbatim. It is distinct from fetch failure and is classified only from returned upstream values. |
| Non-zero value on a zero-traffic row | A cell whose upstream value is non-zero always renders that returned value, even when the row is classified as zero traffic. The classification never hides a returned number. |
| Missing upstream metric | A metric cell whose upstream key is absent renders the same muted 无数据 / No data value for that cell. |
| Stale snapshot with failed refresh | The previous snapshot remains displayed; failed rows remain failed. |
| Overview returned nothing | Treated as that group's fetch failure, never as no data. |

An unfetched group is never rendered as healthy, and a failed group is never
rendered as zero traffic.

## Presentation

The surface is one collection with a window switcher, manual refresh, an
auto-refresh toggle, a Provider switcher when several Providers exist, a
client-side platform filter, a failure banner, and the group list. The wide
layout uses a table and the narrow layout uses one record card per group from
the same loaded collection and request state. There is no pagination.

- The default sort is `upstream_error_rate` descending. Every data column header
  is sortable, and clearing the sort restores that default.
- Failed rows are pinned above the sorted rows, in directory order among
  themselves, regardless of the active sort. A failed group may be exactly the
  group an operator needs to see, so it is never buried by an unrelated sort.
- Successful rows sort client-side by the selected column, comparing upstream
  values; missing values sort after present ones and equal values fall back to
  group ID. Sorting is presentation only and never recomputes or re-fetches a
  value.
- Backend order is the `/admin/groups` directory order. The backend does not
  sort.
- The platform filter is applied client-side over every loaded group, and it
  resets to all platforms when the selected platform is absent from a new
  snapshot.
- Rate fields (`sla`, `error_rate`, `upstream_error_rate`) arrive as fractions
  and are rendered as percentages; only the representation changes.
- Expanding a group loads its account list and shows the remaining group
  metrics. Each account row shows its proxy binding, and a proxy offers an
  explicit egress probe action that shows the measured IP, location, latency,
  check time, and a cached marker. The probe action and the manual refresh both
  ask for a fresh read rather than replaying a cached one. A group failure does
  not remove, reorder, or block any other row's expansion.
- A failed refresh never clears data already on screen: the last snapshot stays
  rendered and the failure is shown as a notice above it. The whole-page error
  state is reserved for a read that produced no snapshot at all.
