package telemetry

import (
	"net/http"
	"strings"
	"testing"
)

func TestHTTPMethodUsesCanonicalBoundedValues(t *testing.T) {
	tests := []struct {
		method string
		want   string
	}{
		{method: http.MethodConnect, want: http.MethodConnect},
		{method: http.MethodDelete, want: http.MethodDelete},
		{method: http.MethodGet, want: http.MethodGet},
		{method: http.MethodHead, want: http.MethodHead},
		{method: http.MethodOptions, want: http.MethodOptions},
		{method: http.MethodPatch, want: http.MethodPatch},
		{method: http.MethodPost, want: http.MethodPost},
		{method: http.MethodPut, want: http.MethodPut},
		{method: http.MethodTrace, want: http.MethodTrace},
		{method: "get", want: "OTHER"},
		{method: strings.Repeat("CUSTOM", 64), want: "OTHER"},
		{method: "", want: "OTHER"},
	}

	for _, tt := range tests {
		if got := HTTPMethod(tt.method); got != tt.want {
			t.Errorf("HTTPMethod(%q) = %q, want %q", tt.method, got, tt.want)
		}
	}
}
