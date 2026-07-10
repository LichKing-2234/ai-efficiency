package quotareset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestWebhookNotifierReturnsErrorForWebhookErrcode(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":40008,"errmsg":"invalid message type"}`))
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
	if err == nil || !strings.Contains(err.Error(), "webhook returned errcode 40008: invalid message type") {
		t.Fatalf("NotifyRequestEvent() error = %v, want errcode failure", err)
	}
}

func TestWebhookNotifierSendsWeComRobotTextPayload(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	var gotPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer server.Close()

	client.QuotaResetNotificationSetting.Create().
		SetEnabled(true).
		SetURL("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=redacted-test-key").
		SetAuthType("none").
		SetCreatedByUserID(1).
		SetUpdatedByUserID(1).
		SaveX(ctx)

	request := createNotificationQuotaResetRequest(t, ctx, client)
	notifier := NewWebhookNotifier(client, "", "https://ai-efficiency.example.com")
	notifier.httpClient = &http.Client{Transport: rewriteURLTransport(t, server.URL)}
	if err := notifier.NotifyRequestEvent(ctx, "quota_reset_notification_test", request); err != nil {
		t.Fatalf("NotifyRequestEvent() error = %v", err)
	}
	if gotPayload["msgtype"] != "text" {
		t.Fatalf("msgtype = %#v, want text payload: %#v", gotPayload["msgtype"], gotPayload)
	}
	text, ok := gotPayload["text"].(map[string]any)
	if !ok {
		t.Fatalf("text payload missing: %#v", gotPayload)
	}
	content, _ := text["content"].(string)
	for _, want := range []string{"AI Efficiency", "额度重置", "Group Alpha", "https://ai-efficiency.example.com/usage/quota-reset?request_id="} {
		if !strings.Contains(content, want) {
			t.Fatalf("content = %q, want substring %q", content, want)
		}
	}
	if strings.Contains(content, "redacted-test-key") {
		t.Fatalf("content leaked webhook key: %q", content)
	}
}

func createNotificationQuotaResetRequest(t *testing.T, ctx context.Context, client *ent.Client) *ent.QuotaResetRequest {
	t.Helper()
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	return createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", []int{2})
}

func rewriteURLTransport(t *testing.T, target string) http.RoundTripper {
	t.Helper()
	targetURL, err := url.Parse(target)
	if err != nil {
		t.Fatalf("parse target url: %v", err)
	}
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		clone := req.Clone(req.Context())
		clone.URL.Scheme = targetURL.Scheme
		clone.URL.Host = targetURL.Host
		return http.DefaultTransport.RoundTrip(clone)
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
