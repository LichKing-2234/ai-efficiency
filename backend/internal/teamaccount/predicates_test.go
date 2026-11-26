package teamaccount

import (
	"context"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ai-efficiency/backend/ent"
)

// The primary-provider resolvers fall back to "any enabled provider" when no
// primary row exists. A usage-only entry (service_type=kiro2cc) must never win
// that fallback, so every provider lookup has to constrain service_type in its
// WHERE clause. Checking the full statement is not enough: service_type also
// appears in the selected column list.

// captureProviderSQL records the provider statements a resolver issues and
// answers each with an empty result set, which is what makes the resolver walk
// its "no primary row" fallback instead of returning early on an error.
func captureProviderSQL(t *testing.T, columns []string, fn func(ctx context.Context, client *ent.Client)) []string {
	t.Helper()
	var captured []string
	matcher := sqlmock.QueryMatcherFunc(func(_, actualSQL string) error {
		captured = append(captured, actualSQL)
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	mock.MatchExpectationsInOrder(false)
	// The resolvers issue one or two provider statements; expect both.
	mock.MatchExpectationsInOrder(false)
	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows(columns))
	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows(columns))
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	fn(context.Background(), client)
	return captured
}

func assertEveryProviderLookupConstrainsServiceType(t *testing.T, statements []string) {
	t.Helper()
	if len(statements) == 0 {
		t.Fatal("no provider statements captured")
	}
	checked := 0
	for _, statement := range statements {
		whereAt := strings.Index(statement, " WHERE ")
		if whereAt < 0 || !strings.Contains(statement, "relay_providers") {
			continue
		}
		checked++
		where := statement[whereAt:]
		if !strings.Contains(where, "service_type") {
			t.Fatalf("provider lookup does not constrain service_type in WHERE: %s", where)
		}
	}
	if checked == 0 {
		t.Fatalf("no relay_providers WHERE clause captured: %v", statements)
	}
}

func TestPrimaryProviderFallbackRejectsUsageOnlyServiceType(t *testing.T) {
	// Empty result sets keep the resolver on its fallback path, so both the
	// primary lookup and the enabled-provider fallback are exercised.
	statements := captureProviderSQL(t, []string{"id"}, func(ctx context.Context, client *ent.Client) {
		if _, err := New(client, nil, nil, "", nil).primaryProviderID(ctx); err != nil {
			t.Fatalf("primaryProviderID() unexpected error: %v", err)
		}
	})
	assertEveryProviderLookupConstrainsServiceType(t, statements)
}
