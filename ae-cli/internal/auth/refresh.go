package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// RefreshAccessToken exchanges a refresh token for a new access token pair.
func RefreshAccessToken(ctx context.Context, serverURL, refreshToken string) (*OAuthResult, error) {
	serverURL = strings.TrimSpace(serverURL)
	refreshToken = strings.TrimSpace(refreshToken)
	if serverURL == "" {
		return nil, fmt.Errorf("server URL is required")
	}
	if refreshToken == "" {
		return nil, fmt.Errorf("refresh token is required")
	}

	body, err := json.Marshal(map[string]string{
		"refresh_token": refreshToken,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal refresh request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL+"/api/v1/auth/refresh", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read refresh response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var envelope struct {
		Data struct {
			Token        string `json:"token"`
			RefreshToken string `json:"refresh_token"`
			Tokens       struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
				ExpiresIn    int    `json:"expires_in"`
			} `json:"tokens"`
			ExpiresIn int `json:"expires_in"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, fmt.Errorf("decode refresh response: %w", err)
	}

	accessToken := strings.TrimSpace(envelope.Data.Tokens.AccessToken)
	if accessToken == "" {
		accessToken = strings.TrimSpace(envelope.Data.Token)
	}
	if accessToken == "" {
		return nil, fmt.Errorf("refresh response missing access token")
	}

	refreshOut := strings.TrimSpace(envelope.Data.Tokens.RefreshToken)
	if refreshOut == "" {
		refreshOut = strings.TrimSpace(envelope.Data.RefreshToken)
	}
	expiresIn := envelope.Data.Tokens.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = envelope.Data.ExpiresIn
	}

	return &OAuthResult{
		AccessToken:  accessToken,
		RefreshToken: refreshOut,
		ExpiresIn:    expiresIn,
	}, nil
}
