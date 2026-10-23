package handler

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/owneraccountoperation"
	"github.com/ai-efficiency/backend/internal/httpclient"
	"github.com/ai-efficiency/backend/internal/owneraccount"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/relayruntime"
	"go.uber.org/zap"
)

func runningHeartbeat(t *testing.T, env *ownerAccountTestEnv, operationID int) time.Time {
	t.Helper()
	row, err := env.client.OwnerAccountOperation.Get(context.Background(), operationID)
	if err != nil {
		t.Fatalf("load operation: %v", err)
	}
	if row.HeartbeatAt == nil {
		t.Fatalf("operation %d has no heartbeat", operationID)
	}
	return *row.HeartbeatAt
}

// TestOwnerAccountWorkerHeartbeatAndTerminalWrite proves the detached worker keeps
// a running row alive, survives gin context cancellation and writes a terminal
// result on its own context.
func TestOwnerAccountWorkerHeartbeatAndTerminalWrite(t *testing.T) {
	env := setupOwnerAccountEnv(t, func(options *owneraccount.Options) {
		options.HeartbeatInterval = 20 * time.Millisecond
		options.OperationTimeout = 30 * time.Second
	})
	env.upstream.setAccounts(ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"))
	prepareOwnerQuotaObservation(t, env)

	release := make(chan struct{})
	env.upstream.blockReset(release)
	w := resetCredit(t, env, true)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202, body: %s", w.Code, w.Body.String())
	}
	operationID := int(env.data(t, w)["operation_id"].(float64))

	// The handler has returned; the worker is still running and heartbeating.
	first := runningHeartbeat(t, env, operationID)
	deadline := time.Now().Add(3 * time.Second)
	var advanced bool
	for time.Now().Before(deadline) {
		if runningHeartbeat(t, env, operationID).After(first) {
			advanced = true
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !advanced {
		t.Fatal("worker heartbeat never advanced while the upstream call was held")
	}
	poll := env.request(http.MethodGet, "/api/v1/user/owner-accounts/operations/"+strconv.Itoa(operationID), nil, env.aliceAuth)
	if poll.Code != http.StatusOK {
		t.Fatalf("poll status = %d, body: %s", poll.Code, poll.Body.String())
	}
	if got := env.data(t, poll)["status"]; got != "running" {
		t.Fatalf("poll status = %v, want running", got)
	}

	close(release)
	operation := env.waitForOperation(t, env.aliceAuth, operationID, "succeeded")
	if got := operation["status"]; got != "succeeded" {
		t.Fatalf("terminal status = %v", got)
	}
	row, err := env.client.OwnerAccountOperation.Get(context.Background(), operationID)
	if err != nil {
		t.Fatalf("load terminal operation: %v", err)
	}
	if row.FinishedAt == nil {
		t.Fatalf("terminal operation has no finished_at")
	}
}

// TestOwnerAccountStaleSweeps covers the lazy sweep on read and the startup sweep.
func TestOwnerAccountStaleSweeps(t *testing.T) {
	t.Run("lazy sweep on read", func(t *testing.T) {
		env := setupOwnerAccountEnv(t, nil)
		env.upstream.setAccounts(ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"))
		heartbeat := time.Now().UTC().Add(-time.Minute)
		running := env.seedOperation(t, env.alice.ID, ownerAccountSeed{
			AccountID: 11, Action: owneraccountoperation.ActionResetCredit,
			Status:    owneraccountoperation.StatusRunning,
			CreatedAt: heartbeat, HeartbeatAt: &heartbeat,
		})

		list := env.data(t, env.request(http.MethodGet, "/api/v1/user/owner-accounts", nil, env.aliceAuth))
		blocked := list["items"].([]any)[0].(map[string]any)["blocked_actions"].([]any)
		if len(blocked) != 1 {
			t.Fatalf("blocked_actions = %v, want one entry", blocked)
		}
		if entry := blocked[0].(map[string]any); entry["action"] != "reset_credit" {
			t.Fatalf("blocked action = %v", entry)
		}
		operation := env.waitForOperation(t, env.aliceAuth, running.ID, "unknown")
		if got := operation["error_code"]; got != "worker-stale" {
			t.Fatalf("stale sweep error_code = %v, want worker-stale", got)
		}
	})

	t.Run("startup sweep", func(t *testing.T) {
		env := setupOwnerAccountEnv(t, nil)
		staleHeartbeat := time.Now().UTC().Add(-time.Minute)
		stale := env.seedOperation(t, env.alice.ID, ownerAccountSeed{
			AccountID: 11, Action: owneraccountoperation.ActionResetCredit,
			Status:    owneraccountoperation.StatusRunning,
			CreatedAt: staleHeartbeat, HeartbeatAt: &staleHeartbeat,
		})
		freshHeartbeat := time.Now().UTC()
		fresh := env.seedOperation(t, env.alice.ID, ownerAccountSeed{
			AccountID: 12, Action: owneraccountoperation.ActionSendInvitation,
			Status:    owneraccountoperation.StatusRunning,
			CreatedAt: freshHeartbeat, HeartbeatAt: &freshHeartbeat,
		})

		swept, err := env.service.SweepStaleOperations(context.Background())
		if err != nil {
			t.Fatalf("startup sweep: %v", err)
		}
		if swept != 1 {
			t.Fatalf("startup sweep count = %d, want 1", swept)
		}
		staleRow, err := env.client.OwnerAccountOperation.Get(context.Background(), stale.ID)
		if err != nil {
			t.Fatalf("load stale row: %v", err)
		}
		if staleRow.Status != owneraccountoperation.StatusUnknown {
			t.Fatalf("stale status = %s, want unknown", staleRow.Status)
		}
		if staleRow.UpstreamCode == nil || *staleRow.UpstreamCode != "worker-stale" {
			t.Fatalf("stale reason = %v, want worker-stale", staleRow.UpstreamCode)
		}
		if staleRow.FinishedAt == nil {
			t.Fatalf("stale row was not terminated")
		}
		freshRow, err := env.client.OwnerAccountOperation.Get(context.Background(), fresh.ID)
		if err != nil {
			t.Fatalf("load fresh row: %v", err)
		}
		if freshRow.Status != owneraccountoperation.StatusRunning {
			t.Fatalf("fresh running row was swept: %s", freshRow.Status)
		}
	})
}

// TestOwnerAccountWorkerPanicBecomesUnknown proves a panicking upstream call is
// contained: the operation becomes unknown instead of crashing the process.
func TestOwnerAccountWorkerPanicBecomesUnknown(t *testing.T) {
	env := setupOwnerAccountEnvWithRuntime(t, nil, relayruntime.Options{
		Factory: func(row *ent.RelayProvider, adminAPIKey string) (relay.Provider, error) {
			inner := relay.NewSub2apiProvider(httpclient.NewDefault(20*time.Second), row.BaseURL, adminAPIKey, row.DefaultModel, zap.NewNop())
			return &ownerAccountCapability{Provider: inner, inner: inner}, nil
		},
		LongRunningFactory: func(row *ent.RelayProvider, adminAPIKey string) (relay.Provider, error) {
			inner := relay.NewSub2apiProvider(httpclient.NewDefault(20*time.Second), row.BaseURL, adminAPIKey, row.DefaultModel, zap.NewNop())
			return &ownerAccountCapability{Provider: inner, inner: inner, panicOnReset: true}, nil
		},
	})
	env.upstream.setAccounts(ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"))
	prepareOwnerQuotaObservation(t, env)

	w := resetCredit(t, env, true)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202, body: %s", w.Code, w.Body.String())
	}
	operationID := int(env.data(t, w)["operation_id"].(float64))
	operation := env.waitForOperation(t, env.aliceAuth, operationID, "unknown")
	if got := operation["error_code"]; got != "worker-panic" {
		t.Fatalf("panic error_code = %v, want worker-panic", got)
	}
}
