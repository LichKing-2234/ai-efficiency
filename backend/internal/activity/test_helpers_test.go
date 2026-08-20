package activity

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/readcache"
)

type activityMemoryStore struct {
	mu     sync.Mutex
	values map[string][]byte
}

func (s *activityMemoryStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[key]
	if !ok {
		return nil, readcache.ErrMiss
	}
	return append([]byte(nil), value...), nil
}

func (s *activityMemoryStore) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = append([]byte(nil), value...)
	return nil
}

func (s *activityMemoryStore) TryAcquireLease(context.Context, string, string, time.Duration) (bool, error) {
	return true, nil
}

func (s *activityMemoryStore) LeaseTTL(context.Context, string) (time.Duration, error) {
	return time.Minute, nil
}

func (s *activityMemoryStore) ReleaseLease(context.Context, string, string) (bool, error) {
	return true, nil
}

func createActivityDirectoryScope(t *testing.T, client *ent.Client, representative, member, ordinary, outside, admin *ent.User) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	source := client.DirectorySource.Create().SetName("company").SetEnabled(true).SetDsl("version: 1").SaveX(ctx)
	run := client.DirectorySyncRun.Create().SetSourceID(source.ID).SetMode("apply").SetStatus("completed").SetPhase("completed").SetCompletedAt(now).SaveX(ctx)
	client.DirectorySource.UpdateOne(source).SetLastSuccessfulRunID(run.ID).SetLastRunID(run.ID).ExecX(ctx)
	client.DirectoryDepartment.Create().SetSourceID(source.ID).SetExternalID("team-root").SetName("Team Root").SetPath("Team Root").SetLastSeenRunID(run.ID).
		SetMetadata(map[string]any{"representative_external_ids": []string{"member-representative"}}).SaveX(ctx)
	client.DirectoryDepartment.Create().SetSourceID(source.ID).SetExternalID("team-child").SetParentExternalID("team-root").SetEffectiveParentExternalID("team-root").SetName("Team Child").SetPath("Team Root / Team Child").SetLastSeenRunID(run.ID).SaveX(ctx)
	client.DirectoryDepartment.Create().SetSourceID(source.ID).SetExternalID("team-outside").SetName("Outside").SetPath("Outside").SetLastSeenRunID(run.ID).SaveX(ctx)
	for _, item := range []struct {
		externalID string
		department string
		user       *ent.User
	}{
		{"member-representative", "team-root", representative},
		{"member-target", "team-child", member},
		{"member-ordinary", "team-child", ordinary},
		{"member-outside", "team-outside", outside},
		{"member-admin", "team-root", admin},
	} {
		directoryMember := client.DirectoryMember.Create().SetSourceID(source.ID).SetExternalID(item.externalID).
			SetEmailNormalized(item.user.Email).SetDisplayName(item.user.Username).SetDepartmentExternalID(item.department).
			SetStatus("active").SetMatchedUserID(item.user.ID).SetLastSeenRunID(run.ID).SaveX(ctx)
		client.DirectoryMemberDepartment.Create().SetSourceID(source.ID).SetDirectoryMemberID(directoryMember.ID).
			SetMemberExternalID(item.externalID).SetMemberEmailNormalized(item.user.Email).SetDepartmentExternalID(item.department).SetLastSeenRunID(run.ID).SaveX(ctx)
	}
}
