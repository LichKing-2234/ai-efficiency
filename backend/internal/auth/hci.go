package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// HCIProvider converts the HCI Auth JWT injected by the trusted Kong ingress
// into the local identity used by AI Efficiency.
type HCIProvider struct {
	secret []byte
}

func NewHCIProvider(secret string) *HCIProvider {
	return &HCIProvider{secret: []byte(strings.TrimSpace(secret))}
}

func (p *HCIProvider) Name() string { return "hci" }

func (p *HCIProvider) Authenticate(_ context.Context, _, _ string) (*UserInfo, error) {
	return nil, fmt.Errorf("hci auth requires ingress headers")
}

func (p *HCIProvider) AuthenticateHeaders(r *http.Request) (*UserInfo, error) {
	if len(p.secret) == 0 {
		return nil, fmt.Errorf("hci auth is not configured")
	}
	raw := strings.TrimSpace(r.Header.Get("x-hci-auth-jwt"))
	if raw == "" {
		return nil, fmt.Errorf("hci auth header is missing")
	}
	token, err := jwt.Parse(raw, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected hci jwt signing method")
		}
		return p.secret, nil
	})
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid hci auth jwt")
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid hci auth claims")
	}
	user, ok := claims["user"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("hci auth user claim is missing")
	}
	email, _ := user["email"].(string)
	email = strings.TrimSpace(email)
	if email == "" || !strings.Contains(email, "@") {
		return nil, fmt.Errorf("hci auth email claim is invalid")
	}
	return &UserInfo{
		Username:   email,
		Email:      email,
		Role:       "user",
		AuthSource: "hci",
	}, nil
}
