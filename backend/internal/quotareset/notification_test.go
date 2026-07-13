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
	"time"
	"unicode/utf8"

	"github.com/ai-efficiency/backend/ent"
	entcredential "github.com/ai-efficiency/backend/ent/credential"
	"github.com/ai-efficiency/backend/ent/quotaresetnotificationsetting"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestevent"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestnode"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestGenericWebhookAdapterRendersVersionedWorkflowPayload(t *testing.T) {
	ctx := notificationAdapterTestContext()
	rendered, err := (genericWebhookAdapter{}).Render(ctx)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if got := rendered.Headers.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	var payload map[string]any
	if err := json.Unmarshal(rendered.Body, &payload); err != nil {
		t.Fatalf("decode generic payload: %v", err)
	}
	if got := payload["schema_version"]; got != float64(2) {
		t.Fatalf("schema_version = %#v, want 2", got)
	}
	if got := payload["event"]; got != string(NotificationNodeActivated) {
		t.Fatalf("event = %#v, want %q", got, NotificationNodeActivated)
	}
	if got := payload["occurred_at"]; got != "2026-07-10T02:00:00Z" {
		t.Fatalf("occurred_at = %#v, want UTC RFC3339", got)
	}
	request := notificationJSONMap(t, payload["request"], "request")
	if request["id"] != float64(123) || request["status"] != "pending" || request["reason"] != "Complete a time-sensitive build investigation." {
		t.Fatalf("request payload = %#v", request)
	}
	requester := notificationJSONMap(t, request["requester"], "request.requester")
	if requester["display_name"] != "Alice" || requester["email"] != "alice@example.com" {
		t.Fatalf("requester payload = %#v", requester)
	}
	if got := requester["departments"]; !reflect.DeepEqual(got, []any{"Department Alpha / Team One"}) {
		t.Fatalf("requester departments = %#v", got)
	}
	group := notificationJSONMap(t, request["subscription_group"], "request.subscription_group")
	if group["id"] != "42" || group["name"] != "Group Alpha" || group["platform"] != "openai" {
		t.Fatalf("subscription group = %#v", group)
	}
	node := notificationJSONMap(t, payload["current_node"], "current_node")
	if node["id"] != float64(456) || node["position"] != float64(1) || node["total"] != float64(3) || node["label"] != "Department Beta" {
		t.Fatalf("current_node = %#v", node)
	}
	history, ok := payload["approval_history"].([]any)
	if !ok || len(history) != 1 {
		t.Fatalf("approval_history = %#v, want one decision", payload["approval_history"])
	}
	decision := notificationJSONMap(t, history[0], "approval_history[0]")
	if decision["actor_display_name"] != "Dana" || decision["decision"] != "approve" || decision["comment"] != "Approved the initial review." {
		t.Fatalf("approval decision = %#v", decision)
	}
	if payload["action_url"] != "https://ai-efficiency.example.com/usage/quota-reset?request_id=123" {
		t.Fatalf("action_url = %#v", payload["action_url"])
	}
	body := string(rendered.Body)
	for _, secret := range []string{"alice-wecom-id", "bob-wecom-id", "notification_ids", "recipients"} {
		if strings.Contains(body, secret) {
			t.Fatalf("generic payload exposed channel recipient data %q: %s", secret, body)
		}
	}
}

func TestWeComAdapterRendersMarkdownRequesterTeamReasonAndMentions(t *testing.T) {
	rendered, err := (weComGroupRobotAdapter{maxBytes: 4096}).Render(notificationAdapterTestContext())
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	msgType, content := decodeWeComNotification(t, rendered.Body)
	if msgType != "markdown" {
		t.Fatalf("msgtype = %q, want markdown", msgType)
	}
	for _, want := range []string{
		"# 额度重置待审批",
		"Alice",
		"Department Alpha / Team One",
		"Group Alpha",
		"Complete a time-sensitive build investigation.",
		"2/3",
		"<@bob-wecom-id>",
		"/usage/quota-reset?request_id=123",
		"Dana",
		"Approved the initial review.",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("content = %q, want %q", content, want)
		}
	}
	if rendered.RecipientCount != 1 || len(rendered.MissingRecipientUserIDs) != 0 {
		t.Fatalf("recipient coverage = %d/%v, want 1/none", rendered.RecipientCount, rendered.MissingRecipientUserIDs)
	}
}

