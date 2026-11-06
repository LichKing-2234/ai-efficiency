package teamusage

import (
	"context"
	"testing"

	_ "github.com/lib/pq"

	"github.com/ai-efficiency/backend/internal/representativescope"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestAppendOrganizationAccountSubjects(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()

	shadow, err := client.User.Create().
		SetUsername("team-dept-a").
		SetEmail("team-dept-a@invalid.account").
		SetAuthSource("relay_sso").
		Save(ctx)
	if err != nil {
		t.Fatalf("create shadow user: %v", err)
	}
	_, err = client.TeamAccount.Create().
		SetDepartmentExternalID("dept-a").
		SetDepartmentDisplayPath("Org / Dept A").
		SetName("Dept A AI").
		SetRelayProviderID(1).
		SetRelayUserID(555).
		SetRelayUsername("team-dept-a").
		SetRelayEmail("team-dept-a@invalid.account").
		SetRelayAuthPassword("ciphertext").
		SetOwnerUserID(shadow.ID).
		SetStatus("active").
		Save(ctx)
	if err != nil {
		t.Fatalf("create team account: %v", err)
	}

	scope := &representativescope.Scope{
		IsRepresentative:         true,
		RepresentedDepartmentIDs: []string{"dept-a"},
		RepresentedSubtreeIDs:    map[string]map[string]struct{}{"dept-a": {"dept-a": {}}},
		Subjects: []representativescope.Subject{
			{SubjectType: "member", UserID: shadow.ID + 1, DisplayName: "Person", Email: "person@example.com"},
		},
	}
	scope.OverviewSubjects = append([]representativescope.Subject(nil), scope.Subjects...)

	service := &Service{client: client}
	enriched, err := service.appendOrganizationAccountSubjects(ctx, scope)
	if err != nil {
		t.Fatalf("appendOrganizationAccountSubjects() error = %v", err)
	}
	if len(enriched.Subjects) != 2 || len(enriched.OverviewSubjects) != 2 {
		t.Fatalf("subjects = %d/%d, want 2/2", len(enriched.Subjects), len(enriched.OverviewSubjects))
	}
	account := enriched.Subjects[1]
	if account.SubjectType != teamAccountSubjectType || account.DisplayName != "Dept A AI" {
		t.Fatalf("account subject = %+v", account)
	}
	if account.RelayUserID == nil || *account.RelayUserID != 555 {
		t.Fatalf("relay user id = %v, want 555", account.RelayUserID)
	}
	if account.UserID != shadow.ID || !account.Selectable {
		t.Fatalf("shadow binding = %d selectable=%v", account.UserID, account.Selectable)
	}
	if account.DepartmentDisplayPath != "Org / Dept A" {
		t.Fatalf("department path = %q", account.DepartmentDisplayPath)
	}

	// A suspended account is not a member.
	if _, err := client.TeamAccount.UpdateOneID(1).SetStatus("suspended").Save(ctx); err != nil {
		t.Fatalf("suspend account: %v", err)
	}
	enriched, err = service.appendOrganizationAccountSubjects(ctx, scope)
	if err != nil {
		t.Fatalf("appendOrganizationAccountSubjects() after suspend error = %v", err)
	}
	if len(enriched.Subjects) != 1 {
		t.Fatalf("suspended account leaked into members: %+v", enriched.Subjects)
	}

	// A non-representative scope is never enriched.
	plain := &representativescope.Scope{IsRepresentative: false}
	unchanged, err := service.appendOrganizationAccountSubjects(ctx, plain)
	if err != nil || len(unchanged.Subjects) != 0 {
		t.Fatalf("non-representative scope changed: %+v, %v", unchanged, err)
	}
}
