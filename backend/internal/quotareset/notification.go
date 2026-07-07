package quotareset

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	entcredential "github.com/ai-efficiency/backend/ent/credential"
	"github.com/ai-efficiency/backend/ent/quotaresetnotificationsetting"
	credentialpkg "github.com/ai-efficiency/backend/internal/credential"
	"github.com/ai-efficiency/backend/internal/pkg"
)

const defaultWebhookTimeout = 5 * time.Second

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
	payload := n.payload(event, req)
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
		return fmt.Errorf("send webhook: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("webhook returned %d", resp.StatusCode)
	}
	return nil
}

func (n *WebhookNotifier) payload(event string, req *ent.QuotaResetRequest) map[string]any {
	payload := map[string]any{
		"event":                      event,
		"request_id":                 req.ID,
		"status":                     req.Status.String(),
		"requester_user_id":          req.RequesterUserID,
		"provider_id":                req.ProviderID,
		"group_id":                   req.GroupID,
		"group_name":                 req.GroupName,
		"group_platform":             req.GroupPlatform,
		"reason_preview":             reasonPreview(req.Reason),
		"resolved_approver_user_ids": req.ResolvedApproverUserIds,
		"occurred_at":                time.Now().UTC().Format(time.RFC3339),
	}
	if n.frontendURL != "" {
		payload["action_url"] = fmt.Sprintf("%s/usage/quota-reset?request_id=%d", n.frontendURL, req.ID)
	}
	return payload
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
