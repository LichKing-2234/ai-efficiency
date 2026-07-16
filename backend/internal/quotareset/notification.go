package quotareset

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	entcredential "github.com/ai-efficiency/backend/ent/credential"
	"github.com/ai-efficiency/backend/ent/quotaresetnotificationsetting"
	credentialpkg "github.com/ai-efficiency/backend/internal/credential"
	"github.com/ai-efficiency/backend/internal/pkg"
)

const (
	defaultWebhookTimeout       = 5 * time.Second
	maxWebhookResponseBodyBytes = 4096
)

var validWeComUserID = regexp.MustCompile(`^[A-Za-z0-9_.@-]{1,128}$`)

type quotaResetNotificationContext struct {
	Requester        WorkflowPerson
	ActiveApprovers  []WorkflowApprover
	StepIndex        int
	StepCount        int
	StepLabel        string
	PreviousDecision *WorkflowDecision
}

type WebhookNotifier struct {
	client        *ent.Client
	encryptionKey string
	frontendURL   string
	httpClient    *http.Client
}

func NewWebhookNotifier(client *ent.Client, encryptionKey string, frontendURL string) *WebhookNotifier {
	return &WebhookNotifier{
		client:        client,
		encryptionKey: encryptionKey,
		frontendURL:   strings.TrimRight(frontendURL, "/"),
		httpClient:    &http.Client{Timeout: defaultWebhookTimeout},
	}
}

func (n *WebhookNotifier) NotifyRequestEvent(ctx context.Context, event string, req *ent.QuotaResetRequest) error {
	if n == nil || n.client == nil || req == nil {
		return nil
	}
	setting, err := n.client.QuotaResetNotificationSetting.Query().
		Order(ent.Asc(quotaresetnotificationsetting.FieldID)).
		First(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load quota reset notification setting: %w", err)
	}
	if !setting.Enabled || strings.TrimSpace(setting.URL) == "" {
		return nil
	}
	parsed, err := url.Parse(strings.TrimSpace(setting.URL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("invalid webhook url")
	}
	channel := setting.Channel
	if channel == quotaresetnotificationsetting.ChannelLegacyAuto {
		channel = quotaresetnotificationsetting.ChannelGenericWebhook
		if isWeComRobotWebhookURL(parsed) {
			channel = quotaresetnotificationsetting.ChannelWecomGroupRobot
		}
		if _, err := n.client.QuotaResetNotificationSetting.UpdateOneID(setting.ID).SetChannel(channel).Save(ctx); err != nil {
			return fmt.Errorf("backfill quota reset notification channel: %w", err)
		}
	}
	notificationContext, err := notificationContextForRequest(req)
	if err != nil {
		return err
	}
	payload := n.payloadForChannel(channel, event, req, notificationContext)
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if setting.AuthType == quotaresetnotificationsetting.AuthTypeBearerToken {
		token, err := n.bearerToken(ctx, setting)
		if err != nil {
			return err
		}
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := n.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send webhook: request failed")
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxWebhookResponseBodyBytes))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("webhook returned %d", resp.StatusCode)
	}
	if err := webhookResponseBusinessError(respBody); err != nil {
		return err
	}
	return nil
}

func (n *WebhookNotifier) payloadForChannel(channel quotaresetnotificationsetting.Channel, event string, req *ent.QuotaResetRequest, ctx quotaResetNotificationContext) any {
	if channel == quotaresetnotificationsetting.ChannelWecomGroupRobot {
		return map[string]any{
			"msgtype": "markdown",
			"markdown": map[string]string{
				"content": n.weComRobotMarkdown(event, req, ctx),
			},
		}
	}
	return n.payload(event, req, ctx)
}

