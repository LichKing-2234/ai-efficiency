package activity

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

const activityCursorVersion = 1

type activityCursor struct {
	Version      int    `json:"v"`
	Collection   string `json:"collection"`
	ScopeVersion string `json:"scope_version"`
	ActorUserID  int    `json:"actor_user_id"`
	Subject      string `json:"subject"`
	LastID       int    `json:"last_id,omitempty"`
	LastValue    string `json:"last_value,omitempty"`
}

func (s *Service) encodeCursor(cursor activityCursor) (string, error) {
	if len(s.cursorSecret) == 0 {
		return "", fmt.Errorf("activity cursor secret is not configured")
	}
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	signature := hmac.New(sha256.New, s.cursorSecret)
	_, _ = signature.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(signature.Sum(nil)), nil
}

func (s *Service) decodeCursor(encoded string) (*activityCursor, error) {
	if len(s.cursorSecret) == 0 {
		return nil, fmt.Errorf("activity cursor secret is not configured")
	}
	parts := strings.Split(encoded, ".")
	if len(parts) != 2 {
		return nil, ErrInvalidCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrInvalidCursor
	}
	providedSignature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidCursor
	}
	expectedSignature := hmac.New(sha256.New, s.cursorSecret)
	_, _ = expectedSignature.Write(payload)
	if !hmac.Equal(providedSignature, expectedSignature.Sum(nil)) {
		return nil, ErrInvalidCursor
	}
	var cursor activityCursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return nil, ErrInvalidCursor
	}
	return &cursor, nil
}
