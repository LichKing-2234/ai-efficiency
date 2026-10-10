package relay_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/google/go-cmp/cmp"
)

func TestListAccountsForPlatformReadsEveryPageAndReturnsSafeRelationships(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/accounts", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("platform"); got != "openai" {
			t.Errorf("platform filter = %q, want openai", got)
		}
		if got := r.URL.Query().Get("type"); got != "" {
			t.Errorf("account type filter = %q, want all types", got)
		}
		if got := r.URL.Query().Get("status"); got != "" {
			t.Errorf("account status filter = %q, want all statuses", got)
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		switch page {
		case 1:
			writeOriginEnvelope(w, map[string]any{
				"items": []any{map[string]any{
					"id": 11, "name": "Account Alpha", "platform": "openai", "type": "oauth",
					"status": "active", "schedulable": true,
					"credentials": map[string]any{"access_token": "must-not-escape"},
					"extra":       map[string]any{"private": "must-not-escape"},
					"account_groups": []any{
						map[string]any{"account_id": 11, "group_id": 101, "priority": 2},
						map[string]any{"account_id": 11, "group_id": 202, "priority": 1},
					},
				}},
				"page": 1, "pages": 2,
			})
		case 2:
			writeOriginEnvelope(w, map[string]any{
				"items": []any{map[string]any{
					"id": 12, "name": "Account Beta", "platform": "openai", "type": "apikey",
					"status": "error", "schedulable": false,
					"account_groups": []any{map[string]any{"account_id": 12, "group_id": 101, "priority": 3}},
				}},
				"page": 2, "pages": 2,
			})
		default:
			t.Fatalf("unexpected account page %d", page)
		}
	})

	reader := newTestProvider(t, mux).(relay.AccountRelationshipReader)
	got, err := reader.ListAccountsForPlatform(context.Background(), "openai")
	if err != nil {
		t.Fatalf("ListAccountsForPlatform() error = %v", err)
	}
	want := []relay.Account{
		{ID: 11, Name: "Account Alpha", Platform: "openai", Type: "oauth", Status: "active", Schedulable: true, GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 101, Priority: 2}, {GroupID: 202, Priority: 1}}},
		{ID: 12, Name: "Account Beta", Platform: "openai", Type: "apikey", Status: "error", GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 101, Priority: 3}}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("accounts mismatch (-want +got):\n%s", diff)
	}
}

func TestSetAccountGroupRelationshipPreservesUnrelatedBindingsAndVerifies(t *testing.T) {
	getCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/accounts/11", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			getCount++
			if getCount > 1 {
				// The reviewed priority 3 is forced to the first position, which
				// shifts the unrelated bindings down without dropping them.
				writeOriginEnvelope(w, map[string]any{"id": 11, "platform": "openai", "account_groups": []any{
					map[string]any{"account_id": 11, "group_id": 101, "priority": 1},
					map[string]any{"account_id": 11, "group_id": 9, "priority": 2},
					map[string]any{"account_id": 11, "group_id": 8, "priority": 3},
				}})
				return
			}
			writeOriginEnvelope(w, map[string]any{"id": 11, "platform": "openai", "account_groups": []any{
				map[string]any{"account_id": 11, "group_id": 9, "priority": 1},
				map[string]any{"account_id": 11, "group_id": 101, "priority": 2},
				map[string]any{"account_id": 11, "group_id": 8, "priority": 3},
			}})
		case http.MethodPut:
			var payload map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode update payload: %v", err)
			}
			if len(payload) != 1 {
				t.Fatalf("update payload keys = %v, want only group_ids", payload)
			}
			var groupIDs []int64
			if err := json.Unmarshal(payload["group_ids"], &groupIDs); err != nil {
				t.Fatalf("decode group_ids: %v", err)
			}
			if diff := cmp.Diff([]int64{101, 9, 8}, groupIDs); diff != "" {
				t.Fatalf("group_ids mismatch (-want +got):\n%s", diff)
			}
			writeOriginEnvelope(w, map[string]any{"id": 11})
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	})

	updater := newTestProvider(t, mux).(relay.AccountRelationshipUpdater)
	priority := 3
	err := updater.SetAccountGroupRelationship(context.Background(), 11, 101, []relay.AccountGroupRelationship{
		{GroupID: 9, Priority: 1},
		{GroupID: 101, Priority: 2},
		{GroupID: 8, Priority: 3},
	}, &priority)
	if err != nil {
		t.Fatalf("SetAccountGroupRelationship() error = %v", err)
	}
	if getCount != 2 {
		t.Fatalf("account GET count = %d, want read and verification", getCount)
	}
}

