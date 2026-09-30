package attributionclaim

import (
	"context"
	"testing"
	"time"
)

// Kiro CLI bills in credit and reports no tokens at all. The pool layer already
// accepted such a bucket, but the claim entry required a positive Token total
// and rejected the whole claim before the pool ever saw it — so a Kiro commit
// was proven and then recorded as costing nothing.
func TestIngestAcceptsCreditOnlyLocalUsage(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	bucket := time.Date(2026, 8, 28, 12, 15, 0, 0, time.UTC)
	usage := LocalUsageBucket{RequestedModel: "kiro-cli", BucketStartUTC: bucket, CreditUsage: 0.0783, RequestCount: 1}

	result, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{f.localClaim("group-credit", usage)}})
	if err != nil || result.Results[0].Group.Status != "persisted" {
		t.Fatalf("credit-only ingest = %+v, %v", result, err)
	}
	pool := f.client.AttributionUsagePool.Query().OnlyX(ctx)
	if pool.CreditUsage != 0.0783 || pool.TotalTokens != 0 || pool.RequestCount != 1 {
		t.Fatalf("pool = credit %v tokens %d requests %d, want the credit recorded with zero tokens",
			pool.CreditUsage, pool.TotalTokens, pool.RequestCount)
	}
}

// Credit grows and regresses like tokens do, so the coverage check that keeps a
// replay from shrinking a pool has to compare it too.
func TestIngestRejectsRegressedCreditAndAcceptsGrowth(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	bucket := time.Date(2026, 8, 28, 12, 15, 0, 0, time.UTC)
	claim := f.localClaim("group-credit", LocalUsageBucket{RequestedModel: "kiro-cli", BucketStartUTC: bucket, CreditUsage: 0.0783, RequestCount: 1})
	if _, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{claim}}); err != nil {
		t.Fatal(err)
	}

	claim.LocalUsage[0].CreditUsage = 0.1567
	claim.LocalUsage[0].RequestCount = 2
	grown, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{claim}})
	if err != nil || grown.Results[0].Group.Status != "persisted" {
		t.Fatalf("credit growth = %+v, %v", grown, err)
	}
	if pool := f.client.AttributionUsagePool.Query().OnlyX(ctx); pool.CreditUsage != 0.1567 {
		t.Fatalf("grown pool credit = %v, want 0.1567", pool.CreditUsage)
	}

	claim.LocalUsage[0].CreditUsage = 0.0783
	regressed, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{claim}})
	if err != nil || regressed.Results[0].Group.Status != "rejected" {
		t.Fatalf("regressed credit = %+v, %v", regressed, err)
	}
	if pool := f.client.AttributionUsagePool.Query().OnlyX(ctx); pool.CreditUsage != 0.1567 {
		t.Fatalf("pool credit after a regressed replay = %v, want it unchanged", pool.CreditUsage)
	}
}

// A bucket carrying neither unit still has nothing to record.
func TestIngestRejectsLocalUsageCarryingNeitherUnit(t *testing.T) {
	f := newFixture(t)
	bucket := time.Date(2026, 8, 28, 12, 15, 0, 0, time.UTC)
	claim := f.localClaim("group-empty", LocalUsageBucket{RequestedModel: "kiro-cli", BucketStartUTC: bucket, RequestCount: 1})
	result, err := f.service.Ingest(context.Background(), f.principal, BatchRequest{Groups: []Request{claim}})
	if err != nil || result.Results[0].Group.Status != "rejected" {
		t.Fatalf("amountless bucket = %+v, %v", result, err)
	}
}
