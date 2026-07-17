package adminsubscription

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/adminsubscriptionjob"
	"github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/testdb"
)

type fakeSubscriptionOperator struct {
	assignCalls []subscriptionCall
	extendCalls []subscriptionCall
	removeCalls []subscriptionCall
	resetCalls  []subscriptionCall
	failUserIDs map[int64]error
}

type subscriptionCall struct {
	Operation string
	UserID    int64
	GroupID   int64
	Days      int
}

type currentFilterResolverCall struct {
	filter CurrentFilter
	limit  int
}

type recordingCurrentFilterResolver struct {
	calls []currentFilterResolverCall
	users []*ent.User
	err   error
}

func (r *recordingCurrentFilterResolver) ResolveCurrentFilterTargets(_ context.Context, filter CurrentFilter, limit int) ([]*ent.User, error) {
	r.calls = append(r.calls, currentFilterResolverCall{filter: filter, limit: limit})
	return r.users, r.err
}

func (f *fakeSubscriptionOperator) AssignSubscriptionForUser(ctx context.Context, userID, groupID int64, validityDays int) error {
	f.assignCalls = append(f.assignCalls, subscriptionCall{Operation: "add", UserID: userID, GroupID: groupID, Days: validityDays})
	return f.errFor(userID)
}

func (f *fakeSubscriptionOperator) ExtendSubscriptionForUser(ctx context.Context, userID, groupID int64, days int) error {
	f.extendCalls = append(f.extendCalls, subscriptionCall{Operation: "extend", UserID: userID, GroupID: groupID, Days: days})
	return f.errFor(userID)
}

func (f *fakeSubscriptionOperator) RemoveSubscriptionForUser(ctx context.Context, userID, groupID int64) error {
	f.removeCalls = append(f.removeCalls, subscriptionCall{Operation: "remove", UserID: userID, GroupID: groupID})
	return f.errFor(userID)
}

func (f *fakeSubscriptionOperator) ResetSubscriptionQuotaForUser(ctx context.Context, userID, groupID int64) error {
	f.resetCalls = append(f.resetCalls, subscriptionCall{Operation: "reset_quota", UserID: userID, GroupID: groupID})
	return f.errFor(userID)
}

func (f *fakeSubscriptionOperator) errFor(userID int64) error {
	if f.failUserIDs == nil {
		return nil
	}
	return f.failUserIDs[userID]
}

type blockingSubscriptionOperator struct{}

func (blockingSubscriptionOperator) AssignSubscriptionForUser(ctx context.Context, userID, groupID int64, validityDays int) error {
	<-ctx.Done()
	return ctx.Err()
}

func (blockingSubscriptionOperator) ExtendSubscriptionForUser(ctx context.Context, userID, groupID int64, days int) error {
	<-ctx.Done()
	return ctx.Err()
}

func (blockingSubscriptionOperator) RemoveSubscriptionForUser(ctx context.Context, userID, groupID int64) error {
	<-ctx.Done()
	return ctx.Err()
}

func (blockingSubscriptionOperator) ResetSubscriptionQuotaForUser(ctx context.Context, userID, groupID int64) error {
	<-ctx.Done()
	return ctx.Err()
}

type slowSubscriptionOperator struct {
	delay       time.Duration
	assignCalls []subscriptionCall
}

func (s *slowSubscriptionOperator) AssignSubscriptionForUser(ctx context.Context, userID, groupID int64, validityDays int) error {
	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
		return ctx.Err()
	}
	s.assignCalls = append(s.assignCalls, subscriptionCall{Operation: "add", UserID: userID, GroupID: groupID, Days: validityDays})
	return nil
}

