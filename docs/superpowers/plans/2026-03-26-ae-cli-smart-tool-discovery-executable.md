# ae-cli Smart Tool Discovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the full smart tool discovery flow so `ae-cli` can discover local AI tools through a backend multi-round LLM tool-call protocol, write native tool configuration safely, cache `relay_api_key_id` mappings, and surface them through the active session-start lifecycle.

**Architecture:** This implementation uses a single authoritative protocol: `POST /api/v1/tools/discover` starts a multi-round conversation and `POST /api/v1/tools/discover/continue` advances it until `status` becomes `complete` or `error`. Backend code owns LLM conversation state, tool schemas, provider lookup, and final response hydration. `ae-cli` owns all local side effects: allowlisted command/file access, config writes, local cache management, login-triggered discovery, and session metadata handoff. Existing code remains authoritative for already-implemented relay, auth, token, and session plumbing; the latest smart-tool-discovery spec is authoritative for the discover feature surface, and the latest session-attribution spec is authoritative for bootstrap/workspace-state lifecycle.

**Tech Stack:** Go 1.24+, Gin, Ent, relay.Provider, Cobra, standard library JSON/fs/os/exec, `gopkg.in/yaml.v3`

**Spec:** `docs/superpowers/specs/2026-03-24-ae-cli-smart-tool-discovery-design.md`

**Status:** 待实施（当前推荐执行版本）

**Authoritative Implementation Rules:**
- The one-shot `POST /api/v1/tools/discover` response example in the spec is illustrative only. The executable contract is the discriminated multi-round `status` protocol.
- `complete` responses must include `provider_name`, `relay_api_key_id`, and actual config write values (`base_url`, secret) after backend-side hydration. `discovered_tools.json` stores `relay_api_key_id`, never API key secrets.
- Provider secrets must not appear in the LLM prompt. The prompt may include provider metadata, but backend code hydrates final config actions after the model chooses `provider_name`.
- Because relay list APIs do not expose existing API key secrets, backend discover hydration creates fresh user keys when it needs a writeable secret for configuration output.
- Discover/session integration must be isolated behind a single helper boundary. If session bootstrap (`POST /api/v1/sessions/bootstrap`) and workspace marker flow from `2026-03-26-session-pr-attribution-design.md` are available at execution time, use them and treat the older `CreateSession` path as compatibility fallback only.
- `write_yaml` is part of scope and must be implemented, tested, and verified in the same feature plan.
- `ae-cli/config/config.go` simplification is explicitly out of scope for this plan. Only the minimum root/session/client changes needed to make discover work with current OAuth token handling are allowed.

---

## File Structure

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `backend/internal/relay/types.go` | Add tool-call capable chat message/request types |
| Modify | `backend/internal/relay/sub2api.go` | Serialize/decode multi-turn tool-call payloads |
| Create | `backend/internal/relay/types_test.go` | JSON serialization tests for tool-call messages |
| Modify | `backend/internal/relay/sub2api_test.go` | Provider request/response tests for tool-call messages |
| Create | `backend/internal/discover/types.go` | Shared request/response/config-action/tool result types |
| Create | `backend/internal/discover/conversation.go` | In-memory conversation store with TTL and round counting |
| Create | `backend/internal/discover/tools.go` | LLM tool definitions and provider-summary types |
| Create | `backend/internal/discover/prompt.go` | System prompt builder without provider secrets |
| Create | `backend/internal/discover/hydrate.go` | Backend-side config action hydration with provider credentials and `relay_api_key_id` |
| Create | `backend/internal/discover/conversation_test.go` | Conversation store tests |
| Create | `backend/internal/discover/prompt_test.go` | Tool definition and prompt contract tests |
| Create | `backend/internal/discover/hydrate_test.go` | Provider hydration tests |
| Create | `backend/internal/handler/discover.go` | `/tools/discover` and `/tools/discover/continue` handlers |
| Create | `backend/internal/handler/discover_test.go` | Handler tests for the discover protocol |
| Modify | `backend/internal/handler/router.go` | Register discover routes |
| Modify | `backend/cmd/server/main.go` | Construct and inject discover handler |
| Create | `ae-cli/internal/discover/types.go` | CLI-side decoded discover payload and cache types |
| Create | `ae-cli/internal/discover/executor.go` | Allowlisted local tool-call executor |
| Create | `ae-cli/internal/discover/actions.go` | `write_json`, `write_yaml`, `set_env`, backup, and atomic write logic |
| Create | `ae-cli/internal/discover/cache.go` | `discovered_tools.json` read/write/expiry logic |
| Create | `ae-cli/internal/discover/run.go` | End-to-end discover loop and confirmation flow |
| Create | `ae-cli/internal/discover/executor_test.go` | Executor allowlist and timeout tests |
| Create | `ae-cli/internal/discover/actions_test.go` | JSON/YAML/env write tests |
| Create | `ae-cli/internal/discover/cache_test.go` | Cache read/write/expiry tests |
| Create | `ae-cli/internal/discover/run_test.go` | Multi-round discover loop tests |
| Modify | `ae-cli/internal/client/client.go` | Add discover API request/response methods |
| Modify | `ae-cli/internal/client/client_test.go` | Add discover client tests |
| Modify | `ae-cli/internal/session/manager.go` | Read discover cache and surface metadata into the active session start/bootstrap flow |
| Modify | `ae-cli/internal/session/session_test.go` | Session-start tests with discover cache |
| Modify | `ae-cli/cmd/root.go` | Build API client base URL from OAuth token file when config URL is empty |
| Modify | `ae-cli/cmd/login.go` | Auto-trigger discover after successful login |
| Create | `ae-cli/cmd/discover.go` | Manual `ae-cli discover` command |
| Modify | `ae-cli/cmd/root_test.go` | Root/token server URL precedence tests |

---

### Task 1: Relay Tool-Call Protocol Foundation

**Files:**
- Modify: `backend/internal/relay/types.go`
- Modify: `backend/internal/relay/sub2api.go`
- Create: `backend/internal/relay/types_test.go`
- Modify: `backend/internal/relay/sub2api_test.go`

- [ ] **Step 1: Write the failing JSON serialization tests**

Create `backend/internal/relay/types_test.go` with:

```go
package relay

import (
	"encoding/json"
	"testing"
)

func TestChatMessageJSON_AssistantWithToolCallsOmitsEmptyContent(t *testing.T) {
	msg := ChatMessage{
		Role: "assistant",
		ToolCalls: []ToolCall{
			{
				ID:   "tc-1",
				Type: "function",
				Function: ToolCallFunction{
					Name:      "check_command",
					Arguments: `{"command":"claude"}`,
				},
			},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded["role"] != "assistant" {
		t.Fatalf("role = %v, want assistant", decoded["role"])
	}
	if _, ok := decoded["content"]; ok {
		t.Fatal("assistant content should be omitted when empty")
	}
	if calls, ok := decoded["tool_calls"].([]any); !ok || len(calls) != 1 {
		t.Fatalf("tool_calls = %#v, want length 1", decoded["tool_calls"])
	}
}

func TestChatMessageJSON_ToolResultIncludesToolCallID(t *testing.T) {
	msg := ChatMessage{
		Role:       "tool",
		Content:    "claude found at /opt/homebrew/bin/claude",
		ToolCallID: "tc-1",
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded["tool_call_id"] != "tc-1" {
		t.Fatalf("tool_call_id = %v, want tc-1", decoded["tool_call_id"])
	}
	if decoded["content"] != "claude found at /opt/homebrew/bin/claude" {
		t.Fatalf("content = %v, want tool output", decoded["content"])
	}
}

func TestChatMessageJSON_UserMessageHasNoToolFields(t *testing.T) {
	msg := ChatMessage{Role: "user", Content: "discover tools"}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if _, ok := decoded["tool_calls"]; ok {
		t.Fatal("tool_calls should be omitted")
	}
	if _, ok := decoded["tool_call_id"]; ok {
		t.Fatal("tool_call_id should be omitted")
	}
}
```

- [ ] **Step 2: Run the relay JSON tests to confirm they fail**

Run: `cd backend && go test ./internal/relay/... -run TestChatMessageJSON -v`
Expected: FAIL because `ChatMessage` does not yet expose `ToolCalls`, `ToolCallID`, or omitempty semantics for empty assistant content.

- [ ] **Step 3: Extend relay chat types**

Update `backend/internal/relay/types.go` so the chat types become:

```go
type ChatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
}

type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

type ChatCompletionWithToolsResponse struct {
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	TokensUsed int        `json:"tokens_used"`
}
```

- [ ] **Step 4: Add a provider round-trip test for tool-call responses**

Append this test to `backend/internal/relay/sub2api_test.go`:

```go
func TestChatCompletionWithToolsReturnsToolCalls(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		msgs := req["messages"].([]any)
		first := msgs[0].(map[string]any)
		if first["role"] != "user" {
			t.Fatalf("role = %v, want user", first["role"])
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"tool_calls": []any{
							map[string]any{
								"id":   "tc-1",
								"type": "function",
								"function": map[string]any{
									"name":      "check_command",
									"arguments": `{"command":"claude"}`,
								},
							},
						},
					},
				},
			},
			"usage": map[string]any{"total_tokens": 42},
		})
	})

	p := newTestProvider(t, mux)
	resp, err := p.ChatCompletionWithTools(context.Background(), relay.ChatCompletionRequest{
		Messages: []relay.ChatMessage{{Role: "user", Content: "discover tools"}},
	}, []relay.ToolDef{{
		Type: "function",
		Function: relay.ToolFuncDef{Name: "check_command"},
	}})
	if err != nil {
		t.Fatalf("ChatCompletionWithTools() unexpected error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Function.Name != "check_command" {
		t.Fatalf("tool name = %q, want check_command", resp.ToolCalls[0].Function.Name)
	}
}
```

