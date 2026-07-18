package quotareset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	entcredential "github.com/ai-efficiency/backend/ent/credential"
	"github.com/ai-efficiency/backend/ent/quotaresetnotificationsetting"
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
	requester, ok := gotPayload["requester"].(map[string]any)
	if !ok || requester["display_name"] != "Alice Example" || requester["email"] != "alice@example.com" {
		t.Fatalf("requester context = %#v", gotPayload["requester"])
	}
	workflow, ok := gotPayload["workflow"].(map[string]any)
	if !ok || workflow["step_label"] != "Company / Group Alpha" || workflow["step_number"] != float64(1) || workflow["step_count"] != float64(2) {
		t.Fatalf("workflow context = %#v", gotPayload["workflow"])
	}
	if gotPayload["group_name"] != "Group Alpha" || gotPayload["reason"] != "Need reset for a build investigation" || gotPayload["action_url"] == nil {
		t.Fatalf("request context = %#v", gotPayload)
	}
	if paths, ok := requester["department_paths"].([]any); !ok || !reflect.DeepEqual(paths, []any{"Company / Group Alpha"}) {
		t.Fatalf("requester department paths = %#v", requester["department_paths"])
	}
	approvers, ok := workflow["active_approvers"].([]any)
	if !ok || len(approvers) != 1 {
		t.Fatalf("active approvers = %#v", workflow["active_approvers"])
	}
	approver, ok := approvers[0].(map[string]any)
	if !ok || approver["display_name"] != "bob" || approver["email"] != "bob@example.org" {
		t.Fatalf("active approver = %#v", approvers[0])
	}
}

func TestWebhookNotifierIncludesPreviousDecision(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	var gotPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client.QuotaResetNotificationSetting.Create().
		SetEnabled(true).
		SetChannel(quotaresetnotificationsetting.ChannelGenericWebhook).
		SetURL(server.URL).
		SetAuthType("none").
		SetCreatedByUserID(1).
		SetUpdatedByUserID(1).
		SaveX(ctx)
	request := createNotificationQuotaResetRequest(t, ctx, client)
	workflow, err := DecodeWorkflow(request.Workflow)
	if err != nil {
		t.Fatalf("DecodeWorkflow() error = %v", err)
	}
	workflow.Steps[0].Status = WorkflowStepApproved
	workflow.Steps[0].Decision = &WorkflowDecision{ActorUserID: workflow.Steps[0].Approvers[0].UserID, ActorDisplayName: "Bob Example", Comment: "Previous approval comment", Approve: true}
	workflow.CurrentStep = 1
	workflow.Steps[1].Status = WorkflowStepActive
	raw, err := EncodeWorkflow(workflow)
	if err != nil {
		t.Fatalf("EncodeWorkflow() error = %v", err)
	}
	request = client.QuotaResetRequest.UpdateOneID(request.ID).SetWorkflow(raw).SetResolvedApproverUserIds(workflow.ActiveApproverUserIDs()).SaveX(ctx)

	if err := NewWebhookNotifier(client, "", "https://ai-efficiency.example.com").NotifyRequestEvent(ctx, "quota_reset_step_activated", request); err != nil {
		t.Fatalf("NotifyRequestEvent() error = %v", err)
	}
	workflowPayload, ok := gotPayload["workflow"].(map[string]any)
	if !ok {
		t.Fatalf("workflow payload = %#v", gotPayload["workflow"])
	}
	previous, ok := workflowPayload["previous_decision"].(map[string]any)
	if !ok || previous["comment"] != "Previous approval comment" {
		t.Fatalf("previous decision = %#v", workflowPayload["previous_decision"])
	}
}

