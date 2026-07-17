package teamusage

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	organizationCursorVersion     = 1
	organizationCursorDepartments = "departments"
	organizationCursorMembers     = "members"
)

type organizationCursorCodec struct {
	key [sha256.Size]byte
}

type organizationCursorPayload struct {
	Version                    int    `json:"v"`
	Collection                 string `json:"collection"`
	ActorUserID                int    `json:"actor_user_id"`
	ScopeVersion               string `json:"scope_version"`
	SnapshotID                 string `json:"snapshot_id"`
	StartDate                  string `json:"start_date"`
	EndDate                    string `json:"end_date"`
	Granularity                string `json:"granularity"`
	Timezone                   string `json:"timezone"`
	ParentDepartmentExternalID string `json:"parent_department_external_id"`
	Offset                     int    `json:"offset"`
}

func newOrganizationCursorCodec(secret string) *organizationCursorCodec {
	return &organizationCursorCodec{key: sha256.Sum256([]byte("ai-efficiency/teamusage/organization-cursor/v1\x00" + secret))}
}

func (c *organizationCursorCodec) Encode(payload organizationCursorPayload) (string, error) {
	if c == nil || !validOrganizationCursorPayload(payload) {
		return "", ErrInvalidOrganizationCursor
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode organization cursor: %w", err)
	}
	mac := hmac.New(sha256.New, c.key[:])
	_, _ = mac.Write(encoded)
	return base64.RawURLEncoding.EncodeToString(encoded) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (c *organizationCursorCodec) Decode(cursor string) (organizationCursorPayload, error) {
	if c == nil {
		return organizationCursorPayload{}, ErrInvalidOrganizationCursor
	}
	payloadPart, signaturePart, ok := strings.Cut(strings.TrimSpace(cursor), ".")
	if !ok || payloadPart == "" || signaturePart == "" || strings.Contains(signaturePart, ".") {
		return organizationCursorPayload{}, ErrInvalidOrganizationCursor
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return organizationCursorPayload{}, ErrInvalidOrganizationCursor
	}
	signature, err := base64.RawURLEncoding.DecodeString(signaturePart)
	if err != nil {
		return organizationCursorPayload{}, ErrInvalidOrganizationCursor
	}
	mac := hmac.New(sha256.New, c.key[:])
	_, _ = mac.Write(payloadBytes)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return organizationCursorPayload{}, ErrInvalidOrganizationCursor
	}
	var payload organizationCursorPayload
	decoder := json.NewDecoder(strings.NewReader(string(payloadBytes)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil || !validOrganizationCursorPayload(payload) {
		return organizationCursorPayload{}, ErrInvalidOrganizationCursor
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return organizationCursorPayload{}, ErrInvalidOrganizationCursor
	}
	return payload, nil
}

func validOrganizationCursorPayload(payload organizationCursorPayload) bool {
	return payload.Version == organizationCursorVersion &&
		(payload.Collection == organizationCursorDepartments || payload.Collection == organizationCursorMembers) &&
		payload.ActorUserID > 0 && payload.Offset > 0 &&
		strings.TrimSpace(payload.ScopeVersion) != "" && strings.TrimSpace(payload.SnapshotID) != "" &&
		strings.TrimSpace(payload.StartDate) != "" && strings.TrimSpace(payload.EndDate) != "" &&
		strings.TrimSpace(payload.Granularity) != "" && strings.TrimSpace(payload.Timezone) != ""
}
