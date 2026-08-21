package attributionledger

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/google/uuid"
)

func TestInstallationIssuesOnlyReporterCredential(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	user := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SaveX(ctx)
	service := NewInstallationService(client, DefaultProtocolContract())
	credentials, err := service.Ensure(ctx, user.ID, uuid.NewString(), "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	if credentials.ReportingEnabled || credentials.ReporterToken == "" {
		t.Fatalf("credentials = %+v", credentials)
	}
	if strings.Contains(credentials.ReporterToken, "aeo_") {
		t.Fatalf("unexpected retired credential: %q", credentials.ReporterToken)
	}
	if _, err := service.AuthenticateReporter(ctx, credentials.ReporterToken); !errors.Is(err, ErrReporterDisabled) {
		t.Fatalf("reporter auth err = %v", err)
	}
}

func TestInstallationReporterRotationInvalidatesOldToken(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	user := client.User.Create().SetUsername("bob").SetEmail("bob@example.org").SetAuthSource("ldap").SaveX(ctx)
	service := NewInstallationService(client, DefaultProtocolContract())
	credentials := mustEnsureInstallation(t, service, user.ID)
	enabled := true
	if _, err := service.SetEnabled(ctx, user.ID, credentials.InstallationID, &enabled); err != nil {
		t.Fatal(err)
	}
	rotated, err := service.Rotate(ctx, user.ID, credentials.InstallationID)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.ReporterToken == "" || rotated.ReporterToken == credentials.ReporterToken {
		t.Fatalf("rotated credentials = %+v", rotated)
	}
	if _, err := service.AuthenticateReporter(ctx, credentials.ReporterToken); err == nil {
		t.Fatal("old reporter token remained valid")
	}
	if _, err := service.AuthenticateReporter(ctx, rotated.ReporterToken); err != nil {
		t.Fatalf("new reporter token: %v", err)
	}
}

func TestInstallationIdentityDoesNotCollapseMatchingLabels(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	user := client.User.Create().SetUsername("carol").SetEmail("carol@example.net").SetAuthSource("ldap").SaveX(ctx)
	service := NewInstallationService(client, DefaultProtocolContract())
	for range 2 {
		if _, err := service.Ensure(ctx, user.ID, uuid.NewString(), "developer-mac", "test"); err != nil {
			t.Fatal(err)
		}
	}
	if count := client.ReportingInstallation.Query().CountX(ctx); count != 2 {
		t.Fatalf("reporting installations = %d, want 2", count)
	}
}

func mustEnsureInstallation(t *testing.T, service *InstallationService, userID int) InstallationCredentials {
	t.Helper()
	credentials, err := service.Ensure(context.Background(), userID, uuid.NewString(), "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	return credentials
}
