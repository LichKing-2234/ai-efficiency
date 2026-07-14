package quotareset

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	defaultWeComMarkdownMaxBytes = 4096
	weComActionMaxBytes          = 320
	weComRequesterMaxBytes       = 512
	weComTeamMaxBytes            = 640
	weComGroupMaxBytes           = 512
	weComNodeMaxBytes            = 512
	weComOptionalMaxBytes        = 768
	weComUnavailableMaxBytes     = 192
)

var safeWeComUserID = regexp.MustCompile(`^[A-Za-z0-9_.@-]{1,128}$`)

type weComGroupRobotAdapter struct{ maxBytes int }

func escapeWeComUserText(value string) string {
	value = strings.TrimSpace(value)
	replacer := strings.NewReplacer(
		"<", "＜",
		">", "＞",
		"[", "［",
		"]", "］",
		"*", "＊",
		"`", "｀",
	)
	return replacer.Replace(value)
}

func validWeComMentionUserID(value string) bool {
	value = strings.TrimSpace(value)
	return safeWeComUserID.MatchString(value) && !strings.EqualFold(value, "all")
}

func weComMention(person NotificationPerson) (string, bool) {
	userID := strings.TrimSpace(person.NotificationIDs["wecom"])
	if !validWeComMentionUserID(userID) {
		displayName := escapeWeComUserText(person.DisplayName)
		if displayName == "" {
			displayName = "未知用户"
		}
		return truncateUTF8(displayName+"（无法 @）", weComUnavailableMaxBytes), false
	}
	return "<@" + userID + ">", true
}

func (a weComGroupRobotAdapter) Render(ctx NotificationContext) (RenderedNotification, error) {
	maxBytes := a.maxBytes
	if maxBytes <= 0 {
		maxBytes = defaultWeComMarkdownMaxBytes
	}
	completedNodes, totalNodes := boundedNotificationWorkflowProgress(ctx.WorkflowCompletedNodes, ctx.WorkflowTotalNodes)
	title, action := weComEventCopy(ctx.Event)
	requiredHead := []string{
		"# " + title,
		truncateUTF8(`<font color="warning">`+escapeWeComUserText(action)+`</font>`, weComActionMaxBytes),
		boundedWeComLine("> 申请人：", ctx.Requester.DisplayName, weComRequesterMaxBytes),
		boundedWeComLine("> 所属团队：", strings.Join(ctx.DepartmentPaths, "、"), weComTeamMaxBytes),
		boundedWeComLine("> 订阅组：", ctx.GroupName, weComGroupMaxBytes),
	}
	requiredTail := make([]string, 0, 3)
	if ctx.CurrentNode != nil {
		node := fmt.Sprintf("%d/%d · %s", ctx.CurrentNode.Position+1, ctx.CurrentNode.Total, ctx.CurrentNode.Label)
		requiredTail = append(requiredTail, boundedWeComLine("> 当前节点：", node, weComNodeMaxBytes))
	}
	requiredTail = append(requiredTail, fmt.Sprintf("> 审批进度：%d/%d", completedNodes, totalNodes))
	actionLine, err := weComActionLink(ctx.ActionURL)
	if err != nil {
		return RenderedNotification{}, err
	}

	missing := make([]int, 0)
	mentioned := 0
	mentionEntries := make([]string, 0, len(ctx.Recipients))
	recipientLabel := weComRecipientLabel(ctx.Event)
	for _, recipient := range uniqueNotificationPeople(ctx.Recipients) {
		mention, mentionable := weComMention(recipient)
		if !mentionable {
			missing = appendUniqueNotificationUserID(missing, recipient.UserID)
		}
		candidateEntries := append(append([]string(nil), mentionEntries...), mention)
		candidateTail := append([]string(nil), requiredTail...)
		candidateTail = append(candidateTail, recipientLabel+strings.Join(candidateEntries, " "), actionLine)
		if joinedWeComMarkdownBytes(requiredHead, candidateTail) > maxBytes {
			if mentionable {
				missing = appendUniqueNotificationUserID(missing, recipient.UserID)
			}
			continue
		}
		mentionEntries = candidateEntries
		if mentionable {
			mentioned++
		}
	}
	if len(mentionEntries) > 0 {
		requiredTail = append(requiredTail, recipientLabel+strings.Join(mentionEntries, " "))
	}
	requiredTail = append(requiredTail, actionLine)

	optional := make([]string, 0, 2)
	if reason := escapeWeComUserText(ctx.Reason); reason != "" {
		optional = append(optional, truncateUTF8("> 申请原因："+reason, weComOptionalMaxBytes))
	}
	if len(ctx.ApprovalHistory) > 0 {
		latest := ctx.ApprovalHistory[len(ctx.ApprovalHistory)-1]
		decision := "> 上一审批：" + escapeWeComUserText(latest.ActorDisplayName)
		if comment := escapeWeComUserText(latest.Comment); comment != "" {
			decision += "：" + comment
		}
		optional = append(optional, truncateUTF8(decision, weComOptionalMaxBytes))
	}
	content := fitWeComMarkdown(requiredHead, optional, requiredTail, maxBytes)
	if strings.TrimSpace(content) == "" {
		return RenderedNotification{}, fmt.Errorf("render WeCom notification: required content exceeds %d bytes", maxBytes)
	}
	payload := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"content": content,
		},
	}
	body, err := marshalNotificationPayload(payload)
	if err != nil {
		return RenderedNotification{}, err
	}
	return RenderedNotification{
		Body:                    body,
		Headers:                 http.Header{"Content-Type": []string{"application/json"}},
		RecipientCount:          mentioned,
		MissingRecipientUserIDs: missing,
	}, nil
}

