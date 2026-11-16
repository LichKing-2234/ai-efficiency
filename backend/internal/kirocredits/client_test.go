package kirocredits

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAdminClientReadsSummaryAndRecords(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if got := r.Header.Get("x-api-key"); got != "test-admin-token" {
			t.Fatalf("x-api-key = %q", got)
		}
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/api/admin/api-keys":
			_, _ = w.Write([]byte(`[{"id":5,"name":"sub2api-1-alice@example.com","enabled":true}]`))
		case "/api/admin/api-keys/5/usage":
			_, _ = w.Write([]byte(`{"apiKeyId":5,"totalRequests":2,"totalInputTokens":300,"totalOutputTokens":30,"totalCredits":1.5,"totalCreditsSaved":0.5,"byModel":[{"model":"auto","requests":2,"inputTokens":300,"outputTokens":30,"credits":1.5}]}`))
		case "/api/admin/api-keys/5/usage/records":
			if got := r.URL.Query().Get("page_size"); got != "500" {
				t.Fatalf("page_size = %q, want 500", got)
			}
			_, _ = w.Write([]byte(`{"records":[{"model":"auto","inputTokens":100,"outputTokens":10,"creditsUsed":0.75,"createdAt":"2026-11-13T03:00:00Z"}],"total":1,"page":1,"pageSize":500,"totalPages":1}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newAdminClient(server.URL+"/", "test-admin-token", 2*time.Second)

	keys, err := client.listKeys(context.Background())
	if err != nil {
		t.Fatalf("listKeys: %v", err)
	}
	if len(keys) != 1 || keys[0].ID != 5 {
		t.Fatalf("keys = %+v", keys)
	}

	summary, err := client.usageSummary(context.Background(), 5)
	if err != nil {
		t.Fatalf("usageSummary: %v", err)
	}
	if summary.TotalCredits != 1.5 || summary.TotalRequests != 2 {
		t.Fatalf("summary = %+v", summary)
	}

	records, err := client.usageRecords(context.Background(), 5, 1, recordPageSize)
	if err != nil {
		t.Fatalf("usageRecords: %v", err)
	}
	if len(records.Records) != 1 || records.Records[0].CreditsUsed == nil || *records.Records[0].CreditsUsed != 0.75 {
		t.Fatalf("records = %+v", records.Records)
	}
	if len(paths) != 3 {
		t.Fatalf("paths = %v", paths)
	}
}

func TestAdminClientReturnsErrorOnNonOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	client := newAdminClient(server.URL, "test-admin-token", 2*time.Second)
	if _, err := client.listKeys(context.Background()); err == nil {
		t.Fatal("listKeys error = nil, want non-200 error")
	}
}