func TestGenericWebhookPayloadExcludesInternalWorkflowFields(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	request := createNotificationQuotaResetRequest(t, ctx, client)
	workflow, err := DecodeWorkflow(request.Workflow)
	if err != nil {
		t.Fatalf("DecodeWorkflow() error = %v", err)
	}
	workflow.Requester.NotificationIDs = map[string]string{"wecom": "alice-internal-wecom"}
	workflow.Steps[0].Approvers[0].NotificationIDs = map[string]string{"wecom": "bob-internal-wecom"}
	workflow.Steps[0].Status = WorkflowStepApproved
	workflow.Steps[0].Decision = &WorkflowDecision{
		ActorUserID:      workflow.Steps[0].Approvers[0].UserID,
		ActorDisplayName: "Bob Example",
		Comment:          "Previous approval comment",
		Approve:          true,
		Admin:            true,
	}
	workflow.Steps[1].Approvers[0].NotificationIDs = map[string]string{"wecom": "carol-internal-wecom"}
	workflow.Steps[1].Status = WorkflowStepActive
	workflow.CurrentStep = 1
	rawWorkflow, err := EncodeWorkflow(workflow)
	if err != nil {
		t.Fatalf("EncodeWorkflow() error = %v", err)
	}
	request = client.QuotaResetRequest.UpdateOneID(request.ID).
		SetWorkflow(rawWorkflow).
		SetResolvedApproverUserIds(workflow.ActiveApproverUserIDs()).
		SaveX(ctx)

	notificationContext, err := notificationContextForRequest(request)
	if err != nil {
		t.Fatalf("notificationContextForRequest() error = %v", err)
	}
	payloadJSON, err := json.Marshal(NewWebhookNotifier(client, "", "https://ai-efficiency.example.com").payloadForChannel(
		quotaresetnotificationsetting.ChannelGenericWebhook,
		"quota_reset_step_activated",
		request,
		notificationContext,
	))
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	assertNoJSONFields(t, payload, "notification_ids", "source", "approve", "admin")
	for _, internalID := range []string{"alice-internal-wecom", "bob-internal-wecom", "carol-internal-wecom"} {
		if strings.Contains(string(payloadJSON), internalID) {
			t.Fatalf("generic webhook leaked internal WeCom id %q: %s", internalID, payloadJSON)
		}
	}
	if payload["status"] != "pending" || payload["group_name"] != "Group Alpha" || payload["reason"] != "Need reset for a build investigation" {
		t.Fatalf("public request fields = %#v", payload)
	}
	want := fmt.Sprintf(
		"https://ai-efficiency.example.com/usage/quota-reset?queue=approvals&request_id=%d",
		request.ID,
	)
	if payload["action_url"] != want {
		t.Fatalf("action_url = %#v, want %q", payload["action_url"], want)
	}
	requester, ok := payload["requester"].(map[string]any)
	if !ok || requester["display_name"] != "Alice Example" || requester["email"] != "alice@example.com" {
		t.Fatalf("requester = %#v", payload["requester"])
	}
	if paths, ok := requester["department_paths"].([]any); !ok || !reflect.DeepEqual(paths, []any{"Company / Group Alpha"}) {
		t.Fatalf("requester department paths = %#v", requester["department_paths"])
	}
	workflowPayload, ok := payload["workflow"].(map[string]any)
	if !ok || workflowPayload["step_number"] != float64(2) || workflowPayload["step_count"] != float64(2) || workflowPayload["step_label"] != "Company / Group Beta" {
		t.Fatalf("workflow progress = %#v", payload["workflow"])
	}
	previous, ok := workflowPayload["previous_decision"].(map[string]any)
	if !ok || previous["actor_display_name"] != "Bob Example" || previous["comment"] != "Previous approval comment" {
		t.Fatalf("previous decision = %#v", workflowPayload["previous_decision"])
	}
	activeApprovers, ok := workflowPayload["active_approvers"].([]any)
	if !ok || len(activeApprovers) != 1 {
		t.Fatalf("active approvers = %#v", workflowPayload["active_approvers"])
	}
	activeApprover, ok := activeApprovers[0].(map[string]any)
	if !ok || activeApprover["display_name"] != "carol" || activeApprover["email"] != "carol@example.org" {
		t.Fatalf("active approver = %#v", activeApprovers[0])
	}
	steps, ok := workflowPayload["steps"].([]any)
	if !ok || len(steps) != 2 {
		t.Fatalf("workflow steps = %#v", workflowPayload["steps"])
	}
	firstStep, ok := steps[0].(map[string]any)
	if !ok || firstStep["step_number"] != float64(1) || firstStep["label"] != "Company / Group Alpha" || firstStep["status"] != WorkflowStepApproved {
		t.Fatalf("first workflow step = %#v", steps[0])
	}
	decision, ok := firstStep["decision"].(map[string]any)
	if !ok || decision["actor_display_name"] != "Bob Example" || decision["comment"] != "Previous approval comment" {
		t.Fatalf("first workflow decision = %#v", firstStep["decision"])
	}
	secondStep, ok := steps[1].(map[string]any)
	if !ok || secondStep["step_number"] != float64(2) || secondStep["label"] != "Company / Group Beta" || secondStep["status"] != WorkflowStepActive {
		t.Fatalf("second workflow step = %#v", steps[1])
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
	if err == nil || err.Error() != "webhook returned errcode 40008" {
		t.Fatalf("NotifyRequestEvent() error = %v, want errcode failure", err)
	}
}

func TestWebhookNotifierSendsWeComMarkdownWithRequesterAndMentions(t *testing.T) {
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
		SetChannel(quotaresetnotificationsetting.ChannelWecomGroupRobot).
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
	if gotPayload["msgtype"] != "markdown" {
		t.Fatalf("msgtype = %#v, want markdown payload: %#v", gotPayload["msgtype"], gotPayload)
	}
	markdown, ok := gotPayload["markdown"].(map[string]any)
	if !ok {
		t.Fatalf("markdown payload missing: %#v", gotPayload)
	}
	content, _ := markdown["content"].(string)
	for _, want := range []string{
		"额度重置待审批",
		"Alice Example",
		"alice@example.com",
		"Company / Group Alpha",
		"Group Alpha",
		"Need reset for a build investigation",
		"1/2",
		"<@bob-wecom>",
		"https://ai-efficiency.example.com/usage/quota-reset?queue=approvals&request_id=",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("content = %q, want substring %q", content, want)
		}
	}
	if strings.Contains(content, "redacted-test-key") {
		t.Fatalf("content leaked webhook key: %q", content)
	}
}

