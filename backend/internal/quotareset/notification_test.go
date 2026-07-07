package quotareset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	entcredential "github.com/ai-efficiency/backend/ent/credential"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestWebhookNotifierSendsBearerTokenAndWritesNoSecretToPayload(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	encryptionKey := "0000000000000000000000000000000000000000000000000000000000000000"
	encrypted, err := pkg.Encrypt(`{"text":"test-token"}`, encryptionKey)
	if err != nil {
		t.Fatalf("encrypt credential: %v", err)
	}
	credential := client.Credential.Create().
		SetName("Quota reset webhook token").
		SetKind(entcredential.KindSecretText).
		SetPayload(encrypted).
		SaveX(ctx)

	var gotAuth string
	var gotPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client.QuotaResetNotificationSetting.Create().
		SetEnabled(true).
		SetURL(server.URL).
		SetAuthType("bearer_token").
		SetCredentialID(credential.ID).
		SetCreatedByUserID(1).
		SetUpdatedByUserID(1).
		SaveX(ctx)

	request := createNotificationQuotaResetRequest(t, ctx, client)
	notifier := NewWebhookNotifier(client, encryptionKey, server.URL)
	if err := notifier.NotifyRequestEvent(ctx, "quota_reset_request_created", request); err != nil {
		t.Fatalf("NotifyRequestEvent() error = %v", err)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("Authorization = %q, want bearer token", gotAuth)
	}
	if strings.Contains(fmt.Sprint(gotPayload), "test-token") {
		t.Fatalf("payload leaked token: %#v", gotPayload)
	}
}

func TestWebhookNotifierReturnsErrorForHTTPFailure(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client.QuotaResetNotificationSetting.Create().
		SetEnabled(true).
		SetURL(server.URL).
		SetAuthType("none").
		SetCreatedByUserID(1).
		SetUpdatedByUserID(1).
		SaveX(ctx)

	request := createNotificationQuotaResetRequest(t, ctx, client)
	notifier := NewWebhookNotifier(client, "", "https://ai-efficiency.example.com")
	err := notifier.NotifyRequestEvent(ctx, "quota_reset_request_created", request)
	if err == nil || !strings.Contains(err.Error(), "webhook returned 500") {
		t.Fatalf("NotifyRequestEvent() error = %v, want webhook returned 500", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("NotifyRequestEvent() returned timeout instead of HTTP status: %v", err)
	}
}

func createNotificationQuotaResetRequest(t *testing.T, ctx context.Context, client *ent.Client) *ent.QuotaResetRequest {
	t.Helper()
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	return createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", []int{2})
}
