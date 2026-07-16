package telemetry

import (
	"fmt"
	"math"
	"strings"
)

type WebVitalSample struct {
	Metric         string  `json:"metric"`
	Value          float64 `json:"value"`
	Route          string  `json:"route"`
	NavigationType string  `json:"navigation_type"`
}

var browserStaticRoutes = map[string]struct{}{
	"/login":                       {},
	"/oauth/authorize":             {},
	"/oauth/device":                {},
	"/usage":                       {},
	"/work-items":                  {},
	"/usage/team":                  {},
	"/usage/quota-reset":           {},
	"/repos":                       {},
	"/events":                      {},
	"/user":                        {},
	"/admin/users":                 {},
	"/admin/directory/offboarding": {},
	"/settings":                    {},
}

var browserNavigationTypes = map[string]struct{}{
	"navigate":           {},
	"reload":             {},
	"back-forward":       {},
	"back-forward-cache": {},
	"prerender":          {},
	"restore":            {},
}

func NormalizeBrowserRoute(raw string) string {
	if len(raw) == 0 || len(raw) > 256 || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return "unmatched"
	}
	if index := strings.IndexAny(raw, "?#"); index >= 0 {
		raw = raw[:index]
	}
	if len(raw) > 1 {
		raw = strings.TrimSuffix(raw, "/")
	}
	if _, ok := browserStaticRoutes[raw]; ok {
		return raw
	}
	parts := strings.Split(raw, "/")
	if len(parts) == 3 && parts[1] == "repos" && parts[2] != "" {
		return "/repos/:id"
	}
	if len(parts) == 4 && parts[1] == "usage" && parts[2] == "members" && parts[3] != "" {
		return "/usage/members/:user_id"
	}
	return "unmatched"
}

func (m *Metrics) ObserveWebVital(sample WebVitalSample) error {
	if err := ValidateWebVitalSample(sample); err != nil {
		return err
	}
	route := NormalizeBrowserRoute(sample.Route)
	labels := []string{sample.Metric, route, sample.NavigationType, m.release}
	if sample.Metric == "CLS" {
		m.browserVitalRatio.WithLabelValues(labels...).Observe(sample.Value)
		return nil
	}
	m.browserVitalSeconds.WithLabelValues(labels...).Observe(sample.Value / 1000)
	return nil
}

func ValidateWebVitalSample(sample WebVitalSample) error {
	if math.IsNaN(sample.Value) || math.IsInf(sample.Value, 0) || sample.Value < 0 {
		return fmt.Errorf("web vital value must be finite and non-negative")
	}
	switch sample.Metric {
	case "CLS":
		if sample.Value > 10 {
			return fmt.Errorf("CLS value exceeds supported maximum")
		}
	case "LCP", "INP", "TTFB":
		if sample.Value > 600000 {
			return fmt.Errorf("web vital duration exceeds supported maximum")
		}
	default:
		return fmt.Errorf("unsupported web vital metric")
	}
	if _, ok := browserNavigationTypes[sample.NavigationType]; !ok {
		return fmt.Errorf("unsupported web vital navigation type")
	}
	return nil
}