- [ ] **Step 5: Re-run focused relay tests**

Run: `cd backend && go test ./internal/relay/... -run 'TestChatMessageJSON|TestChatCompletionWithToolsReturnsToolCalls' -v`
Expected: PASS

- [ ] **Step 6: Re-run the full relay package**

Run: `cd backend && go test ./internal/relay/... -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add backend/internal/relay/types.go backend/internal/relay/types_test.go backend/internal/relay/sub2api.go backend/internal/relay/sub2api_test.go
git commit -m "feat(backend): add relay tool-call message support for discover protocol"
```

---

### Task 2: Backend Discover Package And HTTP Protocol

**Files:**
- Create: `backend/internal/discover/types.go`
- Create: `backend/internal/discover/conversation.go`
- Create: `backend/internal/discover/tools.go`
- Create: `backend/internal/discover/prompt.go`
- Create: `backend/internal/discover/hydrate.go`
- Create: `backend/internal/discover/conversation_test.go`
- Create: `backend/internal/discover/prompt_test.go`
- Create: `backend/internal/discover/hydrate_test.go`
- Create: `backend/internal/handler/discover.go`
- Create: `backend/internal/handler/discover_test.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Write conversation store tests**

Create `backend/internal/discover/conversation_test.go`:

```go
package discover

import (
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
)

func TestConversationStoreCreateAppendAndGet(t *testing.T) {
	store := NewConversationStore(5 * time.Minute)

	id := store.Create([]relay.ChatMessage{{Role: "system", Content: "discover"}})
	conv, ok := store.Get(id)
	if !ok {
		t.Fatal("conversation should exist")
	}
	if conv.Round != 0 {
		t.Fatalf("round = %d, want 0", conv.Round)
	}

	if err := store.Append(id,
		relay.ChatMessage{Role: "assistant", ToolCalls: []relay.ToolCall{{ID: "tc-1", Type: "function"}}},
		relay.ChatMessage{Role: "tool", Content: "ok", ToolCallID: "tc-1"},
	); err != nil {
		t.Fatalf("append: %v", err)
	}

	conv, ok = store.Get(id)
	if !ok {
		t.Fatal("conversation should still exist")
	}
	if len(conv.Messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(conv.Messages))
	}
	if conv.Round != 1 {
		t.Fatalf("round = %d, want 1", conv.Round)
	}
}

func TestConversationStoreExpires(t *testing.T) {
	store := NewConversationStore(10 * time.Millisecond)
	id := store.Create(nil)
	time.Sleep(30 * time.Millisecond)

	if _, ok := store.Get(id); ok {
		t.Fatal("conversation should expire")
	}
}
```

- [ ] **Step 2: Run the conversation tests to verify they fail**

Run: `cd backend && go test ./internal/discover/... -run TestConversationStore -v`
Expected: FAIL because the `discover` package and store do not exist yet.

- [ ] **Step 3: Create discover request/response types and the store**

Create `backend/internal/discover/types.go`:

```go
package discover

type LocalContext struct {
	OS       string   `json:"os"`
	Arch     string   `json:"arch"`
	HomeDir  string   `json:"home_dir"`
	PathDirs []string `json:"path_dirs"`
}

type StartRequest struct {
	LocalContext LocalContext `json:"local_context"`
}

type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Result     string `json:"result"`
}

type ContinueRequest struct {
	ConversationID string       `json:"conversation_id"`
	ToolResults    []ToolResult `json:"tool_results"`
}

type ConfigAction struct {
	Type          string            `json:"type"`
	Path          string            `json:"path,omitempty"`
	File          string            `json:"file,omitempty"`
	MergeKeys     map[string]any    `json:"merge_keys,omitempty"`
	Vars          map[string]string `json:"vars,omitempty"`
	SourceProfile string            `json:"source_profile,omitempty"`
}

type DiscoveredTool struct {
	Name          string         `json:"name"`
	DisplayName   string         `json:"display_name"`
	Version       string         `json:"version,omitempty"`
	Path          string         `json:"path"`
	ProviderName  string         `json:"provider_name"`
	RelayAPIKeyID int64          `json:"relay_api_key_id"`
	ConfigPath    string         `json:"config_path,omitempty"`
	ConfigActions []ConfigAction `json:"config_actions"`
}

type Response struct {
	Status         string             `json:"status"`
	ConversationID string             `json:"conversation_id,omitempty"`
	ToolCalls      []ToolCallEnvelope `json:"tool_calls,omitempty"`
	DiscoveredTools []DiscoveredTool  `json:"discovered_tools,omitempty"`
	Error          string             `json:"error,omitempty"`
}

type ToolCallEnvelope struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}
```

Create `backend/internal/discover/conversation.go`:

```go
package discover

import (
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ai-efficiency/backend/internal/relay"
)

var ErrConversationNotFound = errors.New("discover: conversation not found")

type Conversation struct {
	Messages  []relay.ChatMessage
	Round     int
	ExpiresAt time.Time
}

type ConversationStore struct {
	ttl time.Duration
	mu  sync.RWMutex
	m   map[string]*Conversation
}

func NewConversationStore(ttl time.Duration) *ConversationStore {
	return &ConversationStore{
		ttl: ttl,
		m:   make(map[string]*Conversation),
	}
}

func (s *ConversationStore) Create(messages []relay.ChatMessage) string {
	id := uuid.NewString()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[id] = &Conversation{
		Messages:  append([]relay.ChatMessage(nil), messages...),
		ExpiresAt: time.Now().Add(s.ttl),
	}
	return id
}

func (s *ConversationStore) Get(id string) (*Conversation, bool) {
	s.mu.RLock()
	conv, ok := s.m[id]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Now().After(conv.ExpiresAt) {
		s.Delete(id)
		return nil, false
	}
	return conv, true
}

func (s *ConversationStore) Append(id string, messages ...relay.ChatMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	conv, ok := s.m[id]
	if !ok || time.Now().After(conv.ExpiresAt) {
		delete(s.m, id)
		return ErrConversationNotFound
	}
	conv.Messages = append(conv.Messages, messages...)
	conv.Round++
	conv.ExpiresAt = time.Now().Add(s.ttl)
	return nil
}

func (s *ConversationStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, id)
}
```

- [ ] **Step 4: Re-run the conversation tests**

Run: `cd backend && go test ./internal/discover/... -run TestConversationStore -v`
Expected: PASS

- [ ] **Step 5: Write failing tests for prompt/tool definitions and provider hydration**

Create `backend/internal/discover/prompt_test.go`:

```go
package discover

import (
	"strings"
	"testing"
)

func TestToolDefinitionsExposeExpectedFunctions(t *testing.T) {
	tools := ToolDefinitions()
	if len(tools) != 3 {
		t.Fatalf("tools = %d, want 3", len(tools))
	}
	names := []string{
		tools[0].Function.Name,
		tools[1].Function.Name,
		tools[2].Function.Name,
	}
	want := []string{"check_command", "read_file", "list_dir"}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("tool[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestBuildSystemPromptUsesProviderMetadataWithoutSecrets(t *testing.T) {
	prompt := BuildSystemPrompt([]PromptProvider{
		{Name: "sub2api-claude", DisplayName: "Claude", BaseURL: "http://relay/v1", DefaultModel: "claude-sonnet", IsPrimary: true},
	})
	if !strings.Contains(prompt, "sub2api-claude") {
		t.Fatal("provider name missing from prompt")
	}
	if strings.Contains(prompt, "sk-") {
		t.Fatal("prompt must not contain provider secrets")
	}
}
```

Create `backend/internal/discover/hydrate_test.go`:

```go
package discover

import "testing"

func TestHydrateDiscoveredToolsInjectsRuntimeSecretsAndIDs(t *testing.T) {
	tools, err := HydrateDiscoveredTools([]DraftTool{
		{
			Name:         "claude",
			DisplayName:  "Claude Code",
			Path:         "/opt/homebrew/bin/claude",
			ProviderName: "sub2api-claude",
			ConfigActions: []DraftConfigAction{
				{
					Type: "write_json",
					Path: "/Users/admin/.claude/settings.json",
				},
			},
		},
	}, map[string]ResolvedProvider{
		"sub2api-claude": {
			Name:          "sub2api-claude",
			BaseURL:       "http://relay/v1",
			APIKeySecret:  "sk-user-123",
			RelayAPIKeyID: 123,
		},
	})
	if err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	if tools[0].RelayAPIKeyID != 123 {
		t.Fatalf("relay_api_key_id = %d, want 123", tools[0].RelayAPIKeyID)
	}
	env := tools[0].ConfigActions[0].MergeKeys["env"].(map[string]any)
	if env["ANTHROPIC_API_KEY"] != "sk-user-123" {
		t.Fatalf("env api key not hydrated")
	}
}
```

- [ ] **Step 6: Implement tool definitions, prompt builder, and hydration**

Create `backend/internal/discover/tools.go`:

```go
package discover

import (
	"encoding/json"

	"github.com/ai-efficiency/backend/internal/relay"
)

func ToolDefinitions() []relay.ToolDef {
	return []relay.ToolDef{
		newTool("check_command", "Check whether a CLI command exists and return its version", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string"},
			},
			"required": []string{"command"},
		}),
		newTool("read_file", "Read a local configuration file from an allowlisted path", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
			},
			"required": []string{"path"},
		}),
		newTool("list_dir", "List files in an allowlisted directory", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
			},
			"required": []string{"path"},
		}),
	}
}

