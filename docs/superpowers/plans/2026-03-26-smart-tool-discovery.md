# ae-cli 智能工具发现与自动配置 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 登录后通过后端 LLM 多轮 tool_call 协议自动发现本地 AI 工具，自动写入各工具原生配置，实现零手动配置。

**Architecture:** 后端新增 discover handler 管理多轮 LLM 对话状态（内存 conversation store + 5 分钟过期），通过 relay.Provider 调用 LLM 并注入用户 provider 列表。ae-cli 新增 discover 包实现本地 tool_call 执行器（白名单约束）、config action 写入器、discovered_tools 缓存。扩展 relay.ChatMessage 支持 tool role 以实现多轮 tool_call。

**Tech Stack:** Go 1.26+ (Gin, Ent, relay.Provider), ae-cli (Cobra, JSON/YAML merge, os/exec)

**Spec:** `docs/superpowers/specs/2026-03-24-ae-cli-smart-tool-discovery-design.md`

**Status:** ⚠️ 已被替代（2026-03-26）

**Superseded By:** `docs/superpowers/plans/2026-03-26-ae-cli-smart-tool-discovery-executable.md`

**Replay Status:** 不要继续在本文上补 action 或修冲突。若要实施 smart tool discovery，请使用替代计划。

**Source Of Truth:** 本文仍保留历史 draft 中的占位和冲突，仅作为问题上下文；当前代码基线与最新 spec 的对齐以替代计划为准。

**延迟项：**
- **config.go 简化**（spec Section 5）：移除 `ToolConfig`、`Sub2apiConfig`、`ServerConfig` 延迟到本功能完成后单独处理。原因：当前 `ae-cli/config/config.go` 被 `session/manager.go`、`shell/shell.go`、`router/router.go` 等多处引用，移除需要同步修改所有消费者。本计划聚焦工具发现核心功能，config 简化作为后续清理任务。

---

## File Structure

### 后端新增/修改

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `backend/internal/relay/types.go` | 扩展 ChatMessage 支持 tool_calls、tool_call_id 字段 |
| 修改 | `backend/internal/relay/sub2api.go` | 序列化扩展后的 ChatMessage（含 tool role） |
| 创建 | `backend/internal/discover/conversation.go` | 内存 conversation store（UUID → message history，5 分钟过期） |
| 创建 | `backend/internal/discover/tools.go` | LLM tool 定义（check_command、read_file、list_dir） |
| 创建 | `backend/internal/discover/prompt.go` | System prompt 模板 + provider 注入 |
| 创建 | `backend/internal/handler/discover.go` | POST /api/v1/tools/discover + /continue 端点 |
| 修改 | `backend/internal/handler/router.go` | 注册 discover 路由 |
| 修改 | `backend/cmd/server/main.go` | 注入 DiscoverHandler 依赖 |

### ae-cli 新增/修改

| 操作 | 文件 | 职责 |
|------|------|------|
| 创建 | `ae-cli/internal/discover/discover.go` | 工具发现主流程（多轮 tool_call 循环） |
| 创建 | `ae-cli/internal/discover/executor.go` | 本地 tool_call 执行器（check_command、read_file、list_dir + 白名单） |
| 创建 | `ae-cli/internal/discover/actions.go` | config_action 执行器（write_json、write_yaml、set_env） |
| 创建 | `ae-cli/internal/discover/cache.go` | discovered_tools.json 缓存管理 |
| 创建 | `ae-cli/cmd/discover.go` | `ae-cli discover` 命令（--dry-run、--refresh） |
| 修改 | `ae-cli/cmd/login.go` | 登录成功后自动触发工具发现 |
| 修改 | `ae-cli/internal/client/client.go` | 新增 Discover / DiscoverContinue API 方法 |
| 修改 | `ae-cli/internal/session/manager.go` | Start() 读取 discovered_tools 缓存上报 tool_configs |

### 测试文件

| 文件 | 覆盖 |
|------|------|
| `backend/internal/relay/types_test.go` | ChatMessage 序列化 |
| `backend/internal/discover/conversation_test.go` | conversation store CRUD + 过期 |
| `backend/internal/handler/discover_test.go` | discover 端点集成测试 |
| `ae-cli/internal/discover/executor_test.go` | 白名单验证、命令执行 |
| `ae-cli/internal/discover/actions_test.go` | JSON merge、YAML merge、set_env |
| `ae-cli/internal/discover/cache_test.go` | 缓存读写、过期检查 |
| `ae-cli/internal/discover/discover_test.go` | 多轮循环集成测试 |

---

## Task 1: 扩展 relay.ChatMessage 支持多轮 tool_call

**Files:**
- Modify: `backend/internal/relay/types.go`
- Modify: `backend/internal/relay/sub2api.go`
- Create: `backend/internal/relay/types_test.go`

当前 `ChatMessage` 只有 `Role` + `Content`，不支持 OpenAI 多轮 tool_call 协议所需的 `tool_calls`（assistant 消息）和 `tool_call_id`（tool 消息）字段。

- [ ] **Step 1: 写测试 — ChatMessage 序列化**

`backend/internal/relay/types_test.go`:
```go
package relay

import (
	"encoding/json"
	"testing"
)

func TestChatMessageJSON_AssistantWithToolCalls(t *testing.T) {
	msg := ChatMessage{
		Role: "assistant",
		ToolCalls: []ToolCall{
			{
				ID:   "tc-1",
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "check_command", Arguments: `{"command":"claude"}`},
			},
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)

	if decoded["role"] != "assistant" {
		t.Errorf("role = %v, want assistant", decoded["role"])
	}
	tcs, ok := decoded["tool_calls"].([]interface{})
	if !ok || len(tcs) != 1 {
		t.Fatalf("tool_calls missing or wrong length")
	}
	// content should be omitted when empty
	if _, exists := decoded["content"]; exists {
		t.Error("empty content should be omitted")
	}
}

func TestChatMessageJSON_ToolResult(t *testing.T) {
	msg := ChatMessage{
		Role:       "tool",
		Content:    "claude found at /opt/homebrew/bin/claude",
		ToolCallID: "tc-1",
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)

	if decoded["role"] != "tool" {
		t.Errorf("role = %v, want tool", decoded["role"])
	}
	if decoded["tool_call_id"] != "tc-1" {
		t.Errorf("tool_call_id = %v, want tc-1", decoded["tool_call_id"])
	}
	if decoded["content"] != "claude found at /opt/homebrew/bin/claude" {
		t.Errorf("content mismatch")
	}
}

func TestChatMessageJSON_RegularMessage(t *testing.T) {
	msg := ChatMessage{Role: "user", Content: "hello"}
	data, _ := json.Marshal(msg)
	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)

	if _, exists := decoded["tool_calls"]; exists {
		t.Error("tool_calls should be omitted for regular message")
	}
	if _, exists := decoded["tool_call_id"]; exists {
		t.Error("tool_call_id should be omitted for regular message")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd backend && go test ./internal/relay/... -run TestChatMessageJSON -v`
Expected: FAIL（ChatMessage 缺少 ToolCalls/ToolCallID 字段）

- [ ] **Step 3: 扩展 ChatMessage 结构体**

修改 `backend/internal/relay/types.go`，将：
```go
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
```
改为：
```go
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}
```

> **注意（W1 修复）：** Content 保留 `json:"content"` 不加 omitempty，避免破坏现有 user/system 消息的序列化。assistant 消息即使 content 为空字符串也会序列化为 `"content":""` ，这符合 OpenAI API 规范。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd backend && go test ./internal/relay/... -run TestChatMessageJSON -v`
Expected: PASS（3/3）

- [ ] **Step 5: 运行全量 relay 测试确认无回归**

Run: `cd backend && go test ./internal/relay/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add backend/internal/relay/types.go backend/internal/relay/types_test.go
git commit -m "feat(backend): extend ChatMessage to support multi-turn tool_call protocol"
```

---

## Task 2: 后端 conversation store（内存多轮对话管理）

**Files:**
- Create: `backend/internal/discover/conversation.go`
- Create: `backend/internal/discover/conversation_test.go`

管理多轮 LLM tool_call 对话状态。每个 conversation 有 UUID、message history、5 分钟过期。

- [ ] **Step 1: 写测试**

`backend/internal/discover/conversation_test.go`:
```go
package discover

import (
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
)

func TestConversationStore_CreateAndGet(t *testing.T) {
	store := NewConversationStore(5 * time.Minute)
	id := store.Create([]relay.ChatMessage{
		{Role: "system", Content: "you are a tool discovery assistant"},
	})
	if id == "" {
		t.Fatal("expected non-empty conversation ID")
	}

	conv, ok := store.Get(id)
	if !ok {
		t.Fatal("expected conversation to exist")
	}
	if len(conv.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(conv.Messages))
	}
}

