package directoryfacts

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestReaderReturnsNoCurrentViewWithoutSuccessfulSnapshot(t *testing.T) {
	reader := New(testdb.Open(t))
	view, ok, err := reader.Current(context.Background())
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if ok || view != nil {
		t.Fatalf("Current() = %v/%v, want no current snapshot", view, ok)
	}
}

func TestReaderUsesLatestSuccessfulSnapshotAndScopesEveryFactToItsRun(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	oldSource, oldRun := createReaderSnapshot(t, client, "Old Directory", time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC))
	newSource, newRun := createReaderSnapshot(t, client, "New Directory", time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC))

	client.DirectoryDepartment.Create().SetSourceID(oldSource.ID).SetExternalID("department-old").SetName("Old").SetLastSeenRunID(oldRun.ID).SaveX(ctx)
	root := client.DirectoryDepartment.Create().SetSourceID(newSource.ID).SetExternalID("department-root").SetName("Root").SetLastSeenRunID(newRun.ID).
		SetMetadata(map[string]any{DepartmentRepresentativeIDsKey: []string{"member-alice"}}).SaveX(ctx)
	client.DirectoryDepartment.Create().SetSourceID(newSource.ID).SetExternalID("department-child").SetName("Child").SetEffectiveParentExternalID(root.ExternalID).SetLastSeenRunID(newRun.ID).SaveX(ctx)
	user := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource(entuser.AuthSourceLdap).SetRole(entuser.RoleUser).SaveX(ctx)
	member := client.DirectoryMember.Create().SetSourceID(newSource.ID).SetExternalID("member-alice").SetEmailNormalized("alice@example.com").SetDisplayName("Alice").SetDepartmentExternalID(root.ExternalID).SetMatchedUserID(user.ID).SetLastSeenRunID(newRun.ID).SaveX(ctx)
	client.DirectoryMemberDepartment.Create().SetSourceID(newSource.ID).SetDirectoryMemberID(member.ID).SetMemberExternalID(member.ExternalID).SetMemberEmailNormalized(member.EmailNormalized).SetDepartmentExternalID("department-child").SetLastSeenRunID(newRun.ID).SaveX(ctx)

	view, ok, err := New(client).Current(ctx)
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if !ok || view.Snapshot() != (Snapshot{SourceID: newSource.ID, RunID: newRun.ID}) {
		t.Fatalf("current snapshot = %+v/%v, want source/run %d/%d", view.Snapshot(), ok, newSource.ID, newRun.ID)
	}
	facts, err := view.Load(ctx, Query{AllDepartments: true, AllMembers: true, IncludeMemberships: true, AllUsers: true})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got, want := departmentIDs(facts.Departments()), []string{"department-root", "department-child"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("current departments = %#v, want %#v", got, want)
	}
	if got, want := facts.DepartmentIDsForMember(facts.Members()[0]), []string{"department-child"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("current memberships = %#v, want %#v", got, want)
	}
	if got := facts.UserForMember(facts.Members()[0]); got == nil || got.ID != user.ID {
		t.Fatalf("current local-user match = %+v, want user %d", got, user.ID)
	}
	if got, want := facts.RepresentativeRoots(member.ExternalID), []string{root.ExternalID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("current representative roots = %#v, want %#v", got, want)
	}
}

func TestCurrentViewSupportsConcurrentRequestScopedReads(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	source, run := createReaderSnapshot(t, client, "Example Directory", time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC))
	client.DirectoryDepartment.Create().SetSourceID(source.ID).SetExternalID("department-alpha").SetName("Department Alpha").SetLastSeenRunID(run.ID).SaveX(ctx)
	view, ok, err := New(client).Current(ctx)
	if err != nil || !ok {
		t.Fatalf("Current() = %v/%v, error %v", view, ok, err)
	}

	const readers = 8
	var wait sync.WaitGroup
	errors := make(chan error, readers)
	for range readers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			facts, loadErr := view.Load(ctx, Query{AllDepartments: true})
			if loadErr != nil {
				errors <- loadErr
				return
			}
			if got := departmentIDs(facts.Departments()); !reflect.DeepEqual(got, []string{"department-alpha"}) {
				errors <- fmt.Errorf("departments = %#v", got)
			}
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Errorf("concurrent read: %v", err)
	}
}

func createReaderSnapshot(t *testing.T, client *ent.Client, name string, completedAt time.Time) (*ent.DirectorySource, *ent.DirectorySyncRun) {
	t.Helper()
	ctx := context.Background()
	source := client.DirectorySource.Create().SetName(name).SetEnabled(true).SetDsl("version: 1").SaveX(ctx)
	run := client.DirectorySyncRun.Create().SetSourceID(source.ID).SetMode("apply").SetStatus("completed").SetPhase("completed").SetCompletedAt(completedAt).SaveX(ctx)
	client.DirectorySource.UpdateOne(source).SetLastSuccessfulRunID(run.ID).SetLastRunID(run.ID).ExecX(ctx)
	return source, run
}
