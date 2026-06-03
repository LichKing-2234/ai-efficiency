package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/httpx"
)

type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

type oauthErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
	Message          string `json:"message"`
}

func LoginDevice(ctx context.Context, cfg OAuthConfig) (*OAuthResult, error) {
	cfg = withOAuthDefaults(cfg)

	if _, hasDeadline := ctx.Deadline(); !hasDeadline && cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	deviceResp, err := requestDeviceCode(ctx, cfg)
	if err != nil {
		return nil, err
	}

	fmt.Fprintf(cfg.Output, "Open this URL in a browser:\n%s\n\n", deviceResp.VerificationURI)
	fmt.Fprintf(cfg.Output, "Enter this code:\n%s\n\n", deviceResp.UserCode)
	fmt.Fprintf(cfg.Output, "This code expires in %d seconds.\n", deviceResp.ExpiresIn)

	interval := normalizePollInterval(deviceResp.Interval)
	for {
		token, oauthErr, oauthErrSummary, err := pollDeviceToken(ctx, cfg, deviceResp.DeviceCode)
		if err != nil {
			return nil, err
		}

		switch oauthErr {
		case "":
			return token, nil
		case "authorization_pending":
			cfg.Sleep(interval)
		case "slow_down":
			interval += 5 * time.Second
			cfg.Sleep(interval)
		default:
			return nil, fmt.Errorf("device token exchange failed: %s", oauthErrSummary)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
}

func normalizePollInterval(seconds int) time.Duration {
	if seconds < 1 {
		seconds = 1
	}
	return time.Duration(seconds) * time.Second
}

func IsHeadlessLinux(lookupEnv func(string) string, goos string) bool {
	if goos != "linux" {
		return false
	}
	return strings.TrimSpace(lookupEnv("DISPLAY")) == "" &&
		strings.TrimSpace(lookupEnv("WAYLAND_DISPLAY")) == ""
}

func withOAuthDefaults(cfg OAuthConfig) OAuthConfig {
	if cfg.ClientID == "" {
		cfg.ClientID = "ae-cli"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 3 * time.Minute
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	if cfg.Output == nil {
		cfg.Output = os.Stdout
	}
	if cfg.Sleep == nil {
		cfg.Sleep = time.Sleep
	}
	return cfg
}

func requestDeviceCode(ctx context.Context, cfg OAuthConfig) (*deviceCodeResponse, error) {
	data := url.Values{
		"client_id": {cfg.ClientID},
	}

	var payload deviceCodeResponse
	if err := httpx.DoForm(ctx, cfg.HTTPClient, http.MethodPost, cfg.ServerURL+"/oauth/device/code", data, &payload, httpx.Options{}); err != nil {
		return nil, fmt.Errorf("device code request failed: %w", err)
	}

	return &payload, nil
}

func pollDeviceToken(ctx context.Context, cfg OAuthConfig, deviceCode string) (*OAuthResult, string, string, error) {
	data := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {deviceCode},
		"client_id":   {cfg.ClientID},
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}
	err := httpx.DoForm(ctx, cfg.HTTPClient, http.MethodPost, cfg.ServerURL+"/oauth/token", data, &tokenResp, httpx.Options{})
	if err != nil {
		var statusErr *httpx.StatusError
		if errors.As(err, &statusErr) {
			oauthErr := decodeOAuthErrorBody(statusErr.Body)
			if oauthErr.Error != "" {
				return nil, oauthErr.Error, statusErr.Summary, nil
			}
		}
		return nil, "", "", fmt.Errorf("device token request failed: %w", err)
	}

	return &OAuthResult{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresIn:    tokenResp.ExpiresIn,
	}, "", "", nil
}

func decodeOAuthErrorBody(body string) oauthErrorResponse {
	var errResp oauthErrorResponse
	if strings.TrimSpace(body) == "" {
		return errResp
	}
	_ = json.Unmarshal([]byte(body), &errResp)
	return errResp
}