func (s *slowSubscriptionOperator) ExtendSubscriptionForUser(ctx context.Context, userID, groupID int64, days int) error {
	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (s *slowSubscriptionOperator) RemoveSubscriptionForUser(ctx context.Context, userID, groupID int64) error {
	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (s *slowSubscriptionOperator) ResetSubscriptionQuotaForUser(ctx context.Context, userID, groupID int64) error {
	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func TestStartJobSnapshotsSelectedUsersWithoutRelayMutation(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	alice := createAdminSubscriptionUser(t, ctx, client, "alice", 101)
	bob := createAdminSubscriptionUser(t, ctx, client, "bob", 102)
	operator := &fakeSubscriptionOperator{}
	svc := NewService(client)

	job, err := svc.StartJob(ctx, StartJobRequest{
		Scope:        "selected",
		UserIDs:      []int{bob.ID, alice.ID},
		Operation:    "add",
		ProviderID:   7,
		GroupID:      "42",
		ValidityDays: 30,
	})
	if err != nil {
		t.Fatalf("StartJob error: %v", err)
	}
	if job.Status != adminsubscriptionjob.StatusQueued || job.Phase != adminsubscriptionjob.PhaseQueued {
		t.Fatalf("job status=%s phase=%s, want queued/queued", job.Status, job.Phase)
	}
	if job.TotalCount != 2 || job.ProcessedCount != 0 {
		t.Fatalf("counts total=%d processed=%d, want 2/0", job.TotalCount, job.ProcessedCount)
	}
	if got := job.TargetUserIds; len(got) != 2 || got[0] != bob.ID || got[1] != alice.ID {
		t.Fatalf("target_user_ids = %v, want [%d %d]", got, bob.ID, alice.ID)
	}
	snapshots := TargetSnapshotsFromJob(job)
	if len(snapshots) != 2 || snapshots[0].UserID != bob.ID || snapshots[0].RelayUserID == nil || *snapshots[0].RelayUserID != 102 || snapshots[1].UserID != alice.ID {
		t.Fatalf("target snapshots = %+v, want bob then alice with relay IDs", snapshots)
	}
	if len(operator.assignCalls) != 0 {
		t.Fatalf("assign calls = %d, want 0 before RunJob", len(operator.assignCalls))
	}
}

func TestStartJobCurrentFilterResolverSnapshotsOrderedTargets(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	alice := createAdminSubscriptionUser(t, ctx, client, "alice", 801)
	bob := createAdminSubscriptionUser(t, ctx, client, "bob", 802)
	resolver := &recordingCurrentFilterResolver{users: []*ent.User{bob, alice}}
	svc := NewService(client, resolver)

	job, err := svc.StartJob(ctx, StartJobRequest{
		Scope:        "current_filter",
		FilterQuery:  "  Example Query  ",
		DepartmentID: "  dept-alpha  ",
		AccessStatus: "  configured  ",
		Operation:    "add",
		ProviderID:   7,
		GroupID:      "42",
		ValidityDays: 30,
	})
	if err != nil {
		t.Fatalf("StartJob error: %v", err)
	}
	if len(resolver.calls) != 1 {
		t.Fatalf("resolver calls = %+v, want one", resolver.calls)
	}
	if got := resolver.calls[0]; got.filter.Query != "Example Query" || got.filter.DepartmentID != "dept-alpha" || got.filter.AccessStatus != "configured" || got.limit != MaxTargets+1 {
		t.Fatalf("resolver call = %+v, want trimmed filter and limit %d", got, MaxTargets+1)
	}
	if got := job.TargetUserIds; len(got) != 2 || got[0] != bob.ID || got[1] != alice.ID {
		t.Fatalf("target_user_ids = %v, want resolver order [%d %d]", got, bob.ID, alice.ID)
	}
	snapshots := TargetSnapshotsFromJob(job)
	if len(snapshots) != 2 || snapshots[0].UserID != bob.ID || snapshots[0].Username != "bob" || snapshots[0].RelayUserID == nil || *snapshots[0].RelayUserID != 802 || snapshots[1].UserID != alice.ID {
		t.Fatalf("target snapshots = %+v, want immutable bob then alice snapshots", snapshots)
	}
	if _, err := client.User.UpdateOneID(bob.ID).SetUsername("bob-updated").SetEmail("bob-updated@example.org").SetRelayUserID(999).Save(ctx); err != nil {
		t.Fatalf("update resolver user after job creation: %v", err)
	}
	loaded, err := svc.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob error: %v", err)
	}
	loadedSnapshots := TargetSnapshotsFromJob(loaded)
	if len(loadedSnapshots) != 2 || loadedSnapshots[0].Username != "bob" || loadedSnapshots[0].Email != "bob@example.com" || loadedSnapshots[0].RelayUserID == nil || *loadedSnapshots[0].RelayUserID != 802 {
		t.Fatalf("persisted target snapshots changed with user row: %+v", loadedSnapshots)
	}
}

func TestStartJobCurrentFilterResolverReturnsErrorBeforeCreate(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	resolverErr := errors.New("target resolver unavailable")
	resolver := &recordingCurrentFilterResolver{err: resolverErr}
	svc := NewService(client, resolver)

	_, err := svc.StartJob(ctx, StartJobRequest{
		Scope:        "current_filter",
		Operation:    "add",
		ProviderID:   7,
		GroupID:      "42",
		ValidityDays: 30,
	})
	if !errors.Is(err, resolverErr) {
		t.Fatalf("StartJob error = %v, want resolver error", err)
	}
	if count := client.AdminSubscriptionJob.Query().CountX(ctx); count != 0 {
		t.Fatalf("job count = %d, want zero after resolver failure", count)
	}
}

func TestStartJobCurrentFilterResolverRequiresConfiguration(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	svc := NewService(client)

	_, err := svc.StartJob(ctx, StartJobRequest{
		Scope:        "current_filter",
		Operation:    "add",
		ProviderID:   7,
		GroupID:      "42",
		ValidityDays: 30,
	})
	if err == nil || !strings.Contains(err.Error(), "current filter target resolver is not configured") {
		t.Fatalf("StartJob error = %v, want resolver configuration error", err)
	}
	if count := client.AdminSubscriptionJob.Query().CountX(ctx); count != 0 {
		t.Fatalf("job count = %d, want zero without resolver", count)
	}
}

func TestStartJobCurrentFilterResolverRejectsOversizedTargets(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	users := make([]*ent.User, MaxTargets+1)
	for i := range users {
		users[i] = &ent.User{ID: i + 1, Username: fmt.Sprintf("target-%03d", i+1), Email: fmt.Sprintf("target-%03d@example.com", i+1)}
	}
	resolver := &recordingCurrentFilterResolver{users: users}
	svc := NewService(client, resolver)

	_, err := svc.StartJob(ctx, StartJobRequest{
		Scope:        "current_filter",
		Operation:    "add",
		ProviderID:   7,
		GroupID:      "42",
		ValidityDays: 30,
	})
	var tooMany *TooManyTargetsError
	if !errors.As(err, &tooMany) || tooMany.Max != MaxTargets {
		t.Fatalf("StartJob error = %v, want TooManyTargetsError(%d)", err, MaxTargets)
	}
	if len(resolver.calls) != 1 || resolver.calls[0].limit != MaxTargets+1 {
		t.Fatalf("resolver calls = %+v, want one limit %d call", resolver.calls, MaxTargets+1)
	}
	if count := client.AdminSubscriptionJob.Query().CountX(ctx); count != 0 {
		t.Fatalf("job count = %d, want zero for oversized resolver result", count)
	}
}

func TestStartJobOtherScopesRemainResolverOptional(t *testing.T) {
	t.Run("selected preserves requested order and missing snapshots", func(t *testing.T) {
		client := testdb.Open(t)
		defer client.Close()
		ctx := context.Background()
		alice := createAdminSubscriptionUser(t, ctx, client, "alice", 801)
		missingID := alice.ID + 1000

		job, err := NewService(client).StartJob(ctx, StartJobRequest{
			Scope:        "selected",
			UserIDs:      []int{missingID, alice.ID, missingID},
			Operation:    "add",
			ProviderID:   7,
			GroupID:      "42",
			ValidityDays: 30,
		})
		if err != nil {
			t.Fatalf("StartJob selected: %v", err)
		}
		if got := job.TargetUserIds; len(got) != 2 || got[0] != missingID || got[1] != alice.ID {
			t.Fatalf("selected target ids = %v, want [%d %d]", got, missingID, alice.ID)
		}
		snapshots := TargetSnapshotsFromJob(job)
		if len(snapshots) != 2 || snapshots[0].UserID != missingID || !snapshots[0].Missing || snapshots[1].UserID != alice.ID || snapshots[1].Missing {
			t.Fatalf("selected snapshots = %+v, want missing then alice", snapshots)
		}
	})

	t.Run("all mapped remains id ordered", func(t *testing.T) {
		client := testdb.Open(t)
		defer client.Close()
		ctx := context.Background()
		alice := createAdminSubscriptionUser(t, ctx, client, "alice", 801)
		createAdminSubscriptionUser(t, ctx, client, "bob", 0)
		carol := createAdminSubscriptionUser(t, ctx, client, "carol", 803)

		job, err := NewService(client).StartJob(ctx, StartJobRequest{
			Scope:        "all_mapped",
			Operation:    "add",
			ProviderID:   7,
			GroupID:      "42",
			ValidityDays: 30,
		})
		if err != nil {
			t.Fatalf("StartJob all_mapped: %v", err)
		}
		if got := job.TargetUserIds; len(got) != 2 || got[0] != alice.ID || got[1] != carol.ID {
			t.Fatalf("all_mapped target ids = %v, want [%d %d]", got, alice.ID, carol.ID)
		}
	})
}

func TestRunJobUsesSnapshottedRelayUserID(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	mapped := createAdminSubscriptionUser(t, ctx, client, "mapped", 401)
	operator := &fakeSubscriptionOperator{}
	svc := NewService(client)
	job, err := svc.StartJob(ctx, StartJobRequest{
		Scope:        "selected",
		UserIDs:      []int{mapped.ID},
		Operation:    "add",
		ProviderID:   7,
		GroupID:      "42",
		ValidityDays: 30,
	})
	if err != nil {
		t.Fatalf("StartJob error: %v", err)
	}
	if _, err := client.User.UpdateOneID(mapped.ID).SetRelayUserID(999).Save(ctx); err != nil {
		t.Fatalf("update relay user id: %v", err)
	}

	if err := svc.RunJob(ctx, job.ID, operator); err != nil {
		t.Fatalf("RunJob error: %v", err)
	}
	if len(operator.assignCalls) != 1 || operator.assignCalls[0].UserID != 401 {
		t.Fatalf("assign calls = %+v, want snapshotted relay user 401", operator.assignCalls)
	}
	loaded, err := svc.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob error: %v", err)
	}
	results := ResultsFromJob(loaded)
	if len(results) != 1 || results[0].RelayUserID == nil || *results[0].RelayUserID != 401 {
		t.Fatalf("results = %+v, want snapshotted relay user 401", results)
	}
}

func TestRunJobSkipsUnmappedUsersAndContinues(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	mapped := createAdminSubscriptionUser(t, ctx, client, "mapped", 201)
	unmapped := createAdminSubscriptionUser(t, ctx, client, "unmapped", 0)
	operator := &fakeSubscriptionOperator{}
	svc := NewService(client)
	job, err := svc.StartJob(ctx, StartJobRequest{
		Scope:        "selected",
		UserIDs:      []int{mapped.ID, unmapped.ID},
		Operation:    "add",
		ProviderID:   7,
		GroupID:      "42",
		ValidityDays: 30,
	})
	if err != nil {
		t.Fatalf("StartJob error: %v", err)
	}

	if err := svc.RunJob(ctx, job.ID, operator); err != nil {
		t.Fatalf("RunJob error: %v", err)
	}
	loaded, err := svc.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob error: %v", err)
	}
	if loaded.Status != adminsubscriptionjob.StatusCompleted || loaded.Phase != adminsubscriptionjob.PhaseCompleted {
		t.Fatalf("job status=%s phase=%s, want completed/completed", loaded.Status, loaded.Phase)
	}
	if loaded.ProcessedCount != 2 || loaded.SuccessCount != 1 || loaded.SkippedCount != 1 || loaded.FailedCount != 0 {
		t.Fatalf("counts processed/success/skipped/failed = %d/%d/%d/%d, want 2/1/1/0", loaded.ProcessedCount, loaded.SuccessCount, loaded.SkippedCount, loaded.FailedCount)
	}
	if len(operator.assignCalls) != 1 || operator.assignCalls[0].UserID != 201 {
		t.Fatalf("assign calls = %+v, want one mapped relay user", operator.assignCalls)
	}
	results := ResultsFromJob(loaded)
	if len(results) != 2 || results[1].Status != "skipped" || !strings.Contains(results[1].Message, "not linked") {
		t.Fatalf("results = %+v, want skipped unmapped second row", results)
	}
}

