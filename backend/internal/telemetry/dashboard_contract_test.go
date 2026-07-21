package telemetry

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestPerformanceDashboardContainsRequiredQuantilesPoolsAndPrivacyContract(t *testing.T) {
	data, err := os.ReadFile("../../../deploy/observability/grafana/ai-efficiency-performance.json")
	if err != nil {
		t.Fatalf("read performance dashboard: %v", err)
	}
	var dashboard struct {
		Inputs []struct {
			Name     string `json:"name"`
			Type     string `json:"type"`
			PluginID string `json:"pluginId"`
		} `json:"__inputs"`
		Title      string `json:"title"`
		Templating struct {
			List []struct {
				Name string `json:"name"`
			} `json:"list"`
		} `json:"templating"`
		Panels []struct {
			Title   string `json:"title"`
			Targets []struct {
				Expr string `json:"expr"`
			} `json:"targets"`
		} `json:"panels"`
	}
	if err := json.Unmarshal(data, &dashboard); err != nil {
		t.Fatalf("parse performance dashboard: %v", err)
	}
	if dashboard.Title != "AI Efficiency Performance Baseline" {
		t.Fatalf("dashboard title = %q", dashboard.Title)
	}
	if len(dashboard.Inputs) != 1 || dashboard.Inputs[0].Name != "DS_PROMETHEUS" || dashboard.Inputs[0].Type != "datasource" || dashboard.Inputs[0].PluginID != "prometheus" {
		t.Fatalf("dashboard Prometheus input = %#v, want one DS_PROMETHEUS datasource input", dashboard.Inputs)
	}
	variables := make(map[string]bool)
	for _, variable := range dashboard.Templating.List {
		variables[variable.Name] = true
	}
	for _, required := range []string{"release", "http_route", "browser_route"} {
		if !variables[required] {
			t.Fatalf("dashboard variable %q is missing", required)
		}
	}

	expressions := make(map[string]string)
	var allExpressions strings.Builder
	for _, panel := range dashboard.Panels {
		for _, target := range panel.Targets {
			expressions[panel.Title] += "\n" + target.Expr
			allExpressions.WriteString(target.Expr)
			allExpressions.WriteByte('\n')
		}
	}
	requireDashboardExpressions(t, expressions, "HTTP Request Latency", "histogram_quantile(0.75", "histogram_quantile(0.95", "ai_efficiency_http_request_duration_seconds_bucket", "sum by (le, route, release)", `$release`, `$http_route`)
	requireDashboardExpressions(t, expressions, "Dependency Latency", "histogram_quantile(0.75", "histogram_quantile(0.95", "ai_efficiency_dependency_request_duration_seconds_bucket", "sum by (le, dependency, release)")
	for _, panel := range []string{"LCP", "INP", "TTFB"} {
		requireDashboardExpressions(t, expressions, panel, "histogram_quantile(0.75", "histogram_quantile(0.95", "ai_efficiency_browser_web_vital_seconds_bucket", "sum by (le, route, release)", `$browser_route`)
	}
	requireDashboardExpressions(t, expressions, "CLS", "histogram_quantile(0.75", "histogram_quantile(0.95", "ai_efficiency_browser_web_vital_ratio_bucket", "sum by (le, route, release)", `$browser_route`)
	requireDashboardExpressions(t, expressions, "Database Pool", "ai_efficiency_db_connections", "ai_efficiency_db_wait_total", "ai_efficiency_db_wait_duration_seconds_total", "ai_efficiency_db_connections_closed_total")
	requireDashboardExpressions(t, expressions, "Redis Pool", "ai_efficiency_redis_pool_connections", "ai_efficiency_redis_pool_wait_total", "ai_efficiency_redis_pool_wait_duration_seconds_total", "ai_efficiency_redis_pool_timeout_total")
	requireDashboardExpressions(t, expressions, "Application Cache", "ai_efficiency_cache_events_total", "cache", "outcome")
	requireDashboardExpressions(t, expressions, "HTTP Response Size", "histogram_quantile(0.75", "histogram_quantile(0.95", "ai_efficiency_http_response_bytes_bucket", "sum by (le, route, release)", `$http_route`)
	requireDashboardExpressions(t, expressions, "Team Usage Prewarm Cycle Duration", "histogram_quantile(0.95", "ai_efficiency_team_usage_prewarm_cycle_duration_seconds_bucket", "sum by (le, class, timezone, outcome)")
	requireDashboardExpressions(t, expressions, "Team Usage Prewarm Cycle Outcomes", "ai_efficiency_team_usage_prewarm_cycle_total", "sum by (class, timezone, outcome)")
	requireDashboardExpressions(t, expressions, "Team Usage Prewarm Last Success", "ai_efficiency_team_usage_prewarm_last_success_timestamp_seconds", `class=~"moving|history_29d|history_6d"`)
	requireDashboardExpressions(t, expressions, "Team Usage Prewarm Source Duration", "histogram_quantile(0.95", "ai_efficiency_team_usage_prewarm_source_duration_seconds_bucket", "sum by (le, class, timezone, outcome)")
	requireDashboardExpressions(t, expressions, "Team Usage Prewarm Redis Duration", "histogram_quantile(0.95", "ai_efficiency_team_usage_prewarm_redis_duration_seconds_bucket", "sum by (le, operation, outcome)")
	requireDashboardExpressions(t, expressions, "Team Usage Prewarm Generation Bytes", "ai_efficiency_team_usage_prewarm_generation_bytes")
	requireDashboardExpressions(t, expressions, "Team Usage Prewarm Request Outcomes", "ai_efficiency_team_usage_prewarm_request_total", "sum by (timezone, outcome, fallback_reason)")
	requireDashboardExpressions(t, expressions, "Team Usage Prewarm Skipped Ticks", "ai_efficiency_team_usage_prewarm_cycle_total", `outcome="tick_skipped"`, "sum by (class, timezone, outcome)")
	requireDashboardExpressions(t, expressions, "Team Usage Prewarm Lease Outcomes", "ai_efficiency_team_usage_prewarm_redis_duration_seconds_count", `operation=~"lease_acquire|lease_ttl|lease_release"`, "sum by (operation, outcome)")

	all := strings.ToLower(allExpressions.String())
	for _, forbidden := range []string{"user_id", "request_id", "cache_key", "provider_id", "scope", "email", "query"} {
		if strings.Contains(all, forbidden) {
			t.Fatalf("dashboard expressions contain prohibited label %q", forbidden)
		}
	}
}

func requireDashboardExpressions(t *testing.T, expressions map[string]string, title string, fragments ...string) {
	t.Helper()
	expression, ok := expressions[title]
	if !ok {
		t.Fatalf("dashboard panel %q is missing", title)
	}
	for _, fragment := range fragments {
		if !strings.Contains(expression, fragment) {
			t.Fatalf("dashboard panel %q does not contain %q: %s", title, fragment, expression)
		}
	}
}
