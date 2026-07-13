package quotareset

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

const (
	defaultWebhookTimeout       = 5 * time.Second
	maxWebhookResponseBodyBytes = 4096
)

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
		httpClient: &http.Client{
			Timeout: defaultWebhookTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (n *WebhookNotifier) Notify(ctx context.Context, notificationContext NotificationContext) (*NotificationDeliveryResult, error) {
	result := &NotificationDeliveryResult{}
	if n == nil || n.client == nil {
		return result, nil
	}
	setting, err := loadEnabledNotificationSetting(ctx, n.client)
	if err != nil {
		return nil, err
	}
	if setting == nil {
		return result, nil
	}
	result.ChannelType = setting.ChannelType.String()
	adapter, err := notificationAdapterFor(setting.ChannelType.String())
	if err != nil {
		return result, err
	}
	rendered, err := adapter.Render(notificationContext)
	if err != nil {
		return result, fmt.Errorf("render %s notification: %w", setting.ChannelType, err)
	}
	result.RecipientCount = rendered.RecipientCount
	result.MissingRecipientUserIDs = append([]int(nil), rendered.MissingRecipientUserIDs...)
	rawURL := strings.TrimSpace(setting.URL)
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return result, fmt.Errorf("%w: invalid saved webhook URL", ErrInvalidNotification)
	}
	if err := validateNotificationEndpoint(setting.ChannelType, parsed, setting.AuthType); err != nil {
		return result, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(rendered.Body))
	if err != nil {
		return result, fmt.Errorf("create webhook request: %w", sanitizeWebhookError(setting.ChannelType, rawURL, err))
	}
	request.Header = rendered.Headers.Clone()
	if setting.ChannelType == quotaresetnotificationsetting.ChannelTypeGenericWebhook && setting.AuthType == quotaresetnotificationsetting.AuthTypeBearerToken {
		token, err := n.bearerToken(ctx, setting)
		if err != nil {
			return result, err
		}
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := n.httpClient.Do(request)
	if err != nil {
		return result, redactedWebhookSendError(setting.ChannelType, rawURL, err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxWebhookResponseBodyBytes+1))
	if err != nil {
		return result, fmt.Errorf("read webhook response: %w", sanitizeWebhookError(setting.ChannelType, rawURL, err))
	}
	if len(responseBody) > maxWebhookResponseBodyBytes {
		return result, fmt.Errorf("webhook response exceeds %d bytes", maxWebhookResponseBodyBytes)
	}
	if err := adapter.ValidateResponse(response.StatusCode, responseBody); err != nil {
		return result, sanitizeWebhookError(setting.ChannelType, rawURL, err)
	}
	result.Delivered = true
	return result, nil
}

func loadEnabledNotificationSetting(ctx context.Context, client *ent.Client) (*ent.QuotaResetNotificationSetting, error) {
	rows, err := client.QuotaResetNotificationSetting.Query().
		Where(quotaresetnotificationsetting.Enabled(true)).
		Order(ent.Asc(quotaresetnotificationsetting.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load enabled quota reset notification setting: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if len(rows) != 1 {
		return nil, fmt.Errorf("%w: expected one enabled notification setting, found %d", ErrInvalidNotification, len(rows))
	}
	return rows[0], nil
}

func (n *WebhookNotifier) notificationActionURL(requestID int) string {
	path := fmt.Sprintf("/usage/quota-reset?request_id=%d", requestID)
	if n == nil || n.frontendURL == "" {
		return path
	}
	return n.frontendURL + path
}

func redactedWebhookSendError(channelType quotaresetnotificationsetting.ChannelType, rawURL string, err error) error {
	preview := webhookURLPreview(channelType, rawURL)
	if preview == "" {
		preview = "configured webhook"
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		err = urlErr.Err
	}
	return fmt.Errorf("send webhook to %s: %w", preview, sanitizeWebhookError(channelType, rawURL, err))
}

type sanitizedNotificationError struct {
	message string
}

func (e *sanitizedNotificationError) Error() string { return e.message }

func sanitizeWebhookError(channelType quotaresetnotificationsetting.ChannelType, rawURL string, err error) error {
	if err == nil {
		return nil
	}
	preview := webhookURLPreview(channelType, rawURL)
	if preview == "" {
		preview = "configured webhook"
	}
	message := strings.ReplaceAll(err.Error(), rawURL, preview)
	parsed, parseErr := url.Parse(rawURL)
	if parseErr == nil && parsed.User != nil {
		message = strings.ReplaceAll(message, parsed.User.String()+"@", "")
		if username := parsed.User.Username(); username != "" {
			message = strings.ReplaceAll(message, username, "[redacted]")
			message = strings.ReplaceAll(message, url.QueryEscape(username), "[redacted]")
		}
		if password, ok := parsed.User.Password(); ok && password != "" {
			message = strings.ReplaceAll(message, password, "[redacted]")
			message = strings.ReplaceAll(message, url.QueryEscape(password), "[redacted]")
		}
	}
	if parseErr == nil && parsed.RawQuery != "" {
		message = strings.ReplaceAll(message, "?"+parsed.RawQuery, "")
		message = strings.ReplaceAll(message, parsed.RawQuery, "[redacted query]")
		for _, values := range parsed.Query() {
			for _, value := range values {
				if value == "" {
					continue
				}
				message = strings.ReplaceAll(message, value, "[redacted]")
				message = strings.ReplaceAll(message, url.QueryEscape(value), "[redacted]")
			}
		}
	}
	return &sanitizedNotificationError{message: message}
}

func webhookResponseBusinessError(body []byte) error {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return fmt.Errorf("webhook returned empty business response")
	}
	var response struct {
		ErrCode *int   `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("decode webhook business response: %w", err)
	}
	if response.ErrCode == nil {
		return fmt.Errorf("webhook business response missing errcode")
	}
	if *response.ErrCode == 0 {
		return nil
	}
	errmsg := strings.TrimSpace(response.ErrMsg)
	if errmsg == "" {
		return fmt.Errorf("webhook returned errcode %d", *response.ErrCode)
	}
	return fmt.Errorf("webhook returned errcode %d: %s", *response.ErrCode, errmsg)
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