func TestWebhookNotifierHonorsExplicitGenericChannelForWeComURL(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	var gotPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		_, _ = w.Write([]byte(`{"errcode":0}`))
	}))
	defer server.Close()
	client.QuotaResetNotificationSetting.Create().
		SetEnabled(true).
		SetChannel(quotaresetnotificationsetting.ChannelGenericWebhook).
		SetURL("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=redacted-test-key").
		SetAuthType("none").
		SaveX(ctx)

	notifier := NewWebhookNotifier(client, "", "https://ai-efficiency.example.com")
	notifier.httpClient = &http.Client{Transport: rewriteURLTransport(t, server.URL)}
	if err := notifier.NotifyRequestEvent(ctx, "quota_reset_request_created", createNotificationQuotaResetRequest(t, ctx, client)); err != nil {
		t.Fatalf("NotifyRequestEvent() error = %v", err)
	}
	if gotPayload["event"] != "quota_reset_request_created" || gotPayload["msgtype"] != nil {
		t.Fatalf("generic payload = %#v", gotPayload)
	}
	if gotPayload["status"] != "pending" {
		t.Fatalf("generic status = %#v, want public pending", gotPayload["status"])
	}
}

func TestWebhookNotifierRedactsSecretURLFromTransportErrors(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	client.QuotaResetNotificationSetting.Create().
		SetEnabled(true).
		SetChannel(quotaresetnotificationsetting.ChannelGenericWebhook).
		SetURL("https://hooks.example.com/send?key=secret-value").
		SetAuthType("none").
		SaveX(ctx)
	notifier := NewWebhookNotifier(client, "", "")
	notifier.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial https://hooks.example.com/send?key=secret-value: connection refused")
	})}
	err := notifier.NotifyRequestEvent(ctx, "quota_reset_request_created", createNotificationQuotaResetRequest(t, ctx, client))
	if err == nil || strings.Contains(err.Error(), "secret-value") || strings.Contains(err.Error(), "hooks.example.com") {
		t.Fatalf("NotifyRequestEvent() error = %v, want redacted transport error", err)
	}
}

func TestWebhookBusinessErrorRedactsEchoedSecrets(t *testing.T) {
	err := webhookResponseBusinessError([]byte(`{"errcode":40008,"errmsg":"token: secret-value"}`))
	if err == nil {
		t.Fatal("webhookResponseBusinessError() error = nil")
	}
	message := err.Error()
	if strings.Contains(message, "secret-value") {
		t.Fatalf("business error leaked secret material: %q", message)
	}
	if !strings.Contains(message, "errcode 40008") {
		t.Fatalf("business error = %q, want errcode", message)
	}
}

