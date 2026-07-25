package directorysync

import (
	"context"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/directoryoffboardingaction"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestDirectorySyncSchemaPersistsFactsAndRevocationFloor(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	user := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRole("user").
		SetRelayUserID(42).
		SetTokenValidAfter(now).
		SaveX(ctx)
	if user.TokenValidAfter == nil || !user.TokenValidAfter.Equal(now) {
		t.Fatalf("token_valid_after = %v, want %v", user.TokenValidAfter, now)
	}

	source := client.DirectorySource.Create().
		SetName("Example Directory").
		SetDescription("Synthetic directory source").
		SetScope("full_company").
		SetEnabled(true).
		SetDsl("version: 1\nscope: full_company\nsteps: []\n").
		SetScheduleEnabled(true).
		SetScheduleInterval("daily").
		SetScheduleTimezone("UTC").
		SaveX(ctx)

	run := client.DirectorySyncRun.Create().
		SetSourceID(source.ID).
		SetMode("apply").
		SetTrigger("manual").
		SetStatus("completed").
		SetPhase("completed").
		SetStartedAt(now).
		SetCompletedAt(now).
		SetHTTPRequestCount(2).
		SetDepartmentCount(1).
		SetMemberCount(1).
		SetInvalidMemberCount(0).
		SetWarningCount(0).
		SetWarnings([]map[string]any{}).
		SetSummary(map[string]any{"members": 1}).
		SetPreviewDiff(map[string]any{"creates": 1}).
		SaveX(ctx)

	source = source.Update().
		SetLastRunID(run.ID).
		SetLastSuccessfulRunID(run.ID).
		SaveX(ctx)
	if source.LastRunID == nil || *source.LastRunID != run.ID {
		t.Fatalf("last_run_id = %v, want %d", source.LastRunID, run.ID)
	}
	if source.LastSuccessfulRunID == nil || *source.LastSuccessfulRunID != run.ID {
		t.Fatalf("last_successful_run_id = %v, want %d", source.LastSuccessfulRunID, run.ID)
	}

	department := client.DirectoryDepartment.Create().
		SetSourceID(source.ID).
		SetExternalID("dept-alpha").
		SetParentExternalID("dept-root").
		SetEffectiveParentExternalID("dept-root").
		SetName("Department Alpha").
		SetPath("Department Alpha").
		SetMetadata(map[string]any{"synthetic": true}).
		SetLastSeenRunID(run.ID).
		SaveX(ctx)

	member := client.DirectoryMember.Create().
		SetSourceID(source.ID).
		SetExternalID("member-alice").
		SetEmailNormalized("alice@example.com").
		SetDisplayName("Alice Example").
		SetDepartmentExternalID(department.ExternalID).
		SetStatus("active").
		SetMetadata(map[string]any{"source": "synthetic"}).
		SetMatchedUserID(user.ID).
		SetLastSeenRunID(run.ID).
		SaveX(ctx)

	membership := client.DirectoryMemberDepartment.Create().
		SetSourceID(source.ID).
		SetDirectoryMemberID(member.ID).
		SetMemberExternalID(member.ExternalID).
		SetMemberEmailNormalized(member.EmailNormalized).
		SetDepartmentExternalID(department.ExternalID).
		SetLastSeenRunID(run.ID).
		SaveX(ctx)

	action := client.DirectoryOffboardingAction.Create().
		SetSourceID(source.ID).
		SetUserID(user.ID).
		SetRelayUserID(42).
		SetDirectoryRunID(run.ID).
		SetAction(directoryoffboardingaction.ActionDisableRelayUser).
		SetStatus(directoryoffboardingaction.StatusSucceeded).
		SetReason("missing_from_latest_full_company_directory").
		SetPerformedByUserID(user.ID).
		SaveX(ctx)

	department = client.DirectoryDepartment.GetX(ctx, department.ID)
	if department.ParentExternalID == nil || *department.ParentExternalID != "dept-root" ||
		department.EffectiveParentExternalID == nil || *department.EffectiveParentExternalID != "dept-root" {
		t.Fatalf("department parents = raw:%v effective:%v, want dept-root/dept-root", department.ParentExternalID, department.EffectiveParentExternalID)
	}
	if department.ExternalID != "dept-alpha" || member.EmailNormalized != "alice@example.com" || membership.DepartmentExternalID != department.ExternalID || action.UserID != user.ID {
		t.Fatalf("persisted directory schema rows are inconsistent")
	}
}