func TestWeComAdapterEscapesUserControlledMentionAndMarkdownSyntax(t *testing.T) {
	ctx := notificationAdapterTestContext()
	ctx.Reason = "<@all> **approve now**"
	ctx.Requester.DisplayName = "Alice[ops]`"
	rendered, err := (weComGroupRobotAdapter{maxBytes: 4096}).Render(ctx)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	_, content := decodeWeComNotification(t, rendered.Body)
	if strings.Contains(content, "<@all>") || strings.Contains(content, "**approve now**") || strings.Contains(content, "Alice[ops]`") {
		t.Fatalf("content retained user-controlled mention or Markdown syntax: %q", content)
	}
	for _, want := range []string{"＜@all＞", "＊＊approve now＊＊", "Alice［ops］｀", "<@bob-wecom-id>"} {
		if !strings.Contains(content, want) {
			t.Fatalf("escaped content = %q, want %q", content, want)
		}
	}
}

func TestWeComAdapterReportsMissingRecipientCoverageWithoutFailing(t *testing.T) {
	ctx := notificationAdapterTestContext()
	ctx.Recipients = []NotificationPerson{
		{UserID: 7, DisplayName: "Bob", NotificationIDs: map[string]string{"wecom": "bob-wecom-id"}},
		{UserID: 8, DisplayName: "Carol", NotificationIDs: map[string]string{"wecom": "<@all>"}},
		{UserID: 7, DisplayName: "Bob duplicate", NotificationIDs: map[string]string{"wecom": "bob-wecom-id"}},
	}
	rendered, err := (weComGroupRobotAdapter{maxBytes: 4096}).Render(ctx)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	_, content := decodeWeComNotification(t, rendered.Body)
	if strings.Count(content, "<@bob-wecom-id>") != 1 {
		t.Fatalf("content = %q, want one unique Bob mention", content)
	}
	if !strings.Contains(content, "Carol（无法 @）") || strings.Contains(content, "<@all>") {
		t.Fatalf("content = %q, want escaped unavailable Carol recipient", content)
	}
	if rendered.RecipientCount != 1 || !reflect.DeepEqual(rendered.MissingRecipientUserIDs, []int{8}) {
		t.Fatalf("recipient coverage = %d/%v, want 1/[8]", rendered.RecipientCount, rendered.MissingRecipientUserIDs)
	}
}

func TestWeComAdapterKeepsRequiredFieldsWithinByteLimit(t *testing.T) {
	ctx := notificationAdapterTestContext()
	ctx.Requester.DisplayName = "Alice-" + strings.Repeat("申", 3000)
	ctx.DepartmentPaths = []string{"Department Alpha / Team One-" + strings.Repeat("团", 3000)}
	ctx.GroupName = "Group Alpha-" + strings.Repeat("组", 3000)
	ctx.CurrentNode.Label = "Department Beta-" + strings.Repeat("节", 3000)
	ctx.Reason = "Reason-" + strings.Repeat("理", 5000) + "-reason-tail"
	ctx.Recipients = make([]NotificationPerson, 0, 40)
	for i := 0; i < 40; i++ {
		ctx.Recipients = append(ctx.Recipients, NotificationPerson{
			UserID:          i + 1,
			DisplayName:     fmt.Sprintf("Approver %02d", i+1),
			NotificationIDs: map[string]string{"wecom": fmt.Sprintf("approver-%02d-%s", i+1, strings.Repeat("x", 105))},
		})
	}

	rendered, err := (weComGroupRobotAdapter{maxBytes: 4096}).Render(ctx)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	_, content := decodeWeComNotification(t, rendered.Body)
	if got := len([]byte(content)); got > 4096 {
		t.Fatalf("content bytes = %d, want <= 4096", got)
	}
	if content == "" || !utf8.ValidString(content) {
		t.Fatalf("content is blank or invalid UTF-8: %q", content)
	}
	for _, required := range []string{
		"额度重置待审批",
		"申请人：Alice-",
		"所属团队：Department Alpha / Team One-",
		"订阅组：Group Alpha-",
		"当前节点：2/3 · Department Beta-",
		"[进入待处理](https://ai-efficiency.example.com/usage/quota-reset?request_id=123)",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("bounded content = %q, want required %q", content, required)
		}
	}
	if len(rendered.MissingRecipientUserIDs) == 0 {
		t.Fatalf("missing recipient ids = %v, want recipients that did not fit", rendered.MissingRecipientUserIDs)
	}
	if strings.Contains(content, "reason-tail") {
		t.Fatalf("low-priority reason tail survived before required content was bounded: %q", content)
	}
}