func TestRunJobRecordsPerUserTimeoutAndCompletes(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	mapped := createAdminSubscriptionUser(t, ctx, client, "mapped", 501)
	svc := NewService(client)
	svc.perTargetTimeout = 10 * time.Millisecond
	job, err := svc.StartJob(ctx, StartJobRequest{
		Scope:        "selected",
		UserIDs:      []int{mapped.ID},
		Operation:    "add",
		ProviderID:   7,
		GroupID:      "42",
		ValidityDays: 30,
	})
	if err != nil {
		t.Fatalf("StartJob error: %v", err)
	}

	if err := svc.RunJob(ctx, job.ID, blockingSubscriptionOperator{}); err != nil {
		t.Fatalf("RunJob error: %v", err)
	}
	loaded, err := svc.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob error: %v", err)
	}
	if loaded.Status != adminsubscriptionjob.StatusCompleted || loaded.ProcessedCount != 1 || loaded.FailedCount != 1 {
		t.Fatalf("job status=%s processed=%d failed=%d, want completed 1/1", loaded.Status, loaded.ProcessedCount, loaded.FailedCount)
	}
	results := ResultsFromJob(loaded)
	if len(results) != 1 || results[0].Status != "failed" || !strings.Contains(results[0].Message, "deadline exceeded") {
		t.Fatalf("results = %+v, want per-user timeout failure", results)
	}
}