func TestConversationStore_Append(t *testing.T) {
	store := NewConversationStore(5 * time.Minute)
	id := store.Create([]relay.ChatMessage{
		{Role: "system", Content: "hello"},
	})

	store.Append(id, relay.ChatMessage{Role: "user", Content: "discover tools"})
	conv, _ := store.Get(id)
	if len(conv.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(conv.Messages))
	}
}

func TestConversationStore_Delete(t *testing.T) {
	store := NewConversationStore(5 * time.Minute)
	id := store.Create(nil)
	store.Delete(id)
	_, ok := store.Get(id)
	if ok {
		t.Error("expected conversation to be deleted")
	}
}

func TestConversationStore_Expiry(t *testing.T) {
	store := NewConversationStore(1 * time.Millisecond)
	id := store.Create(nil)
	time.Sleep(5 * time.Millisecond)
	store.Cleanup()
	_, ok := store.Get(id)
	if ok {
		t.Error("expected conversation to be expired")
	}
}

func TestConversationStore_GetNotFound(t *testing.T) {
	store := NewConversationStore(5 * time.Minute)
	_, ok := store.Get("nonexistent")
	if ok {
		t.Error("expected not found")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd backend && go test ./internal/discover/... -run TestConversationStore -v`
Expected: FAIL（包不存在）

- [ ] **Step 3: 实现 conversation store**

`backend/internal/discover/conversation.go`:
```go
package discover

import (
	"sync"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/google/uuid"
)

// Conversation holds the state of a multi-turn tool discovery session.
type Conversation struct {
	Messages  []relay.ChatMessage
	CreatedAt time.Time
}

// ConversationStore manages in-memory conversations with TTL-based expiry.
type ConversationStore struct {
	mu    sync.Mutex
	convs map[string]*Conversation
	ttl   time.Duration
}

// NewConversationStore creates a store with the given TTL for conversations.
func NewConversationStore(ttl time.Duration) *ConversationStore {
	return &ConversationStore{
		convs: make(map[string]*Conversation),
		ttl:   ttl,
	}
}

// Create starts a new conversation with initial messages and returns its ID.
func (s *ConversationStore) Create(messages []relay.ChatMessage) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := uuid.New().String()
	s.convs[id] = &Conversation{
		Messages:  messages,
		CreatedAt: time.Now(),
	}
	return id
}

// Get retrieves a conversation by ID. Returns a deep copy. Returns false if not found or expired.
func (s *ConversationStore) Get(id string) (*Conversation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	conv, ok := s.convs[id]
	if !ok {
		return nil, false
	}
	if time.Since(conv.CreatedAt) > s.ttl {
		delete(s.convs, id)
		return nil, false
	}
	// Return deep copy to avoid concurrent mutation (W8)
	msgs := make([]relay.ChatMessage, len(conv.Messages))
	copy(msgs, conv.Messages)
	return &Conversation{Messages: msgs, CreatedAt: conv.CreatedAt}, true
}

// Append adds a message to an existing conversation.
func (s *ConversationStore) Append(id string, msg relay.ChatMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if conv, ok := s.convs[id]; ok {
		conv.Messages = append(conv.Messages, msg)
	}
}

// Delete removes a conversation.
func (s *ConversationStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.convs, id)
}

// Cleanup removes all expired conversations.
func (s *ConversationStore) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, conv := range s.convs {
		if now.Sub(conv.CreatedAt) > s.ttl {
			delete(s.convs, id)
		}
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd backend && go test ./internal/discover/... -run TestConversationStore -v`
Expected: PASS（5/5）

- [ ] **Step 5: Commit**

```bash
git add backend/internal/discover/conversation.go backend/internal/discover/conversation_test.go
git commit -m "feat(backend): add in-memory conversation store for tool discovery"
```

---

## Task 3: 后端 LLM tool 定义 + system prompt

**Files:**
- Create: `backend/internal/discover/tools.go`
- Create: `backend/internal/discover/prompt.go`

定义 LLM 可用的三个 tool（check_command、read_file、list_dir）和 system prompt 模板。

- [ ] **Step 1: 创建 tool 定义（直接使用 relay.ToolDef 类型）**

`backend/internal/discover/tools.go`:
```go
package discover

import (
	"encoding/json"

	"github.com/ai-efficiency/backend/internal/relay"
)

// ToolDefs returns the typed tool definitions for the discovery LLM.
func ToolDefs() []relay.ToolDef {
	return []relay.ToolDef{
		{
			Type: "function",
			Function: relay.ToolFuncDef{
				Name:        "check_command",
				Description: "Check if a command exists on the local machine and get its version. Only allowed commands: claude, codex, cursor, aider, continue, copilot, cody.",
				Parameters: mustJSON(map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"command": map[string]interface{}{
							"type":        "string",
							"description": "The command name to check, e.g. 'claude'",
						},
					},
					"required": []string{"command"},
				}),
			},
		},
		{
			Type: "function",
			Function: relay.ToolFuncDef{
				Name:        "read_file",
				Description: "Read the contents of a file on the local machine. Only allowed paths under user's home directory config locations.",
				Parameters: mustJSON(map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Absolute path to the file to read",
						},
					},
					"required": []string{"path"},
				}),
			},
		},
		{
			Type: "function",
			Function: relay.ToolFuncDef{
				Name:        "list_dir",
				Description: "List the contents of a directory (depth 1, max 100 entries). Only allowed directories under user's home.",
				Parameters: mustJSON(map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Absolute path to the directory to list",
						},
					},
					"required": []string{"path"},
				}),
			},
		},
	}
}

