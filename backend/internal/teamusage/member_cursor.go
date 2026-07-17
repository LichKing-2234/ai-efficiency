package teamusage

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

const memberCursorVersion = 1

type memberCursorCodec struct {
	key [sha256.Size]byte
}

type memberCursorPayload struct {
	Version      int    `json:"v"`
	ActorUserID  int    `json:"actor_user_id"`
	ScopeVersion string `json:"scope_version"`
	SnapshotID   string `json:"snapshot_id"`
	StartDate    string `json:"start_date"`
	EndDate      string `json:"end_date"`
	Granularity  string `json:"granularity"`
	Timezone     string `json:"timezone"`
	Offset       int    `json:"offset"`
}

func newMemberCursorCodec(secret string) *memberCursorCodec {
	return &memberCursorCodec{key: sha256.Sum256([]byte("ai-efficiency/teamusage/member-cursor/v1\x00" + secret))}
}

func (c *memberCursorCodec) Encode(payload memberCursorPayload) (string, error) {
	if c == nil || !validMemberCursorPayload(payload) {
		return "", ErrInvalidMemberCursor
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode member cursor: %w", err)
	}
	mac := hmac.New(sha256.New, c.key[:])
	_, _ = mac.Write(encoded)
	return base64.RawURLEncoding.EncodeToString(encoded) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (c *memberCursorCodec) Decode(cursor string) (memberCursorPayload, error) {
	if c == nil {
		return memberCursorPayload{}, ErrInvalidMemberCursor
	}
	payloadPart, signaturePart, ok := strings.Cut(strings.TrimSpace(cursor), ".")
	if !ok || payloadPart == "" || signaturePart == "" || strings.Contains(signaturePart, ".") {
		return memberCursorPayload{}, ErrInvalidMemberCursor
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return memberCursorPayload{}, ErrInvalidMemberCursor
	}
	signature, err := base64.RawURLEncoding.DecodeString(signaturePart)
	if err != nil {
		return memberCursorPayload{}, ErrInvalidMemberCursor
	}
	mac := hmac.New(sha256.New, c.key[:])
	_, _ = mac.Write(payloadBytes)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return memberCursorPayload{}, ErrInvalidMemberCursor
	}
	var payload memberCursorPayload
	decoder := json.NewDecoder(strings.NewReader(string(payloadBytes)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil || !validMemberCursorPayload(payload) {
		return memberCursorPayload{}, ErrInvalidMemberCursor
	}
	return payload, nil
}

func validMemberCursorPayload(payload memberCursorPayload) bool {
	return payload.Version == memberCursorVersion && payload.ActorUserID > 0 && payload.Offset > 0 &&
		strings.TrimSpace(payload.ScopeVersion) != "" && strings.TrimSpace(payload.SnapshotID) != "" &&
		strings.TrimSpace(payload.StartDate) != "" && strings.TrimSpace(payload.EndDate) != "" &&
		strings.TrimSpace(payload.Granularity) != "" && strings.TrimSpace(payload.Timezone) != ""
}