func newTool(name, description string, schema map[string]any) relay.ToolDef {
	raw, _ := json.Marshal(schema)
	return relay.ToolDef{
		Type: "function",
		Function: relay.ToolFuncDef{
			Name:        name,
			Description: description,
			Parameters:  raw,
		},
	}
}
```

Create `backend/internal/discover/prompt.go`:

```go
package discover

import (
	"fmt"
	"strings"
)

type PromptProvider struct {
	Name         string
	DisplayName  string
	BaseURL      string
	DefaultModel string
	IsPrimary    bool
}

func BuildSystemPrompt(providers []PromptProvider) string {
	var b strings.Builder
	b.WriteString("你是 ae-cli 的工具发现助手。\n")
	b.WriteString("目标：发现本地 AI 工具、读取现有配置、选择 provider_name、输出 config_actions。\n")
	b.WriteString("禁止输出 provider secret；后端会在最终响应中注入运行时凭据。\n")
	b.WriteString("可用 provider：\n")
	for _, p := range providers {
		fmt.Fprintf(&b, "- name=%s display_name=%s base_url=%s default_model=%s is_primary=%t\n",
			p.Name, p.DisplayName, p.BaseURL, p.DefaultModel, p.IsPrimary)
	}
	b.WriteString("返回时只使用上述 provider_name。")
	return b.String()
}
```

Create `backend/internal/discover/hydrate.go`:

```go
package discover

import "fmt"

type DraftConfigAction struct {
	Type          string         `json:"type"`
	Path          string         `json:"path,omitempty"`
	File          string         `json:"file,omitempty"`
	SourceProfile string         `json:"source_profile,omitempty"`
	MergeKeys     map[string]any `json:"merge_keys,omitempty"`
	Vars          map[string]any `json:"vars,omitempty"`
}

type DraftTool struct {
	Name          string              `json:"name"`
	DisplayName   string              `json:"display_name"`
	Version       string              `json:"version,omitempty"`
	Path          string              `json:"path"`
	ProviderName  string              `json:"provider_name"`
	ConfigPath    string              `json:"config_path,omitempty"`
	ConfigActions []DraftConfigAction `json:"config_actions"`
}

type ResolvedProvider struct {
	Name          string
	BaseURL       string
	APIKeySecret  string
	RelayAPIKeyID int64
}

func HydrateDiscoveredTools(drafts []DraftTool, providers map[string]ResolvedProvider) ([]DiscoveredTool, error) {
	out := make([]DiscoveredTool, 0, len(drafts))
	for _, draft := range drafts {
		provider, ok := providers[draft.ProviderName]
		if !ok {
			return nil, fmt.Errorf("unknown provider_name %q", draft.ProviderName)
		}

		tool := DiscoveredTool{
			Name:          draft.Name,
			DisplayName:   draft.DisplayName,
			Version:       draft.Version,
			Path:          draft.Path,
			ProviderName:  draft.ProviderName,
			RelayAPIKeyID: provider.RelayAPIKeyID,
			ConfigPath:    draft.ConfigPath,
		}

		for _, action := range draft.ConfigActions {
			switch action.Type {
			case "write_json":
				tool.ConfigActions = append(tool.ConfigActions, ConfigAction{
					Type: action.Type,
					Path: action.Path,
					MergeKeys: map[string]any{
						"env": map[string]any{
							"ANTHROPIC_BASE_URL": provider.BaseURL,
							"ANTHROPIC_API_KEY":  provider.APIKeySecret,
						},
					},
				})
			case "write_yaml":
				tool.ConfigActions = append(tool.ConfigActions, ConfigAction{
					Type: action.Type,
					Path: action.Path,
					MergeKeys: map[string]any{
						"openai_base_url": provider.BaseURL,
						"openai_api_key":  provider.APIKeySecret,
					},
				})
			case "set_env":
				tool.ConfigActions = append(tool.ConfigActions, ConfigAction{
					Type:          action.Type,
					File:          action.File,
					SourceProfile: action.SourceProfile,
					Vars: map[string]string{
						"OPENAI_BASE_URL": provider.BaseURL,
						"OPENAI_API_KEY":  provider.APIKeySecret,
					},
				})
			default:
				return nil, fmt.Errorf("unsupported config action %q", action.Type)
			}
		}

		out = append(out, tool)
	}
	return out, nil
}
```

- [ ] **Step 7: Add failing handler tests for the discover protocol**

Create `backend/internal/handler/discover_test.go`:

```go
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/discover"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/gin-gonic/gin"
)

type stubDiscoverProvider struct {
	withTools func(ctx context.Context, req relay.ChatCompletionRequest, tools []relay.ToolDef) (*relay.ChatCompletionWithToolsResponse, error)
}

func (s *stubDiscoverProvider) Ping(context.Context) error { return nil }
func (s *stubDiscoverProvider) Name() string { return "stub" }
func (s *stubDiscoverProvider) Authenticate(context.Context, string, string) (*relay.User, error) { return nil, nil }
func (s *stubDiscoverProvider) GetUser(context.Context, int64) (*relay.User, error) { return nil, nil }
func (s *stubDiscoverProvider) FindUserByEmail(context.Context, string) (*relay.User, error) { return nil, nil }
func (s *stubDiscoverProvider) ChatCompletion(context.Context, relay.ChatCompletionRequest) (*relay.ChatCompletionResponse, error) {
	return nil, nil
}
func (s *stubDiscoverProvider) ChatCompletionWithTools(ctx context.Context, req relay.ChatCompletionRequest, tools []relay.ToolDef) (*relay.ChatCompletionWithToolsResponse, error) {
	return s.withTools(ctx, req, tools)
}
func (s *stubDiscoverProvider) GetUsageStats(context.Context, int64, time.Time, time.Time) (*relay.UsageStats, error) { return nil, nil }
func (s *stubDiscoverProvider) ListUserAPIKeys(context.Context, int64) ([]relay.APIKey, error) { return nil, nil }
func (s *stubDiscoverProvider) CreateUserAPIKey(context.Context, int64, string) (*relay.APIKeyWithSecret, error) { return nil, nil }