func mustJSON(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
```

- [ ] **Step 2: 创建 system prompt 模板**

`backend/internal/discover/prompt.go`:
```go
package discover

import (
	"fmt"
	"strings"
)

// ProviderInfo holds the provider details injected into the LLM prompt.
type ProviderInfo struct {
	Name         string
	DisplayName  string
	BaseURL      string
	APIKey       string
	APIKeyID     int64
	DefaultModel string
	IsPrimary    bool
}

// BuildSystemPrompt generates the system prompt with provider info injected.
func BuildSystemPrompt(providers []ProviderInfo) string {
	var sb strings.Builder
	sb.WriteString(`你是 ae-cli 的工具发现助手。你的任务是：
1. 检测用户本地安装了哪些 AI 编程工具（如 Claude Code、Codex CLI、Cursor 等）
2. 读取各工具的现有配置文件，了解当前配置状态
3. 根据提供的 provider 列表，为每个工具生成配置写入方案

检测策略：
- 使用 check_command 检查常见工具命令：claude, codex, cursor, aider, continue, copilot, cody
- 使用 read_file 读取已知配置路径（如 ~/.claude/settings.json）
- 使用 list_dir 探索配置目录

配置写入规则：
- 优先使用工具的原生配置方式（配置文件 > 环境变量）
- 不覆盖用户已有的非 ae-cli 配置
- 使用 merge 而非 overwrite（保留用户其他配置项）
- 默认使用 is_primary=true 的 provider

已知工具配置路径：
| 工具 | 配置方式 | 路径 |
|------|---------|------|
| Claude Code | JSON 配置 | ~/.claude/settings.json 中的 env 字段 |
| Codex CLI | 环境变量 | OPENAI_BASE_URL + OPENAI_API_KEY |
| Cursor | JSON 配置 | ~/.cursor/config.json |
| Aider | YAML 配置 | ~/.aider.conf.yml |

`)

	sb.WriteString("可用的 Provider 列表：\n")
	for _, p := range providers {
		primary := ""
		if p.IsPrimary {
			primary = " [PRIMARY]"
		}
		sb.WriteString(fmt.Sprintf("- %s (%s)%s: base_url=%s, api_key=%s, api_key_id=%d, model=%s\n",
			p.Name, p.DisplayName, primary, p.BaseURL, p.APIKey, p.APIKeyID, p.DefaultModel))
	}

	sb.WriteString(`
完成检测后，输出 JSON 格式的 discovered_tools 数组。每个工具包含：
- name: 工具标识符（如 "claude", "codex"）
- display_name: 显示名称
- version: 版本号（如果检测到）
- path: 可执行文件路径
- provider_name: 使用的 provider 名称
- api_key_id: provider 的 API key ID（整数）
- config_actions: 配置写入方案数组

config_actions 支持的 type：
- write_json: 合并写入 JSON 文件。字段：path, merge_keys
- write_yaml: 合并写入 YAML 文件。字段：path, merge_keys
- set_env: 写入环境变量文件。字段：file（默认 ~/.ae-cli/env）, vars, source_profile

输出格式示例：
{"discovered_tools": [{"name": "claude", "display_name": "Claude Code", "version": "1.2.3", "path": "/opt/homebrew/bin/claude", "provider_name": "sub2api-claude", "api_key_id": 123, "config_actions": [{"type": "write_json", "path": "~/.claude/settings.json", "merge_keys": {"env": {"ANTHROPIC_BASE_URL": "...", "ANTHROPIC_API_KEY": "..."}}}]}]}
`)
	return sb.String()
}
```

- [ ] **Step 3: 运行编译检查**

Run: `cd backend && go build ./internal/discover/...`
Expected: 成功

- [ ] **Step 4: Commit**

```bash
git add backend/internal/discover/tools.go backend/internal/discover/prompt.go
git commit -m "feat(backend): add LLM tool definitions and system prompt for discovery"
```

---

## Task 4: 后端 discover handler（多轮 tool_call 端点）

**Files:**
- Create: `backend/internal/handler/discover.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/cmd/server/main.go`

实现 `POST /api/v1/tools/discover` 和 `POST /api/v1/tools/discover/continue` 两个端点。后端加载用户 provider 列表注入 LLM prompt，管理多轮对话状态，返回 tool_call 或最终结果。

- [ ] **Step 1: 创建 discover handler**

`backend/internal/handler/discover.go`:
```go
package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/discover"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const maxDiscoverRounds = 20

// DiscoverHandler handles tool discovery endpoints.
// 注意（C4）：discover 端点使用自定义 discoverResponse 结构而非 pkg.Success/pkg.Error 信封，
// 因为响应是 discriminated union（status 字段区分 tool_call_required/complete/error），
// ae-cli 客户端直接解码此结构。这是有意的设计偏差。
type DiscoverHandler struct {
	entClient       *ent.Client
	providerHandler *ProviderHandler
	convStore       *discover.ConversationStore
	logger          *zap.Logger
}

// NewDiscoverHandler creates a new discover handler.
func NewDiscoverHandler(
	entClient *ent.Client,
	providerHandler *ProviderHandler,
	convStore *discover.ConversationStore,
	logger *zap.Logger,
) *DiscoverHandler {
	return &DiscoverHandler{
		entClient:       entClient,
		providerHandler: providerHandler,
		convStore:       convStore,
		logger:          logger,
	}
}

type discoverRequest struct {
	LocalContext struct {
		OS       string   `json:"os"`
		Arch     string   `json:"arch"`
		HomeDir  string   `json:"home_dir"`
		PathDirs []string `json:"path_dirs"`
	} `json:"local_context"`
}

type discoverContinueRequest struct {
	ConversationID string `json:"conversation_id"`
	ToolResults    []struct {
		ToolCallID string `json:"tool_call_id"`
		Result     string `json:"result"`
	} `json:"tool_results"`
}

type discoverResponse struct {
	Status         string          `json:"status"`
	ConversationID string          `json:"conversation_id"`
	ToolCalls      []relay.ToolCall `json:"tool_calls,omitempty"`
	DiscoveredTools json.RawMessage `json:"discovered_tools,omitempty"`
	Error          string          `json:"error,omitempty"`
}

// Discover handles POST /api/v1/tools/discover
func (h *DiscoverHandler) Discover(c *gin.Context) {
	var req discoverRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid request")
		return
	}

	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "not authenticated")
		return
	}

	// Load user's providers (reuse ProviderHandler logic)
	providers, err := h.loadUserProviders(c, uc.UserID)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to load providers")
		return
	}
	if len(providers) == 0 {
		c.JSON(http.StatusOK, discoverResponse{
			Status: "error",
			Error:  "no providers configured for user",
		})
		return
	}

	// Build system prompt with provider info
	systemPrompt := discover.BuildSystemPrompt(providers)

	// Build user message with local context
	localCtxJSON, _ := json.Marshal(req.LocalContext)
	userMsg := "请检测我本地安装的 AI 编程工具并生成配置方案。\n\n本地环境信息：\n" + string(localCtxJSON)

	// Build tool definitions as relay.ToolDef
	toolDefs := discover.ToolDefs()

	// Create initial messages
	messages := []relay.ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userMsg},
	}

	// Get primary provider for LLM call
	primaryProvider := h.getPrimaryRelayProvider(c)
	if primaryProvider == nil {
		c.JSON(http.StatusOK, discoverResponse{
			Status: "error",
			Error:  "no relay provider available for LLM",
		})
		return
	}

	// Call LLM with tools
	resp, err := primaryProvider.ChatCompletionWithTools(c.Request.Context(), relay.ChatCompletionRequest{
		Messages: messages,
	}, toolDefs)
	if err != nil {
		h.logger.Error("discover LLM call failed", zap.Error(err))
		c.JSON(http.StatusOK, discoverResponse{
			Status: "error",
			Error:  "LLM service unavailable",
		})
		return
	}

	// Store conversation
	convID := h.convStore.Create(messages)

	// Append assistant response to conversation
	assistantMsg := relay.ChatMessage{
		Role:      "assistant",
		Content:   resp.Content,
		ToolCalls: resp.ToolCalls,
	}
	h.convStore.Append(convID, assistantMsg)

	h.sendDiscoverResponse(c, convID, resp)
}

// DiscoverContinue handles POST /api/v1/tools/discover/continue
func (h *DiscoverHandler) DiscoverContinue(c *gin.Context) {
	var req discoverContinueRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid request")
		return
	}

	conv, ok := h.convStore.Get(req.ConversationID)
	if !ok {
		c.JSON(http.StatusOK, discoverResponse{
			Status:         "error",
			ConversationID: req.ConversationID,
			Error:          "conversation not found or expired",
		})
		return
	}

	// Check max rounds (count assistant messages with tool_calls as rounds)
	roundCount := 0
	for _, m := range conv.Messages {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			roundCount++
		}
	}
	if roundCount >= maxDiscoverRounds {
		h.convStore.Delete(req.ConversationID)
		c.JSON(http.StatusOK, discoverResponse{
			Status:         "error",
			ConversationID: req.ConversationID,
			Error:          "max discovery rounds exceeded",
		})
		return
	}

	// Append tool results to conversation
	for _, tr := range req.ToolResults {
		h.convStore.Append(req.ConversationID, relay.ChatMessage{
			Role:       "tool",
			Content:    tr.Result,
			ToolCallID: tr.ToolCallID,
		})
	}

	// Reload conversation with new messages
	conv, _ = h.convStore.Get(req.ConversationID)

	// Get primary provider for LLM call
	primaryProvider := h.getPrimaryRelayProvider(c)
	if primaryProvider == nil {
		c.JSON(http.StatusOK, discoverResponse{
			Status:         "error",
			ConversationID: req.ConversationID,
			Error:          "no relay provider available",
		})
		return
	}

	toolDefs := discover.ToolDefs()

	resp, err := primaryProvider.ChatCompletionWithTools(c.Request.Context(), relay.ChatCompletionRequest{
		Messages: conv.Messages,
	}, toolDefs)
	if err != nil {
		h.logger.Error("discover continue LLM call failed", zap.Error(err))
		c.JSON(http.StatusOK, discoverResponse{
			Status:         "error",
			ConversationID: req.ConversationID,
			Error:          "LLM service unavailable",
		})
		return
	}

	// Append assistant response
	assistantMsg := relay.ChatMessage{
		Role:      "assistant",
		Content:   resp.Content,
		ToolCalls: resp.ToolCalls,
	}
	h.convStore.Append(req.ConversationID, assistantMsg)

	h.sendDiscoverResponse(c, req.ConversationID, resp)
}

func (h *DiscoverHandler) sendDiscoverResponse(c *gin.Context, convID string, resp *relay.ChatCompletionWithToolsResponse) {
	if len(resp.ToolCalls) > 0 {
		c.JSON(http.StatusOK, discoverResponse{
			Status:         "tool_call_required",
			ConversationID: convID,
			ToolCalls:      resp.ToolCalls,
		})
		return
	}

	// LLM finished — parse discovered_tools from content
	h.convStore.Delete(convID)
	content := resp.Content

	// Try to extract JSON using json.Decoder (handles strings with braces correctly)
	raw := json.RawMessage(content)
	if !json.Valid(raw) {
		if extracted, ok := extractJSON(content); ok {
			raw = json.RawMessage(extracted)
		}
	}

	c.JSON(http.StatusOK, discoverResponse{
		Status:          "complete",
		ConversationID:  convID,
		DiscoveredTools: raw,
	})
}

