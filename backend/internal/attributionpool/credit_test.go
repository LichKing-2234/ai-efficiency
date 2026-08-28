package attributionpool

import (
	"context"
	"testing"
	"time"
)

// A credit-only bucket — Kiro CLI reports credit and no tokens at all — must
// materialize into the pool with its credit recorded and grow like any other
// contribution. Before credit was a pool dimension such a bucket was rejected
// outright ("local usage Token total is inconsistent") and the agent's commits
// were provable but never priced.
func TestApplyLocalGroupRecordsCreditOnlyUsage(t *testing.T) {
	fixture := newPoolFixture(t)
	ctx := context.Background()
	group := fixture.client.AttributionClaimGroup.GetX(ctx, fixture.groupID)
	allocations := group.CommitAllocations

	first := []map[string]any{{
		"requested_model": "gpt-5.6-sol", "bucket_start_utc": fixture.now.UTC().Truncate(15 * time.Minute),
		"input_tokens": int64(0), "output_tokens": int64(0), "cache_creation_tokens": int64(0),
		"cache_read_tokens": int64(0), "total_tokens": int64(0), "credit_usage": 0.0783, "request_count": 1,
	}}
	applyLocalGroupChangeInTransaction(t, fixture, nil, nil, allocations, first)

	pool := fixture.client.AttributionUsagePool.Query().OnlyX(ctx)
	if pool.CreditUsage != 0.0783 || pool.TotalTokens != 0 || pool.RequestCount != 1 {
		t.Fatalf("pool = credit %v tokens %d requests %d, want the credit recorded with zero tokens",
			pool.CreditUsage, pool.TotalTokens, pool.RequestCount)
	}

	late := []map[string]any{{
		"requested_model": "gpt-5.6-sol", "bucket_start_utc": fixture.now.UTC().Truncate(15 * time.Minute),
		"input_tokens": int64(0), "output_tokens": int64(0), "cache_creation_tokens": int64(0),
		"cache_read_tokens": int64(0), "total_tokens": int64(0), "credit_usage": 0.1567, "request_count": 2,
	}}
	applyLocalGroupChangeInTransaction(t, fixture, allocations, first, allocations, late)
	updated := fixture.client.AttributionUsagePool.Query().OnlyX(ctx)
	if updated.ID != pool.ID || updated.CreditUsage != 0.1567 || updated.RequestCount != 2 {
		t.Fatalf("updated pool = credit %v requests %d, want stable identity and the grown credit",
			updated.CreditUsage, updated.RequestCount)
	}
}

// A bucket with neither tokens nor credit carries nothing and stays rejected.
func TestLocalGroupContributionsRejectAnEmptyBucket(t *testing.T) {
	usage := []map[string]any{{
		"requested_model": "gpt-test", "bucket_start_utc": time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC),
		"input_tokens": int64(0), "output_tokens": int64(0), "cache_creation_tokens": int64(0),
		"cache_read_tokens": int64(0), "total_tokens": int64(0), "credit_usage": 0.0, "request_count": 1,
	}}
	if _, err := localGroupContributions("shadow_v2", 1, 1, nil, usage); err == nil {
		t.Fatal("want an amountless bucket rejected")
	}
}