func TestRunJobDoesNotUseBaseJobTimeoutAsHardCapForMultipleTargets(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	alice := createAdminSubscriptionUser(t, ctx, client, "alice", 701)
	bob := createAdminSubscriptionUser(t, ctx, client, "bob", 702)
	svc := NewService(client)
	svc.jobTimeout = 20 * time.Millisecond
	svc.perTargetTimeout = 500 * time.Millisecond
	operator := &slowSubscriptionOperator{delay: 30 * time.Millisecond}
	job, err := svc.StartJob(ctx, StartJobRequest{
		Scope:        "selected",
		UserIDs:      []int{alice.ID, bob.ID},
		Operation:    "add",
		ProviderID:   7,
		GroupID:      "42",
		ValidityDays: 30,
	})
	if err != nil {
		t.Fatalf("StartJob error: %v", err)
	}

	if err := svc.RunJob(ctx, job.ID, operator); err != nil {
		t.Fatalf("RunJob error: %v", err)
	}
	if len(operator.assignCalls) != 2 {
		t.Fatalf("assign calls = %+v, want both targets processed", operator.assignCalls)
	}
	loaded, err := svc.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob error: %v", err)
	}
	if loaded.Status != adminsubscriptionjob.StatusCompleted || loaded.SuccessCount != 2 || loaded.FailedCount != 0 {
		t.Fatalf("job status=%s success=%d failed=%d, want completed 2/0", loaded.Status, loaded.SuccessCount, loaded.FailedCount)
	}
}