func (h *DiscoverHandler) loadUserProviders(c *gin.Context, userID int) ([]discover.ProviderInfo, error) {
	ctx := c.Request.Context()

	user, err := h.entClient.User.Get(ctx, userID)
	if err != nil {
		return nil, err
	}

	providers, err := h.entClient.RelayProvider.Query().
		Where(relayprovider.EnabledEQ(true)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	var result []discover.ProviderInfo
	for _, p := range providers {
		rp := h.providerHandler.getOrCreateRelayProvider(p)

		var apiKey string
		var apiKeyID int64
		if user.RelayUserID != nil {
			keys, err := rp.ListUserAPIKeys(ctx, int64(*user.RelayUserID))
			if err == nil {
				for _, k := range keys {
					if k.Name == "ae-cli-auto" && k.Status == "active" {
						apiKeyID = k.ID
						break
					}
				}
			}
			if apiKeyID == 0 {
				// 首次：创建新 key，secret 仅在创建时返回
				newKey, err := rp.CreateUserAPIKey(ctx, int64(*user.RelayUserID), "ae-cli-auto")
				if err == nil {
					apiKey = newKey.Secret
					apiKeyID = newKey.ID
				}
			}
			// 注意：已有 key 时 apiKey 为空。这是预期行为——
			// API key secret 仅在创建时返回一次，后续无法从 relay server 获取。
			// 后端在 system prompt 中仅注入 api_key_id 和 base_url，
			// LLM 生成的 config_actions 中的 api_key 由后端在返回 "complete"
			// 响应前填充（从 ProviderHandler.ListForUser 的缓存逻辑获取）。
			// 如果用户已有 key 但需要重新配置工具，应先调用 GET /api/v1/providers
			// 获取 key（该端点在首次调用时创建 key 并返回 secret）。
		}

		result = append(result, discover.ProviderInfo{
			Name:         p.Name,
			DisplayName:  p.DisplayName,
			BaseURL:      p.BaseURL,
			APIKey:       apiKey,
			APIKeyID:     apiKeyID,
			DefaultModel: p.DefaultModel,
			IsPrimary:    p.IsPrimary,
		})
	}
	return result, nil
}

// 设计说明（C3 修复）：
// API key secret 仅在创建时可获取。对于已有 key 的用户，discover 流程改为：
// 1. ae-cli 在调用 discover 之前，先调用 GET /api/v1/providers 获取 provider 列表（含 API key）
// 2. ae-cli 将 provider 信息（含 API key）缓存到本地
// 3. 后端 discover handler 的 system prompt 中只注入 provider 的 base_url 和 model 信息
// 4. LLM 生成 config_actions 时使用占位符 {{API_KEY}}
// 5. ae-cli 收到 complete 响应后，用本地缓存的 API key 替换占位符
// 这样 API key 不经过 LLM prompt，更安全。

func (h *DiscoverHandler) getPrimaryRelayProvider(c *gin.Context) relay.Provider {
	ctx := c.Request.Context()
	p, err := h.entClient.RelayProvider.Query().
		Where(relayprovider.IsPrimaryEQ(true), relayprovider.EnabledEQ(true)).
		First(ctx)
	if err != nil {
		return nil
	}
	return h.providerHandler.getOrCreateRelayProvider(p)
}

// extractJSON finds the first valid JSON object in a string using json.Decoder.
func extractJSON(s string) (string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == '{' {
			dec := json.NewDecoder(strings.NewReader(s[i:]))
			var raw json.RawMessage
			if err := dec.Decode(&raw); err == nil {
				return string(raw), true
			}
		}
	}
	return "", false
}
```

- [ ] **Step 2: 注册路由**

修改 `backend/internal/handler/router.go`，在 SetupRouter 参数列表末尾（`adminSettingsHandler` 之后）添加 `discoverHandler *DiscoverHandler`：

```go
func SetupRouter(
	// ... 现有参数不变 ...
	adminSettingsHandler *AdminSettingsHandler,
	discoverHandler *DiscoverHandler, // 新增，末尾位置
) *gin.Engine {
```

在 `adminSettingsHandler` 路由块之后、`return r` 之前添加：
```go
	// Tool discovery (ae-cli)
	if discoverHandler != nil {
		toolsGroup := protected.Group("/tools")
		{
			toolsGroup.POST("/discover", discoverHandler.Discover)
			toolsGroup.POST("/discover/continue", discoverHandler.DiscoverContinue)
		}
	}
```

需要在 router.go 的 import 中添加 `"github.com/ai-efficiency/backend/internal/discover"`（如果 DiscoverHandler 引用了 discover 包类型）。实际上 DiscoverHandler 在 handler 包内，无需额外 import。

- [ ] **Step 4: 在 main.go 中注入依赖**

修改 `backend/cmd/server/main.go`，在 import 中添加 `"github.com/ai-efficiency/backend/internal/discover"`。

在 `adminSettingsHandler` 初始化之后（约 line 193）添加：
```go
	// Init discover handler
	convStore := discover.NewConversationStore(5 * time.Minute)
	discoverHandler := handler.NewDiscoverHandler(entClient, providerHandler, convStore, logger)
```

更新 `SetupRouter` 调用，在末尾参数 `adminSettingsHandler` 之后添加 `discoverHandler`：
```go
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
		discoverHandler, // 新增
	)
```

- [ ] **Step 5: 编译检查**

Run: `cd backend && go build ./...`
Expected: 成功

- [ ] **Step 6: 写 discover handler 测试（M4 修复）**

`backend/internal/handler/discover_test.go` — 至少覆盖以下场景：
- 初始 discover 请求返回 tool_call_required（mock relay provider 返回 tool_calls）
- continue 请求带 tool_results 返回 complete
- conversation 过期返回 error
- max rounds 超限返回 error
- 未认证请求返回 401

测试使用 `httptest.NewRecorder` + `gin.CreateTestContext`，mock `relay.Provider` 接口。
具体测试代码在实现时根据实际 handler 结构编写。

- [ ] **Step 7: 运行测试**

Run: `cd backend && go test ./internal/handler/... -run TestDiscover -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add backend/internal/handler/discover.go backend/internal/handler/discover_test.go backend/internal/discover/tools.go backend/internal/handler/router.go backend/cmd/server/main.go
git commit -m "feat(backend): add tool discovery endpoints with multi-turn LLM protocol"
```

---

## Task 5: ae-cli 本地 tool_call 执行器（白名单约束）

**Files:**
- Create: `ae-cli/internal/discover/executor.go`
- Create: `ae-cli/internal/discover/executor_test.go`

实现 check_command、read_file、list_dir 三个本地执行器，所有操作受白名单约束。

- [ ] **Step 1: 写测试**

`ae-cli/internal/discover/executor_test.go`:
```go
package discover

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckCommand_Allowed(t *testing.T) {
	e := NewExecutor("/tmp/test-home")
	// "ls" is not in allowlist
	_, err := e.CheckCommand("ls")
	if err == nil {
		t.Error("expected error for disallowed command")
	}
}

