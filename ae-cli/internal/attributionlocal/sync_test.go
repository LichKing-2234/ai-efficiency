package attributionlocal

import (
	"context"
	"testing"
)

func TestSync_ReplaysSpooledEventsBeforeNewScan(t *testing.T) {
	t.Parallel()

	fixture := setupSyncEngineWithSpool(t)
	if err := fixture.Engine.Replay(context.Background(), "/tmp/repo"); err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if !fixture.Client.SawUpload("spooled-dedupe-key") {
		t.Fatal("expected spooled event upload")
	}
}