func weComRecipientLabel(event NotificationEvent) string {
	if event == NotificationNodeActivated {
		return "待审批："
	}
	return "通知对象："
}

func fitWeComMarkdown(requiredHead, optional, requiredTail []string, maxBytes int) string {
	required := append(append([]string(nil), requiredHead...), requiredTail...)
	if joinedWeComMarkdownBytes(required) > maxBytes {
		return ""
	}
	lines := append([]string(nil), requiredHead...)
	for _, line := range optional {
		candidate := append(append([]string(nil), lines...), line)
		candidate = append(candidate, requiredTail...)
		if joinedWeComMarkdownBytes(candidate) <= maxBytes {
			lines = append(lines, line)
		}
	}
	lines = append(lines, requiredTail...)
	return strings.Join(lines, "\n\n")
}

func boundedWeComLine(prefix, value string, maxBytes int) string {
	value = escapeWeComUserText(value)
	if value == "" {
		value = "-"
	}
	return truncateUTF8(prefix+value, maxBytes)
}

func truncateUTF8(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		if utf8.ValidString(value) {
			return value
		}
		return strings.ToValidUTF8(value, "")
	}
	suffix := "..."
	if maxBytes <= len(suffix) {
		return suffix[:maxBytes]
	}
	prefixBytes := maxBytes - len(suffix)
	for prefixBytes > 0 && !utf8.RuneStart(value[prefixBytes]) {
		prefixBytes--
	}
	prefix := value[:prefixBytes]
	if !utf8.ValidString(prefix) {
		prefix = strings.ToValidUTF8(prefix, "")
	}
	return prefix + suffix
}

func weComActionLink(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	parsed, err := url.Parse(rawURL)
	if err != nil || rawURL == "" || strings.ContainsAny(rawURL, "\r\n") {
		return "", fmt.Errorf("render WeCom notification: invalid action URL")
	}
	if parsed.IsAbs() {
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return "", fmt.Errorf("render WeCom notification: invalid action URL")
		}
	} else if !strings.HasPrefix(rawURL, "/") {
		return "", fmt.Errorf("render WeCom notification: invalid action URL")
	}
	if strings.ContainsAny(rawURL, "()[]") {
		return "", fmt.Errorf("render WeCom notification: invalid action URL")
	}
	return "[进入待处理](" + rawURL + ")", nil
}

func joinedWeComMarkdownBytes(lineSets ...[]string) int {
	lineCount := 0
	byteCount := 0
	for _, lines := range lineSets {
		for _, line := range lines {
			if lineCount > 0 {
				byteCount += 2
			}
			byteCount += len([]byte(line))
			lineCount++
		}
	}
	return byteCount
}

func uniqueNotificationPeople(people []NotificationPerson) []NotificationPerson {
	result := make([]NotificationPerson, 0, len(people))
	indexes := make(map[int]int, len(people))
	zeroKeys := make(map[string]struct{})
	for _, person := range people {
		if person.UserID <= 0 {
			key := strings.TrimSpace(person.Email) + "\x00" + strings.TrimSpace(person.DisplayName)
			if _, exists := zeroKeys[key]; exists {
				continue
			}
			zeroKeys[key] = struct{}{}
			result = append(result, person)
			continue
		}
		if index, exists := indexes[person.UserID]; exists {
			mergeNotificationPerson(&result[index], person)
			continue
		}
		indexes[person.UserID] = len(result)
		result = append(result, person)
	}
	return result
}

func mergeNotificationPerson(target *NotificationPerson, source NotificationPerson) {
	if target.DisplayName == "" {
		target.DisplayName = source.DisplayName
	}
	if target.Email == "" {
		target.Email = source.Email
	}
	if target.NotificationIDs == nil {
		target.NotificationIDs = make(map[string]string)
	}
	for key, value := range source.NotificationIDs {
		if target.NotificationIDs[key] == "" && strings.TrimSpace(value) != "" {
			target.NotificationIDs[key] = strings.TrimSpace(value)
		}
	}
}

func appendUniqueNotificationUserID(ids []int, userID int) []int {
	for _, existing := range ids {
		if existing == userID {
			return ids
		}
	}
	return append(ids, userID)
}

func weComEventCopy(event NotificationEvent) (string, string) {
	switch event {
	case NotificationNodeActivated:
		return "额度重置待审批", "新的额度重置申请等待审批"
	case NotificationRejected:
		return "额度重置申请已拒绝", "额度重置申请已被拒绝"
	case NotificationCancelled:
		return "额度重置申请已取消", "额度重置申请已由申请人取消"
	case NotificationResetSucceeded:
		return "额度重置成功", "订阅组额度已成功重置"
	case NotificationResetFailed:
		return "额度重置失败", "订阅组额度重置失败，需要跟进"
	case NotificationTest:
		return "额度重置通知测试", "通知渠道测试消息已送达"
	default:
		return "额度重置状态更新", "额度重置申请状态已更新"
	}
}

func (weComGroupRobotAdapter) ValidateResponse(statusCode int, body []byte) error {
	if statusCode < http.StatusOK || statusCode > 299 {
		return fmt.Errorf("webhook returned %d", statusCode)
	}
	return webhookResponseBusinessError(bytes.TrimSpace(body))
}
