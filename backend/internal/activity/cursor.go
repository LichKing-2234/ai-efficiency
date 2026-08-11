package activity

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const activityCursorVersion = 1

type activityCursor struct {
	Version      int    `json:"v"`
	Collection   string `json:"collection"`
	ScopeVersion string `json:"scope_version"`
	ActorUserID  int    `json:"actor_user_id"`
	Subject      string `json:"subject"`
	FromUnixNano int64  `json:"from_unix_nano"`
	ToUnixNano   int64  `json:"to_unix_nano"`
	Offset       int    `json:"offset"`
	LastID       int    `json:"last_id,omitempty"`
	LastValue    string `json:"last_value,omitempty"`
}

func (s *Service) paginateMemberActivity(result *MemberActivity, authorization *authorizationScope, actorUserID, targetUserID int, options DetailPageOptions) error {
	subject := "member:" + strconv.Itoa(targetUserID)
	if err := paginateActivityPage(s, &result.PRs, "prs", authorization, actorUserID, subject, result.Window, options.PRLimit, options.PRCursor); err != nil {
		return err
	}
	if err := paginateActivityPage(s, &result.Commits, "commits", authorization, actorUserID, subject, result.Window, options.CommitLimit, options.CommitCursor); err != nil {
		return err
	}
	if err := paginateActivityPage(s, &result.Buckets, "buckets", authorization, actorUserID, subject, result.Window, options.BucketLimit, options.BucketCursor); err != nil {
		return err
	}
	return nil
}

func paginateActivityPage[T any](service *Service, page *Page[T], collection string, authorization *authorizationScope, actorUserID int, subject string, window Window, limit int, cursor string) error {
	if page.Items == nil {
		page.Items = []T{}
	}
	if limit <= 0 && strings.TrimSpace(cursor) == "" {
		return nil
	}
	if limit <= 0 {
		return ErrInvalidCursor
	}
	if limit > 100 {
		limit = 100
	}
	offset := 0
	if strings.TrimSpace(cursor) != "" {
		decoded, err := service.decodeCursor(cursor)
		if err != nil {
			return err
		}
		if decoded.ScopeVersion != authorization.Version {
			return ErrSnapshotExpired
		}
		if decoded.Version != activityCursorVersion || decoded.Collection != collection || decoded.ActorUserID != actorUserID || decoded.Subject != subject || decoded.FromUnixNano != window.From.UnixNano() || decoded.ToUnixNano != window.To.UnixNano() || decoded.Offset < 0 {
			return ErrInvalidCursor
		}
		offset = decoded.Offset
	}
	if offset > len(page.Items) {
		return ErrSnapshotExpired
	}
	originalLength := len(page.Items)
	end := offset + limit
	if end > originalLength {
		end = originalLength
	}
	items := append([]T(nil), page.Items[offset:end]...)
	page.Items = items
	page.NextCursor = ""
	if end < originalLength {
		next, err := service.encodeCursor(activityCursor{
			Version: activityCursorVersion, Collection: collection, ScopeVersion: authorization.Version,
			ActorUserID: actorUserID, Subject: subject, FromUnixNano: window.From.UnixNano(), ToUnixNano: window.To.UnixNano(), Offset: end,
		})
		if err != nil {
			return err
		}
		page.NextCursor = next
	}
	return nil
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