func TestCheckCommand_AllowedCommand(t *testing.T) {
	e := NewExecutor("/tmp/test-home")
	// Even if not installed, should not return allowlist error
	result, err := e.CheckCommand("claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Result should indicate found or not found, not an allowlist error
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestReadFile_AllowedPath(t *testing.T) {
	home := t.TempDir()
	e := NewExecutor(home)

	// Create a file in allowed path
	claudeDir := filepath.Join(home, ".claude")
	os.MkdirAll(claudeDir, 0o755)
	testFile := filepath.Join(claudeDir, "settings.json")
	os.WriteFile(testFile, []byte(`{"env":{}}`), 0o644)

	result, err := e.ReadFile(testFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != `{"env":{}}` {
		t.Errorf("got %q, want %q", result, `{"env":{}}`)
	}
}

func TestReadFile_DisallowedPath(t *testing.T) {
	e := NewExecutor("/tmp/test-home")
	_, err := e.ReadFile("/etc/passwd")
	if err == nil {
		t.Error("expected error for disallowed path")
	}
}

func TestListDir_AllowedDir(t *testing.T) {
	home := t.TempDir()
	e := NewExecutor(home)

	// Create some files in home
	os.WriteFile(filepath.Join(home, "file1.txt"), nil, 0o644)

	result, err := e.ListDir(home)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestListDir_DisallowedDir(t *testing.T) {
	e := NewExecutor("/tmp/test-home")
	_, err := e.ListDir("/etc")
	if err == nil {
		t.Error("expected error for disallowed directory")
	}
}

func TestReadFile_Truncation(t *testing.T) {
	home := t.TempDir()
	e := NewExecutor(home)

	claudeDir := filepath.Join(home, ".claude")
	os.MkdirAll(claudeDir, 0o755)
	testFile := filepath.Join(claudeDir, "big.json")

	// Write >10KB file
	bigContent := make([]byte, 12*1024)
	for i := range bigContent {
		bigContent[i] = 'x'
	}
	os.WriteFile(testFile, bigContent, 0o644)

	result, err := e.ReadFile(testFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) > 10*1024+50 { // 10KB + truncation message
		t.Errorf("result should be truncated, got %d bytes", len(result))
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd ae-cli && go test ./internal/discover/... -run TestCheckCommand -v`
Expected: FAIL（包不存在）

- [ ] **Step 3: 实现执行器**

`ae-cli/internal/discover/executor.go`:
```go
package discover

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const maxResultBytes = 10 * 1024 // 10KB

var allowedCommands = map[string]bool{
	"claude": true, "codex": true, "cursor": true,
	"aider": true, "continue": true, "copilot": true, "cody": true,
}

// Executor runs tool_call operations locally with allowlist constraints.
type Executor struct {
	homeDir string
}

// NewExecutor creates an executor rooted at the given home directory.
func NewExecutor(homeDir string) *Executor {
	return &Executor{homeDir: homeDir}
}

// CheckCommand checks if a command exists and returns its version.
func (e *Executor) CheckCommand(command string) (string, error) {
	if !allowedCommands[command] {
		return "", fmt.Errorf("error: command not in allowlist")
	}

	path, err := exec.LookPath(command)
	if err != nil {
		return fmt.Sprintf("%s not found in PATH", command), nil
	}

	// Try --version
	out, err := exec.Command(command, "--version").CombinedOutput()
	if err != nil {
		return fmt.Sprintf("%s found at %s, version unknown", command, path), nil
	}

	version := strings.TrimSpace(string(out))
	if len(version) > 200 {
		version = version[:200]
	}
	return fmt.Sprintf("%s found at %s, version: %s", command, path, version), nil
}

// ReadFile reads a file if it's in the allowed paths.
func (e *Executor) ReadFile(path string) (string, error) {
	if !e.isAllowedReadPath(path) {
		return "", fmt.Errorf("error: path not in allowlist")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("error: %w", err)
	}

	result := string(data)
	if len(result) > maxResultBytes {
		result = result[:maxResultBytes] + "\n... (truncated at 10KB)"
	}
	return result, nil
}

// ListDir lists directory contents if it's in the allowed directories.
func (e *Executor) ListDir(path string) (string, error) {
	if !e.isAllowedListDir(path) {
		return "", fmt.Errorf("error: path not in allowlist")
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("error: %w", err)
	}

	var sb strings.Builder
	count := 0
	for _, entry := range entries {
		if count >= 100 {
			sb.WriteString("... (truncated at 100 entries)\n")
			break
		}
		kind := "file"
		if entry.IsDir() {
			kind = "dir"
		}
		sb.WriteString(fmt.Sprintf("%s (%s)\n", entry.Name(), kind))
		count++
	}
	return sb.String(), nil
}

// Execute dispatches a tool_call by name.
func (e *Executor) Execute(toolName string, argsJSON string) string {
	switch toolName {
	case "check_command":
		var args struct {
			Command string `json:"command"`
		}
		if err := parseJSON(argsJSON, &args); err != nil {
			return "error: invalid arguments"
		}
		result, err := e.CheckCommand(args.Command)
		if err != nil {
			return err.Error()
		}
		return result

	case "read_file":
		var args struct {
			Path string `json:"path"`
		}
		if err := parseJSON(argsJSON, &args); err != nil {
			return "error: invalid arguments"
		}
		// Expand ~ to home dir
		path := e.expandHome(args.Path)
		result, err := e.ReadFile(path)
		if err != nil {
			return err.Error()
		}
		return result

	case "list_dir":
		var args struct {
			Path string `json:"path"`
		}
		if err := parseJSON(argsJSON, &args); err != nil {
			return "error: invalid arguments"
		}
		path := e.expandHome(args.Path)
		result, err := e.ListDir(path)
		if err != nil {
			return err.Error()
		}
		return result

	default:
		return fmt.Sprintf("error: unknown tool %s", toolName)
	}
}

func (e *Executor) expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(e.homeDir, path[2:])
	}
	return path
}

func (e *Executor) isAllowedReadPath(path string) bool {
	// Resolve to absolute
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	// Directory-based allowlist (any file under these dirs is allowed)
	allowedDirs := []string{
		".claude", ".cursor", ".config/codex", ".config/continue",
		".copilot", ".cody", ".ae-cli",
	}
	for _, dir := range allowedDirs {
		prefix := filepath.Join(e.homeDir, dir)
		if strings.HasPrefix(abs, prefix+"/") || abs == prefix {
			return true
		}
	}

	// Exact file allowlist
	allowedFiles := []string{
		".zshrc", ".bashrc", ".bash_profile", ".profile",
	}
	for _, f := range allowedFiles {
		if abs == filepath.Join(e.homeDir, f) {
			return true
		}
	}

	// Glob pattern for .aider* (files starting with .aider in home dir)
	base := filepath.Base(abs)
	dir := filepath.Dir(abs)
	if dir == e.homeDir && strings.HasPrefix(base, ".aider") {
		return true
	}

	return false
}

func (e *Executor) isAllowedListDir(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	allowed := []string{
		e.homeDir,
		filepath.Join(e.homeDir, ".claude"),
		filepath.Join(e.homeDir, ".cursor"),
		filepath.Join(e.homeDir, ".config"),
		filepath.Join(e.homeDir, ".config", "codex"),
		filepath.Join(e.homeDir, ".config", "continue"),
	}

	for _, dir := range allowed {
		if abs == dir {
			return true
		}
	}
	return false
}

func parseJSON(s string, v interface{}) error {
	return json.Unmarshal([]byte(s), v)
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd ae-cli && go test ./internal/discover/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add ae-cli/internal/discover/executor.go ae-cli/internal/discover/executor_test.go
git commit -m "feat(ae-cli): add local tool_call executor with allowlist constraints"
```

---

## Task 6: ae-cli config action 写入器 + 缓存管理

**Files:**
- Create: `ae-cli/internal/discover/actions.go`
- Create: `ae-cli/internal/discover/actions_test.go`
- Create: `ae-cli/internal/discover/cache.go`
- Create: `ae-cli/internal/discover/cache_test.go`

实现 write_json、write_yaml、set_env 三种配置写入操作，以及 discovered_tools.json 缓存管理。

- [ ] **Step 1: 写 actions 测试**

`ae-cli/internal/discover/actions_test.go`:
```go
package discover

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteJSON_MergeKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Write existing file
	existing := map[string]interface{}{
		"theme": "dark",
		"env":   map[string]interface{}{"EXISTING": "value"},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(path, data, 0o644)

	action := ConfigAction{
		Type: "write_json",
		Path: path,
		MergeKeys: map[string]interface{}{
			"env": map[string]interface{}{
				"ANTHROPIC_BASE_URL": "http://localhost:3000/v1",
				"ANTHROPIC_API_KEY":  "sk-test",
			},
		},
	}

	err := ExecuteAction(action, false)
	if err != nil {
		t.Fatalf("ExecuteAction: %v", err)
	}

	// Read back
	result, _ := os.ReadFile(path)
	var parsed map[string]interface{}
	json.Unmarshal(result, &parsed)

	if parsed["theme"] != "dark" {
		t.Error("existing key 'theme' should be preserved")
	}
	env := parsed["env"].(map[string]interface{})
	if env["EXISTING"] != "value" {
		t.Error("existing env key should be preserved")
	}
	if env["ANTHROPIC_BASE_URL"] != "http://localhost:3000/v1" {
		t.Error("new env key should be merged")
	}
}

func TestWriteJSON_CreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.json")

	action := ConfigAction{
		Type: "write_json",
		Path: path,
		MergeKeys: map[string]interface{}{
			"key": "value",
		},
	}

	err := ExecuteAction(action, false)
	if err != nil {
		t.Fatalf("ExecuteAction: %v", err)
	}

	data, _ := os.ReadFile(path)
	var parsed map[string]interface{}
	json.Unmarshal(data, &parsed)
	if parsed["key"] != "value" {
		t.Error("expected key=value in new file")
	}
}

func TestWriteJSON_CreatesBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	os.WriteFile(path, []byte(`{"old":"data"}`), 0o644)

	action := ConfigAction{
		Type:      "write_json",
		Path:      path,
		MergeKeys: map[string]interface{}{"new": "data"},
	}

	ExecuteAction(action, false)

	backup := path + ".bak"
	data, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal("backup file should exist")
	}
	if string(data) != `{"old":"data"}` {
		t.Error("backup should contain original content")
	}
}

func TestSetEnv_WritesEnvFile(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "env")
	profileFile := filepath.Join(dir, ".zshrc")
	os.WriteFile(profileFile, []byte("# existing content\n"), 0o644)

	action := ConfigAction{
		Type: "set_env",
		File: envFile,
		Vars: map[string]string{
			"OPENAI_BASE_URL": "http://localhost:3000/v1",
			"OPENAI_API_KEY":  "sk-test",
		},
		SourceProfile: profileFile,
	}

	err := ExecuteAction(action, false)
	if err != nil {
		t.Fatalf("ExecuteAction: %v", err)
	}

	// Check env file
	envData, _ := os.ReadFile(envFile)
	envContent := string(envData)
	if !strings.Contains(envContent, "export OPENAI_BASE_URL=") {
		t.Error("env file should contain OPENAI_BASE_URL export")
	}

	// Check profile has source line
	profileData, _ := os.ReadFile(profileFile)
	profileContent := string(profileData)
	if !strings.Contains(profileContent, "source "+envFile) {
		t.Error("profile should contain source line")
	}
}

func TestSetEnv_NoDoubleSource(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "env")
	profileFile := filepath.Join(dir, ".zshrc")
	os.WriteFile(profileFile, []byte("# ae-cli managed\nsource "+envFile+"\n"), 0o644)

	action := ConfigAction{
		Type:          "set_env",
		File:          envFile,
		Vars:          map[string]string{"KEY": "val"},
		SourceProfile: profileFile,
	}

	ExecuteAction(action, false)

	data, _ := os.ReadFile(profileFile)
	count := strings.Count(string(data), "source "+envFile)
	if count != 1 {
		t.Errorf("source line should appear exactly once, got %d", count)
	}
}

