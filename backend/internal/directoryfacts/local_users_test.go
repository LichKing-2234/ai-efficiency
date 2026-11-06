package directoryfacts

import (
	"context"
	"testing"

	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestLocalUsersHidesOrganizationAccountIdentities(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()

	person, err := client.User.Create().
		SetUsername("person").
		SetEmail("person@example.com").
		SetAuthSource("ldap").
		Save(ctx)
	if err != nil {
		t.Fatalf("create person: %v", err)
	}
	shadow, err := client.User.Create().
		SetUsername("team-dept-a").
		SetEmail("team-dept-a@invalid.account").
		SetRelayUserID(900).
		SetAuthSource("relay_sso").
		Save(ctx)
	if err != nil {
		t.Fatalf("create shadow user: %v", err)
	}
	if _, err := client.TeamAccount.Create().
		SetDepartmentExternalID("dept-a").
		SetDepartmentDisplayPath("Org / Dept A").
		SetName("Dept A AI").
		SetRelayProviderID(1).
		SetRelayUserID(900).
		SetRelayUsername("team-dept-a").
		SetRelayEmail("team-dept-a@invalid.account").
		SetRelayAuthPassword("ciphertext").
		SetOwnerUserID(person.ID).
		SetStatus("active").
		Save(ctx); err != nil {
		t.Fatalf("create team account: %v", err)
	}

	store := New(client)
	page, err := store.LocalUsers(ctx, nil, LocalUserQuery{Limit: 10})
	if err != nil {
		t.Fatalf("LocalUsers() error = %v", err)
	}
	if len(page.IDs) != 1 || page.IDs[0] != person.ID {
		t.Fatalf("LocalUsers() ids = %v, want only the person %d (shadow %d hidden)", page.IDs, person.ID, shadow.ID)
	}
}