func (n *WebhookNotifier) payload(event string, req *ent.QuotaResetRequest, ctx quotaResetNotificationContext) map[string]any {
	workflowPayload := map[string]any{
		"step_number":      min(ctx.StepIndex+1, ctx.StepCount),
		"step_count":       ctx.StepCount,
		"step_label":       ctx.StepLabel,
		"active_approvers": ctx.ActiveApprovers,
	}
	if ctx.PreviousDecision != nil {
		workflowPayload["previous_decision"] = ctx.PreviousDecision
	}
	payload := map[string]any{
		"event":                      event,
		"request_id":                 req.ID,
		"status":                     publicQuotaResetStatus(req.Status),
		"requester_user_id":          req.RequesterUserID,
		"provider_id":                req.ProviderID,
		"group_id":                   req.GroupID,
		"group_name":                 req.GroupName,
		"group_platform":             req.GroupPlatform,
		"reason":                     reasonPreview(req.Reason),
		"reason_preview":             reasonPreview(req.Reason),
		"resolved_approver_user_ids": req.ResolvedApproverUserIds,
		"requester":                  ctx.Requester,
		"workflow":                   workflowPayload,
		"occurred_at":                time.Now().UTC().Format(time.RFC3339),
	}
	if n.frontendURL != "" {
		payload["action_url"] = fmt.Sprintf("%s/usage/quota-reset?request_id=%d", n.frontendURL, req.ID)
	}
	return payload
}

func (n *WebhookNotifier) weComRobotMarkdown(event string, req *ent.QuotaResetRequest, ctx quotaResetNotificationContext) string {
	title := "# 额度重置审批通知"
	if event == "quota_reset_request_created" || event == "quota_reset_step_activated" || event == "quota_reset_notification_test" {
		title = "# 额度重置待审批"
	}
	requester := firstWorkflowValue(ctx.Requester.DisplayName, "未知用户")
	if email := strings.TrimSpace(ctx.Requester.Email); email != "" {
		requester += " (" + email + ")"
	}
	team := strings.Join(ctx.Requester.DepartmentPaths, ", ")
	if team == "" {
		team = "未同步"
	}
	lines := []string{
		title,
		"> 事件：" + safeWeComText(quotaResetWebhookEventLabel(event)),
		"> 申请人：" + safeWeComText(requester),
		"> 所属团队：" + safeWeComText(team),
		"> 订阅组：" + safeWeComText(firstWorkflowValue(req.GroupName, req.GroupID)),
		fmt.Sprintf("> 审批进度：%d/%d", min(ctx.StepIndex+1, ctx.StepCount), ctx.StepCount),
	}
	if label := strings.TrimSpace(ctx.StepLabel); label != "" {
		lines = append(lines, "> 当前节点："+safeWeComText(label))
	}
	if reason := reasonPreview(req.Reason); reason != "" {
		lines = append(lines, "> 申请原因："+safeWeComText(reason))
	}
	if ctx.PreviousDecision != nil {
		lines = append(lines, "> 上一审批："+safeWeComText(firstWorkflowValue(ctx.PreviousDecision.ActorDisplayName, "未知用户"))+"："+safeWeComText(ctx.PreviousDecision.Comment))
	}
	mentions := make([]string, 0, len(ctx.ActiveApprovers))
	for _, approver := range ctx.ActiveApprovers {
		userID := strings.TrimSpace(approver.NotificationIDs["wecom"])
		if validWeComUserID.MatchString(userID) && !strings.EqualFold(userID, "all") {
			mentions = append(mentions, "<@"+userID+">")
			continue
		}
		mentions = append(mentions, safeWeComText(firstWorkflowValue(approver.DisplayName, approver.Email, fmt.Sprintf("User #%d", approver.UserID)))+"（无法@）")
	}
	if len(mentions) > 0 {
		lines = append(lines, "审批人："+strings.Join(mentions, " "))
	}
	if n.frontendURL != "" {
		lines = append(lines, "[前往处理]("+fmt.Sprintf("%s/usage/quota-reset?request_id=%d", n.frontendURL, req.ID)+")")
	}
	return strings.Join(lines, "\n")
}

