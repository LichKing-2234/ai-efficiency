package deployment

import "testing"

func TestRequireExplicitDBDSN(t *testing.T) {
	tests := []struct {
		name    string
		version string
		dsn     string
		want    bool
	}{
		{
			name:    "release build with empty dsn requires explicit dsn",
			version: "v1.2.3",
			dsn:     "",
			want:    true,
		},
		{
			name:    "release build with dsn does not require explicit dsn",
			version: "v1.2.3",
			dsn:     "postgres://example",
			want:    false,
		},
		{
			name:    "dev build with empty dsn still requires explicit dsn",
			version: "dev",
			dsn:     "",
			want:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RequireExplicitDBDSN(VersionInfo{Version: tc.version}, tc.dsn)
			if got != tc.want {
				t.Fatalf("RequireExplicitDBDSN(%q, %q) = %v, want %v", tc.version, tc.dsn, got, tc.want)
			}
		})
	}
}
