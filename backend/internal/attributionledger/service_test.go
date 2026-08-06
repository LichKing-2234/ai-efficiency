package attributionledger

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/checkpoint"
	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/repo"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

func TestCompactBucketReplayRevisionAndConservation(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	user, err := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	repoRow, err := client.RepoConfig.Create().
		SetName("repo-a").
		SetFullName("example/repo-a").
		SetCloneURL("https://example.com/example/repo-a.git").
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	checkpointService := checkpoint.NewService(client, checkpoint.ServiceOptions{
		RepoService: repo.NewService(client, "", zap.NewNop()),
	})
	observed := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	captured := observed.Add(time.Minute)
	if err := checkpointService.RecordCheckpointForUser(ctx, user.ID, checkpoint.CommitCheckpointRequest{
		EventID:       "checkpoint-commit-old",
		RepoConfigID:  repoRow.ID,
		WorkspaceID:   "workspace-a",
		CommitSHA:     "commit-old",
		BindingSource: "manual",
		CapturedAt:    &captured,
	}); err != nil {
		t.Fatal(err)
	}

	installations := NewInstallationService(client)
	installationID := uuid.NewString()
	credentials, err := installations.Ensure(ctx, user.ID, installationID, "test machine", "test")
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	if _, err := installations.SetEnabled(ctx, user.ID, installationID, &enabled, nil); err != nil {
		t.Fatal(err)
	}
	principal, err := installations.AuthenticateReporter(ctx, credentials.ReporterToken)
	if err != nil {
		t.Fatal(err)
	}

	tokens := Tokens{FreshInput: 70, CacheRead: 30, Output: 40, Reasoning: 15, ProviderTotal: 140, Processed: 140}
	bucket := UsageBucket{
		SchemaVersion: CurrentSchemaVersion,
		BucketID:      "bucket-a",
		Tool:          "codex",
		Model:         "gpt-test",
		SessionSlices: []SessionSlice{{
			ConversationID: "conversation-a",
			ObservedStart:  observed,
			ObservedEnd:    observed.Add(30 * time.Second),
			TokenAtomCount: 4,
			AtomSetDigest:  "atoms-a",
		}},
		ObservedStart:        observed,
		ObservedEnd:          observed.Add(30 * time.Second),
		Tokens:               tokens,
		RequestCount:         2,
		SourceEventCount:     4,
		SourceDigest:         "source-a",
		ExtractorVersion:     "test",
		NormalizationVersion: 1,
		TokenQuality:         "measured",
		InitialRevision: AllocationRevision{
			RevisionID:      "revision-a-1",
			Sequence:        1,
			Reason:          "checkpoint closed bucket",
			EvidenceVersion: "test",
			Allocations: []Allocation{{
				Target: AllocationTarget{Status: "bound_auto", RepoConfigID: repoRow.ID, RepoKey: repoRow.RepoKey, WorkspaceID: "workspace-a", CommitSHA: "commit-old"},
				Tokens: tokens,
			}},
		},
	}
	first, err := NewService(client, nil).CreateBuckets(ctx, principal, BatchRequest{Buckets: []UsageBucket{bucket}})
	if err != nil {
		t.Fatal(err)
	}
	if first.CreatedBuckets != 1 || first.CreatedRevisions != 1 {
		t.Fatalf("first result = %+v", first)
	}
	replay, err := NewService(client, nil).CreateBuckets(ctx, principal, BatchRequest{Buckets: []UsageBucket{bucket}})
	if err != nil {
		t.Fatal(err)
	}
	if replay.DuplicateBuckets != 1 || replay.DuplicateRevisions != 1 {
		t.Fatalf("replay result = %+v", replay)
	}
	if count, _ := client.AttributionUsageBucket.Query().Count(ctx); count != 1 {
		t.Fatalf("bucket rows = %d, want 1", count)
	}
	if count, _ := client.AttributionAllocationRevision.Query().Count(ctx); count != 1 {
		t.Fatalf("revision rows = %d, want 1", count)
	}
	rewriteCaptured := observed.Add(2 * time.Minute)
	if err := checkpointService.RecordRewriteForUser(ctx, user.ID, checkpoint.CommitRewriteRequest{
		EventID:       "rewrite-old-to-new",
		RepoConfigID:  repoRow.ID,
		WorkspaceID:   "workspace-a",
		RewriteType:   "rebase",
		OldCommitSHA:  "commit-old",
		NewCommitSHA:  "commit-rewritten",
		BindingSource: "manual",
		CapturedAt:    &rewriteCaptured,
	}); err != nil {
		t.Fatal(err)
	}

	created, err := NewService(client, nil).CreateRevision(ctx, principal, bucket.BucketID, RevisionRequest{
		SchemaVersion: CurrentSchemaVersion,
		AllocationRevision: AllocationRevision{
			RevisionID:      "revision-a-2",
			Sequence:        2,
			Reason:          "commit rewritten",
			EvidenceVersion: "test",
			Allocations: []Allocation{{
				Target: AllocationTarget{Status: "bound_auto", RepoConfigID: repoRow.ID, RepoKey: repoRow.RepoKey, WorkspaceID: "workspace-a", CommitSHA: "commit-rewritten", Lineage: "rewrite"},
				Tokens: tokens,
			}},
		},
	})
	if err != nil || !created {
		t.Fatalf("create revision: created=%v err=%v", created, err)
	}
	cherryCaptured := rewriteCaptured.Add(time.Minute)
	if err := checkpointService.RecordCheckpointForUser(ctx, user.ID, checkpoint.CommitCheckpointRequest{
		EventID: "checkpoint-cherry", RepoConfigID: repoRow.ID, WorkspaceID: "workspace-a",
		CommitSHA: "commit-cherry", BindingSource: "manual", CapturedAt: &cherryCaptured,
	}); err != nil {
		t.Fatal(err)
	}
	created, err = NewService(client, nil).CreateRevision(ctx, principal, bucket.BucketID, RevisionRequest{
		SchemaVersion: CurrentSchemaVersion,
		AllocationRevision: AllocationRevision{
			RevisionID: "revision-a-3", Sequence: 3, Reason: "cherry-pick inherited usage", EvidenceVersion: "test",
			Allocations: []Allocation{{
				Target: AllocationTarget{
					Status: "bound_auto", RepoConfigID: repoRow.ID, RepoKey: repoRow.RepoKey, WorkspaceID: "workspace-a", CommitSHA: "commit-rewritten", Lineage: "rebase",
					InheritedCommits: []CommitReference{{RepoConfigID: repoRow.ID, RepoKey: repoRow.RepoKey, WorkspaceID: "workspace-a", CommitSHA: "commit-cherry", Branch: "target", Lineage: "cherry-pick"}},
				},
				Tokens: tokens,
			}},
		},
	})
	if err != nil || !created {
		t.Fatalf("create inherited revision: created=%v err=%v", created, err)
	}
	report, err := NewService(client, nil).Report(ctx, user.ID, observed.Add(-time.Hour), observed.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if report.MeasuredTokens != 140 || report.BoundTokens != 140 || report.UnboundTokens != 0 || len(report.Repositories) != 1 {
		t.Fatalf("report = %+v", report)
	}
	if report.Repositories[0].Tokens != 140 || report.Repositories[0].InheritedTokens != 140 {
		t.Fatalf("repo totals duplicated inherited usage: %+v", report.Repositories[0])
	}
	var direct, inherited *CommitReport
	for index := range report.Repositories[0].Commits {
		commit := &report.Repositories[0].Commits[index]
		switch commit.CommitSHA {
		case "commit-rewritten":
			direct = commit
		case "commit-cherry":
			inherited = commit
		}
	}
	if direct == nil || direct.Tokens != 140 || inherited == nil || inherited.Tokens != 0 || inherited.InheritedTokens != 140 || len(inherited.InheritedFromCommitSHAs) != 1 || inherited.InheritedFromCommitSHAs[0] != "commit-rewritten" {
		t.Fatalf("commit lineage direct=%+v inherited=%+v", direct, inherited)
	}

	conflict := bucket
	conflict.Tokens.Output++
	conflict.Tokens.Processed++
	_, err = NewService(client, nil).CreateBuckets(ctx, principal, BatchRequest{Buckets: []UsageBucket{conflict}})
	if !errors.Is(err, ErrImmutableBucketConflict) {
		t.Fatalf("conflict err = %v", err)
	}
}

func TestNewInstallationDoesNotEnableReporting(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	user, err := client.User.Create().SetUsername("bob").SetEmail("bob@example.org").SetAuthSource("ldap").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	service := NewInstallationService(client)
	credentials, err := service.Ensure(ctx, user.ID, uuid.NewString(), "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	if credentials.ReportingEnabled || credentials.OTelEnabled {
		t.Fatalf("credential issuance enabled reporting: %+v", credentials)
	}
	if _, err := service.AuthenticateReporter(ctx, credentials.ReporterToken); !errors.Is(err, ErrReporterDisabled) {
		t.Fatalf("reporter auth err = %v", err)
	}
	if _, err := service.AuthenticateOTLP(ctx, credentials.OTLPToken); !errors.Is(err, ErrOTLPDisabled) {
		t.Fatalf("OTLP auth err = %v", err)
	}
}

func TestInstallationCredentialRotationInvalidatesOldTokens(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	user := client.User.Create().SetUsername("dave").SetEmail("dave@example.net").SetAuthSource("ldap").SaveX(ctx)
	service := NewInstallationService(client)
	credentials, err := service.Ensure(ctx, user.ID, uuid.NewString(), "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	if _, err := service.SetEnabled(ctx, user.ID, credentials.InstallationID, &enabled, &enabled); err != nil {
		t.Fatal(err)
	}
	rotated, err := service.Rotate(ctx, user.ID, credentials.InstallationID)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.ReporterToken == "" || rotated.OTLPToken == "" || rotated.ReporterToken == credentials.ReporterToken || rotated.OTLPToken == credentials.OTLPToken {
		t.Fatalf("rotated credentials = %+v", rotated)
	}
	if _, err := service.AuthenticateReporter(ctx, credentials.ReporterToken); err == nil {
		t.Fatal("old reporter token remained valid after rotation")
	}
	if _, err := service.AuthenticateOTLP(ctx, credentials.OTLPToken); err == nil {
		t.Fatal("old OTLP token remained valid after rotation")
	}
	if _, err := service.AuthenticateReporter(ctx, rotated.ReporterToken); err != nil {
		t.Fatalf("new reporter token: %v", err)
	}
	if _, err := service.AuthenticateOTLP(ctx, rotated.OTLPToken); err != nil {
		t.Fatalf("new OTLP token: %v", err)
	}
}

func TestLateOTLPEvidenceRefreshesOnlyCorrelationMetadata(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	user := client.User.Create().SetUsername("carol").SetEmail("carol@example.com").SetAuthSource("ldap").SaveX(ctx)
	installations := NewInstallationService(client)
	credentials, err := installations.Ensure(ctx, user.ID, uuid.NewString(), "test machine", "test")
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	if _, err := installations.SetEnabled(ctx, user.ID, credentials.InstallationID, &enabled, &enabled); err != nil {
		t.Fatal(err)
	}
	principal, err := installations.AuthenticateReporter(ctx, credentials.ReporterToken)
	if err != nil {
		t.Fatal(err)
	}
	store := &attributionMemoryStore{values: map[string][]byte{}}
	correlation := NewCorrelationStore(store, "test")
	service := NewService(client, correlation)
	observed := time.Date(2026, 8, 5, 11, 0, 0, 0, time.UTC)
	correlation.now = func() time.Time { return observed.Add(time.Minute) }
	tokens := Tokens{FreshInput: 70, CacheRead: 30, Output: 40, Reasoning: 15, ProviderTotal: 140, Processed: 140}
	bucket := UsageBucket{
		SchemaVersion: CurrentSchemaVersion, BucketID: "bucket-late-otel", Tool: "codex", Model: "gpt-test",
		SessionSlices: []SessionSlice{{ConversationID: "conversation-late", ObservedStart: observed, ObservedEnd: observed.Add(time.Minute), TokenAtomCount: 1, AtomSetDigest: "atoms-late"}},
		ObservedStart: observed, ObservedEnd: observed.Add(time.Minute), Tokens: tokens, RequestCount: 1,
		SourceEventCount: 1, SourceDigest: "source-late", ExtractorVersion: "test", NormalizationVersion: 1, TokenQuality: "measured",
		InitialRevision: AllocationRevision{RevisionID: "revision-late-1", Sequence: 1, Reason: "unbound", EvidenceVersion: "test", Allocations: []Allocation{{Target: AllocationTarget{Status: "unbound"}, Tokens: tokens}}},
	}
	if _, err := service.CreateBuckets(ctx, principal, BatchRequest{Buckets: []UsageBucket{bucket}}); err != nil {
		t.Fatal(err)
	}
	before := client.AttributionUsageBucket.Query().OnlyX(ctx)
	if before.RequestCorrelationQuality != "unlinked" || before.RequestIDCoverageCount != 0 {
		t.Fatalf("before correlation = %+v", before)
	}
	evidence := []RequestEvidence{{
		ConversationID: "conversation-late", RequestID: "request-http-1", ObservedAt: observed.Add(30 * time.Second), EventName: "codex.api_request", Transport: "http", StatusCode: 200,
	}}
	if err := correlation.Put(ctx, principal.InstallationID, evidence); err != nil {
		t.Fatal(err)
	}
	updated, err := service.RefreshCorrelation(ctx, principal, evidence)
	if err != nil {
		t.Fatal(err)
	}
	if updated != 1 {
		t.Fatalf("updated = %d, want 1", updated)
	}
	after := client.AttributionUsageBucket.Query().OnlyX(ctx)
	if after.RequestCorrelationQuality != "advisory" || after.RequestIDCoverageCount != 1 || after.RequestSetDigest == "" {
		t.Fatalf("after correlation = %+v", after)
	}
	if tokensFromEntity(after) != tokens || before.ImmutableDigest != after.ImmutableDigest {
		t.Fatalf("late OTLP mutated token fact: before=%+v after=%+v", before, after)
	}
	if count := client.AttributionAllocationRevision.Query().CountX(ctx); count != 1 {
		t.Fatalf("late OTLP created allocation revisions: %d", count)
	}
	report, err := service.Report(ctx, user.ID, observed.Add(-time.Hour), observed.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Repositories) != 1 || report.Repositories[0].Commits == nil || report.Repositories[0].Worktrees == nil || report.Repositories[0].Branches == nil {
		t.Fatalf("unbound report collections must be JSON arrays: %+v", report.Repositories)
	}
}

func TestCorrelationStoreKeepsSuccessAndFailureRetentionSeparate(t *testing.T) {
	store := &attributionMemoryStore{values: map[string][]byte{}, ttls: map[string]time.Duration{}}
	correlation := NewCorrelationStore(store, "test")
	observed := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	correlation.now = func() time.Time { return observed }
	if err := correlation.Put(context.Background(), "installation-a", []RequestEvidence{
		{ConversationID: "conversation-a", RequestID: "request-ok", ObservedAt: observed, EventName: "codex.api_request", Transport: "http"},
		{ConversationID: "conversation-a", RequestID: "request-failed", ObservedAt: observed.Add(time.Second), EventName: "codex.api_request", Transport: "http", StatusCode: 500, Failed: true},
	}); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	valueCount := len(store.values)
	retentions := map[time.Duration]bool{}
	var retentionErr string
	for key, ttl := range store.ttls {
		retentions[ttl] = true
		if strings.HasSuffix(key, ":success") && ttl != requestEvidenceTTL {
			retentionErr = fmt.Sprintf("success ttl = %s, want %s", ttl, requestEvidenceTTL)
		}
		if strings.HasSuffix(key, ":failed") && ttl != failedRequestEvidenceTTL {
			retentionErr = fmt.Sprintf("failure ttl = %s, want %s", ttl, failedRequestEvidenceTTL)
		}
	}
	var successKey string
	for key := range store.values {
		if strings.HasSuffix(key, ":success") {
			successKey = key
		}
	}
	beforeSuccessSets := store.sets[successKey]
	store.mu.Unlock()
	if valueCount != 2 {
		t.Fatalf("stored evidence keys = %d, want separate success and failure keys", valueCount)
	}
	if retentionErr != "" {
		t.Fatal(retentionErr)
	}
	if !retentions[requestEvidenceTTL] || !retentions[failedRequestEvidenceTTL] {
		t.Fatalf("retentions = %v", retentions)
	}
	if err := correlation.Put(context.Background(), "installation-a", []RequestEvidence{{
		ConversationID: "conversation-a", RequestID: "request-failed-2", ObservedAt: observed.Add(2 * time.Second), EventName: "codex.api_request", Transport: "http", StatusCode: 503, Failed: true,
	}}); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	afterSuccessSets := store.sets[successKey]
	store.mu.Unlock()
	if afterSuccessSets != beforeSuccessSets {
		t.Fatalf("failed evidence rewrote successful evidence: before=%d after=%d", beforeSuccessSets, afterSuccessSets)
	}
}

func TestCorrelationStoreDoesNotRefreshExistingEvidenceRetention(t *testing.T) {
	firstObservedAt := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		retention time.Duration
		failed    bool
	}{
		{name: "success", retention: requestEvidenceTTL},
		{name: "failed", retention: failedRequestEvidenceTTL, failed: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &attributionMemoryStore{values: map[string][]byte{}}
			correlation := NewCorrelationStore(store, "test")
			currentTime := firstObservedAt
			correlation.now = func() time.Time { return currentTime }
			if err := correlation.Put(context.Background(), "installation-a", []RequestEvidence{{
				ConversationID: "conversation-a", RequestID: "request-expired", ObservedAt: firstObservedAt,
				EventName: "codex.api_request", Transport: "http", Failed: tt.failed,
			}}); err != nil {
				t.Fatal(err)
			}
			currentTime = firstObservedAt.Add(tt.retention + time.Minute)
			freshObservedAt := currentTime
			if err := correlation.Put(context.Background(), "installation-a", []RequestEvidence{{
				ConversationID: "conversation-a", RequestID: "request-fresh", ObservedAt: freshObservedAt,
				EventName: "codex.api_request", Transport: "http", Failed: tt.failed,
			}}); err != nil {
				t.Fatal(err)
			}
			summary, err := correlation.Match(context.Background(), "installation-a", []SessionSlice{{
				ConversationID: "conversation-a",
				ObservedStart:  firstObservedAt.Add(-time.Minute),
				ObservedEnd:    freshObservedAt.Add(time.Minute),
			}})
			if err != nil {
				t.Fatal(err)
			}
			if summary.RequestIDCount != 1 {
				t.Fatalf("request ID count = %d, want only the fresh request ID", summary.RequestIDCount)
			}
		})
	}
}

type attributionMemoryStore struct {
	mu     sync.Mutex
	values map[string][]byte
	ttls   map[string]time.Duration
	sets   map[string]int
}

func (s *attributionMemoryStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[key]
	if !ok {
		return nil, readcache.ErrMiss
	}
	return append([]byte(nil), value...), nil
}

func (s *attributionMemoryStore) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ttls == nil {
		s.ttls = map[string]time.Duration{}
	}
	if s.sets == nil {
		s.sets = map[string]int{}
	}
	s.values[key] = append([]byte(nil), value...)
	s.ttls[key] = ttl
	s.sets[key]++
	return nil
}

func (s *attributionMemoryStore) TryAcquireLease(context.Context, string, string, time.Duration) (bool, error) {
	return false, nil
}

func (s *attributionMemoryStore) LeaseTTL(context.Context, string) (time.Duration, error) {
	return 0, nil
}

func (s *attributionMemoryStore) ReleaseLease(context.Context, string, string) (bool, error) {
	return false, nil
}