func TestGetLatestJobAbandonsStaleActiveJob(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	mapped := createAdminSubscriptionUser(t, ctx, client, "stale", 601)
	svc := NewService(client)
	svc.staleJobAfter = time.Millisecond
	job, err := svc.StartJob(ctx, StartJobRequest{
		Scope:        "selected",
		UserIDs:      []int{mapped.ID},
		Operation:    "add",
		ProviderID:   7,
		GroupID:      "42",
		ValidityDays: 30,
	})
	if err != nil {
		t.Fatalf("StartJob error: %v", err)
	}
	staleTime := time.Now().Add(-2 * time.Hour)
	if _, err := client.AdminSubscriptionJob.UpdateOneID(job.ID).
		SetStatus(adminsubscriptionjob.StatusRunning).
		SetPhase(adminsubscriptionjob.PhaseProcessing).
		SetUpdatedAt(staleTime).
		Save(ctx); err != nil {
		t.Fatalf("make job stale: %v", err)
	}

	latest, err := svc.GetLatestJob(ctx)
	if err != nil {
		t.Fatalf("GetLatestJob error: %v", err)
	}
	if latest.ID != job.ID || latest.Status != adminsubscriptionjob.StatusAbandoned || latest.Phase != adminsubscriptionjob.PhaseFailed {
		t.Fatalf("latest job id/status/phase = %d/%s/%s, want abandoned stale job %d", latest.ID, latest.Status, latest.Phase, job.ID)
	}
	if latest.LastError == nil || !strings.Contains(*latest.LastError, "abandoned") {
		t.Fatalf("last_error = %v, want abandoned message", latest.LastError)
	}
}

