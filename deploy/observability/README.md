# Performance Observability

AI Efficiency exposes Prometheus metrics on a dedicated listener. The default
address is `127.0.0.1:9090`, separate from the public application port. The
listener serves only `/metrics` and has no SPA fallback or API routes.

For a systemd installation, scrape the loopback listener:

```yaml
scrape_configs:
  - job_name: ai-efficiency
    static_configs:
      - targets: ["127.0.0.1:9090"]
```

Docker Compose sets `AE_METRICS_LISTEN_ADDRESS=:9090` inside the backend
container but intentionally does not publish port 9090. A Prometheus container
on the same private Compose network can scrape `backend:9090`. Do not add a
public host port or route this listener through the public reverse proxy.

Import `grafana/ai-efficiency-performance.json` into an internal Grafana and
select the Prometheus datasource when prompted. The dashboard is a baseline
view, not a pass/fail gate. It provides release-preserving HTTP and dependency
p75/p95, Web Vitals p75/p95, database and Redis pool state, and application
cache event rates. HTTP API routes and browser routes use separate filters so
one route namespace cannot hide the other. Issue #136 owns production sampling
sufficiency and route-specific budget ratification.

The application cache panel groups the closed outcome set by the stable
production names `work_items_counts`, `personal_usage`, `representative_scope`,
`team_usage_summary`, `team_usage_trend`, `team_usage_members`, `team_usage_organization`,
`team_usage_origin`, `repository_inventory`, and `provider_metadata`. The backend preinitializes every name/outcome pair, so a
scrape distinguishes a quiet cache from missing instrumentation without adding
keys, actors, scopes, providers, ranges, credentials, or cached values as
labels.

The temporary Team Overview compatibility adapter intentionally has no dedicated
cache name, Redis key, or metric. It consumes the split Summary, Trend, and Members
lanes; unreachable legacy `team-usage-snapshot` values expire under their existing TTL.

## Segmented Team Usage Prewarm

The optional segmented Team Usage prewarm remains disabled by default. Keep
`AE_TEAM_USAGE_PREWARM_ENABLED=false` until the separate feature-disabled Redis
benchmark and staging acceptance in the implementation plan pass. Because the
prewarm recorder is constructed only on the enabled runtime path, absent
`ai_efficiency_team_usage_prewarm_*` series are expected while the feature is
disabled or before its optional dependencies initialize.

The dashboard adds bounded operational views for:

- p95 cycle, Relay source, and Redis operation duration;
- cycle outcomes and last-success age for `moving`, `history_6d`, and
  `history_29d` refreshes by configured timezone;
- the latest complete generation size, counted with provider-wide current stats
  once;
- request outcomes and exact-fallback reasons;
- skipped moving ticks and lease acquire, TTL, and release outcomes.

Cycle classes, outcomes, Redis operations, request outcomes, fallback reasons,
and validation/cache outcomes are closed runtime enums. Timezone values come
only from the validated maximum-four allowlist. Dashboard queries group only by
those bounded labels. They never group by provider, user, request, scope, cache
key, source row, or credential data.

For an enabled staging runtime, first check cycle outcomes and last-success age
for missing or delayed publications. Use source and Redis p95 panels to separate
upstream delay from cache delay, then check request outcomes to confirm whether
traffic used a full prewarm hit, partial-today repair, or the retained exact
fallback. A skipped tick means the preceding moving cycle still occupied the
local scheduler. A busy lease commonly means another Pod owns the collapsed
deployment-wide operation; correlate sustained busy or skipped rates with
last-success age before treating either as a failure. A zero generation gauge
is the initial state until one explicitly registered complete batch publishes.

Rollback sets `AE_TEAM_USAGE_PREWARM_ENABLED=false` and rolls the application
through the normal deployment path. Readers and background cycles then stop and
requests immediately use the retained exact scope-origin path. Do not flush
Redis: immutable prewarm values and manifests expire under their bounded TTLs.
Staging benchmark and acceptance evidence are environment-sensitive Task 9
work and are not established by the local dashboard contract tests.

The browser defaults to a 10 percent page sample. Custom frontend builds can
set `VITE_WEB_VITALS_SAMPLE_RATE` from `0` to `1`; invalid values return to the
10 percent default. Sampling starts after the initial Vue Router redirects and
authorization guards finish, so it uses the final rendered route and current
access token. Only authenticated sampled pages load the `web-vitals` chunk and
submit LCP, INP, CLS, and TTFB.

The backend never stores raw Web Vitals events. It validates and immediately
aggregates each accepted sample into fixed-memory Prometheus histogram buckets;
metric IDs, request IDs, users, route parameters, queries, and page content are
not retained. Configure a bounded Prometheus TSDB retention period, for example
`--storage.tsdb.retention.time=15d`, according to the internal monitoring data
policy.