func TestWebhookNotifierUsesExplicitChannelInsteadOfURLShape(t *testing.T) {
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

	createNotificationSetting(t, ctx, client, quotaresetnotificationsetting.ChannelTypeGenericWebhook,
		"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic-test-key", quotaresetnotificationsetting.AuthTypeNone, nil)
	notifier := NewWebhookNotifier(client, "", "https://ai-efficiency.example.com")
	notifier.httpClient = &http.Client{Transport: rewriteURLTransport(t, server.URL)}
	if _, err := notifier.Notify(ctx, notificationAdapterTestContext()); err != nil {
		t.Fatalf("Notify() error = %v", err)
	}
	if gotPayload["schema_version"] != float64(2) || gotPayload["msgtype"] != nil {
		t.Fatalf("payload = %#v, want generic v2 payload selected by channel_type", gotPayload)
	}
}

func TestWorkflowActivationNotifiesOnlyActiveNode(t *testing.T) {
	fixture := newWorkflowCreationFixture(t, false, true)
	deliveries := 0
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deliveries++
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	createNotificationSetting(t, fixture.ctx, fixture.client, quotaresetnotificationsetting.ChannelTypeGenericWebhook,
		server.URL, quotaresetnotificationsetting.AuthTypeNone, nil)
	fixture.service.notifier = NewWebhookNotifier(fixture.client, "", "https://ai-efficiency.example.com")

	request, err := fixture.service.CreateRequest(fixture.ctx, CreateRequestInput{
		RequesterUserID: fixture.requester.ID,
		GroupID:         "42",
		Reason:          "Complete a time-sensitive build investigation.",
	})
	if err != nil {
		t.Fatalf("CreateRequest() error = %v", err)
	}
	nodes := workflowRequestNodes(t, fixture.ctx, fixture.client, request.ID)
	if deliveries != 1 {
		t.Fatalf("HTTP deliveries = %d, want 1 for the active node only", deliveries)
	}
	if len(nodes) != 2 || nodes[0].Status != quotaresetrequestnode.StatusSkippedNoApprover || nodes[1].Status != quotaresetrequestnode.StatusActive {
		t.Fatalf("workflow nodes = %#v", nodes)
	}
	currentNode := notificationJSONMap(t, payload["current_node"], "current_node")
	if currentNode["id"] != float64(nodes[1].ID) {
		t.Fatalf("notified node id = %#v, want active node %d", currentNode["id"], nodes[1].ID)
	}
	requestPayload := notificationJSONMap(t, payload["request"], "request")
	requesterPayload := notificationJSONMap(t, requestPayload["requester"], "request.requester")
	if requesterPayload["display_name"] != "member-alice" || !reflect.DeepEqual(requesterPayload["departments"], []any{"Department Alpha"}) {
		t.Fatalf("requester snapshot payload = %#v", requesterPayload)
	}
	approvers, ok := currentNode["approvers"].([]any)
	if !ok || len(approvers) != 1 || notificationJSONMap(t, approvers[0], "current_node.approvers[0]")["display_name"] != "member-approver-beta" {
		t.Fatalf("current approver snapshots = %#v", currentNode["approvers"])
	}
	assertNotificationEventMetadata(t, fixture.ctx, fixture.client, request.ID, 1)
}

