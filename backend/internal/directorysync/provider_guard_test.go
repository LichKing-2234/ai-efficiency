package directorysync

import (
	"context"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ai-efficiency/backend/ent"
)

// primaryRelayProviderID falls back to "any enabled provider" when no primary
// row exists. A usage-only entry (service_type=kiro2cc) must never win that
// fallback, so every provider lookup has to constrain service_type in its WHERE
// clause. Checking the full statement is not enough: service_type also appears
// in the selected column list.

func TestPrimaryRelayProviderFallbackRejectsUsageOnlyServiceType(t *testing.T) {
	var captured []string
	matcher := sqlmock.QueryMatcherFunc(func(_, actualSQL string) error {
		captured = append(captured, actualSQL)
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	// Empty result sets keep the resolver on its fallback path, so both the
	// primary lookup and the enabled-provider fallback are exercised.
	// The resolvers issue one or two provider statements; expect both.
	mock.MatchExpectationsInOrder(false)
	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	service := &Service{client: client}
	if _, err := service.primaryRelayProviderID(context.Background()); err != nil {
		t.Fatalf("primaryRelayProviderID() unexpected error: %v", err)
	}

	if len(captured) == 0 {
		t.Fatal("no provider statements captured")
	}
	checked := 0
	for _, statement := range captured {
		whereAt := strings.Index(statement, " WHERE ")
		if whereAt < 0 || !strings.Contains(statement, "relay_providers") {
			continue
		}
		checked++
		if where := statement[whereAt:]; !strings.Contains(where, "service_type") {
			t.Fatalf("provider lookup does not constrain service_type in WHERE: %s", where)
		}
	}
	if checked == 0 {
		t.Fatalf("no relay_providers WHERE clause captured: %v", captured)
	}
}