func TestRunJobRecordsPerUserFailures(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	alice := createAdminSubscriptionUser(t, ctx, client, "alice", 301)
	bob := createAdminSubscriptionUser(t, ctx, client, "bob", 302)
	operator := &fakeSubscriptionOperator{failUserIDs: map[int64]error{302: errors.New("relay rejected subscription")}}
	svc := NewService(client)
	job, err := svc.StartJob(ctx, StartJobRequest{
		Scope:        "selected",
		UserIDs:      []int{alice.ID, bob.ID},
		Operation:    "add",
		ProviderID:   7,
		GroupID:      "42",
		ValidityDays: 30,
	})
	if err != nil {
		t.Fatalf("StartJob error: %v", err)
	}

	if err := svc.RunJob(ctx, job.ID, operator); err != nil {
		t.Fatalf("RunJob error: %v", err)
	}
	loaded, err := svc.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob error: %v", err)
	}
	if loaded.ProcessedCount != 2 || loaded.SuccessCount != 1 || loaded.FailedCount != 1 {
		t.Fatalf("counts processed/success/failed = %d/%d/%d, want 2/1/1", loaded.ProcessedCount, loaded.SuccessCount, loaded.FailedCount)
	}
	results := ResultsFromJob(loaded)
	if len(results) != 2 || results[1].Status != "failed" || results[1].Message != "relay rejected subscription" {
		t.Fatalf("results = %+v, want failed relay row", results)
	}
}

func TestRunJobResetsSubscriptionQuotaForSelectedUsers(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	alice := createAdminSubscriptionUser(t, ctx, client, "alice", 301)
	bob := createAdminSubscriptionUser(t, ctx, client, "bob", 302)
	operator := &fakeSubscriptionOperator{}
	svc := NewService(client)
	job, err := svc.StartJob(ctx, StartJobRequest{
		Scope:      "selected",
		UserIDs:    []int{alice.ID, bob.ID},
		Operation:  "reset_quota",
		ProviderID: 7,
		GroupID:    "42",
	})
	if err != nil {
		t.Fatalf("StartJob error: %v", err)
	}

	if err := svc.RunJob(ctx, job.ID, operator); err != nil {
		t.Fatalf("RunJob error: %v", err)
	}

	if len(operator.resetCalls) != 2 {
		t.Fatalf("reset calls = %+v, want two selected users", operator.resetCalls)
	}
	if operator.resetCalls[0].UserID != 301 || operator.resetCalls[0].GroupID != 42 || operator.resetCalls[1].UserID != 302 || operator.resetCalls[1].GroupID != 42 {
		t.Fatalf("reset calls = %+v, want relay users 301/302 group 42", operator.resetCalls)
	}
	loaded, err := svc.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob error: %v", err)
	}
	if loaded.ProcessedCount != 2 || loaded.SuccessCount != 2 || loaded.FailedCount != 0 {
		t.Fatalf("counts processed/success/failed = %d/%d/%d, want 2/2/0", loaded.ProcessedCount, loaded.SuccessCount, loaded.FailedCount)
	}
}

func TestStartJobRejectsOversizedTargetsBeforeCreate(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	userIDs := make([]int, 0, MaxTargets+1)
	for i := 0; i < MaxTargets+1; i++ {
		u := createAdminSubscriptionUser(t, ctx, client, fmt.Sprintf("bulk-%03d", i), 1000+i)
		userIDs = append(userIDs, u.ID)
	}
	svc := NewService(client)

	_, err := svc.StartJob(ctx, StartJobRequest{
		Scope:        "selected",
		UserIDs:      userIDs,
		Operation:    "add",
		ProviderID:   7,
		GroupID:      "42",
		ValidityDays: 30,
	})
	if err == nil || !strings.Contains(err.Error(), "maximum is 500") {
		t.Fatalf("StartJob err = %v, want maximum error", err)
	}
	count, countErr := client.AdminSubscriptionJob.Query().Count(ctx)
	if countErr != nil {
		t.Fatalf("count jobs: %v", countErr)
	}
	if count != 0 {
		t.Fatalf("job count = %d, want 0", count)
	}
}

func createAdminSubscriptionUser(t *testing.T, ctx context.Context, client *ent.Client, username string, relayUserID int) *ent.User {
	t.Helper()
	create := client.User.Create().
		SetUsername(username).
		SetEmail(username + "@example.com").
		SetAuthSource(user.AuthSourceLdap).
		SetRole(user.RoleUser)
	if relayUserID > 0 {
		create.SetRelayUserID(relayUserID)
	}
	u, err := create.Save(ctx)
	if err != nil {
		t.Fatalf("create user %s: %v", username, err)
	}
	return u
}