func TestWorkflowAdminFallbackUsesOnlyResolvableCurrentAdminRecipients(t *testing.T) {
	fixture := newWorkflowDecisionFixture(t, []workflowNodeFixture{{adminFallback: true}})
	source := createQuotaResetDirectorySource(t, fixture.ctx, fixture.client)
	department := createQuotaResetDepartment(t, fixture.ctx, fixture.client, source.ID, "department-admin", "Department Admin", nil)
	createQuotaResetMember(t, fixture.ctx, fixture.client, source.ID, "admin-wecom-id", fixture.admin.Email, department.ExternalID, &fixture.admin.ID)
	unresolvedAdmin := createQuotaResetUser(t, fixture.ctx, fixture.client, "admin-without-wecom", "admin-without-wecom@example.org", nil, "admin")

	notificationContext, err := fixture.service.notificationContextForRequest(
		fixture.ctx,
		fixture.request.ID,
		fixture.nodes[0].ID,
		NotificationNodeActivated,
	)
	if err != nil {
		t.Fatalf("notificationContextForRequest() error = %v", err)
	}
	if len(notificationContext.Recipients) != 1 || notificationContext.Recipients[0].UserID != fixture.admin.ID || notificationContext.Recipients[0].NotificationIDs["wecom"] != "admin-wecom-id" {
		t.Fatalf("admin fallback recipients = %#v, want only resolvable admin %d and not %d", notificationContext.Recipients, fixture.admin.ID, unresolvedAdmin.ID)
	}
}

func TestApprovalReuseDoesNotNotifySatisfiedLaterNodes(t *testing.T) {
	fixture := newWorkflowDecisionFixture(t, []workflowNodeFixture{{}, {}, {}, {}})
	fixture.replaceApproverIDs(t, 0, fixture.actorA.ID)
	fixture.replaceApproverIDs(t, 1, fixture.actorB.ID)
	fixture.replaceApproverIDs(t, 2, fixture.actorA.ID)
	fixture.replaceApproverIDs(t, 3, fixture.actorA.ID)
	deliveries := 0
	var notifiedNodeIDs []int
	var notifiedDecisionActor string
	var notifiedDecisionComment string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deliveries++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		node := notificationJSONMap(t, payload["current_node"], "current_node")
		notifiedNodeIDs = append(notifiedNodeIDs, int(node["id"].(float64)))
		history, ok := payload["approval_history"].([]any)
		if !ok || len(history) != 1 {
			t.Fatalf("approval_history = %#v, want the completed first-node decision", payload["approval_history"])
		}
		decision := notificationJSONMap(t, history[0], "approval_history[0]")
		notifiedDecisionActor, _ = decision["actor_display_name"].(string)
		notifiedDecisionComment, _ = decision["comment"].(string)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	createNotificationSetting(t, fixture.ctx, fixture.client, quotaresetnotificationsetting.ChannelTypeGenericWebhook,
		server.URL, quotaresetnotificationsetting.AuthTypeNone, nil)
	fixture.service.notifier = NewWebhookNotifier(fixture.client, "", "https://ai-efficiency.example.com")

	updated, err := fixture.service.Approve(fixture.ctx, DecisionInput{
		ActorUserID:    fixture.actorA.ID,
		RequestID:      fixture.request.ID,
		RequestNodeID:  fixture.nodes[0].ID,
		DecisionReason: "Approved at the first review",
	})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if updated.Status.String() != "pending" {
		t.Fatalf("status = %s, want pending", updated.Status)
	}
	if deliveries != 1 || !reflect.DeepEqual(notifiedNodeIDs, []int{fixture.nodes[1].ID}) {
		t.Fatalf("HTTP deliveries/nodes = %d/%v, want one delivery for node %d", deliveries, notifiedNodeIDs, fixture.nodes[1].ID)
	}
	if notifiedDecisionActor != fixture.actorA.Username || notifiedDecisionComment != "Approved at the first review" {
		t.Fatalf("prior decision = %q/%q, want immutable actor/comment", notifiedDecisionActor, notifiedDecisionComment)
	}
	for _, node := range fixture.nodes[2:] {
		if got := fixture.client.QuotaResetRequestNode.GetX(fixture.ctx, node.ID).Status; got != quotaresetrequestnode.StatusSatisfiedByPriorApproval {
			t.Fatalf("auto-satisfied node %d status = %s", node.ID, got)
		}
	}
	assertNotificationEventMetadata(t, fixture.ctx, fixture.client, fixture.request.ID, 1)
}