func TestDiscoverStartReturnsToolCallRequired(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &DiscoverHandler{
		provider: &stubDiscoverProvider{
			withTools: func(ctx context.Context, req relay.ChatCompletionRequest, tools []relay.ToolDef) (*relay.ChatCompletionWithToolsResponse, error) {
				return &relay.ChatCompletionWithToolsResponse{
					ToolCalls: []relay.ToolCall{{
						ID:   "tc-1",
						Type: "function",
						Function: relay.ToolCallFunction{
							Name:      "check_command",
							Arguments: `{"command":"claude"}`,
						},
					}},
				}, nil
			},
		},
		store: discover.NewConversationStore(5 * time.Minute),
	}

	body := bytes.NewBufferString(`{"local_context":{"os":"darwin","arch":"arm64","home_dir":"/Users/admin","path_dirs":["/opt/homebrew/bin"]}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tools/discover", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user_id", 1)

	h.Start(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp discover.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "tool_call_required" {
		t.Fatalf("status = %q, want tool_call_required", resp.Status)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool_calls = %d, want 1", len(resp.ToolCalls))
	}
}
```

- [ ] **Step 8: Implement `DiscoverHandler`, route registration, and server wiring**

Create `backend/internal/handler/discover.go` with this structure:

```go
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	"github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/discover"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	maxDiscoverRounds = 20
	discoverTTL       = 5 * time.Minute
)

type DiscoverHandler struct {
	entClient      *ent.Client
	provider       relay.Provider
	store          *discover.ConversationStore
	encryptionKey  string
	logger         *zap.Logger
}

func NewDiscoverHandler(entClient *ent.Client, provider relay.Provider, encryptionKey string, logger *zap.Logger) *DiscoverHandler {
	return &DiscoverHandler{
		entClient:     entClient,
		provider:      provider,
		store:         discover.NewConversationStore(discoverTTL),
		encryptionKey: encryptionKey,
		logger:        logger,
	}
}

func (h *DiscoverHandler) Start(c *gin.Context) {
	var req discover.StartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, discover.Response{Status: "error", Error: err.Error()})
		return
	}

	userID := c.GetInt("user_id")
	promptProviders, resolvedProviders, err := h.loadResolvedProviders(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, discover.Response{Status: "error", Error: "failed to load providers"})
		return
	}

	messages := []relay.ChatMessage{
		{Role: "system", Content: discover.BuildSystemPrompt(promptProviders)},
		{Role: "user", Content: buildUserPrompt(req.LocalContext)},
	}
	resp, err := h.provider.ChatCompletionWithTools(c.Request.Context(), relay.ChatCompletionRequest{
		Messages: messages,
	}, discover.ToolDefinitions())
	if err != nil {
		c.JSON(http.StatusBadGateway, discover.Response{Status: "error", Error: err.Error()})
		return
	}

	conversationID := h.store.Create(messages)

	if len(resp.ToolCalls) > 0 {
		_ = h.store.Append(conversationID, relay.ChatMessage{
			Role:      "assistant",
			ToolCalls: resp.ToolCalls,
		})
		c.JSON(http.StatusOK, discover.Response{
			Status:         "tool_call_required",
			ConversationID: conversationID,
			ToolCalls:      projectToolCalls(resp.ToolCalls),
		})
		return
	}

	drafts, err := decodeDraftTools(resp.Content)
	if err != nil {
		c.JSON(http.StatusBadGateway, discover.Response{Status: "error", ConversationID: conversationID, Error: err.Error()})
		return
	}
	tools, err := discover.HydrateDiscoveredTools(drafts, resolvedProviders)
	if err != nil {
		c.JSON(http.StatusBadGateway, discover.Response{Status: "error", ConversationID: conversationID, Error: err.Error()})
		return
	}
	h.store.Delete(conversationID)
	c.JSON(http.StatusOK, discover.Response{
		Status:          "complete",
		ConversationID:  conversationID,
		DiscoveredTools: tools,
	})
}

func (h *DiscoverHandler) Continue(c *gin.Context) {
	var req discover.ContinueRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, discover.Response{Status: "error", Error: err.Error()})
		return
	}

	conv, ok := h.store.Get(req.ConversationID)
	if !ok {
		c.JSON(http.StatusNotFound, discover.Response{Status: "error", ConversationID: req.ConversationID, Error: "conversation expired or not found"})
		return
	}
	if conv.Round >= maxDiscoverRounds {
		h.store.Delete(req.ConversationID)
		c.JSON(http.StatusBadRequest, discover.Response{Status: "error", ConversationID: req.ConversationID, Error: "maximum discover rounds exceeded"})
		return
	}

	toolMessages := make([]relay.ChatMessage, 0, len(req.ToolResults))
	for _, result := range req.ToolResults {
		toolMessages = append(toolMessages, relay.ChatMessage{
			Role:       "tool",
			Content:    result.Result,
			ToolCallID: result.ToolCallID,
		})
	}
	if err := h.store.Append(req.ConversationID, toolMessages...); err != nil {
		c.JSON(http.StatusNotFound, discover.Response{Status: "error", ConversationID: req.ConversationID, Error: err.Error()})
		return
	}

	userID := c.GetInt("user_id")
	_, resolvedProviders, err := h.loadResolvedProviders(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, discover.Response{Status: "error", ConversationID: req.ConversationID, Error: "failed to load providers"})
		return
	}

	resp, err := h.provider.ChatCompletionWithTools(c.Request.Context(), relay.ChatCompletionRequest{
		Messages: conv.Messages,
	}, discover.ToolDefinitions())
	if err != nil {
		c.JSON(http.StatusBadGateway, discover.Response{Status: "error", ConversationID: req.ConversationID, Error: err.Error()})
		return
	}

	if len(resp.ToolCalls) > 0 {
		_ = h.store.Append(req.ConversationID, relay.ChatMessage{Role: "assistant", ToolCalls: resp.ToolCalls})
		c.JSON(http.StatusOK, discover.Response{
			Status:         "tool_call_required",
			ConversationID: req.ConversationID,
			ToolCalls:      projectToolCalls(resp.ToolCalls),
		})
		return
	}

	drafts, err := decodeDraftTools(resp.Content)
	if err != nil {
		c.JSON(http.StatusBadGateway, discover.Response{Status: "error", ConversationID: req.ConversationID, Error: err.Error()})
		return
	}
	tools, err := discover.HydrateDiscoveredTools(drafts, resolvedProviders)
	if err != nil {
		c.JSON(http.StatusBadGateway, discover.Response{Status: "error", ConversationID: req.ConversationID, Error: err.Error()})
		return
	}
	h.store.Delete(req.ConversationID)
	c.JSON(http.StatusOK, discover.Response{
		Status:          "complete",
		ConversationID:  req.ConversationID,
		DiscoveredTools: tools,
	})
}

func (h *DiscoverHandler) loadResolvedProviders(ctx context.Context, userID int) ([]discover.PromptProvider, map[string]discover.ResolvedProvider, error) {
	u, err := h.entClient.User.Query().Where(user.IDEQ(userID)).Only(ctx)
	if err != nil {
		return nil, nil, err
	}

	providers, err := h.entClient.RelayProvider.Query().
		Where(relayprovider.EnabledEQ(true)).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}

	promptProviders := make([]discover.PromptProvider, 0, len(providers))
	resolved := make(map[string]discover.ResolvedProvider, len(providers))

	for _, p := range providers {
		adminKey, err := pkg.Decrypt(p.AdminAPIKey, h.encryptionKey)
		if err != nil {
			return nil, nil, err
		}
		rp := relay.NewSub2apiProvider(http.DefaultClient, p.BaseURL, p.AdminURL, adminKey, p.DefaultModel, h.logger)

		if u.RelayUserID == nil {
			relayUser, err := rp.FindUserByEmail(ctx, u.Email)
			if err != nil {
				return nil, nil, err
			}
			if relayUser == nil {
				return nil, nil, fmt.Errorf("relay user not found for %s", u.Email)
			}
			relayID := int(relayUser.ID)
			if _, err := h.entClient.User.UpdateOneID(u.ID).SetRelayUserID(relayID).Save(ctx); err != nil {
				return nil, nil, err
			}
			u.RelayUserID = &relayID
		}

		key, err := rp.CreateUserAPIKey(ctx, int64(*u.RelayUserID), fmt.Sprintf("ae-cli-discover-%d", time.Now().Unix()))
		if err != nil {
			return nil, nil, err
		}

		promptProviders = append(promptProviders, discover.PromptProvider{
			Name:         p.Name,
			DisplayName:  p.DisplayName,
			BaseURL:      p.BaseURL,
			DefaultModel: p.DefaultModel,
			IsPrimary:    p.IsPrimary,
		})
		resolved[p.Name] = discover.ResolvedProvider{
			Name:          p.Name,
			BaseURL:       p.BaseURL,
			APIKeySecret:  key.Secret,
			RelayAPIKeyID: key.ID,
		}
	}

	return promptProviders, resolved, nil
}

func buildUserPrompt(local discover.LocalContext) string {
	raw, _ := json.Marshal(local)
	return "本地执行上下文如下，请使用工具发现已安装 AI 工具并输出 discovered_tools JSON:\n" + string(raw)
}

func decodeDraftTools(content string) ([]discover.DraftTool, error) {
	var drafts []discover.DraftTool
	if err := json.Unmarshal([]byte(content), &drafts); err != nil {
		return nil, fmt.Errorf("decode discovered tools: %w", err)
	}
	return drafts, nil
}

func projectToolCalls(calls []relay.ToolCall) []discover.ToolCallEnvelope {
	out := make([]discover.ToolCallEnvelope, 0, len(calls))
	for _, call := range calls {
		args := map[string]any{}
		_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
		out = append(out, discover.ToolCallEnvelope{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: args,
		})
	}
	return out
}
```

Add route registration in `backend/internal/handler/router.go`:

```go
func SetupRouter(
	entClient *ent.Client,
	authService *auth.Service,
	repoService *repo.Service,
	analysisService analysisScanner,
	webhookHandler *webhook.Handler,
	syncService prSyncer,
	settingsHandler *SettingsHandler,
	chatHandler *ChatHandler,
	aggregator *efficiency.Aggregator,
	optimizer optimizerService,
	encryptionKey string,
	corsMiddleware gin.HandlerFunc,
	oauthHandler *oauth.Handler,
	providerHandler *ProviderHandler,
	adminSettingsHandler *AdminSettingsHandler,
	discoverHandler *DiscoverHandler,
) *gin.Engine {
	if discoverHandler != nil {
		protected.POST("/tools/discover", discoverHandler.Start)
		protected.POST("/tools/discover/continue", discoverHandler.Continue)
	}
}
```

Wire the handler in `backend/cmd/server/main.go`:

```go
discoverHandler := handler.NewDiscoverHandler(entClient, relayProvider, cfg.Encryption.Key, logger)

r := handler.SetupRouter(
	entClient,
	authService,
	repoService,
	analysisService,
	webhookHandler,
	syncService,
	settingsHandler,
	chatHandler,
	aggregator,
	optimizer,
	cfg.Encryption.Key,
	middleware.CORS(nil),
	oauthHandler,
	providerHandler,
	adminSettingsHandler,
	discoverHandler,
)
```

- [ ] **Step 9: Run backend discover tests and focused build**

Run: `cd backend && go test ./internal/discover/... ./internal/handler/... -run 'TestConversationStore|TestToolDefinitions|TestBuildSystemPrompt|TestHydrateDiscoveredTools|TestDiscover' -v`
Expected: PASS

Run: `cd backend && go build ./cmd/server`
Expected: PASS

- [ ] **Step 10: Commit**

```bash
git add backend/internal/discover backend/internal/handler/discover.go backend/internal/handler/discover_test.go backend/internal/handler/router.go backend/cmd/server/main.go
git commit -m "feat(backend): add multi-round discover protocol and provider hydration"
```

---

### Task 3: CLI Local Execution, Config Writes, And Cache

**Files:**
- Create: `ae-cli/internal/discover/types.go`
- Create: `ae-cli/internal/discover/executor.go`
- Create: `ae-cli/internal/discover/actions.go`
- Create: `ae-cli/internal/discover/cache.go`
- Create: `ae-cli/internal/discover/executor_test.go`
- Create: `ae-cli/internal/discover/actions_test.go`
- Create: `ae-cli/internal/discover/cache_test.go`
- Modify: `ae-cli/go.mod`

- [ ] **Step 1: Write failing executor tests**

Create `ae-cli/internal/discover/executor_test.go`:

```go
package discover

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecuteToolCallRejectsUnknownCommand(t *testing.T) {
	exec := NewExecutor("/Users/test", []string{"/opt/homebrew/bin"})
	_, err := exec.Execute(ToolCall{
		ID:        "tc-1",
		Name:      "check_command",
		Arguments: map[string]any{"command": "python"},
	})
	if err == nil || !strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("expected allowlist error, got %v", err)
	}
}

func TestExecuteToolCallReadsAllowlistedFile(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".claude", "settings.json")
	if err := os.WriteFile(target, []byte(`{"env":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	exec := NewExecutor(home, []string{"/opt/homebrew/bin"})
	out, err := exec.Execute(ToolCall{
		ID:        "tc-2",
		Name:      "read_file",
		Arguments: map[string]any{"path": target},
	})
	if err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if !strings.Contains(out, `"env"`) {
		t.Fatalf("unexpected read output: %q", out)
	}
}
```

- [ ] **Step 2: Write failing action and cache tests**

Create `ae-cli/internal/discover/actions_test.go`:

```go
package discover

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecuteWriteJSONDeepMergesAndBacksUp(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(target, []byte(`{"env":{"EXISTING":"1"},"theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	err := ExecuteAction(ConfigAction{
		Type: "write_json",
		Path: target,
		MergeKeys: map[string]any{
			"env": map[string]any{
				"ANTHROPIC_API_KEY": "sk-user-123",
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("ExecuteAction(): %v", err)
	}

	if _, err := os.Stat(target + ".bak"); err != nil {
		t.Fatalf("expected backup file: %v", err)
	}
}

func TestExecuteWriteYAMLMergesExistingConfig(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, ".aider.conf.yml")
	if err := os.WriteFile(target, []byte("model: old\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := ExecuteAction(ConfigAction{
		Type: "write_yaml",
		Path: target,
		MergeKeys: map[string]any{
			"openai_api_key": "sk-user-123",
			"openai_base_url": "http://relay/v1",
		},
	}, false)
	if err != nil {
		t.Fatalf("ExecuteAction(): %v", err)
	}
}

func TestExecuteSetEnvWritesManagedEnvFile(t *testing.T) {
	home := t.TempDir()
	profile := filepath.Join(home, ".zshrc")
	envFile := filepath.Join(home, ".ae-cli", "env")
	if err := os.WriteFile(profile, []byte("# shell profile\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := ExecuteAction(ConfigAction{
		Type:          "set_env",
		File:          envFile,
		SourceProfile: profile,
		Vars: map[string]string{
			"OPENAI_API_KEY": "sk-user-123",
			"OPENAI_BASE_URL": "http://relay/v1",
		},
	}, false)
	if err != nil {
		t.Fatalf("ExecuteAction(): %v", err)
	}
}
```

Create `ae-cli/internal/discover/cache_test.go`:

```go
package discover

import (
	"testing"
	"time"
)

func TestCacheReadWriteAndExpiry(t *testing.T) {
	dir := t.TempDir()
	path := CachePathFromHome(dir)

	cache := CacheFile{
		DiscoveredAt: time.Now().UTC(),
		Tools: []CachedTool{
			{
				Name:          "claude",
				DisplayName:   "Claude Code",
				ProviderName:  "sub2api-claude",
				RelayAPIKeyID: 123,
				ConfigPath:    "/Users/test/.claude/settings.json",
			},
		},
	}
	if err := WriteCache(path, cache); err != nil {
		t.Fatalf("WriteCache(): %v", err)
	}

	got, err := ReadCache(path)
	if err != nil {
		t.Fatalf("ReadCache(): %v", err)
	}
	if got.Tools[0].RelayAPIKeyID != 123 {
		t.Fatalf("relay_api_key_id = %d, want 123", got.Tools[0].RelayAPIKeyID)
	}

	if got.IsExpired(7 * 24 * time.Hour) {
		t.Fatal("fresh cache should not be expired")
	}
}
```

- [ ] **Step 3: Run the discover unit tests to verify they fail**

Run: `cd ae-cli && go test ./internal/discover/... -run 'TestExecuteToolCall|TestExecuteWrite|TestCacheReadWrite' -v`
Expected: FAIL because the discover package does not yet exist.

- [ ] **Step 4: Implement CLI discover types and executor**

Create `ae-cli/internal/discover/types.go`:

```go
package discover

import "time"

type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type ConfigAction struct {
	Type          string            `json:"type"`
	Path          string            `json:"path,omitempty"`
	File          string            `json:"file,omitempty"`
	MergeKeys     map[string]any    `json:"merge_keys,omitempty"`
	Vars          map[string]string `json:"vars,omitempty"`
	SourceProfile string            `json:"source_profile,omitempty"`
}

type DiscoveredTool struct {
	Name          string         `json:"name"`
	DisplayName   string         `json:"display_name"`
	Version       string         `json:"version,omitempty"`
	Path          string         `json:"path"`
	ProviderName  string         `json:"provider_name"`
	RelayAPIKeyID int64          `json:"relay_api_key_id"`
	ConfigPath    string         `json:"config_path,omitempty"`
	ConfigActions []ConfigAction `json:"config_actions"`
}

type Response struct {
	Status          string           `json:"status"`
	ConversationID  string           `json:"conversation_id,omitempty"`
	ToolCalls       []ToolCall       `json:"tool_calls,omitempty"`
	DiscoveredTools []DiscoveredTool `json:"discovered_tools,omitempty"`
	Error           string           `json:"error,omitempty"`
}

type CachedTool struct {
	Name          string `json:"name"`
	DisplayName   string `json:"display_name"`
	ProviderName  string `json:"provider_name"`
	RelayAPIKeyID int64  `json:"relay_api_key_id"`
	ConfigPath    string `json:"config_path,omitempty"`
}

type CacheFile struct {
	DiscoveredAt time.Time    `json:"discovered_at"`
	Tools        []CachedTool `json:"tools"`
}
```

Create `ae-cli/internal/discover/executor.go`:

```go
package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxToolResultBytes = 10 * 1024
	toolTimeout        = 5 * time.Second
)

var allowedCommands = map[string]struct{}{
	"claude": {},
	"codex":  {},
	"cursor": {},
	"aider":  {},
	"continue": {},
	"copilot": {},
	"cody": {},
}

type Executor struct {
	homeDir  string
	pathDirs []string
}

func NewExecutor(homeDir string, pathDirs []string) *Executor {
	return &Executor{homeDir: homeDir, pathDirs: pathDirs}
}

func (e *Executor) Execute(call ToolCall) (string, error) {
	switch call.Name {
	case "check_command":
		return e.checkCommand(call)
	case "read_file":
		return e.readFile(call)
	case "list_dir":
		return e.listDir(call)
	default:
		return "", fmt.Errorf("tool %q not supported", call.Name)
	}
}

func (e *Executor) checkCommand(call ToolCall) (string, error) {
	command, _ := call.Arguments["command"].(string)
	if _, ok := allowedCommands[command]; !ok {
		return "", fmt.Errorf("command %q not in allowlist", command)
	}

	ctx, cancel := context.WithTimeout(context.Background(), toolTimeout)
	defer cancel()

	whichOut, err := exec.CommandContext(ctx, "which", command).CombinedOutput()
	if err != nil {
		return truncate(string(whichOut)), nil
	}
	path := strings.TrimSpace(string(whichOut))
	versionOut, _ := exec.CommandContext(ctx, command, "--version").CombinedOutput()
	payload, _ := json.Marshal(map[string]string{
		"path":    path,
		"version": truncate(strings.TrimSpace(string(versionOut))),
	})
	return truncate(string(payload)), nil
}

func (e *Executor) readFile(call ToolCall) (string, error) {
	path, _ := call.Arguments["path"].(string)
	resolved, err := e.resolveAllowedPath(path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", err
	}
	return truncate(string(data)), nil
}

func (e *Executor) listDir(call ToolCall) (string, error) {
	path, _ := call.Arguments["path"].(string)
	resolved, err := e.resolveAllowedDir(path)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(entries))
	for i, entry := range entries {
		if i >= 100 {
			break
		}
		names = append(names, entry.Name())
	}
	data, _ := json.Marshal(names)
	return truncate(string(data)), nil
}

func truncate(s string) string {
	if len(s) <= maxToolResultBytes {
		return s
	}
	return s[:maxToolResultBytes]
}

func (e *Executor) resolveAllowedPath(input string) (string, error) {
	resolved, err := e.resolve(input)
	if err != nil {
		return "", err
	}
	allowedPrefixes := []string{
		filepath.Join(e.homeDir, ".claude"),
		filepath.Join(e.homeDir, ".cursor"),
		filepath.Join(e.homeDir, ".config", "codex"),
		filepath.Join(e.homeDir, ".config", "continue"),
		filepath.Join(e.homeDir, ".ae-cli"),
		filepath.Join(e.homeDir, ".zshrc"),
		filepath.Join(e.homeDir, ".bashrc"),
		filepath.Join(e.homeDir, ".bash_profile"),
		filepath.Join(e.homeDir, ".profile"),
	}
	for _, prefix := range allowedPrefixes {
		if resolved == prefix || strings.HasPrefix(resolved, prefix+string(os.PathSeparator)) {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("path %q not in allowlist", input)
}

func (e *Executor) resolveAllowedDir(input string) (string, error) {
	resolved, err := e.resolve(input)
	if err != nil {
		return "", err
	}
	allowed := map[string]struct{}{
		e.homeDir: {},
		filepath.Join(e.homeDir, ".claude"): {},
		filepath.Join(e.homeDir, ".cursor"): {},
		filepath.Join(e.homeDir, ".config"): {},
		filepath.Join(e.homeDir, ".config", "codex"): {},
		filepath.Join(e.homeDir, ".config", "continue"): {},
	}
	if _, ok := allowed[resolved]; !ok {
		return "", fmt.Errorf("directory %q not in allowlist", input)
	}
	return resolved, nil
}

func (e *Executor) resolve(input string) (string, error) {
	if strings.HasPrefix(input, "~/") {
		input = filepath.Join(e.homeDir, strings.TrimPrefix(input, "~/"))
	}
	resolved, err := filepath.EvalSymlinks(input)
	if err == nil {
		return resolved, nil
	}
	return filepath.Clean(input), nil
}
```

- [ ] **Step 5: Implement config actions and cache with full YAML support**

Create `ae-cli/internal/discover/actions.go`:

```go
package discover

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

func ExecuteAction(action ConfigAction, dryRun bool) error {
	switch action.Type {
	case "write_json":
		return executeWriteJSON(action, dryRun)
	case "write_yaml":
		return executeWriteYAML(action, dryRun)
	case "set_env":
		return executeSetEnv(action, dryRun)
	default:
		return fmt.Errorf("unsupported action type %q", action.Type)
	}
}

func executeWriteJSON(action ConfigAction, dryRun bool) error {
	var current map[string]any
	if data, err := os.ReadFile(action.Path); err == nil {
		if err := json.Unmarshal(data, &current); err != nil {
			return fmt.Errorf("parse json: %w", err)
		}
	}
	if current == nil {
		current = map[string]any{}
	}
	merged := deepMerge(current, action.MergeKeys)
	if dryRun {
		return nil
	}
	return writeStructuredFile(action.Path, merged, marshalJSON)
}

func executeWriteYAML(action ConfigAction, dryRun bool) error {
	var current map[string]any
	if data, err := os.ReadFile(action.Path); err == nil {
		if err := yaml.Unmarshal(data, &current); err != nil {
			return fmt.Errorf("parse yaml: %w", err)
		}
	}
	if current == nil {
		current = map[string]any{}
	}
	merged := deepMerge(current, action.MergeKeys)
	if dryRun {
		return nil
	}
	return writeStructuredFile(action.Path, merged, yaml.Marshal)
}

func executeSetEnv(action ConfigAction, dryRun bool) error {
	lines := make([]string, 0, len(action.Vars))
	keys := make([]string, 0, len(action.Vars))
	for key := range action.Vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("export %s=%q", key, action.Vars[key]))
	}
	content := strings.Join(lines, "\n") + "\n"
	if dryRun {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(action.File), 0o700); err != nil {
		return err
	}
	if err := writeWithBackup(action.File, []byte(content)); err != nil {
		return err
	}

	profileData, _ := os.ReadFile(action.SourceProfile)
	sourceLine := "source " + action.File
	if !strings.Contains(string(profileData), sourceLine) {
		appendix := "\n# ae-cli managed\n" + sourceLine + "\n"
		if err := os.WriteFile(action.SourceProfile, append(profileData, []byte(appendix)...), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func deepMerge(dst, src map[string]any) map[string]any {
	for key, value := range src {
		if srcMap, ok := value.(map[string]any); ok {
			if dstMap, ok := dst[key].(map[string]any); ok {
				dst[key] = deepMerge(dstMap, srcMap)
				continue
			}
		}
		dst[key] = value
	}
	return dst
}

func marshalJSON(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

func writeStructuredFile(path string, value map[string]any, marshal func(any) ([]byte, error)) error {
	data, err := marshal(value)
	if err != nil {
		return err
	}
	return writeWithBackup(path, data)
}

func writeWithBackup(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if existing, err := os.ReadFile(path); err == nil {
		if err := os.WriteFile(path+".bak", existing, 0o600); err != nil {
			return err
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
```

Create `ae-cli/internal/discover/cache.go`:

```go
package discover

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func CachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home dir: %w", err)
	}
	return CachePathFromHome(home), nil
}

func CachePathFromHome(home string) string {
	return filepath.Join(home, ".ae-cli", "discovered_tools.json")
}

func ReadCache(path string) (CacheFile, error) {
	var cache CacheFile
	data, err := os.ReadFile(path)
	if err != nil {
		return cache, err
	}
	err = json.Unmarshal(data, &cache)
	return cache, err
}

func WriteCache(path string, cache CacheFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c CacheFile) IsExpired(ttl time.Duration) bool {
	return time.Since(c.DiscoveredAt) > ttl
}
```

- [ ] **Step 6: Re-run focused discover unit tests**

Run: `cd ae-cli && go test ./internal/discover/... -run 'TestExecuteToolCall|TestExecuteWrite|TestExecuteSetEnv|TestCacheReadWrite' -v`
Expected: PASS

- [ ] **Step 7: Make YAML support explicit in `ae-cli/go.mod`**

Run: `cd ae-cli && go mod edit -require=gopkg.in/yaml.v3@v3.0.1`
Expected: `ae-cli/go.mod` now lists `gopkg.in/yaml.v3` as a direct requirement.

- [ ] **Step 8: Re-run the discover unit tests after module update**

Run: `cd ae-cli && go test ./internal/discover/... -v`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add ae-cli/internal/discover ae-cli/go.mod ae-cli/go.sum
git commit -m "feat(ae-cli): add local discover executor, config writers, and cache"
```

---

### Task 4: CLI Discover Command, Login Trigger, Session/Bootstrap Integration, And Root Wiring

**Files:**
- Create: `ae-cli/internal/discover/run.go`
- Modify: `ae-cli/internal/client/client.go`
- Modify: `ae-cli/internal/client/client_test.go`
- Modify: `ae-cli/internal/session/manager.go`
- Modify: `ae-cli/internal/session/session_test.go`
- Modify: `ae-cli/cmd/root.go`
- Modify: `ae-cli/cmd/login.go`
- Create: `ae-cli/cmd/discover.go`
- Modify: `ae-cli/cmd/root_test.go`

- [ ] **Step 1: Write failing discover client tests**

Append these tests to `ae-cli/internal/client/client_test.go`:

```go
func TestDiscoverStart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tools/discover" {
			t.Fatalf("path = %s, want /api/v1/tools/discover", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "tool_call_required",
			"conversation_id": "conv-1",
			"tool_calls": []any{
				map[string]any{
					"id": "tc-1",
					"name": "check_command",
					"arguments": map[string]any{"command": "claude"},
				},
			},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	resp, err := c.DiscoverStart(context.Background(), DiscoverStartRequest{
		LocalContext: LocalContext{OS: "darwin", Arch: "arm64"},
	})
	if err != nil {
		t.Fatalf("DiscoverStart(): %v", err)
	}
	if resp.Status != "tool_call_required" {
		t.Fatalf("status = %q, want tool_call_required", resp.Status)
	}
}

func TestDiscoverContinue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tools/discover/continue" {
			t.Fatalf("path = %s, want /api/v1/tools/discover/continue", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "complete",
			"conversation_id": "conv-1",
			"discovered_tools": []any{},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	resp, err := c.DiscoverContinue(context.Background(), DiscoverContinueRequest{
		ConversationID: "conv-1",
		ToolResults: []ToolResult{{ToolCallID: "tc-1", Result: "ok"}},
	})
	if err != nil {
		t.Fatalf("DiscoverContinue(): %v", err)
	}
	if resp.Status != "complete" {
		t.Fatalf("status = %q, want complete", resp.Status)
	}
}
```

- [ ] **Step 2: Implement discover API methods in the client**

Add these types and methods to `ae-cli/internal/client/client.go`:

```go
var ErrBootstrapUnsupported = errors.New("bootstrap unsupported")

type LocalContext struct {
	OS       string   `json:"os"`
	Arch     string   `json:"arch"`
	HomeDir  string   `json:"home_dir"`
	PathDirs []string `json:"path_dirs"`
}

type DiscoverStartRequest struct {
	LocalContext LocalContext `json:"local_context"`
}

type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Result     string `json:"result"`
}

type DiscoverContinueRequest struct {
	ConversationID string       `json:"conversation_id"`
	ToolResults    []ToolResult `json:"tool_results"`
}

type DiscoverToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type DiscoverConfigAction struct {
	Type          string            `json:"type"`
	Path          string            `json:"path,omitempty"`
	File          string            `json:"file,omitempty"`
	MergeKeys     map[string]any    `json:"merge_keys,omitempty"`
	Vars          map[string]string `json:"vars,omitempty"`
	SourceProfile string            `json:"source_profile,omitempty"`
}

type DiscoverTool struct {
	Name          string                 `json:"name"`
	DisplayName   string                 `json:"display_name"`
	Version       string                 `json:"version,omitempty"`
	Path          string                 `json:"path"`
	ProviderName  string                 `json:"provider_name"`
	RelayAPIKeyID int64                  `json:"relay_api_key_id"`
	ConfigPath    string                 `json:"config_path,omitempty"`
	ConfigActions []DiscoverConfigAction `json:"config_actions"`
}

type DiscoverResponse struct {
	Status          string             `json:"status"`
	ConversationID  string             `json:"conversation_id,omitempty"`
	ToolCalls       []DiscoverToolCall `json:"tool_calls,omitempty"`
	DiscoveredTools []DiscoverTool     `json:"discovered_tools,omitempty"`
	Error           string             `json:"error,omitempty"`
}

type BootstrapSessionRequest struct {
	RepoFullName  string                   `json:"repo_full_name"`
	Branch        string                   `json:"branch"`
	WorkspaceID   string                   `json:"workspace_id"`
	WorkspaceRoot string                   `json:"workspace_root"`
	ToolConfigs   []map[string]interface{} `json:"tool_configs,omitempty"`
}

func (c *Client) DiscoverStart(ctx context.Context, req DiscoverStartRequest) (*DiscoverResponse, error) {
	return c.postDiscover(ctx, "/api/v1/tools/discover", req)
}

func (c *Client) DiscoverContinue(ctx context.Context, req DiscoverContinueRequest) (*DiscoverResponse, error) {
	return c.postDiscover(ctx, "/api/v1/tools/discover/continue", req)
}

func (c *Client) postDiscover(ctx context.Context, path string, payload any) (*DiscoverResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal discover request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create discover request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send discover request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("discover status %d: %s", resp.StatusCode, string(raw))
	}

	var decoded DiscoverResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode discover response: %w", err)
	}
	return &decoded, nil
}

func (c *Client) BootstrapSession(ctx context.Context, req BootstrapSessionRequest) (*Session, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal bootstrap request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/sessions/bootstrap", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create bootstrap request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send bootstrap request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNotImplemented {
		return nil, ErrBootstrapUnsupported
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("bootstrap status %d: %s", resp.StatusCode, string(raw))
	}

	var envelope struct {
		Data Session `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode bootstrap response: %w", err)
	}
	return &envelope.Data, nil
}
```

- [ ] **Step 3: Write failing tests for root URL resolution and session metadata handoff**

Alignment note:
- Current code still calls `CreateSession`.
- Latest design introduces `POST /api/v1/sessions/bootstrap`, workspace marker `/.ae/session.json`, and runtime bundle state.
- Implement discover/session integration behind one helper that always attempts `POST /api/v1/sessions/bootstrap` first and falls back to the legacy `CreateSession` path only when the backend explicitly returns `404` or `501`.

Add these tests:

In `ae-cli/cmd/root_test.go`:

```go
func TestResolveServerURLPrefersFlagThenTokenThenConfig(t *testing.T) {
	token := &auth.TokenFile{ServerURL: "https://token.example"}
	if got := resolveServerURL("https://flag.example", token, "https://config.example"); got != "https://flag.example" {
		t.Fatalf("got %q, want flag server", got)
	}
	if got := resolveServerURL("", token, "https://config.example"); got != "https://token.example" {
		t.Fatalf("got %q, want token server", got)
	}
	if got := resolveServerURL("", nil, "https://config.example"); got != "https://config.example" {
		t.Fatalf("got %q, want config server", got)
	}
}
```

In `ae-cli/internal/session/session_test.go`:

```go
func TestStartPassesDiscoveredMetadataToRemoteSessionStart(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	cachePath := filepath.Join(tmpHome, ".ae-cli", "discovered_tools.json")
	cache := map[string]any{
		"discovered_at": time.Now().UTC(),
		"tools": []map[string]any{
			{
				"name": "claude",
				"display_name": "Claude Code",
				"provider_name": "sub2api-claude",
				"relay_api_key_id": 123,
				"config_path": filepath.Join(tmpHome, ".claude", "settings.json"),
			},
		},
	}
	data, _ := json.Marshal(cache)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	var captured []map[string]interface{}
	mgr := NewManager(client.New("http://example.test", "tok"), &config.Config{})
	mgr.startRemoteSession = func(ctx context.Context, sessionID, repo, branch string, toolConfigs []map[string]interface{}) (*client.Session, error) {
		captured = toolConfigs
		return &client.Session{
			ID:        sessionID,
			Status:    "active",
			StartedAt: time.Now().UTC(),
		}, nil
	}

	if _, err := mgr.Start(); err != nil {
		t.Fatalf("Start(): %v", err)
	}
	if len(captured) != 1 {
		t.Fatalf("tool_configs = %d, want 1", len(captured))
	}
}

func TestStartRemoteSessionCompatPrefersBootstrapAndFallsBack(t *testing.T) {
	c := client.New("http://example.test", "tok")
	mgr := NewManager(c, &config.Config{})

	bootstrapCalled := false
	createCalled := false

	c.BootstrapSessionFunc = func(ctx context.Context, req client.BootstrapSessionRequest) (*client.Session, error) {
		bootstrapCalled = true
		return nil, client.ErrBootstrapUnsupported
	}
	c.CreateSessionFunc = func(ctx context.Context, req client.CreateSessionRequest) (*client.Session, error) {
		createCalled = true
		return &client.Session{ID: req.ID, Status: "active", StartedAt: time.Now().UTC()}, nil
	}

	_, err := mgr.startRemoteSessionCompat(context.Background(), "sess-1", "org/repo", "main", []map[string]interface{}{
		{"tool_name": "claude", "provider_name": "sub2api-claude", "relay_api_key_id": 123},
	})
	if err != nil {
		t.Fatalf("startRemoteSessionCompat(): %v", err)
	}
	if !bootstrapCalled {
		t.Fatal("expected bootstrap to be attempted first")
	}
	if !createCalled {
		t.Fatal("expected create-session fallback after unsupported bootstrap")
	}
}
```

- [ ] **Step 4: Implement root URL resolution and discover cache reporting**

Update `ae-cli/cmd/root.go`:

```go
func resolveServerURL(flagValue string, token *auth.TokenFile, configURL string) string {
	if flagValue != "" {
		return flagValue
	}
	if token != nil && token.ServerURL != "" {
		return token.ServerURL
	}
	return configURL
}

var rootCmd = &cobra.Command{
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "version" || cmd.Name() == "login" {
			return nil
		}

		var err error
		cfg, err = config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		if serverURL != "" {
			cfg.Server.URL = serverURL
		}
		tokenPath, err := auth.DefaultTokenPath()
		if err != nil {
			return fmt.Errorf("default token path: %w", err)
		}
		tokenFile, _ := auth.ReadToken(tokenPath)
		token := resolveToken(cfg.Server.Token, tokenPath)
		apiClient = client.New(resolveServerURL(serverURL, tokenFile, cfg.Server.URL), token)
		return nil
	},
}
```

Update `ae-cli/internal/session/manager.go` so `Start()` reads the discover cache before session creation/bootstrap and routes the metadata through one helper:

```go
type remoteSessionStarter func(ctx context.Context, sessionID, repo, branch string, toolConfigs []map[string]interface{}) (*client.Session, error)

type Manager struct {
	client *client.Client
	config *config.Config
	startRemoteSession remoteSessionStarter
}

func NewManager(c *client.Client, cfg *config.Config) *Manager {
	m := &Manager{
		client: c,
		config: cfg,
	}
	m.startRemoteSession = m.startRemoteSessionCompat
	return m
}

func (m *Manager) Start() (*State, error) {
	toolConfigs, err := loadDiscoverMetadataForSession()
	if err != nil {
		return nil, fmt.Errorf("loading discover cache: %w", err)
	}

	sess, err := m.startRemoteSession(context.Background(), sessionID, repo, branch, toolConfigs)
}

func loadDiscoverMetadataForSession() ([]map[string]interface{}, error) {
	path, err := discover.CachePath()
	if err != nil {
		return nil, err
	}
	cache, err := discover.ReadCache(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	configs := make([]map[string]interface{}, 0, len(cache.Tools))
	for _, tool := range cache.Tools {
		configs = append(configs, map[string]interface{}{
			"tool_name":       tool.Name,
			"provider_name":   tool.ProviderName,
			"relay_api_key_id": tool.RelayAPIKeyID,
		})
	}
	return configs, nil
}

func (m *Manager) startRemoteSessionCompat(ctx context.Context, sessionID, repo, branch string, toolConfigs []map[string]interface{}) (*client.Session, error) {
	workspaceRoot, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	workspaceID := deriveWorkspaceID(workspaceRoot)

	sess, err := m.client.BootstrapSession(ctx, client.BootstrapSessionRequest{
		RepoFullName:  repo,
		Branch:        branch,
		WorkspaceID:   workspaceID,
		WorkspaceRoot: workspaceRoot,
		ToolConfigs:   toolConfigs,
	})
	if err == nil {
		return sess, nil
	}
	if !errors.Is(err, client.ErrBootstrapUnsupported) {
		return nil, err
	}

	// Compatibility fallback until bootstrap is fully available on the backend.
	return m.client.CreateSession(ctx, client.CreateSessionRequest{
		ID:           sessionID,
		RepoFullName: repo,
		Branch:       branch,
		ToolConfigs:  toolConfigs,
	})
}
```

- [ ] **Step 5: Write failing tests for the end-to-end discover loop**

Create `ae-cli/internal/discover/run_test.go`:

```go
package discover

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

func TestRunCompletesMultiRoundDiscoverAndWritesCache(t *testing.T) {
	home := t.TempDir()
	var calls int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/tools/discover":
			calls++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "tool_call_required",
				"conversation_id": "conv-1",
				"tool_calls": []any{
					map[string]any{
						"id": "tc-1",
						"name": "check_command",
						"arguments": map[string]any{"command": "claude"},
					},
				},
			})
		case "/api/v1/tools/discover/continue":
			calls++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "complete",
				"conversation_id": "conv-1",
				"discovered_tools": []any{
					map[string]any{
						"name": "claude",
						"display_name": "Claude Code",
						"path": "/opt/homebrew/bin/claude",
						"provider_name": "sub2api-claude",
						"relay_api_key_id": 123,
						"config_path": filepath.Join(home, ".claude", "settings.json"),
						"config_actions": []any{},
					},
				},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	runner := NewRunner(client.New(srv.URL, "tok"), &Executor{homeDir: home}, RunOptions{DryRun: false, Yes: true, HomeDir: home})
	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run(): %v", err)
	}
	if len(result.Tools) != 1 {
		t.Fatalf("tools = %d, want 1", len(result.Tools))
	}
	if calls != 2 {
		t.Fatalf("http calls = %d, want 2", calls)
	}
	if _, err := os.Stat(CachePathFromHome(home)); err != nil {
		t.Fatalf("expected cache file: %v", err)
	}
}
```

- [ ] **Step 6: Implement discover runner, command, and login trigger**

Create `ae-cli/internal/discover/run.go`:

```go
package discover

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

const (
	maxRounds       = 20
)

type RunOptions struct {
	DryRun bool
	Yes    bool
	HomeDir string
}

type Result struct {
	Tools []DiscoveredTool
}

type Runner struct {
	client   *client.Client
	exec     *Executor
	opts     RunOptions
}

func NewRunner(c *client.Client, exec *Executor, opts RunOptions) *Runner {
	return &Runner{client: c, exec: exec, opts: opts}
}

func (r *Runner) Run(ctx context.Context) (*Result, error) {
	startResp, err := r.client.DiscoverStart(ctx, client.DiscoverStartRequest{
		LocalContext: client.LocalContext{
			OS: runtime.GOOS,
			Arch: runtime.GOARCH,
			HomeDir: r.opts.HomeDir,
			PathDirs: filepath.SplitList(os.Getenv("PATH")),
		},
	})
	if err != nil {
		return nil, err
	}

	resp := startResp
	for round := 0; round < maxRounds; round++ {
		switch resp.Status {
		case "complete":
			discovered := mapTools(resp.DiscoveredTools)
			if err := r.persist(discovered); err != nil {
				return nil, err
			}
			return &Result{Tools: discovered}, nil
		case "error":
			return nil, fmt.Errorf(resp.Error)
		case "tool_call_required":
			results := make([]client.ToolResult, 0, len(resp.ToolCalls))
			for _, call := range resp.ToolCalls {
				out, err := r.exec.Execute(ToolCall{
					ID: call.ID,
					Name: call.Name,
					Arguments: call.Arguments,
				})
				if err != nil {
					out = "error: " + err.Error()
				}
				results = append(results, client.ToolResult{
					ToolCallID: call.ID,
					Result: out,
				})
			}
			resp, err = r.client.DiscoverContinue(ctx, client.DiscoverContinueRequest{
				ConversationID: resp.ConversationID,
				ToolResults: results,
			})
			if err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unknown discover status %q", resp.Status)
		}
	}
	return nil, fmt.Errorf("maximum discover rounds exceeded")
}

func (r *Runner) persist(tools []DiscoveredTool) error {
	cache := CacheFile{DiscoveredAt: time.Now().UTC()}
	for _, tool := range tools {
		if !r.opts.DryRun && !r.opts.Yes {
			ok, err := promptConfirm(tool)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
		}

		succeeded := true
		for _, action := range tool.ConfigActions {
			if err := ExecuteAction(action, r.opts.DryRun); err != nil {
				fmt.Printf("warning: failed to apply %s for %s: %v\n", action.Type, tool.Name, err)
				succeeded = false
				break
			}
		}
		if !succeeded {
			continue
		}

		cache.Tools = append(cache.Tools, CachedTool{
			Name:          tool.Name,
			DisplayName:   tool.DisplayName,
			ProviderName:  tool.ProviderName,
			RelayAPIKeyID: tool.RelayAPIKeyID,
			ConfigPath:    tool.ConfigPath,
		})
	}

	if r.opts.DryRun {
		return nil
	}
	return WriteCache(CachePathFromHome(r.opts.HomeDir), cache)
}

func mapTools(in []client.DiscoverTool) []DiscoveredTool {
	out := make([]DiscoveredTool, 0, len(in))
	for _, tool := range in {
		actions := make([]ConfigAction, 0, len(tool.ConfigActions))
		for _, action := range tool.ConfigActions {
			actions = append(actions, ConfigAction{
				Type:          action.Type,
				Path:          action.Path,
				File:          action.File,
				MergeKeys:     action.MergeKeys,
				Vars:          action.Vars,
				SourceProfile: action.SourceProfile,
			})
		}
		out = append(out, DiscoveredTool{
			Name:          tool.Name,
			DisplayName:   tool.DisplayName,
			Version:       tool.Version,
			Path:          tool.Path,
			ProviderName:  tool.ProviderName,
			RelayAPIKeyID: tool.RelayAPIKeyID,
			ConfigPath:    tool.ConfigPath,
			ConfigActions: actions,
		})
	}
	return out
}

func promptConfirm(tool DiscoveredTool) (bool, error) {
	fmt.Printf("Apply configuration for %s (%s)? [y/N]: ", tool.DisplayName, tool.Name)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false, err
	}
	answer := strings.TrimSpace(strings.ToLower(line))
	return answer == "y" || answer == "yes", nil
}
```

Create `ae-cli/cmd/discover.go`:

```go
package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	discoverpkg "github.com/ai-efficiency/ae-cli/internal/discover"
	"github.com/spf13/cobra"
)

var (
	discoverDryRun bool
	discoverYes    bool
)

var discoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Discover and configure local AI tools",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		runner := discoverpkg.NewRunner(
			apiClient,
			discoverpkg.NewExecutor(home, filepath.SplitList(os.Getenv("PATH"))),
			discoverpkg.RunOptions{DryRun: discoverDryRun, Yes: discoverYes, HomeDir: home},
		)
		result, err := runner.Run(context.Background())
		if err != nil {
			return err
		}
		fmt.Printf("Discovered %d tools\n", len(result.Tools))
		return nil
	},
}

func init() {
	discoverCmd.Flags().BoolVar(&discoverDryRun, "dry-run", false, "show changes without writing files")
	discoverCmd.Flags().BoolVar(&discoverYes, "yes", false, "skip confirmation prompts")
	rootCmd.AddCommand(discoverCmd)
}
```

Update `ae-cli/cmd/login.go` after successful token write:

```go
		apiClient := client.New(serverURL, token.AccessToken)
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve home dir: %w", err)
		}
		runner := discover.NewRunner(
			apiClient,
			discover.NewExecutor(home, filepath.SplitList(os.Getenv("PATH"))),
			discover.RunOptions{DryRun: false, Yes: false, HomeDir: home},
		)
		if _, err := runner.Run(context.Background()); err != nil {
			fmt.Printf("warning: smart tool discovery failed: %v\n", err)
		}
```

- [ ] **Step 7: Run focused CLI discover tests**

Run: `cd ae-cli && go test ./internal/client/... ./internal/discover/... ./internal/session/... ./cmd/... -run 'TestDiscover|TestResolveServerURL|TestStartPassesDiscoveredMetadataToRemoteSessionStart|TestStartRemoteSessionCompatPrefersBootstrapAndFallsBack|TestRunCompletesMultiRoundDiscover' -v`
Expected: PASS

- [ ] **Step 8: Run full ae-cli build and test verification**

Run: `cd ae-cli && go build ./...`
Expected: PASS

Run: `cd ae-cli && go test ./internal/client/... ./internal/discover/... ./internal/session/... ./internal/auth/...`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add ae-cli/internal/client/client.go ae-cli/internal/client/client_test.go ae-cli/internal/discover ae-cli/internal/session/manager.go ae-cli/internal/session/session_test.go ae-cli/cmd/root.go ae-cli/cmd/root_test.go ae-cli/cmd/login.go ae-cli/cmd/discover.go
git commit -m "feat(ae-cli): add discover command, login trigger, and session tool reporting"
```

---

### Task 5: Full Discover Verification And Contract Lock

- [ ] **Step 1: Run focused backend discover verification**

Run: `cd backend && go test ./internal/relay/... ./internal/discover/... ./internal/handler/... -run 'TestChatMessageJSON|TestChatCompletionWithToolsReturnsToolCalls|TestConversationStore|TestToolDefinitions|TestHydrateDiscoveredTools|TestDiscover' -v`
Expected: PASS

- [ ] **Step 2: Run focused ae-cli discover verification**

Run: `cd ae-cli && go test ./internal/client/... ./internal/discover/... ./internal/session/... ./cmd/... -run 'TestDiscover|TestResolveServerURL|TestStartPassesDiscoveredMetadataToRemoteSessionStart|TestStartRemoteSessionCompatPrefersBootstrapAndFallsBack|TestRunCompletesMultiRoundDiscover' -v`
Expected: PASS

- [ ] **Step 3: Run end-to-end package builds**

Run: `cd backend && go build ./cmd/server`
Expected: PASS

Run: `cd ae-cli && go build ./...`
Expected: PASS

- [ ] **Step 4: Re-read the spec and confirm contract lock**

Manual checklist:
- `discover` and `discover/continue` use the multi-round `status` protocol only
- backend prompt excludes provider secrets
- `complete` responses include `provider_name` and `relay_api_key_id`
- `write_yaml` is implemented and tested
- discover metadata is routed through the active session lifecycle, preferring bootstrap/marker flow when available

Expected: all five checks map directly to the implemented tasks above.

- [ ] **Step 5: Commit**

```bash
git add docs/superpowers/plans/2026-03-26-ae-cli-smart-tool-discovery-executable.md
git commit -m "docs(plans): add executable smart tool discovery implementation plan"
```

---

## Self-Review

- **Spec coverage:** This plan covers the backend multi-round protocol, provider hydration, local executor, config writes including YAML, cache persistence, login auto-trigger, and session reporting.
- **Placeholder scan:** The old draft's placeholders (`same pattern`, `write according to actual structure`, deferred `write_yaml`) are intentionally removed.
- **Type consistency:** The plan uses `provider_name` and `relay_api_key_id` consistently across backend responses, cache storage, and session reporting.