func TestWriteJSON_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.json")

	action := ConfigAction{
		Type:      "write_json",
		Path:      path,
		MergeKeys: map[string]interface{}{"key": "secret"},
	}

	ExecuteAction(action, false)

	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file permissions should be 0600, got %o", info.Mode().Perm())
	}
}

func TestDryRun_NoWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "should-not-exist.json")

	action := ConfigAction{
		Type:      "write_json",
		Path:      path,
		MergeKeys: map[string]interface{}{"key": "value"},
	}

	err := ExecuteAction(action, true) // dry run
	if err != nil {
		t.Fatalf("dry run should not error: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file should not be created in dry run mode")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd ae-cli && go test ./internal/discover/... -run "TestWriteJSON|TestSetEnv|TestDryRun" -v`
Expected: FAIL

- [ ] **Step 3: 实现 actions.go**

`ae-cli/internal/discover/actions.go`:
```go
package discover

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConfigAction represents a single configuration write operation.
type ConfigAction struct {
	Type          string                 `json:"type"`
	Path          string                 `json:"path,omitempty"`
	MergeKeys     map[string]interface{} `json:"merge_keys,omitempty"`
	File          string                 `json:"file,omitempty"`
	Vars          map[string]string      `json:"vars,omitempty"`
	SourceProfile string                 `json:"source_profile,omitempty"`
}

// ExecuteAction runs a config action. If dryRun is true, no files are modified.
func ExecuteAction(action ConfigAction, dryRun bool) error {
	switch action.Type {
	case "write_json":
		return executeWriteJSON(action, dryRun)
	case "write_yaml":
		return executeWriteYAML(action, dryRun)
	case "set_env":
		return executeSetEnv(action, dryRun)
	default:
		return fmt.Errorf("unknown action type: %s", action.Type)
	}
}

func executeWriteJSON(action ConfigAction, dryRun bool) error {
	if dryRun {
		return nil
	}

	// Read existing file if it exists
	existing := make(map[string]interface{})
	if data, err := os.ReadFile(action.Path); err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			return fmt.Errorf("parse existing JSON %s: %w", action.Path, err)
		}
		// Backup
		os.WriteFile(action.Path+".bak", data, 0o600)
	}

	// Deep merge
	merged := deepMerge(existing, action.MergeKeys)

	// Write atomically
	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(action.Path), 0o700); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	tmpFile := action.Path + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0o600); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	return os.Rename(tmpFile, action.Path)
}

func executeWriteYAML(action ConfigAction, dryRun bool) error {
	// YAML support is a stretch goal; for v1 we only need JSON and env
	return fmt.Errorf("write_yaml not yet implemented")
}

func executeSetEnv(action ConfigAction, dryRun bool) error {
	if dryRun {
		return nil
	}

	// Write env file
	var sb strings.Builder
	for k, v := range action.Vars {
		sb.WriteString(fmt.Sprintf("export %s=%q\n", k, v))
	}

	if err := os.MkdirAll(filepath.Dir(action.File), 0o700); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	if err := os.WriteFile(action.File, []byte(sb.String()), 0o600); err != nil {
		return fmt.Errorf("write env file: %w", err)
	}

	// Inject source line into profile if needed
	if action.SourceProfile != "" {
		sourceLine := "source " + action.File
		profileData, err := os.ReadFile(action.SourceProfile)
		if err != nil {
			return fmt.Errorf("read profile: %w", err)
		}
		if !strings.Contains(string(profileData), sourceLine) {
			f, err := os.OpenFile(action.SourceProfile, os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
				return fmt.Errorf("open profile: %w", err)
			}
			defer f.Close()
			f.WriteString("\n# ae-cli managed\n" + sourceLine + "\n")
		}
	}

	return nil
}

// deepMerge recursively merges src into dst. src values override dst.
func deepMerge(dst, src map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range dst {
		result[k] = v
	}
	for k, v := range src {
		if srcMap, ok := v.(map[string]interface{}); ok {
			if dstMap, ok := result[k].(map[string]interface{}); ok {
				result[k] = deepMerge(dstMap, srcMap)
				continue
			}
		}
		result[k] = v
	}
	return result
}
```

- [ ] **Step 4: 运行 actions 测试确认通过**

Run: `cd ae-cli && go test ./internal/discover/... -run "TestWriteJSON|TestSetEnv|TestDryRun" -v`
Expected: PASS

- [ ] **Step 5: 写 cache 测试**

`ae-cli/internal/discover/cache_test.go`:
```go
package discover

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCache_WriteAndRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "discovered_tools.json")

	tools := []DiscoveredTool{
		{
			Name:         "claude",
			DisplayName:  "Claude Code",
			ProviderName: "sub2api",
			APIKeyID:     123,
			ConfigPath:   "/home/user/.claude/settings.json",
		},
	}

	if err := WriteCache(path, tools); err != nil {
		t.Fatalf("WriteCache: %v", err)
	}

	cache, err := ReadCache(path)
	if err != nil {
		t.Fatalf("ReadCache: %v", err)
	}

	if len(cache.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(cache.Tools))
	}
	if cache.Tools[0].Name != "claude" {
		t.Errorf("name = %q, want claude", cache.Tools[0].Name)
	}
	if cache.Tools[0].APIKeyID != 123 {
		t.Errorf("api_key_id = %d, want 123", cache.Tools[0].APIKeyID)
	}
}

func TestCache_IsExpired(t *testing.T) {
	cache := &ToolCache{
		DiscoveredAt: time.Now().Add(-8 * 24 * time.Hour), // 8 days ago
	}
	if !cache.IsExpired() {
		t.Error("cache should be expired after 7 days")
	}

	cache.DiscoveredAt = time.Now().Add(-1 * time.Hour)
	if cache.IsExpired() {
		t.Error("cache should not be expired after 1 hour")
	}
}

func TestCache_ReadNotFound(t *testing.T) {
	_, err := ReadCache("/nonexistent/path")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestCache_DefaultPath(t *testing.T) {
	path := DefaultCachePath("/home/user")
	expected := filepath.Join("/home/user", ".ae-cli", "discovered_tools.json")
	if path != expected {
		t.Errorf("path = %q, want %q", path, expected)
	}
}

func TestCache_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "discovered_tools.json")

	WriteCache(path, nil)

	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("permissions = %o, want 0600", info.Mode().Perm())
	}
}
```

- [ ] **Step 6: 实现 cache.go**

`ae-cli/internal/discover/cache.go`:
```go
package discover

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const cacheExpiry = 7 * 24 * time.Hour

// DiscoveredTool represents a discovered AI tool with its provider mapping.
type DiscoveredTool struct {
	Name         string `json:"name"`
	DisplayName  string `json:"display_name"`
	ProviderName string `json:"provider_name"`
	APIKeyID     int64  `json:"api_key_id"`
	ConfigPath   string `json:"config_path,omitempty"`
}

// ToolCache is the on-disk cache of discovered tools.
type ToolCache struct {
	DiscoveredAt time.Time        `json:"discovered_at"`
	Tools        []DiscoveredTool `json:"tools"`
}

// IsExpired returns true if the cache is older than 7 days.
func (c *ToolCache) IsExpired() bool {
	return time.Since(c.DiscoveredAt) > cacheExpiry
}

// DefaultCachePath returns ~/.ae-cli/discovered_tools.json.
func DefaultCachePath(homeDir string) string {
	return filepath.Join(homeDir, ".ae-cli", "discovered_tools.json")
}

// WriteCache writes the discovered tools to the cache file.
func WriteCache(path string, tools []DiscoveredTool) error {
	cache := ToolCache{
		DiscoveredAt: time.Now(),
		Tools:        tools,
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}

// ReadCache reads the discovered tools cache from disk.
func ReadCache(path string) (*ToolCache, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read cache: %w", err)
	}
	var cache ToolCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("parse cache: %w", err)
	}
	return &cache, nil
}
```

- [ ] **Step 7: 运行全部 discover 包测试**

Run: `cd ae-cli && go test ./internal/discover/... -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add ae-cli/internal/discover/actions.go ae-cli/internal/discover/actions_test.go ae-cli/internal/discover/cache.go ae-cli/internal/discover/cache_test.go
git commit -m "feat(ae-cli): add config action writer and discovered tools cache"
```

---

## Task 7: ae-cli 工具发现主流程（多轮 tool_call 循环）

**Files:**
- Create: `ae-cli/internal/discover/discover.go`
- Modify: `ae-cli/internal/client/client.go`
- Create: `ae-cli/internal/discover/discover_test.go`

实现 ae-cli 侧的多轮 tool_call 循环：调用后端 discover 端点 → 收到 tool_call → 本地执行 → 回传结果 → 循环直到 complete。

- [ ] **Step 1: 在 client.go 中添加 Discover API 方法**

在 `ae-cli/internal/client/client.go` 中添加：
```go
// DiscoverRequest is the initial tool discovery request.
type DiscoverRequest struct {
	LocalContext struct {
		OS       string   `json:"os"`
		Arch     string   `json:"arch"`
		HomeDir  string   `json:"home_dir"`
		PathDirs []string `json:"path_dirs"`
	} `json:"local_context"`
}