func TestWebhookNotifierReturnsWeComBusinessError(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":40008,"errmsg":"invalid message type"}`))
	}))
	defer server.Close()
	createNotificationSetting(t, ctx, client, quotaresetnotificationsetting.ChannelTypeWecomGroupRobot,
		"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic-test-key", quotaresetnotificationsetting.AuthTypeNone, nil)
	notifier := NewWebhookNotifier(client, "", "https://ai-efficiency.example.com")
	notifier.httpClient = &http.Client{Transport: rewriteURLTransport(t, server.URL)}

	_, err := notifier.Notify(ctx, notificationAdapterTestContext())
	if err == nil || !strings.Contains(err.Error(), "webhook returned errcode 40008: invalid message type") {
		t.Fatalf("Notify() error = %v, want WeCom errcode failure", err)
	}
}

func TestNotificationTestReturnsMentionCoverageWarning(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	admin := createQuotaResetUser(t, ctx, client, "admin", "admin@example.com", nil, "admin")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer server.Close()
	createNotificationSetting(t, ctx, client, quotaresetnotificationsetting.ChannelTypeWecomGroupRobot,
		"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic-test-key", quotaresetnotificationsetting.AuthTypeNone, nil)
	notifier := NewWebhookNotifier(client, "", "https://ai-efficiency.example.com")
	notifier.httpClient = &http.Client{Transport: rewriteURLTransport(t, server.URL)}
	service := NewService(client, nil, nil, notifier)

	result, err := service.TestNotificationSettings(ctx, admin.ID)
	if err != nil {
		t.Fatalf("TestNotificationSettings() error = %v", err)
	}
	if !result.Delivered || result.RecipientCount != 0 || result.MissingRecipientCount != 1 || result.Warning != "wecom_recipient_unavailable" {
		t.Fatalf("test result = %+v, want delivered coverage warning", result)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal test result: %v", err)
	}
	var publicResult map[string]any
	if err := json.Unmarshal(encoded, &publicResult); err != nil {
		t.Fatalf("decode test result: %v", err)
	}
	if _, exposed := publicResult["missing_recipient_user_ids"]; exposed {
		t.Fatalf("test result exposed recipient ids: %s", encoded)
	}
}

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
	createNotificationSetting(t, ctx, client, quotaresetnotificationsetting.ChannelTypeGenericWebhook,
		server.URL, quotaresetnotificationsetting.AuthTypeBearerToken, &credential.ID)
	notifier := NewWebhookNotifier(client, encryptionKey, "https://ai-efficiency.example.com")
	if _, err := notifier.Notify(ctx, notificationAdapterTestContext()); err != nil {
		t.Fatalf("Notify() error = %v", err)
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
	createNotificationSetting(t, ctx, client, quotaresetnotificationsetting.ChannelTypeGenericWebhook,
		server.URL, quotaresetnotificationsetting.AuthTypeNone, nil)
	notifier := NewWebhookNotifier(client, "", "https://ai-efficiency.example.com")

	_, err := notifier.Notify(ctx, notificationAdapterTestContext())
	if err == nil || !strings.Contains(err.Error(), "webhook returned 500") {
		t.Fatalf("Notify() error = %v, want webhook returned 500", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Notify() returned timeout instead of HTTP status: %v", err)
	}
}

func TestWebhookNotifierRedactsQueryStringFromDeliveryErrors(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	createNotificationSetting(t, ctx, client, quotaresetnotificationsetting.ChannelTypeWecomGroupRobot,
		"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic-test-key", quotaresetnotificationsetting.AuthTypeNone, nil)
	notifier := NewWebhookNotifier(client, "", "https://ai-efficiency.example.com")
	notifier.httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("synthetic delivery failure for %s", request.URL.String())
	})}

	_, err := notifier.Notify(ctx, notificationAdapterTestContext())
	if err == nil {
		t.Fatal("Notify() error = nil, want synthetic delivery failure")
	}
	if strings.Contains(err.Error(), "synthetic-test-key") || strings.Contains(err.Error(), "?key=") {
		t.Fatalf("Notify() error leaked webhook query string: %v", err)
	}
	if !strings.Contains(err.Error(), "https://qyapi.weixin.qq.com/cgi-bin/webhook/send") {
		t.Fatalf("Notify() error = %v, want redacted endpoint context", err)
	}
}