func notificationContextForRequest(req *ent.QuotaResetRequest) (quotaResetNotificationContext, error) {
	ctx := quotaResetNotificationContext{
		Requester: WorkflowPerson{UserID: req.RequesterUserID, DepartmentPaths: []string{}},
		StepCount: 1,
	}
	if req.WorkflowVersion != workflowVersionV2 {
		for _, userID := range req.ResolvedApproverUserIds {
			ctx.ActiveApprovers = append(ctx.ActiveApprovers, WorkflowApprover{UserID: userID})
		}
		return ctx, nil
	}
	workflow, err := DecodeWorkflow(req.Workflow)
	if err != nil {
		return quotaResetNotificationContext{}, err
	}
	ctx.Requester = workflow.Requester
	ctx.StepCount = len(workflow.Steps)
	ctx.StepIndex = workflow.CurrentStep
	if workflow.CurrentStep < len(workflow.Steps) {
		step := workflow.Steps[workflow.CurrentStep]
		ctx.StepLabel = step.Label
		ctx.ActiveApprovers = append([]WorkflowApprover(nil), step.Approvers...)
	}
	for index := min(workflow.CurrentStep, len(workflow.Steps)) - 1; index >= 0; index-- {
		if workflow.Steps[index].Decision != nil {
			ctx.PreviousDecision = workflow.Steps[index].Decision
			break
		}
	}
	return ctx, nil
}

func safeWeComText(value string) string {
	return strings.NewReplacer("<", "＜", ">", "＞", "&", "＆", "`", "｀").Replace(strings.TrimSpace(value))
}

func isWeComRobotWebhookURL(parsed *url.URL) bool {
	if parsed == nil {
		return false
	}
	return strings.EqualFold(parsed.Hostname(), "qyapi.weixin.qq.com") && parsed.Path == "/cgi-bin/webhook/send"
}

func quotaResetWebhookEventLabel(event string) string {
	switch event {
	case "quota_reset_notification_test":
		return "测试通知"
	case "quota_reset_request_created":
		return "新申请待审批"
	case "quota_reset_request_cancelled":
		return "申请已取消"
	case "quota_reset_request_rejected":
		return "申请已拒绝"
	case "quota_reset_request_reset_succeeded":
		return "额度已重置"
	case "quota_reset_request_reset_failed":
		return "额度重置失败"
	default:
		return event
	}
}

func webhookResponseBusinessError(body []byte) error {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil
	}
	var response struct {
		ErrCode *int   `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.ErrCode == nil || *response.ErrCode == 0 {
		return nil
	}
	return fmt.Errorf("webhook returned errcode %d", *response.ErrCode)
}

func (n *WebhookNotifier) bearerToken(ctx context.Context, setting *ent.QuotaResetNotificationSetting) (string, error) {
	if setting.CredentialID == nil {
		return "", fmt.Errorf("webhook bearer token credential is required")
	}
	credential, err := n.client.Credential.Get(ctx, *setting.CredentialID)
	if err != nil {
		return "", fmt.Errorf("load webhook credential: %w", err)
	}
	if credential.Kind != entcredential.KindSecretText {
		return "", fmt.Errorf("webhook credential must be secret_text")
	}
	decrypted, err := pkg.Decrypt(credential.Payload, n.encryptionKey)
	if err != nil {
		return "", fmt.Errorf("decrypt webhook credential: %w", err)
	}
	payload, err := credentialpkg.ParsePayload(credentialpkg.KindSecretText, json.RawMessage(decrypted))
	if err != nil {
		return "", fmt.Errorf("parse webhook credential: %w", err)
	}
	secret, ok := payload.(credentialpkg.SecretTextPayload)
	if !ok {
		return "", fmt.Errorf("webhook credential must be secret_text")
	}
	token := strings.TrimSpace(secret.Text)
	if token == "" {
		return "", fmt.Errorf("webhook bearer token credential is empty")
	}
	return token, nil
}

func reasonPreview(reason string) string {
	reason = strings.TrimSpace(reason)
	runes := []rune(reason)
	if len(runes) <= 160 {
		return reason
	}
	return string(runes[:160])
}
