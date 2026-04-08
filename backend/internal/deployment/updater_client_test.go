package deployment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpdaterClientApplyAndRollback(t *testing.T) {
	var gotApplyReq ApplyRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			if r.Method != http.MethodGet {
				t.Fatalf("expected GET for /status, got %s", r.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(UpdateStatus{Phase: "idle"})
		case "/apply":
			if r.Method != http.MethodPost {
				t.Fatalf("expected POST for /apply, got %s", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&gotApplyReq); err != nil {
				t.Fatalf("decode apply request: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(UpdateStatus{Phase: "applying", TargetVersion: gotApplyReq.TargetVersion})
		case "/rollback":
			if r.Method != http.MethodPost {
				t.Fatalf("expected POST for /rollback, got %s", r.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(UpdateStatus{Phase: "rollback_started", Message: "rollback queued"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewUpdaterClient(srv.Client(), srv.URL)

	applyResp, err := client.Apply(context.Background(), ApplyRequest{TargetVersion: "v0.5.0"})
	if err != nil {
		t.Fatalf("expected no apply error, got %v", err)
	}
	if gotApplyReq.TargetVersion != "v0.5.0" {
		t.Fatalf("expected apply target v0.5.0, got %q", gotApplyReq.TargetVersion)
	}
	if applyResp.TargetVersion != "v0.5.0" {
		t.Fatalf("expected apply response target v0.5.0, got %q", applyResp.TargetVersion)
	}

	rollbackResp, err := client.Rollback(context.Background())
	if err != nil {
		t.Fatalf("expected no rollback error, got %v", err)
	}
	if rollbackResp.Phase != "rollback_started" {
		t.Fatalf("expected rollback phase rollback_started, got %q", rollbackResp.Phase)
	}
}