func notificationAdapterTestContext() NotificationContext {
	return NotificationContext{
		Event:      NotificationNodeActivated,
		OccurredAt: time.Date(2026, time.July, 10, 10, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60)),
		RequestID:  123,
		Status:     "pending",
		Requester: NotificationPerson{
			UserID:          1,
			DisplayName:     "Alice",
			Email:           "alice@example.com",
			NotificationIDs: map[string]string{"wecom": "alice-wecom-id"},
		},
		Recipients: []NotificationPerson{{
			UserID:          7,
			DisplayName:     "Bob",
			Email:           "bob@example.org",
			NotificationIDs: map[string]string{"wecom": "bob-wecom-id"},
		}},
		DepartmentPaths: []string{"Department Alpha / Team One"},
		GroupID:         "42",
		GroupName:       "Group Alpha",
		GroupPlatform:   "openai",
		Reason:          "Complete a time-sensitive build investigation.",
		CurrentNode: &NotificationNode{
			ID:       456,
			Position: 1,
			Total:    3,
			Label:    "Department Beta",
			Approvers: []NotificationPerson{{
				UserID:          7,
				DisplayName:     "Bob",
				Email:           "bob@example.org",
				NotificationIDs: map[string]string{"wecom": "bob-wecom-id"},
			}},
		},
		ApprovalHistory: []NotificationDecision{{
			ActorDisplayName: "Dana",
			Decision:         "approve",
			Comment:          "Approved the initial review.",
			CreatedAt:        time.Date(2026, time.July, 10, 1, 30, 0, 0, time.UTC),
		}},
		ActionURL: "https://ai-efficiency.example.com/usage/quota-reset?request_id=123",
	}
}

func notificationJSONMap(t *testing.T, value any, label string) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v, want JSON object", label, value)
	}
	return result
}

func decodeWeComNotification(t *testing.T, body []byte) (string, string) {
	t.Helper()
	var payload struct {
		MsgType  string `json:"msgtype"`
		Markdown struct {
			Content string `json:"content"`
		} `json:"markdown"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode WeCom payload: %v", err)
	}
	return payload.MsgType, payload.Markdown.Content
}

func createNotificationSetting(t *testing.T, ctx context.Context, client *ent.Client, channelType quotaresetnotificationsetting.ChannelType, webhookURL string, authType quotaresetnotificationsetting.AuthType, credentialID *int) {
	t.Helper()
	create := client.QuotaResetNotificationSetting.Create().
		SetEnabled(true).
		SetChannelType(channelType).
		SetChannelTypeConfigured(true).
		SetURL(webhookURL).
		SetAuthType(authType).
		SetCreatedByUserID(1).
		SetUpdatedByUserID(1)
	if credentialID != nil {
		create.SetCredentialID(*credentialID)
	}
	create.SaveX(ctx)
}

func assertNotificationEventMetadata(t *testing.T, ctx context.Context, client *ent.Client, requestID, wantCount int) {
	t.Helper()
	events := client.QuotaResetRequestEvent.Query().
		Where(
			quotaresetrequestevent.RequestIDEQ(requestID),
			quotaresetrequestevent.EventTypeEQ(quotaresetrequestevent.EventTypeNotificationSent),
		).
		AllX(ctx)
	if len(events) != wantCount {
		t.Fatalf("notification_sent events = %d, want %d", len(events), wantCount)
	}
	for _, event := range events {
		if len(event.Metadata) != 4 || event.Metadata["event"] != string(NotificationNodeActivated) || event.Metadata["channel_type"] != "generic_webhook" {
			t.Fatalf("notification metadata = %#v, want redacted delivery facts", event.Metadata)
		}
		if _, ok := event.Metadata["recipient_count"]; !ok {
			t.Fatalf("notification metadata missing recipient_count: %#v", event.Metadata)
		}
		if _, ok := event.Metadata["missing_recipient_count"]; !ok {
			t.Fatalf("notification metadata missing missing_recipient_count: %#v", event.Metadata)
		}
		for _, forbidden := range []string{"recipient_ids", "missing_recipient_user_ids", "comment", "reason"} {
			if _, ok := event.Metadata[forbidden]; ok {
				t.Fatalf("notification metadata exposed %s: %#v", forbidden, event.Metadata)
			}
		}
	}
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
