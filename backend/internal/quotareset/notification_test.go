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
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ai-efficiency/backend/ent"
	entcredential "github.com/ai-efficiency/backend/ent/credential"
	"github.com/ai-efficiency/backend/ent/quotaresetnotificationsetting"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestevent"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestnode"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestnodeapprover"
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

func TestGenericWebhookAdapterBoundsUserControlledPayload(t *testing.T) {
	ctx := notificationAdapterTestContext()
	ctx.Requester.DisplayName = "Requester-" + strings.Repeat("申", 12000)
	ctx.Requester.Email = strings.Repeat("e", 12000) + "@example.com"
	ctx.Requester.NotificationIDs = map[string]string{"wecom": "generic-hidden-requester-channel-id"}
	ctx.Status = "pending-" + strings.Repeat("状", 4000)
	ctx.DepartmentPaths = make([]string, 0, 80)
	for i := 0; i < 80; i++ {
		ctx.DepartmentPaths = append(ctx.DepartmentPaths, fmt.Sprintf("Department %02d-%s", i, strings.Repeat("部", 2000)))
	}
	ctx.GroupID = "group-" + strings.Repeat("标", 4000)
	ctx.GroupName = "Group Alpha-" + strings.Repeat("组", 4000)
	ctx.GroupPlatform = "platform-" + strings.Repeat("平", 4000)
	ctx.Reason = "Reason-" + strings.Repeat("理", 30000) + "-reason-tail"
	ctx.CurrentNode.Label = "Node-" + strings.Repeat("节", 6000)
	ctx.CurrentNode.Approvers = make([]NotificationPerson, 0, 80)
	for i := 0; i < 80; i++ {
		ctx.CurrentNode.Approvers = append(ctx.CurrentNode.Approvers, NotificationPerson{
			UserID:          i + 1,
			DisplayName:     fmt.Sprintf("Approver %02d-%s", i, strings.Repeat("审", 2000)),
			Email:           fmt.Sprintf("approver-%02d-%s@example.org", i, strings.Repeat("e", 2000)),
			NotificationIDs: map[string]string{"wecom": fmt.Sprintf("generic-hidden-approver-channel-id-%d", i)},
		})
	}
	ctx.ApprovalHistory = make([]NotificationDecision, 0, 80)
	for i := 0; i < 80; i++ {
		ctx.ApprovalHistory = append(ctx.ApprovalHistory, NotificationDecision{
			ActorDisplayName: fmt.Sprintf("Reviewer %02d-%s", i, strings.Repeat("批", 2000)),
			Decision:         "approve-" + strings.Repeat("决", 1000),
			Comment:          fmt.Sprintf("Comment %02d-%s-comment-tail", i, strings.Repeat("评", 5000)),
			CreatedAt:        time.Date(2026, time.July, 10, 2, i%60, 0, 0, time.UTC),
		})
	}
	ctx.ActionURL = "https://ai-efficiency.example.com/usage/quota-reset?request_id=123&context=" + strings.Repeat("链", 6000)

	rendered, err := (genericWebhookAdapter{}).Render(ctx)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if got := len(rendered.Body); got > 64*1024 {
		t.Fatalf("generic payload bytes = %d, want <= 65536", got)
	}
	if !utf8.Valid(rendered.Body) {
		t.Fatal("generic payload is not valid UTF-8")
	}
	var payload map[string]any
	if err := json.Unmarshal(rendered.Body, &payload); err != nil {
		t.Fatalf("decode bounded generic payload: %v", err)
	}
	for _, key := range []string{"schema_version", "event", "request", "current_node", "approval_history", "action_url", "occurred_at"} {
		if _, exists := payload[key]; !exists {
			t.Fatalf("bounded payload missing stable key %q: %#v", key, payload)
		}
	}
	if payload["schema_version"] != float64(2) || payload["event"] != string(NotificationNodeActivated) {
		t.Fatalf("bounded payload schema/event = %#v/%#v", payload["schema_version"], payload["event"])
	}
	request := notificationJSONMap(t, payload["request"], "request")
	if request["id"] != float64(123) || request["reason"] == "" || strings.Contains(fmt.Sprint(request["reason"]), "reason-tail") {
		t.Fatalf("bounded request = %#v, want required id and truncated reason", request)
	}
	requester := notificationJSONMap(t, request["requester"], "request.requester")
	departments, ok := requester["departments"].([]any)
	if !ok || len(departments) > 16 {
		t.Fatalf("bounded departments = %#v, want at most 16", requester["departments"])
	}
	node := notificationJSONMap(t, payload["current_node"], "current_node")
	if node["id"] != float64(456) {
		t.Fatalf("bounded current_node = %#v, want required node id", node)
	}
	approvers, ok := node["approvers"].([]any)
	if !ok || len(approvers) > 20 {
		t.Fatalf("bounded approvers = %#v, want at most 20", node["approvers"])
	}
	history, ok := payload["approval_history"].([]any)
	if !ok || len(history) > 16 {
		t.Fatalf("bounded approval_history = %#v, want at most 16", payload["approval_history"])
	}
	if actionURL, ok := payload["action_url"].(string); !ok || !strings.Contains(actionURL, "request_id=123") {
		t.Fatalf("bounded action_url = %#v, want required request link", payload["action_url"])
	}
	body := string(rendered.Body)
	for _, forbidden := range []string{"generic-hidden-", "notification_ids", "recipients"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("bounded generic payload exposed channel data %q", forbidden)
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

func TestWeComAdapterUsesEventAppropriateRecipientLabel(t *testing.T) {
	tests := []struct {
		event     NotificationEvent
		wantLabel string
	}{
		{event: NotificationNodeActivated, wantLabel: "待审批："},
		{event: NotificationRejected, wantLabel: "通知对象："},
		{event: NotificationCancelled, wantLabel: "通知对象："},
		{event: NotificationResetSucceeded, wantLabel: "通知对象："},
		{event: NotificationResetFailed, wantLabel: "通知对象："},
		{event: NotificationTest, wantLabel: "通知对象："},
	}
	for _, tt := range tests {
		t.Run(string(tt.event), func(t *testing.T) {
			ctx := notificationAdapterTestContext()
			ctx.Event = tt.event
			rendered, err := (weComGroupRobotAdapter{maxBytes: 4096}).Render(ctx)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			_, content := decodeWeComNotification(t, rendered.Body)
			if !strings.Contains(content, tt.wantLabel+"<@bob-wecom-id>") {
				t.Fatalf("content = %q, want recipient label %q", content, tt.wantLabel)
			}
			for _, label := range []string{"待审批：", "通知对象："} {
				if label != tt.wantLabel && strings.Contains(content, label) {
					t.Fatalf("content = %q, retained wrong recipient label %q", content, label)
				}
			}
		})
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

func TestWeComAdapterRejectsReservedMentionUserIDs(t *testing.T) {
	ctx := notificationAdapterTestContext()
	ctx.Recipients = []NotificationPerson{
		{UserID: 8, DisplayName: "Reserved Lower", NotificationIDs: map[string]string{"wecom": "all"}},
		{UserID: 9, DisplayName: "Reserved Upper", NotificationIDs: map[string]string{"wecom": " ALL "}},
	}

	rendered, err := (weComGroupRobotAdapter{maxBytes: 4096}).Render(ctx)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	_, content := decodeWeComNotification(t, rendered.Body)
	if strings.Contains(strings.ToLower(content), "<@all>") {
		t.Fatalf("content rendered reserved group mention: %q", content)
	}
	for _, want := range []string{"Reserved Lower（无法 @）", "Reserved Upper（无法 @）"} {
		if !strings.Contains(content, want) {
			t.Fatalf("content = %q, want unavailable marker %q", content, want)
		}
	}
	if rendered.RecipientCount != 0 || !reflect.DeepEqual(rendered.MissingRecipientUserIDs, []int{8, 9}) {
		t.Fatalf("recipient coverage = %d/%v, want 0/[8 9]", rendered.RecipientCount, rendered.MissingRecipientUserIDs)
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

func TestWebhookNotifierReportsActualChannelAndDeliveryState(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	notifier := NewWebhookNotifier(client, "", "https://ai-efficiency.example.com")

	result, err := notifier.Notify(ctx, notificationAdapterTestContext())
	if err != nil {
		t.Fatalf("Notify() without setting error = %v", err)
	}
	if result == nil || result.Delivered || result.ChannelType != "" {
		t.Fatalf("Notify() without setting result = %+v, want non-delivered result without channel", result)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	createNotificationSetting(t, ctx, client, quotaresetnotificationsetting.ChannelTypeGenericWebhook,
		server.URL, quotaresetnotificationsetting.AuthTypeNone, nil)

	result, err = notifier.Notify(ctx, notificationAdapterTestContext())
	if err != nil {
		t.Fatalf("Notify() error = %v", err)
	}
	if result == nil || !result.Delivered || result.ChannelType != quotaresetnotificationsetting.ChannelTypeGenericWebhook.String() {
		t.Fatalf("Notify() result = %+v, want delivered generic_webhook snapshot", result)
	}
}

func TestWorkflowNotificationDoesNotAuditSentWhenSettingDisabledAfterPreload(t *testing.T) {
	fixture := newWorkflowDecisionFixture(t, []workflowNodeFixture{{}})
	fixture.replaceApproverIDs(t, 0, fixture.actorA.ID)
	var deliveries atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deliveries.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	createNotificationSetting(t, fixture.ctx, fixture.client, quotaresetnotificationsetting.ChannelTypeGenericWebhook,
		server.URL, quotaresetnotificationsetting.AuthTypeNone, nil)
	setting := fixture.client.QuotaResetNotificationSetting.Query().OnlyX(fixture.ctx)
	inner := NewWebhookNotifier(fixture.client, "", "https://ai-efficiency.example.com")
	fixture.service.notifier = notificationNotifierFunc(func(ctx context.Context, notificationContext NotificationContext) (*NotificationDeliveryResult, error) {
		fixture.client.QuotaResetNotificationSetting.UpdateOneID(setting.ID).SetEnabled(false).SaveX(ctx)
		return inner.Notify(ctx, notificationContext)
	})

	if err := fixture.service.notifyRequestEvent(fixture.ctx, fixture.request.ID, fixture.nodes[0].ID, NotificationNodeActivated); err != nil {
		t.Fatalf("notifyRequestEvent() error = %v", err)
	}
	if got := deliveries.Load(); got != 0 {
		t.Fatalf("HTTP deliveries = %d, want 0 after setting disable", got)
	}
	sentEvents := fixture.client.QuotaResetRequestEvent.Query().
		Where(
			quotaresetrequestevent.RequestIDEQ(fixture.request.ID),
			quotaresetrequestevent.EventTypeEQ(quotaresetrequestevent.EventTypeNotificationSent),
		).
		AllX(fixture.ctx)
	if len(sentEvents) != 0 {
		t.Fatalf("notification_sent events = %d, want 0 for non-delivery", len(sentEvents))
	}
}

func TestNotificationTestDoesNotClaimDeliveryWhenSettingDisabledAfterPreload(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	admin := createQuotaResetUser(t, ctx, client, "admin-disabled", "admin-disabled@example.com", nil, "admin")
	var deliveries atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deliveries.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	createNotificationSetting(t, ctx, client, quotaresetnotificationsetting.ChannelTypeGenericWebhook,
		server.URL, quotaresetnotificationsetting.AuthTypeNone, nil)
	setting := client.QuotaResetNotificationSetting.Query().OnlyX(ctx)
	inner := NewWebhookNotifier(client, "", "https://ai-efficiency.example.com")
	service := NewService(client, nil, nil, notificationNotifierFunc(func(ctx context.Context, notificationContext NotificationContext) (*NotificationDeliveryResult, error) {
		client.QuotaResetNotificationSetting.UpdateOneID(setting.ID).SetEnabled(false).SaveX(ctx)
		return inner.Notify(ctx, notificationContext)
	}))

	result, err := service.TestNotificationSettings(ctx, admin.ID)
	if err != nil {
		t.Fatalf("TestNotificationSettings() error = %v", err)
	}
	if result == nil || result.Delivered {
		t.Fatalf("test result = %+v, want non-delivered result", result)
	}
	if got := deliveries.Load(); got != 0 {
		t.Fatalf("HTTP deliveries = %d, want 0 after setting disable", got)
	}
}

func TestWorkflowNotificationAuditsActualNotifierChannel(t *testing.T) {
	fixture := newWorkflowDecisionFixture(t, []workflowNodeFixture{{}})
	fixture.replaceApproverIDs(t, 0, fixture.actorA.ID)
	var deliveries atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deliveries.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer server.Close()
	createNotificationSetting(t, fixture.ctx, fixture.client, quotaresetnotificationsetting.ChannelTypeGenericWebhook,
		server.URL, quotaresetnotificationsetting.AuthTypeNone, nil)
	setting := fixture.client.QuotaResetNotificationSetting.Query().OnlyX(fixture.ctx)
	inner := NewWebhookNotifier(fixture.client, "", "https://ai-efficiency.example.com")
	inner.httpClient = &http.Client{Transport: rewriteURLTransport(t, server.URL)}
	fixture.service.notifier = notificationNotifierFunc(func(ctx context.Context, notificationContext NotificationContext) (*NotificationDeliveryResult, error) {
		fixture.client.QuotaResetNotificationSetting.UpdateOneID(setting.ID).
			SetChannelType(quotaresetnotificationsetting.ChannelTypeWecomGroupRobot).
			SetURL("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic-channel-switch-key").
			SetAuthType(quotaresetnotificationsetting.AuthTypeNone).
			SaveX(ctx)
		return inner.Notify(ctx, notificationContext)
	})

	if err := fixture.service.notifyRequestEvent(fixture.ctx, fixture.request.ID, fixture.nodes[0].ID, NotificationNodeActivated); err != nil {
		t.Fatalf("notifyRequestEvent() error = %v", err)
	}
	if got := deliveries.Load(); got != 1 {
		t.Fatalf("HTTP deliveries = %d, want 1", got)
	}
	event := fixture.client.QuotaResetRequestEvent.Query().
		Where(
			quotaresetrequestevent.RequestIDEQ(fixture.request.ID),
			quotaresetrequestevent.EventTypeEQ(quotaresetrequestevent.EventTypeNotificationSent),
		).
		OnlyX(fixture.ctx)
	if event.Metadata["channel_type"] != quotaresetnotificationsetting.ChannelTypeWecomGroupRobot.String() {
		t.Fatalf("notification metadata = %#v, want actual WeCom channel", event.Metadata)
	}
}

func TestNotificationTestUsesActualChannelAfterConcurrentSwitch(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	admin := createQuotaResetUser(t, ctx, client, "admin-switch", "admin-switch@example.com", nil, "admin")
	source := createQuotaResetDirectorySource(t, ctx, client)
	department := createQuotaResetDepartment(t, ctx, client, source.ID, "department-switch", "Department Switch", nil)
	member := createQuotaResetMember(t, ctx, client, source.ID, "switch-member", admin.Email, department.ExternalID, &admin.ID)
	client.DirectoryMember.UpdateOneID(member.ID).
		SetMetadata(map[string]any{"wecom_userid": "all"}).
		SaveX(ctx)

	var deliveredContent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Markdown struct {
				Content string `json:"content"`
			} `json:"markdown"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode switched test notification: %v", err)
		}
		deliveredContent = payload.Markdown.Content
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer server.Close()
	createNotificationSetting(t, ctx, client, quotaresetnotificationsetting.ChannelTypeGenericWebhook,
		server.URL, quotaresetnotificationsetting.AuthTypeNone, nil)
	setting := client.QuotaResetNotificationSetting.Query().OnlyX(ctx)
	inner := NewWebhookNotifier(client, "", "https://ai-efficiency.example.com")
	inner.httpClient = &http.Client{Transport: rewriteURLTransport(t, server.URL)}
	service := NewService(client, nil, nil, notificationNotifierFunc(func(ctx context.Context, notificationContext NotificationContext) (*NotificationDeliveryResult, error) {
		client.QuotaResetNotificationSetting.UpdateOneID(setting.ID).
			SetChannelType(quotaresetnotificationsetting.ChannelTypeWecomGroupRobot).
			SetURL("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic-test-switch-key").
			SetAuthType(quotaresetnotificationsetting.AuthTypeNone).
			SaveX(ctx)
		return inner.Notify(ctx, notificationContext)
	}))

	result, err := service.TestNotificationSettings(ctx, admin.ID)
	if err != nil {
		t.Fatalf("TestNotificationSettings() error = %v", err)
	}
	if result == nil || !result.Delivered || result.RecipientCount != 0 || result.MissingRecipientCount != 1 || result.Warning != "wecom_recipient_unavailable" {
		t.Fatalf("test result = %+v, want actual-channel unavailable-recipient warning", result)
	}
	if strings.Contains(strings.ToLower(deliveredContent), "<@all>") {
		t.Fatalf("switched test notification rendered reserved group mention: %q", deliveredContent)
	}
}

func TestWebhookNotifierRejectsRedirectWithoutFollowing(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	var sourceDeliveries atomic.Int32
	var targetDeliveries atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		sourceDeliveries.Add(1)
		http.Redirect(w, r, "/target", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/target", func(w http.ResponseWriter, r *http.Request) {
		targetDeliveries.Add(1)
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	createNotificationSetting(t, ctx, client, quotaresetnotificationsetting.ChannelTypeGenericWebhook,
		server.URL+"/redirect", quotaresetnotificationsetting.AuthTypeNone, nil)
	notifier := NewWebhookNotifier(client, "", "https://ai-efficiency.example.com")

	_, err := notifier.Notify(ctx, notificationAdapterTestContext())
	if err == nil || !strings.Contains(err.Error(), "webhook returned 307") {
		t.Fatalf("Notify() error = %v, want redirect status failure", err)
	}
	if got := sourceDeliveries.Load(); got != 1 {
		t.Fatalf("redirect source deliveries = %d, want 1", got)
	}
	if got := targetDeliveries.Load(); got != 0 {
		t.Fatalf("redirect target deliveries = %d, want 0", got)
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

func TestWorkflowActivationSkipsCancelledRequestAfterDelayedContext(t *testing.T) {
	fixture := newWorkflowDecisionFixture(t, []workflowNodeFixture{{}})
	fixture.replaceApproverIDs(t, 0, fixture.actorA.ID)
	deliveredEvents := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode notification payload: %v", err)
		}
		deliveredEvents = append(deliveredEvents, fmt.Sprint(payload["event"]))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	createNotificationSetting(t, fixture.ctx, fixture.client, quotaresetnotificationsetting.ChannelTypeGenericWebhook,
		server.URL, quotaresetnotificationsetting.AuthTypeNone, nil)
	fixture.service.notifier = NewWebhookNotifier(fixture.client, "", "https://ai-efficiency.example.com")

	if _, err := fixture.service.Cancel(fixture.ctx, fixture.requester.ID, fixture.request.ID); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if !reflect.DeepEqual(deliveredEvents, []string{string(NotificationCancelled)}) {
		t.Fatalf("events after cancellation = %v, want cancellation delivery", deliveredEvents)
	}

	err := fixture.service.notifyActiveNode(fixture.ctx, fixture.request.ID, fixture.nodes[0].ID)
	if err == nil {
		t.Fatal("notifyActiveNode() error = nil for cancelled request")
	}
	if !reflect.DeepEqual(deliveredEvents, []string{string(NotificationCancelled)}) {
		t.Fatalf("events after delayed activation = %v, want no activation delivery", deliveredEvents)
	}
	sentEvents := fixture.client.QuotaResetRequestEvent.Query().
		Where(
			quotaresetrequestevent.RequestIDEQ(fixture.request.ID),
			quotaresetrequestevent.EventTypeEQ(quotaresetrequestevent.EventTypeNotificationSent),
		).
		AllX(fixture.ctx)
	if len(sentEvents) != 1 || sentEvents[0].Metadata["event"] != string(NotificationCancelled) {
		t.Fatalf("notification_sent events = %#v, want cancellation only", sentEvents)
	}
}

func TestWorkflowAdminFallbackPreservesMissingRecipientCoverage(t *testing.T) {
	fixture := newWorkflowDecisionFixture(t, []workflowNodeFixture{{adminFallback: true}})
	source := createQuotaResetDirectorySource(t, fixture.ctx, fixture.client)
	department := createQuotaResetDepartment(t, fixture.ctx, fixture.client, source.ID, "department-admin", "Department Admin", nil)
	createQuotaResetMember(t, fixture.ctx, fixture.client, source.ID, "admin-wecom-id", fixture.admin.Email, department.ExternalID, &fixture.admin.ID)
	unresolvedAdmin := createQuotaResetUser(t, fixture.ctx, fixture.client, "admin-without-wecom", "admin-without-wecom@example.org", nil, "admin")

	for _, event := range []NotificationEvent{NotificationNodeActivated, NotificationCancelled} {
		t.Run(string(event), func(t *testing.T) {
			notificationContext, err := fixture.service.notificationContextForRequest(
				fixture.ctx,
				fixture.request.ID,
				fixture.nodes[0].ID,
				event,
			)
			if err != nil {
				t.Fatalf("notificationContextForRequest() error = %v", err)
			}
			if len(notificationContext.Recipients) != 2 || notificationContext.Recipients[0].UserID != fixture.admin.ID || notificationContext.Recipients[1].UserID != unresolvedAdmin.ID {
				t.Fatalf("admin fallback recipients = %#v, want both current admins", notificationContext.Recipients)
			}
			if notificationContext.Recipients[0].NotificationIDs["wecom"] != "admin-wecom-id" || len(notificationContext.Recipients[1].NotificationIDs) != 0 {
				t.Fatalf("admin fallback identities = %#v, want live resolved and unresolved identities retained", notificationContext.Recipients)
			}
			rendered, err := (weComGroupRobotAdapter{maxBytes: 4096}).Render(notificationContext)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			_, content := decodeWeComNotification(t, rendered.Body)
			if !strings.Contains(content, "<@admin-wecom-id>") || !strings.Contains(content, "admin-without-wecom（无法 @）") {
				t.Fatalf("content = %q, want resolved mention and unresolved marker", content)
			}
			if rendered.RecipientCount != 1 || !reflect.DeepEqual(rendered.MissingRecipientUserIDs, []int{unresolvedAdmin.ID}) {
				t.Fatalf("recipient coverage = %d/%v, want 1/%v", rendered.RecipientCount, rendered.MissingRecipientUserIDs, []int{unresolvedAdmin.ID})
			}
		})
	}
}

func TestWorkflowNotificationsRevalidateSnapshottedApprovers(t *testing.T) {
	tests := []struct {
		name               string
		configureDirectory func(t *testing.T, fixture *workflowDecisionFixture, source *ent.DirectorySource, department *ent.DirectoryDepartment)
		wantRecipient      func(fixture *workflowDecisionFixture) *ent.User
		wantDisplayName    string
		wantWeComRecipient string
		forbiddenWeComID   string
	}{
		{
			name: "all snapshotted approvers unusable routes only current admins",
			configureDirectory: func(t *testing.T, fixture *workflowDecisionFixture, source *ent.DirectorySource, department *ent.DirectoryDepartment) {
				inactive := createQuotaResetMember(t, fixture.ctx, fixture.client, source.ID, "live-approver-alpha", fixture.actorA.Email, department.ExternalID, &fixture.actorA.ID)
				fixture.client.DirectoryMember.UpdateOneID(inactive.ID).SetStatus("inactive").SaveX(fixture.ctx)
				createQuotaResetMember(t, fixture.ctx, fixture.client, source.ID, "live-approver-beta", fixture.actorB.Email, department.ExternalID, &fixture.actorB.ID)
				fixture.client.User.UpdateOneID(fixture.actorB.ID).SetTokenValidAfter(time.Now().UTC()).SaveX(fixture.ctx)
			},
			wantRecipient:      func(fixture *workflowDecisionFixture) *ent.User { return fixture.admin },
			wantDisplayName:    "live-admin",
			wantWeComRecipient: "live-admin",
		},
		{
			name: "one snapshotted approver usable routes only that approver",
			configureDirectory: func(t *testing.T, fixture *workflowDecisionFixture, source *ent.DirectorySource, department *ent.DirectoryDepartment) {
				createQuotaResetMember(t, fixture.ctx, fixture.client, source.ID, "live-approver-alpha", fixture.actorA.Email, department.ExternalID, &fixture.actorA.ID)
				inactive := createQuotaResetMember(t, fixture.ctx, fixture.client, source.ID, "live-approver-beta", fixture.actorB.Email, department.ExternalID, &fixture.actorB.ID)
				fixture.client.DirectoryMember.UpdateOneID(inactive.ID).SetStatus("inactive").SaveX(fixture.ctx)
			},
			wantRecipient:      func(fixture *workflowDecisionFixture) *ent.User { return fixture.actorA },
			wantDisplayName:    "live-approver-alpha",
			wantWeComRecipient: "live-approver-alpha",
		},
		{
			name: "usable approver without WeCom id does not trigger admin fallback",
			configureDirectory: func(t *testing.T, fixture *workflowDecisionFixture, source *ent.DirectorySource, department *ent.DirectoryDepartment) {
				member := createQuotaResetMember(t, fixture.ctx, fixture.client, source.ID, "", fixture.actorA.Email, department.ExternalID, &fixture.actorA.ID)
				fixture.client.DirectoryMember.UpdateOneID(member.ID).SetDisplayName("Live Approver Alpha").SaveX(fixture.ctx)
				inactive := createQuotaResetMember(t, fixture.ctx, fixture.client, source.ID, "live-approver-beta", fixture.actorB.Email, department.ExternalID, &fixture.actorB.ID)
				fixture.client.DirectoryMember.UpdateOneID(inactive.ID).SetStatus("inactive").SaveX(fixture.ctx)
			},
			wantRecipient:   func(fixture *workflowDecisionFixture) *ent.User { return fixture.actorA },
			wantDisplayName: "Live Approver Alpha",
		},
		{
			name: "matched user id conflict does not email fallback or leak notification id",
			configureDirectory: func(t *testing.T, fixture *workflowDecisionFixture, source *ent.DirectorySource, department *ent.DirectoryDepartment) {
				formerUser := createQuotaResetUser(t, fixture.ctx, fixture.client, "former-approver", "former-approver@example.net", nil, "user")
				createQuotaResetMember(t, fixture.ctx, fixture.client, source.ID, "former-user-wecom-id", fixture.actorA.Email, department.ExternalID, &formerUser.ID)
				inactive := createQuotaResetMember(t, fixture.ctx, fixture.client, source.ID, "live-approver-beta", fixture.actorB.Email, department.ExternalID, &fixture.actorB.ID)
				fixture.client.DirectoryMember.UpdateOneID(inactive.ID).SetStatus("inactive").SaveX(fixture.ctx)
			},
			wantRecipient:      func(fixture *workflowDecisionFixture) *ent.User { return fixture.admin },
			wantDisplayName:    "live-admin",
			wantWeComRecipient: "live-admin",
			forbiddenWeComID:   "former-user-wecom-id",
		},
	}

	for _, event := range []NotificationEvent{NotificationNodeActivated, NotificationCancelled} {
		for _, tt := range tests {
			t.Run(string(event)+"/"+tt.name, func(t *testing.T) {
				fixture := newWorkflowDecisionFixture(t, []workflowNodeFixture{{}})
				fixture.replaceApproverIDs(t, 0, fixture.actorA.ID, fixture.actorB.ID)

				source := createQuotaResetDirectorySource(t, fixture.ctx, fixture.client)
				department := createQuotaResetDepartment(t, fixture.ctx, fixture.client, source.ID, "department-live", "Department Live", nil)
				createQuotaResetMember(t, fixture.ctx, fixture.client, source.ID, "live-admin", fixture.admin.Email, department.ExternalID, &fixture.admin.ID)
				tt.configureDirectory(t, fixture, source, department)

				notificationContext, err := fixture.service.notificationContextForRequest(
					fixture.ctx,
					fixture.request.ID,
					fixture.nodes[0].ID,
					event,
				)
				if err != nil {
					t.Fatalf("notificationContextForRequest() error = %v", err)
				}
				wantRecipient := tt.wantRecipient(fixture)
				if len(notificationContext.Recipients) != 1 || notificationContext.Recipients[0].UserID != wantRecipient.ID {
					t.Fatalf("recipients = %#v, want only user %d", notificationContext.Recipients, wantRecipient.ID)
				}
				recipient := notificationContext.Recipients[0]
				if recipient.DisplayName != tt.wantDisplayName || recipient.NotificationIDs["wecom"] != tt.wantWeComRecipient {
					t.Fatalf("live recipient = %#v, want display name %q and WeCom id %q", recipient, tt.wantDisplayName, tt.wantWeComRecipient)
				}
				if tt.forbiddenWeComID != "" {
					for _, recipient := range notificationContext.Recipients {
						if recipient.NotificationIDs["wecom"] == tt.forbiddenWeComID {
							t.Fatalf("recipients leaked conflicting member identity %q: %#v", tt.forbiddenWeComID, notificationContext.Recipients)
						}
					}
					rendered, err := (weComGroupRobotAdapter{maxBytes: 4096}).Render(notificationContext)
					if err != nil {
						t.Fatalf("Render() error = %v", err)
					}
					_, content := decodeWeComNotification(t, rendered.Body)
					if strings.Contains(content, tt.forbiddenWeComID) {
						t.Fatalf("notification content leaked conflicting member identity %q: %s", tt.forbiddenWeComID, content)
					}
				}
				if notificationContext.CurrentNode == nil || notificationContext.CurrentNode.AdminFallback {
					t.Fatalf("current node = %#v, want immutable non-fallback snapshot", notificationContext.CurrentNode)
				}
				if len(notificationContext.CurrentNode.Approvers) != 2 {
					t.Fatalf("current node approvers = %#v, want two immutable snapshots", notificationContext.CurrentNode.Approvers)
				}
				wantSnapshotNames := map[int]string{
					fixture.actorA.ID: fixture.actorA.Username,
					fixture.actorB.ID: fixture.actorB.Username,
				}
				for _, approver := range notificationContext.CurrentNode.Approvers {
					if approver.DisplayName != wantSnapshotNames[approver.UserID] || len(approver.NotificationIDs) != 0 {
						t.Fatalf("current node approver = %#v, want unchanged audit snapshot", approver)
					}
				}
			})
		}
	}
}

func TestCurrentNotificationPeopleQueriesOnlyPotentialDirectoryMatches(t *testing.T) {
	ctx := context.Background()
	client, dsn := testdb.OpenWithDSN(t)
	matchedUser := createQuotaResetUser(t, ctx, client, "matched-user", "matched-user@example.com", nil, "user")
	conflictingEmailUser := createQuotaResetUser(t, ctx, client, "email-conflict", "email-conflict@example.org", nil, "user")
	emailFallbackUser := createQuotaResetUser(t, ctx, client, "email-fallback", "email-fallback@example.com", nil, "user")
	unrelatedUser := createQuotaResetUser(t, ctx, client, "unrelated-user", "unrelated-user@example.org", nil, "user")
	source := createQuotaResetDirectorySource(t, ctx, client)
	department := createQuotaResetDepartment(t, ctx, client, source.ID, "department-targeted", "Department Targeted", nil)
	createQuotaResetMember(t, ctx, client, source.ID, "matched-id-wecom", conflictingEmailUser.Email, department.ExternalID, &matchedUser.ID)
	createQuotaResetMember(t, ctx, client, source.ID, "email-fallback-wecom", emailFallbackUser.Email, department.ExternalID, nil)
	inactive := createQuotaResetMember(t, ctx, client, source.ID, "inactive-conflict-wecom", "inactive-conflict@example.net", department.ExternalID, &conflictingEmailUser.ID)
	client.DirectoryMember.UpdateOneID(inactive.ID).SetStatus("inactive").SaveX(ctx)
	createQuotaResetMember(t, ctx, client, source.ID, "unrelated-wecom", unrelatedUser.Email, department.ExternalID, &unrelatedUser.ID)

	queryLogs := make([]string, 0)
	debugClient, err := ent.Open("postgres", dsn, ent.Debug(), ent.Log(func(values ...any) {
		queryLogs = append(queryLogs, fmt.Sprint(values...))
	}))
	if err != nil {
		t.Fatalf("open debug ent client: %v", err)
	}
	t.Cleanup(func() { _ = debugClient.Close() })
	service := NewService(debugClient, nil, nil, nil)

	people, err := service.currentNotificationPeople(ctx, []*ent.User{emailFallbackUser, matchedUser, conflictingEmailUser})
	if err != nil {
		t.Fatalf("currentNotificationPeople() error = %v", err)
	}
	if len(people) != 3 || people[0].UserID != emailFallbackUser.ID || people[1].UserID != matchedUser.ID || people[2].UserID != conflictingEmailUser.ID {
		t.Fatalf("notification people order = %#v, want requested user order", people)
	}
	if people[0].NotificationIDs["wecom"] != "email-fallback-wecom" || people[1].NotificationIDs["wecom"] != "matched-id-wecom" {
		t.Fatalf("resolved notification identities = %#v, want email fallback and matched-id identities", people)
	}
	if len(people[2].NotificationIDs) != 0 {
		t.Fatalf("inactive/conflicting identity resolved for user %d: %#v", conflictingEmailUser.ID, people[2])
	}

	directoryQuery := ""
	for _, queryLog := range queryLogs {
		if strings.Contains(queryLog, `FROM "directory_members"`) {
			directoryQuery = queryLog
		}
	}
	whereIndex := strings.Index(directoryQuery, " WHERE ")
	if whereIndex < 0 {
		t.Fatalf("directory member query missing WHERE clause: %q", directoryQuery)
	}
	whereClause := directoryQuery[whereIndex:]
	for _, required := range []string{`"source_id"`, `"matched_user_id"`, `"email_normalized"`, " OR "} {
		if !strings.Contains(whereClause, required) {
			t.Fatalf("directory member WHERE clause = %q, want %q", whereClause, required)
		}
	}
	if strings.Contains(directoryQuery, unrelatedUser.Email) {
		t.Fatalf("directory member query included unrelated email: %q", directoryQuery)
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

func TestWebhookNotifierRejectsOversizedOrMalformedWeComResponse(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "oversized nonzero errcode",
			body: `{"errcode":40008,"errmsg":"` + strings.Repeat("x", maxWebhookResponseBodyBytes+256) + `"}`,
		},
		{
			name: "malformed business response",
			body: `{"errcode":`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			client := testdb.Open(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			createNotificationSetting(t, ctx, client, quotaresetnotificationsetting.ChannelTypeWecomGroupRobot,
				"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic-response-key", quotaresetnotificationsetting.AuthTypeNone, nil)
			notifier := NewWebhookNotifier(client, "", "https://ai-efficiency.example.com")
			notifier.httpClient = &http.Client{Transport: rewriteURLTransport(t, server.URL)}

			if _, err := notifier.Notify(ctx, notificationAdapterTestContext()); err == nil {
				t.Fatalf("Notify() error = nil for %s response", tt.name)
			}
		})
	}
}

func TestNotificationTestReturnsMentionCoverageWarning(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	admin := createQuotaResetUser(t, ctx, client, "admin", "admin@example.com", nil, "admin")
	var deliveredContent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Markdown struct {
				Content string `json:"content"`
			} `json:"markdown"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode test notification: %v", err)
		}
		deliveredContent = payload.Markdown.Content
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
	if strings.Contains(deliveredContent, "admin（无法 @）") || strings.Contains(deliveredContent, "待审批：") {
		t.Fatalf("test notification included unresolved triggering admin: %q", deliveredContent)
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

func TestNotificationTestRejectsReservedWeComMentionUserID(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	admin := createQuotaResetUser(t, ctx, client, "admin-reserved", "admin-reserved@example.com", nil, "admin")
	source := createQuotaResetDirectorySource(t, ctx, client)
	department := createQuotaResetDepartment(t, ctx, client, source.ID, "department-reserved", "Department Reserved", nil)
	member := createQuotaResetMember(t, ctx, client, source.ID, "reserved-member", admin.Email, department.ExternalID, &admin.ID)
	client.DirectoryMember.UpdateOneID(member.ID).
		SetMetadata(map[string]any{"wecom_userid": " ALL "}).
		SaveX(ctx)

	var deliveredContent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Markdown struct {
				Content string `json:"content"`
			} `json:"markdown"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode test notification: %v", err)
		}
		deliveredContent = payload.Markdown.Content
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer server.Close()
	createNotificationSetting(t, ctx, client, quotaresetnotificationsetting.ChannelTypeWecomGroupRobot,
		"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic-reserved-id-key", quotaresetnotificationsetting.AuthTypeNone, nil)
	notifier := NewWebhookNotifier(client, "", "https://ai-efficiency.example.com")
	notifier.httpClient = &http.Client{Transport: rewriteURLTransport(t, server.URL)}
	service := NewService(client, nil, nil, notifier)

	result, err := service.TestNotificationSettings(ctx, admin.ID)
	if err != nil {
		t.Fatalf("TestNotificationSettings() error = %v", err)
	}
	if !result.Delivered || result.RecipientCount != 0 || result.MissingRecipientCount != 1 || result.Warning != "wecom_recipient_unavailable" {
		t.Fatalf("test result = %+v, want delivered unavailable-recipient warning", result)
	}
	if strings.Contains(strings.ToLower(deliveredContent), "<@all>") {
		t.Fatalf("test notification rendered reserved group mention: %q", deliveredContent)
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

func TestNotificationFailurePreservesRenderedRecipientCoverage(t *testing.T) {
	fixture := newWorkflowDecisionFixture(t, []workflowNodeFixture{{}})
	fixture.client.QuotaResetRequestNodeApprover.Delete().
		Where(quotaresetrequestnodeapprover.RequestNodeIDEQ(fixture.nodes[0].ID)).
		ExecX(fixture.ctx)
	for _, user := range []*ent.User{fixture.actorA, fixture.actorB} {
		notificationIDs := map[string]string{"wecom": "all"}
		if user.ID == fixture.actorA.ID {
			notificationIDs["wecom"] = "approver-alpha-wecom-id"
		}
		fixture.client.QuotaResetRequestNodeApprover.Create().
			SetRequestNodeID(fixture.nodes[0].ID).
			SetUserID(user.ID).
			SetDisplayName(user.Username).
			SetEmail(user.Email).
			SetSource(quotaresetrequestnodeapprover.SourceConfigured).
			SetSourceDepartmentExternalIds([]string{"department-review"}).
			SetNotificationIds(notificationIDs).
			SaveX(fixture.ctx)
	}
	source := createQuotaResetDirectorySource(t, fixture.ctx, fixture.client)
	department := createQuotaResetDepartment(t, fixture.ctx, fixture.client, source.ID, "department-coverage", "Department Coverage", nil)
	createQuotaResetMember(t, fixture.ctx, fixture.client, source.ID, "approver-alpha-wecom-id", fixture.actorA.Email, department.ExternalID, &fixture.actorA.ID)
	createQuotaResetMember(t, fixture.ctx, fixture.client, source.ID, "all", fixture.actorB.Email, department.ExternalID, &fixture.actorB.ID)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	createNotificationSetting(t, fixture.ctx, fixture.client, quotaresetnotificationsetting.ChannelTypeWecomGroupRobot,
		"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic-coverage-key", quotaresetnotificationsetting.AuthTypeNone, nil)
	notifier := NewWebhookNotifier(fixture.client, "", "https://ai-efficiency.example.com")
	notifier.httpClient = &http.Client{Transport: rewriteURLTransport(t, server.URL)}
	fixture.service.notifier = notifier

	err := fixture.service.notifyRequestEvent(fixture.ctx, fixture.request.ID, fixture.nodes[0].ID, NotificationNodeActivated)
	if err == nil || !strings.Contains(err.Error(), "webhook returned 500") {
		t.Fatalf("notifyRequestEvent() error = %v, want webhook returned 500", err)
	}
	event := fixture.client.QuotaResetRequestEvent.Query().
		Where(
			quotaresetrequestevent.RequestIDEQ(fixture.request.ID),
			quotaresetrequestevent.EventTypeEQ(quotaresetrequestevent.EventTypeNotificationFailed),
		).
		OnlyX(fixture.ctx)
	if got := notificationMetadataInt(t, event.Metadata, "recipient_count"); got != 1 {
		t.Fatalf("notification_failed recipient_count = %d, want 1", got)
	}
	if got := notificationMetadataInt(t, event.Metadata, "missing_recipient_count"); got != 1 {
		t.Fatalf("notification_failed missing_recipient_count = %d, want 1", got)
	}
	if event.Metadata["channel_type"] != quotaresetnotificationsetting.ChannelTypeWecomGroupRobot.String() {
		t.Fatalf("notification_failed metadata = %#v, want actual WeCom channel", event.Metadata)
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

func TestSanitizedNotificationErrorDoesNotExposeOriginalCause(t *testing.T) {
	const secret = "synthetic-chain-secret"
	rawURL := "https://robot.example.test/webhook/send?key=" + secret
	unsafeCause := &secretBearingNotificationError{message: "secret-bearing cause for " + rawURL}
	sanitized := sanitizeWebhookError(quotaresetnotificationsetting.ChannelTypeWecomGroupRobot, rawURL, unsafeCause)
	returned := fmt.Errorf("read webhook response: %w", sanitized)

	if !strings.Contains(returned.Error(), "read webhook response") || !strings.Contains(returned.Error(), "https://robot.example.test/webhook/send") {
		t.Fatalf("sanitized error lost safe context: %v", returned)
	}
	depth := 0
	for chainErr := error(returned); chainErr != nil; chainErr = errors.Unwrap(chainErr) {
		if strings.Contains(chainErr.Error(), secret) {
			t.Fatalf("error chain element %d exposed secret: %v", depth, chainErr)
		}
		depth++
		if depth > 8 {
			t.Fatal("sanitized error chain did not terminate")
		}
	}
	var recovered *secretBearingNotificationError
	if errors.As(returned, &recovered) {
		t.Fatalf("errors.As recovered unsafe cause: %v", recovered)
	}
}

func TestNotificationFailureRedactsResponseReadSecrets(t *testing.T) {
	fixture := newWorkflowDecisionFixture(t, []workflowNodeFixture{{}})
	const (
		username = "synthetic-robot-user"
		password = "synthetic-robot-password"
		queryKey = "synthetic-read-key"
	)
	robotURL := "https://" + username + ":" + password + "@qyapi.weixin.qq.com/cgi-bin/webhook/send?key=" + queryKey
	createNotificationSetting(t, fixture.ctx, fixture.client, quotaresetnotificationsetting.ChannelTypeWecomGroupRobot,
		robotURL, quotaresetnotificationsetting.AuthTypeNone, nil)
	notifier := NewWebhookNotifier(fixture.client, "", "https://ai-efficiency.example.com")
	notifier.httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		endpoint := request.URL.Scheme + "://" + request.URL.User.String() + "@" + request.URL.Host + request.URL.Path
		readErr := fmt.Errorf("synthetic response read failure from %s; query %s", endpoint, request.URL.RawQuery)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       &notificationReadErrorBody{err: readErr},
			Request:    request,
		}, nil
	})}
	fixture.service.notifier = notifier

	err := fixture.service.notifyRequestEvent(fixture.ctx, fixture.request.ID, fixture.nodes[0].ID, NotificationNodeActivated)
	if err == nil {
		t.Fatal("notifyRequestEvent() error = nil, want response read failure")
	}
	event := fixture.client.QuotaResetRequestEvent.Query().
		Where(
			quotaresetrequestevent.RequestIDEQ(fixture.request.ID),
			quotaresetrequestevent.EventTypeEQ(quotaresetrequestevent.EventTypeNotificationFailed),
		).
		OnlyX(fixture.ctx)
	for label, message := range map[string]string{"returned": err.Error(), "persisted": event.ErrorMessage} {
		for _, forbidden := range []string{username, password, queryKey, "?key=", "key=" + queryKey} {
			if strings.Contains(message, forbidden) {
				t.Fatalf("%s notification error leaked %q: %s", label, forbidden, message)
			}
		}
		if !strings.Contains(message, "read webhook response") || !strings.Contains(message, "qyapi.weixin.qq.com/cgi-bin/webhook/send") {
			t.Fatalf("%s notification error lost useful context: %s", label, message)
		}
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

func notificationMetadataInt(t *testing.T, metadata map[string]any, key string) int {
	t.Helper()
	switch value := metadata[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		t.Fatalf("notification metadata %s = %#v, want integer", key, metadata[key])
		return 0
	}
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

type notificationReadErrorBody struct{ err error }

func (b *notificationReadErrorBody) Read([]byte) (int, error) { return 0, b.err }

func (b *notificationReadErrorBody) Close() error { return nil }

type secretBearingNotificationError struct{ message string }

func (e *secretBearingNotificationError) Error() string { return e.message }

type notificationNotifierFunc func(context.Context, NotificationContext) (*NotificationDeliveryResult, error)

func (fn notificationNotifierFunc) Notify(ctx context.Context, notificationContext NotificationContext) (*NotificationDeliveryResult, error) {
	return fn(ctx, notificationContext)
}