// TestSetAccountGroupRelationshipForcesFirstPositionWhenPriorityIsUnreachable
// covers the drift that froze Relationship Operation 85: account 26 held only
// two groups, so the reviewed priority 3 could never be expressed and the
// reorder was rejected instead of binding the relationship.
func TestSetAccountGroupRelationshipForcesFirstPositionWhenPriorityIsUnreachable(t *testing.T) {
	getCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/accounts/26", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			getCount++
			if getCount > 1 {
				writeOriginEnvelope(w, map[string]any{"id": 26, "platform": "openai", "account_groups": []any{
					map[string]any{"account_id": 26, "group_id": 20, "priority": 1},
					map[string]any{"account_id": 26, "group_id": 2, "priority": 2},
				}})
				return
			}
			writeOriginEnvelope(w, map[string]any{"id": 26, "platform": "openai", "account_groups": []any{
				map[string]any{"account_id": 26, "group_id": 2, "priority": 1},
				map[string]any{"account_id": 26, "group_id": 20, "priority": 2},
			}})
		case http.MethodPut:
			var payload map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode update payload: %v", err)
			}
			var groupIDs []int64
			if err := json.Unmarshal(payload["group_ids"], &groupIDs); err != nil {
				t.Fatalf("decode group_ids: %v", err)
			}
			if diff := cmp.Diff([]int64{20, 2}, groupIDs); diff != "" {
				t.Fatalf("group_ids mismatch (-want +got):\n%s", diff)
			}
			writeOriginEnvelope(w, map[string]any{"id": 26})
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	})

	updater := newTestProvider(t, mux).(relay.AccountRelationshipUpdater)
	priority := 3
	err := updater.SetAccountGroupRelationship(context.Background(), 26, 20, []relay.AccountGroupRelationship{
		{GroupID: 2, Priority: 1},
		{GroupID: 20, Priority: 2},
	}, &priority)
	if err != nil {
		t.Fatalf("SetAccountGroupRelationship() error = %v, want nil for an unreachable reviewed priority", err)
	}
	if getCount != 2 {
		t.Fatalf("account GET count = %d, want read and verification", getCount)
	}
}

// TestSetAccountGroupRelationshipRemovesWithoutDesiredPriority covers the
// removal path: a nil reviewed priority must drop the group instead of binding
// it, which is what keeps a `remove` step from re-adding an account.
func TestSetAccountGroupRelationshipRemovesWithoutDesiredPriority(t *testing.T) {
	getCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/accounts/11", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			getCount++
			if getCount > 1 {
				writeOriginEnvelope(w, map[string]any{"id": 11, "platform": "openai", "account_groups": []any{
					map[string]any{"account_id": 11, "group_id": 9, "priority": 1},
				}})
				return
			}
			writeOriginEnvelope(w, map[string]any{"id": 11, "platform": "openai", "account_groups": []any{
				map[string]any{"account_id": 11, "group_id": 9, "priority": 1},
				map[string]any{"account_id": 11, "group_id": 101, "priority": 2},
			}})
		case http.MethodPut:
			var payload map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode update payload: %v", err)
			}
			var groupIDs []int64
			if err := json.Unmarshal(payload["group_ids"], &groupIDs); err != nil {
				t.Fatalf("decode group_ids: %v", err)
			}
			if diff := cmp.Diff([]int64{9}, groupIDs); diff != "" {
				t.Fatalf("group_ids mismatch (-want +got):\n%s", diff)
			}
			writeOriginEnvelope(w, map[string]any{"id": 11})
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	})

	updater := newTestProvider(t, mux).(relay.AccountRelationshipUpdater)
	err := updater.SetAccountGroupRelationship(context.Background(), 11, 101, []relay.AccountGroupRelationship{
		{GroupID: 9, Priority: 1},
		{GroupID: 101, Priority: 2},
	}, nil)
	if err != nil {
		t.Fatalf("SetAccountGroupRelationship() error = %v", err)
	}
	if getCount != 2 {
		t.Fatalf("account GET count = %d, want read and verification", getCount)
	}
}