// DiscoverResponse is the unified response for discover endpoints.
type DiscoverResponse struct {
	Status         string          `json:"status"`
	ConversationID string          `json:"conversation_id"`
	ToolCalls      []ToolCallItem  `json:"tool_calls,omitempty"`
	DiscoveredTools json.RawMessage `json:"discovered_tools,omitempty"`
	Error          string          `json:"error,omitempty"`
}

// ToolCallItem represents a single tool call from the LLM.
type ToolCallItem struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// DiscoverContinueRequest sends tool results back to continue the conversation.
type DiscoverContinueRequest struct {
	ConversationID string `json:"conversation_id"`
	ToolResults    []struct {
		ToolCallID string `json:"tool_call_id"`
		Result     string `json:"result"`
	} `json:"tool_results"`
}

// Discover initiates tool discovery.
func (c *Client) Discover(ctx context.Context, req DiscoverRequest) (*DiscoverResponse, error) {
	return c.doDiscoverRequest(ctx, "/api/v1/tools/discover", req)
}

// DiscoverContinue continues a tool discovery conversation.
func (c *Client) DiscoverContinue(ctx context.Context, req DiscoverContinueRequest) (*DiscoverResponse, error) {
	return c.doDiscoverRequest(ctx, "/api/v1/tools/discover/continue", req)
}

func (c *Client) doDiscoverRequest(ctx context.Context, path string, payload interface{}) (*DiscoverResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	c.setHeaders(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var result DiscoverResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}
```

- [ ] **Step 2: 实现 discover.go 主流程**

`ae-cli/internal/discover/discover.go`:
```go
package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

const (
	maxRounds    = 20
	roundTimeout = 30 * time.Second
	totalTimeout = 3 * time.Minute
)

// Options configures the discovery process.
type Options struct {
	DryRun      bool
	AutoConfirm bool // --yes flag, skip confirmation prompts
}

// DiscoveredToolResult is the parsed result from the LLM.
type DiscoveredToolResult struct {
	Name         string         `json:"name"`
	DisplayName  string         `json:"display_name"`
	Version      string         `json:"version,omitempty"`
	Path         string         `json:"path,omitempty"`
	ProviderName string         `json:"provider_name"`
	APIKeyID     int64          `json:"api_key_id"`
	ConfigActions []ConfigAction `json:"config_actions"`
}

// Run executes the full tool discovery flow.
func Run(ctx context.Context, apiClient *client.Client, homeDir string, opts Options) ([]DiscoveredToolResult, error) {
	ctx, cancel := context.WithTimeout(ctx, totalTimeout)
	defer cancel()

	executor := NewExecutor(homeDir)

	// Build local context
	var req client.DiscoverRequest
	req.LocalContext.OS = runtime.GOOS
	req.LocalContext.Arch = runtime.GOARCH
	req.LocalContext.HomeDir = homeDir
	req.LocalContext.PathDirs = strings.Split(os.Getenv("PATH"), ":")

	// Initial request
	resp, err := apiClient.Discover(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("discover request: %w", err)
	}

	// Multi-turn loop
	for round := 0; round < maxRounds; round++ {
		switch resp.Status {
		case "complete":
			return handleComplete(resp, homeDir, opts)
		case "error":
			return nil, fmt.Errorf("discovery error: %s", resp.Error)
		case "tool_call_required":
			// Execute tool calls locally
			var toolResults []struct {
				ToolCallID string `json:"tool_call_id"`
				Result     string `json:"result"`
			}
			for _, tc := range resp.ToolCalls {
				result := executor.Execute(tc.Function.Name, tc.Function.Arguments)
				toolResults = append(toolResults, struct {
					ToolCallID string `json:"tool_call_id"`
					Result     string `json:"result"`
				}{
					ToolCallID: tc.ID,
					Result:     result,
				})
				fmt.Printf("  [%s] %s\n", tc.Function.Name, truncate(result, 80))
			}

			// Continue conversation
			contReq := client.DiscoverContinueRequest{
				ConversationID: resp.ConversationID,
			}
			contReq.ToolResults = toolResults

			resp, err = apiClient.DiscoverContinue(ctx, contReq)
			if err != nil {
				return nil, fmt.Errorf("discover continue: %w", err)
			}
		default:
			return nil, fmt.Errorf("unknown status: %s", resp.Status)
		}
	}

	return nil, fmt.Errorf("max discovery rounds (%d) exceeded", maxRounds)
}

func handleComplete(resp *client.DiscoverResponse, homeDir string, opts Options) ([]DiscoveredToolResult, error) {
	// Parse discovered_tools from response
	var wrapper struct {
		DiscoveredTools []DiscoveredToolResult `json:"discovered_tools"`
	}
	if err := json.Unmarshal(resp.DiscoveredTools, &wrapper); err != nil {
		// Try parsing as direct array
		var tools []DiscoveredToolResult
		if err2 := json.Unmarshal(resp.DiscoveredTools, &tools); err2 != nil {
			return nil, fmt.Errorf("parse discovered tools: %w (raw: %.200s)", err, string(resp.DiscoveredTools))
		}
		wrapper.DiscoveredTools = tools
	}

	if len(wrapper.DiscoveredTools) == 0 {
		fmt.Println("No AI tools discovered on this machine.")
		return nil, nil
	}

	// Print summary
	fmt.Printf("\nDiscovered %d tool(s):\n", len(wrapper.DiscoveredTools))
	for _, tool := range wrapper.DiscoveredTools {
		fmt.Printf("  - %s (%s) → provider: %s\n", tool.DisplayName, tool.Name, tool.ProviderName)
		for _, action := range tool.ConfigActions {
			fmt.Printf("    [%s] %s\n", action.Type, actionSummary(action))
		}
	}

	if opts.DryRun {
		fmt.Println("\n(dry run — no changes written)")
		return wrapper.DiscoveredTools, nil
	}

	// Interactive confirmation (M1: spec 要求写入前确认)
	if !opts.AutoConfirm {
		fmt.Print("\nApply these configurations? [y/N] ")
		var answer string
		fmt.Scanln(&answer)
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Cancelled. Run with --yes to skip confirmation.")
			return wrapper.DiscoveredTools, nil
		}
	}

	// Execute config actions
	fmt.Println("\nApplying configurations...")
	var successTools []DiscoveredTool
	for _, tool := range wrapper.DiscoveredTools {
		allOK := true
		for _, action := range tool.ConfigActions {
			// Expand ~ in paths
			action.Path = expandHome(action.Path, homeDir)
			action.File = expandHome(action.File, homeDir)
			action.SourceProfile = expandHome(action.SourceProfile, homeDir)

			if err := ExecuteAction(action, false); err != nil {
				fmt.Printf("  [WARN] %s: %s — %v\n", tool.Name, action.Type, err)
				allOK = false
			}
		}
		if allOK {
			configPath := ""
			if len(tool.ConfigActions) > 0 {
				configPath = tool.ConfigActions[0].Path
			}
			successTools = append(successTools, DiscoveredTool{
				Name:         tool.Name,
				DisplayName:  tool.DisplayName,
				ProviderName: tool.ProviderName,
				APIKeyID:     tool.APIKeyID,
				ConfigPath:   configPath,
			})
			fmt.Printf("  [OK] %s configured\n", tool.DisplayName)
		}
	}

	// Write cache
	cachePath := DefaultCachePath(homeDir)
	if err := WriteCache(cachePath, successTools); err != nil {
		fmt.Printf("  [WARN] failed to write cache: %v\n", err)
	}

	return wrapper.DiscoveredTools, nil
}

func expandHome(path, homeDir string) string {
	if strings.HasPrefix(path, "~/") {
		return homeDir + path[1:]
	}
	return path
}