func TestWeComMarkdownLabelsApproverWithoutMentionID(t *testing.T) {
	notifier := NewWebhookNotifier(nil, "", "")
	content := notifier.weComRobotMarkdown("quota_reset_request_created", &ent.QuotaResetRequest{
		ID:        7,
		GroupID:   "42",
		GroupName: "Group Alpha",
		Reason:    "Need reset",
	}, quotaResetNotificationContext{
		Requester:       WorkflowPerson{DisplayName: "Alice Example", Email: "alice@example.com", DepartmentPaths: []string{"Company / Group Alpha"}},
		ActiveApprovers: []WorkflowApprover{{UserID: 2, DisplayName: "Bob Example", Email: "bob@example.org", NotificationIDs: map[string]string{}}},
		StepIndex:       0,
		StepCount:       1,
		StepLabel:       "Company / Group Alpha",
	})
	if !strings.Contains(content, "Bob Example（无法@）") {
		t.Fatalf("content = %q, want missing mention label", content)
	}
}

func TestWeComMarkdownFlattensAndNeutralizesHostileReasonAndComment(t *testing.T) {
	notifier := NewWebhookNotifier(nil, "", "https://ai-efficiency.example.com")
	content := notifier.weComRobotMarkdown("quota_reset_step_activated", &ent.QuotaResetRequest{
		ID:        7,
		GroupID:   "42",
		GroupName: "Group Alpha",
		Reason:    "Need reset\r\n[malicious](https://evil.example)\n> forged reason",
	}, quotaResetNotificationContext{
		Requester: WorkflowPerson{
			DisplayName:     "Alice Example",
			Email:           "alice@example.com",
			DepartmentPaths: []string{"Company / Group Alpha"},
		},
		StepIndex: 0,
		StepCount: 1,
		StepLabel: "Company / Group Alpha",
		PreviousDecision: &WorkflowDecision{
			ActorDisplayName: "Bob Example",
			Comment:          "Approved\r[malicious](https://evil.example)\n# forged comment",
		},
	})

	for _, want := range []string{
		"> 申请原因：Need reset ［malicious］（https://evil.example） ＞ forged reason",
		"> 上一审批：Bob Example：Approved ［malicious］（https://evil.example） # forged comment",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("content = %q, want flattened safe text %q", content, want)
		}
	}
	for _, hostile := range []string{
		"\r",
		"[malicious](https://evil.example)",
		"\n> forged reason",
		"\n# forged comment",
	} {
		if strings.Contains(content, hostile) {
			t.Fatalf("content retained hostile markdown %q: %q", hostile, content)
		}
	}
}

func createNotificationQuotaResetRequest(t *testing.T, ctx context.Context, client *ent.Client) *ent.QuotaResetRequest {
	t.Helper()
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, ctx, client, "bob", "bob@example.org", nil, "user")
	finalApprover := createQuotaResetUser(t, ctx, client, "carol", "carol@example.org", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	workflow := workflowFixtureForUsers(requester.ID, approver.ID, finalApprover.ID, false)
	workflow.Requester.DisplayName = "Alice Example"
	workflow.Requester.DepartmentPaths = []string{"Company / Group Alpha"}
	workflow.Steps[0].Approvers[0].NotificationIDs = map[string]string{"wecom": "bob-wecom"}
	return createPendingWorkflowRequest(t, ctx, client, requester, provider, workflow)
}

func assertNoJSONFields(t *testing.T, value any, forbidden ...string) {
	t.Helper()
	forbiddenFields := make(map[string]struct{}, len(forbidden))
	for _, field := range forbidden {
		forbiddenFields[field] = struct{}{}
	}
	var visit func(string, any)
	visit = func(path string, value any) {
		switch typed := value.(type) {
		case map[string]any:
			for field, child := range typed {
				if _, forbidden := forbiddenFields[field]; forbidden {
					t.Fatalf("generic webhook leaked field %q at %s", field, path)
				}
				visit(path+"."+field, child)
			}
		case []any:
			for index, child := range typed {
				visit(fmt.Sprintf("%s[%d]", path, index), child)
			}
		}
	}
	visit("payload", value)
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
