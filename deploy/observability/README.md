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
`repository_inventory`, and `provider_metadata`. The backend preinitializes every name/outcome pair, so a
scrape distinguishes a quiet cache from missing instrumentation without adding
keys, actors, scopes, providers, ranges, credentials, or cached values as
labels.

The temporary Team Overview compatibility adapter intentionally has no dedicated
cache name, Redis key, or metric. It consumes the split Summary, Trend, and Members
lanes; unreachable legacy `team-usage-snapshot` values expire under their existing TTL.

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
