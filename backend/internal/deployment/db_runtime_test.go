package deployment

import "testing"

func TestRequireExplicitDBDSN(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want bool
	}{
		{
			name: "empty dsn requires explicit dsn",
			dsn:  "",
			want: true,
		},
		{
			name: "postgres url dsn is allowed",
			dsn:  "postgres://example",
			want: false,
		},
		{
			name: "postgresql url dsn is allowed",
			dsn:  "postgresql://example",
			want: false,
		},
		{
			name: "postgresql url dsn is case insensitive",
			dsn:  "PostgreSQL://example",
			want: false,
		},
		{
			name: "keyword dsn is allowed",
			dsn:  "host=127.0.0.1 user=postgres dbname=ai_efficiency sslmode=disable",
			want: false,
		},
		{
			name: "sqlite style dsn is rejected",
			dsn:  "file:ai_efficiency.db?_fk=1",
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RequireExplicitDBDSN(tc.dsn)
			if got != tc.want {
				t.Fatalf("RequireExplicitDBDSN(%q) = %v, want %v", tc.dsn, got, tc.want)
			}
		})
	}
}
