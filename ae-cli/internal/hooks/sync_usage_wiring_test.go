package hooks

import (
	"context"
	"fmt"
	"testing"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
	"github.com/ai-efficiency/ae-cli/internal/client"
)

type wiringUsageClient struct{}

func (wiringUsageClient) SendToolUsageEvent(context.Context, client.ToolUsageEventRequest) error {
	return nil
}

type wiringV2Client struct{}

func (wiringV2Client) SendAttributionV2Claims(context.Context, []client.AttributionV2ClaimGroup) (*client.AttributionV2ClaimBatchResult, error) {
	return nil, fmt.Errorf("no claims expected in this test")
}

// wiringUploader is what a compact machine with a live user session presents:
// claims through the reporter credential, usage through the user session.
type wiringUploader struct {
	usage attributionlocal.BackendClient
}

func (wiringUploader) UploadHookEvent(context.Context, HookEvent) error { return nil }
func (wiringUploader) V2ClaimClient() attributionlocal.V2ClaimBackendClient {
	return wiringV2Client{}
}
func (wiringUploader) RelayProviderID() int { return 7 }
func (wiringUploader) AttributionProtocol() client.AttributionProtocol {
	return client.AttributionProtocol{
		LedgerEpoch:       client.AttributionLedgerEpochFormalV2,
		V1WritePolicy:     client.AttributionV1WritePolicyUpgradeNeeded,
		MinimumCLIVersion: "0.2.0",
	}
}
func (u wiringUploader) ToolUsageClient() attributionlocal.BackendClient { return u.usage }

// A compact machine must upload usage alongside its claims. The previous shape
// returned right after the claim sync, so from the day compact reporting was
// enabled no tool usage event left the machine — dashboards starved while
// claims flowed, and nothing reported it.
func TestPendingSyncPassRunsUsageSyncOnACompactMachine(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	usageRuns := 0
	restore := runAttributionSync
	runAttributionSync = func(ctx context.Context, opts attributionlocal.RunOptions, syncClient attributionlocal.BackendClient) error {
		usageRuns++
		if syncClient == nil {
			t.Fatal("usage sync started without a client")
		}
		if !opts.ManagedUpload || opts.WorkspaceID == "" {
			t.Fatalf("usage sync options incomplete: %+v", opts)
		}
		return nil
	}
	t.Cleanup(func() { runAttributionSync = restore })

	execCtx := ExecutionContext{WorkspaceID: "workspace-wiring", RepoRoot: t.TempDir()}
	err := runPendingSyncPass(context.Background(), execCtx, wiringUploader{usage: wiringUsageClient{}}, &SyncTask{})
	if err != nil {
		t.Fatalf("sync pass: %v", err)
	}
	if usageRuns != 1 {
		t.Fatalf("usage sync runs = %d, want 1", usageRuns)
	}
}

// A usage failure must surface rather than vanish behind a clean claim sync.
func TestPendingSyncPassReportsAUsageFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	restore := runAttributionSync
	runAttributionSync = func(context.Context, attributionlocal.RunOptions, attributionlocal.BackendClient) error {
		return fmt.Errorf("usage endpoint rejected the batch")
	}
	t.Cleanup(func() { runAttributionSync = restore })

	execCtx := ExecutionContext{WorkspaceID: "workspace-wiring", RepoRoot: t.TempDir()}
	err := runPendingSyncPass(context.Background(), execCtx, wiringUploader{usage: wiringUsageClient{}}, &SyncTask{})
	if err == nil {
		t.Fatal("want the usage failure returned")
	}
}

// A compact machine whose user session has lapsed has no way to authenticate
// usage uploads; the claim sync still runs and nothing panics.
func TestPendingSyncPassSkipsUsageWithoutAUserSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	restore := runAttributionSync
	runAttributionSync = func(context.Context, attributionlocal.RunOptions, attributionlocal.BackendClient) error {
		t.Fatal("usage sync must not run without a client")
		return nil
	}
	t.Cleanup(func() { runAttributionSync = restore })

	execCtx := ExecutionContext{WorkspaceID: "workspace-wiring", RepoRoot: t.TempDir()}
	if err := runPendingSyncPass(context.Background(), execCtx, wiringUploader{usage: nil}, &SyncTask{}); err != nil {
		t.Fatalf("sync pass: %v", err)
	}
}
