package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
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

	interval := time.Duration(deviceResp.Interval) * time.Second
	for {
		token, oauthErr, err := pollDeviceToken(ctx, cfg, deviceResp.DeviceCode)
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
			return nil, fmt.Errorf("device token exchange failed: %s", oauthErr)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.ServerURL+"/oauth/device/code", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create device code request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("device code request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp oauthErrorResponse
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		return nil, fmt.Errorf("device code request failed: %s", errResp.Error)
	}

	var payload deviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode device code response: %w", err)
	}

	return &payload, nil
}

func pollDeviceToken(ctx context.Context, cfg OAuthConfig, deviceCode string) (*OAuthResult, string, error) {
	data := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {deviceCode},
		"client_id":   {cfg.ClientID},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.ServerURL+"/oauth/token", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, "", fmt.Errorf("create device token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("device token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp oauthErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			return nil, "", fmt.Errorf("decode device token error: %w", err)
		}
		return nil, errResp.Error, nil
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, "", fmt.Errorf("decode token response: %w", err)
	}

	return &OAuthResult{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresIn:    tokenResp.ExpiresIn,
	}, "", nil
}
