package telemetry

import "testing"

func TestHTTPStatusClassBoundaries(t *testing.T) {
	tests := []struct {
		status int
		want   string
	}{
		{status: 99, want: "unknown"},
		{status: 100, want: "1xx"},
		{status: 199, want: "1xx"},
		{status: 200, want: "2xx"},
		{status: 299, want: "2xx"},
		{status: 300, want: "3xx"},
		{status: 399, want: "3xx"},
		{status: 400, want: "4xx"},
		{status: 499, want: "4xx"},
		{status: 500, want: "5xx"},
		{status: 599, want: "5xx"},
		{status: 600, want: "unknown"},
	}

	for _, tt := range tests {
		if got := HTTPStatusClass(tt.status); got != tt.want {
			t.Errorf("HTTPStatusClass(%d) = %q, want %q", tt.status, got, tt.want)
		}
	}
}
