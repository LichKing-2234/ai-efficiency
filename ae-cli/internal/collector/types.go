package collector

type CodexSnapshot struct {
	SourceSessionID   string         `json:"source_session_id,omitempty"`
	InputTokens       int64          `json:"input_tokens,omitempty"`
	CachedInputTokens int64          `json:"cached_input_tokens,omitempty"`
	OutputTokens      int64          `json:"output_tokens,omitempty"`
	ReasoningTokens   int64          `json:"reasoning_tokens,omitempty"`
	TotalTokens       int64          `json:"total_tokens,omitempty"`
	RawPayload        map[string]any `json:"raw_payload,omitempty"`
}

type ClaudeSnapshot struct {
	SourceSessionID   string         `json:"source_session_id,omitempty"`
	InputTokens       int64          `json:"input_tokens,omitempty"`
	OutputTokens      int64          `json:"output_tokens,omitempty"`
	CachedInputTokens int64          `json:"cached_input_tokens,omitempty"`
	RawPayload        map[string]any `json:"raw_payload,omitempty"`
}

type KiroSnapshot struct {
	ConversationID  string         `json:"conversation_id,omitempty"`
	CreditUsage     float64        `json:"credit_usage,omitempty"`
	ContextUsagePct float64        `json:"context_usage_pct,omitempty"`
	RawPayload      map[string]any `json:"raw_payload,omitempty"`
}

type Snapshot struct {
	Codex  *CodexSnapshot  `json:"codex,omitempty"`
	Claude *ClaudeSnapshot `json:"claude,omitempty"`
	Kiro   *KiroSnapshot   `json:"kiro,omitempty"`
}

type Paths struct {
	CodexFiles    []string
	ClaudeFiles   []string
	KiroFiles     []string
	WorkspaceRoot string
}
