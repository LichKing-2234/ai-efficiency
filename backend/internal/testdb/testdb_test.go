package testdb

import (
	"context"
	"testing"
)

func TestOpenWithDSNProvidesIsolatedSchemas(t *testing.T) {
	client1, dsn1 := OpenWithDSN(t)
	client2, dsn2 := OpenWithDSN(t)

	if dsn1 == dsn2 {
		t.Fatalf("expected unique DSNs, got identical value %q", dsn1)
	}

	ctx := context.Background()
	client1.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRole("admin").
		SaveX(ctx)

	if count := client1.User.Query().CountX(ctx); count != 1 {
		t.Fatalf("client1 user count = %d, want 1", count)
	}
	if count := client2.User.Query().CountX(ctx); count != 0 {
		t.Fatalf("client2 user count = %d, want 0", count)
	}
}
