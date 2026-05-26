package relay

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
)

type User struct {
	ID            int64   `json:"id"`
	Email         string  `json:"email"`
	Username      string  `json:"username"`
	Role          string  `json:"role"`
	Notes         string  `json:"notes,omitempty"`
	Concurrency   int     `json:"concurrency,omitempty"`
	AllowedGroups []Group `json:"allowed_groups,omitempty"`
	// AllowedGroupIDs holds IDs that need hydration when allowed_groups is not already a complete group object list.
	AllowedGroupIDs []int64 `json:"-"`
}

type CreateUserRequest struct {
	Username      string  `json:"username"`
	Email         string  `json:"email"`
	Password      string  `json:"password"`
	Role          string  `json:"role,omitempty"`
	Notes         string  `json:"notes,omitempty"`
	Concurrency   int     `json:"concurrency,omitempty"`
	AllowedGroups []int64 `json:"allowed_groups,omitempty"`
}

type UpdateUserRequest struct {
	Email       string `json:"email,omitempty"`
	Password    string `json:"password,omitempty"`
	Username    string `json:"username,omitempty"`
	Notes       string `json:"notes,omitempty"`
	Concurrency *int   `json:"concurrency,omitempty"`
}

type APIKey struct {
	ID         int64      `json:"id"`
	UserID     int64      `json:"user_id"`
	Key        string     `json:"key"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	Group      *Group     `json:"group"`
}

type Group struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	Platform         string `json:"platform"`
	IsExclusive      bool   `json:"is_exclusive,omitempty"`
	SubscriptionType string `json:"subscription_type,omitempty"`
}

func (u *User) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID            int64           `json:"id"`
		Email         string          `json:"email"`
		Username      string          `json:"username"`
		Role          string          `json:"role"`
		Notes         string          `json:"notes,omitempty"`
		Concurrency   int             `json:"concurrency,omitempty"`
		AllowedGroups json.RawMessage `json:"allowed_groups"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	u.ID = raw.ID
	u.Email = raw.Email
	u.Username = raw.Username
	u.Role = raw.Role
	u.Notes = raw.Notes
	u.Concurrency = raw.Concurrency
	u.AllowedGroups = nil
	u.AllowedGroupIDs = nil

	groups, ids, err := decodeAllowedGroupFacts(raw.AllowedGroups)
	if err != nil {
		return err
	}
	u.AllowedGroups = groups
	u.AllowedGroupIDs = ids
	return nil
}

func decodeAllowedGroupFacts(raw json.RawMessage) ([]Group, []int64, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil, nil
	}

	var groups []Group
	if err := json.Unmarshal(raw, &groups); err == nil {
		ids := make([]int64, 0, len(groups))
		for _, group := range groups {
			if group.ID > 0 && strings.TrimSpace(group.Platform) == "" {
				ids = append(ids, group.ID)
			}
		}
		return groups, ids, nil
	}

	var ids []int64
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil, nil, err
	}
	return nil, ids, nil
}

type APIKeyCreateRequest struct {
	Name      string     `json:"name"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	GroupID   string     `json:"group_id,omitempty"`
}

type APIKeyWithSecret struct {
	APIKey
	Secret string `json:"secret"`
}

type UsageLog struct {
	ID           int64     `json:"id"`
	RequestID    string    `json:"request_id"`
	CreatedAt    time.Time `json:"created_at"`
	APIKeyID     int64     `json:"api_key_id"`
	UserID       int64     `json:"user_id"`
	AccountID    string    `json:"account_id"`
	GroupID      string    `json:"group_id"`
	Model        string    `json:"model"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	CacheTokens  int64     `json:"cache_tokens"`
	TotalTokens  int64     `json:"total_tokens"`
	TotalCost    float64   `json:"total_cost"`
	ActualCost   float64   `json:"actual_cost"`
}

type UsageStats struct {
	TotalTokens int64   `json:"total_tokens"`
	TotalCost   float64 `json:"total_cost"`
}

type ChatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatCompletionResponse struct {
	Content    string `json:"content"`
	TokensUsed int    `json:"tokens_used"`
}

type ToolDef struct {
	Type     string      `json:"type"`
	Function ToolFuncDef `json:"function"`
}

type ToolFuncDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type ChatCompletionWithToolsResponse struct {
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	TokensUsed int        `json:"tokens_used"`
}