func actionSummary(a ConfigAction) string {
	switch a.Type {
	case "write_json":
		return a.Path
	case "set_env":
		keys := make([]string, 0, len(a.Vars))
		for k := range a.Vars {
			keys = append(keys, k)
		}
		return strings.Join(keys, ", ")
	default:
		return a.Type
	}
}

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
```

- [ ] **Step 3: 编译检查**

Run: `cd ae-cli && go build ./internal/discover/...`
Expected: 成功

- [ ] **Step 4: 写 discover 主流程测试（M5 修复）**

`ae-cli/internal/discover/discover_test.go` — 至少覆盖以下场景：
- 单轮完成（mock server 直接返回 complete）
- 多轮 tool_call 循环（mock server 返回 tool_call_required → continue → complete）
- max rounds 超限返回错误
- server 返回 error status
- dry run 模式不写入文件

测试使用 `httptest.NewServer` mock 后端 API，验证多轮循环逻辑。
具体测试代码在实现时根据实际 client 接口编写。

- [ ] **Step 5: 运行测试**

Run: `cd ae-cli && go test ./internal/discover/... -run TestRun -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add ae-cli/internal/discover/discover.go ae-cli/internal/discover/discover_test.go ae-cli/internal/client/client.go
git commit -m "feat(ae-cli): implement multi-turn tool discovery flow with client API"
```

---

## Task 8: ae-cli discover 命令 + login 集成

**Files:**
- Create: `ae-cli/cmd/discover.go`
- Modify: `ae-cli/cmd/login.go`

添加 `ae-cli discover` 命令（支持 --dry-run、--refresh），并在 login 成功后自动触发工具发现。

- [ ] **Step 1: 创建 discover 命令**

`ae-cli/cmd/discover.go`:
```go
package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/discover"
	"github.com/spf13/cobra"
)

var (
	discoverDryRun  bool
	discoverRefresh bool
	discoverYes     bool
)

var discoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Discover and configure local AI tools",
	Long:  "Detects installed AI tools (Claude Code, Codex CLI, etc.) and configures them with your provider's API keys.",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Guard: ensure user is logged in (W4)
		if cfg.Server.URL == "" {
			return fmt.Errorf("server URL not configured. Run `ae-cli login` first")
		}
		tokenPath, _ := auth.DefaultTokenPath()
		tf, err := auth.ReadToken(tokenPath)
		if err != nil || !tf.IsValid() {
			return fmt.Errorf("not logged in or token expired. Run `ae-cli login` first")
		}

		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("get home dir: %w", err)
		}

		// Check cache unless --refresh
		if !discoverRefresh && !discoverDryRun {
			cachePath := discover.DefaultCachePath(home)
			cache, err := discover.ReadCache(cachePath)
			if err == nil && !cache.IsExpired() {
				fmt.Printf("Tool discovery cache is fresh (discovered %s). Use --refresh to re-discover.\n",
					cache.DiscoveredAt.Format(time.RFC3339))
				fmt.Printf("Cached tools: %d\n", len(cache.Tools))
				for _, t := range cache.Tools {
					fmt.Printf("  - %s (%s)\n", t.DisplayName, t.Name)
				}
				return nil
			}
		}

		fmt.Println("Discovering local AI tools...")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		tools, err := discover.Run(ctx, apiClient, home, discover.Options{
			DryRun:      discoverDryRun,
			AutoConfirm: discoverYes,
		})
		if err != nil {
			return fmt.Errorf("tool discovery: %w", err)
		}

		if len(tools) == 0 {
			fmt.Println("No AI tools found.")
		} else {
			fmt.Printf("\nDone! %d tool(s) configured.\n", len(tools))
		}
		return nil
	},
}

func init() {
	discoverCmd.Flags().BoolVar(&discoverDryRun, "dry-run", false, "Show discovery results without writing configs")
	discoverCmd.Flags().BoolVar(&discoverRefresh, "refresh", false, "Force re-discovery even if cache is fresh")
	discoverCmd.Flags().BoolVar(&discoverYes, "yes", false, "Skip confirmation prompts")
	rootCmd.AddCommand(discoverCmd)
}
```

- [ ] **Step 2: 修改 login.go，登录成功后自动触发发现**

在 `ae-cli/cmd/login.go` 的 `fmt.Printf("Login successful!...")` 之后追加：
```go
		// Auto-discover tools after login
		fmt.Println("\nDiscovering local AI tools...")
		home, _ := os.UserHomeDir()
		discoverCtx, discoverCancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer discoverCancel()

		// Need a fresh client with the new token
		discoverClient := client.New(serverURL, token.AccessToken)
		tools, discoverErr := discover.Run(discoverCtx, discoverClient, home, discover.Options{
			AutoConfirm: true, // login 后自动发现不需要交互确认
		})
		if discoverErr != nil {
			fmt.Printf("Tool discovery failed (non-fatal): %v\n", discoverErr)
			fmt.Println("You can retry later with: ae-cli discover")
		} else if len(tools) > 0 {
			fmt.Printf("%d tool(s) configured.\n", len(tools))
		}
```

需要在 login.go 的 import 中添加 `"os"`, `"github.com/ai-efficiency/ae-cli/internal/discover"`, `"github.com/ai-efficiency/ae-cli/internal/client"`。

- [ ] **Step 3: 编译检查**

Run: `cd ae-cli && go build ./...`
Expected: 成功

- [ ] **Step 4: Commit**

```bash
git add ae-cli/cmd/discover.go ae-cli/cmd/login.go
git commit -m "feat(ae-cli): add discover command and auto-discover on login"
```

---

## Task 9: session 启动时上报 tool_configs

**Files:**
- Modify: `ae-cli/internal/session/manager.go`

session Start() 读取 discovered_tools 缓存，将 tool_configs（含 api_key_id）上报给后端。

- [ ] **Step 1: 修改 manager.go 的 Start 方法**

在 `ae-cli/internal/session/manager.go` 的 `Start()` 方法中，在 `m.client.CreateSession` 调用之前，读取缓存并构建 tool_configs：

```go
func (m *Manager) Start() (*State, error) {
	repo, err := detectRepo()
	if err != nil {
		return nil, fmt.Errorf("detecting git repo: %w", err)
	}

	branch, err := detectBranch()
	if err != nil {
		return nil, fmt.Errorf("detecting git branch: %w", err)
	}

	sessionID := uuid.New().String()

	// Load discovered tools for tool_configs
	var toolConfigs []map[string]interface{}
	home, _ := os.UserHomeDir()
	if home != "" {
		cachePath := discover.DefaultCachePath(home)
		cache, err := discover.ReadCache(cachePath)
		if err == nil && !cache.IsExpired() {
			for _, t := range cache.Tools {
				toolConfigs = append(toolConfigs, map[string]interface{}{
					"tool_name":        t.Name,
					"provider_name":    t.ProviderName,
					"relay_api_key_id": t.APIKeyID,
				})
			}
		}
	}

	sess, err := m.client.CreateSession(context.Background(), client.CreateSessionRequest{
		ID:           sessionID,
		RepoFullName: repo,
		Branch:       branch,
		ToolConfigs:  toolConfigs,
	})
	// ... rest unchanged
```

需要在 import 中添加 `"github.com/ai-efficiency/ae-cli/internal/discover"`。

- [ ] **Step 2: 编译检查**

Run: `cd ae-cli && go build ./...`
Expected: 成功

- [ ] **Step 3: 运行全量 ae-cli 测试**

Run: `cd ae-cli && go test ./... -v -count=1 2>&1 | tail -30`
Expected: PASS（无回归）

- [ ] **Step 4: Commit**

```bash
git add ae-cli/internal/session/manager.go
git commit -m "feat(ae-cli): report discovered tool_configs on session start"
```

---

## Task 10: 集成验证 + 全量编译检查

**Files:**
- 无新增文件，验证所有 Task 的集成

确保后端和 ae-cli 全量编译通过，所有新增测试通过，无回归。

- [ ] **Step 1: 后端全量编译**

Run: `cd backend && go build ./...`
Expected: 成功

- [ ] **Step 2: 后端全量测试**

Run: `cd backend && go test ./... -count=1 2>&1 | tail -20`
Expected: PASS（无失败）

- [ ] **Step 3: ae-cli 全量编译**

Run: `cd ae-cli && go build ./...`
Expected: 成功

- [ ] **Step 4: ae-cli 全量测试**

Run: `cd ae-cli && go test ./... -count=1 2>&1 | tail -30`
Expected: PASS（无失败）

- [ ] **Step 5: 验证新增路由注册**

Run: `cd backend && grep -n "discover" internal/handler/router.go`
Expected: 看到 `/tools/discover` 和 `/tools/discover/continue` 路由

- [ ] **Step 6: 验证 ae-cli discover 命令注册**

Run: `cd ae-cli && go run . discover --help`
Expected: 看到 discover 命令的帮助信息，包含 --dry-run、--refresh、--yes 标志

- [ ] **Step 7: 更新 plan 状态**

将本计划文件头部的 `**Status:** 待实施` 改为 `**Status:** ✅ 已完成（YYYY-MM-DD）`

- [ ] **Step 8: Commit**

```bash
git add docs/superpowers/plans/2026-03-26-smart-tool-discovery.md
git commit -m "docs(plans): mark smart tool discovery plan as complete"
```
