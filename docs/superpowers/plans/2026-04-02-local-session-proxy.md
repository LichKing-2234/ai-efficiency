# Local Session Proxy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a session-bound local proxy inside `ae-cli start` that serves Codex and Claude through local OpenAI/Anthropic-compatible endpoints, records per-session usage, ingests hook/events locally, and uploads normalized usage/checkpoint data to backend for PR attribution.

**Architecture:** Keep `session` as the primary entity and add a local data plane next to the existing runtime bundle. `ae-cli start` will bootstrap a backend session, fetch user/provider credentials, start a loopback-only proxy child process, inject tool config for Codex and Claude, and route hook traffic through the proxy. Backend will persist `session_usage_events` and `session_events`, then prefer local usage data over relay-ledger reconstruction during attribution.

**Tech Stack:** Go (`cobra`, `net/http`, `httputil`, `ent`, `gin`), Vue 3 already unchanged for phase 1, existing `ae-cli` runtime/hook infrastructure, existing backend checkpoint/attribution pipeline, OpenAI-compatible and Anthropic-compatible HTTP payloads.

---

## File Structure

### New files

- `backend/ent/schema/session_usage_event.go`
  Session-scoped request usage facts emitted by the local proxy.
- `backend/ent/schema/session_event.go`
  Session lifecycle and tool/hook event records emitted by the local proxy.
- `backend/internal/sessionusage/service.go`
  Validation and persistence for usage events.
- `backend/internal/sessionusage/service_test.go`
  Unit tests for usage event persistence and query semantics.
- `backend/internal/sessionevent/service.go`
  Validation and persistence for session/tool lifecycle events.
- `backend/internal/sessionevent/service_test.go`
  Unit tests for event persistence semantics.
- `backend/internal/handler/session_usage.go`
  HTTP ingest handlers for `/api/v1/session-usage-events` and `/api/v1/session-events`.
- `backend/internal/handler/session_usage_test.go`
  HTTP tests for usage/event ingest endpoints.
- `ae-cli/internal/proxy/server.go`
  Proxy process bootstrap, routing table, auth, and shutdown lifecycle.
- `ae-cli/internal/proxy/openai.go`
  OpenAI-compatible request proxying and usage extraction.
- `ae-cli/internal/proxy/anthropic.go`
  Anthropic-compatible request proxying and usage extraction.
- `ae-cli/internal/proxy/events.go`
  Local event ingress HTTP handlers and normalized event persistence to local queue/backend.
- `ae-cli/internal/proxy/config.go`
  Session-local proxy config, token generation, and runtime bundle serialization.
- `ae-cli/internal/proxy/server_test.go`
  End-to-end proxy tests for startup, auth, request logging, and shutdown.
- `ae-cli/internal/toolconfig/codex.go`
  Codex session-local config generation and cleanup.
- `ae-cli/internal/toolconfig/claude.go`
  Claude env/settings generation and cleanup.
- `ae-cli/internal/toolconfig/toolconfig_test.go`
  Tests for generated Codex/Claude configuration content.

### Modified files

- `backend/internal/handler/router.go`
  Register new ingest routes.
- `backend/internal/attribution/service.go`
  Prefer local `session_usage_events` when calculating checkpoint/PR intervals.
- `backend/internal/attribution/service_test.go`
  Cover local-usage-first attribution and relay fallback.
- `backend/internal/handler/session.go`
  Expose session edges for usage/events in detail view if already returning edges.
- `backend/internal/handler/session_detail_http_test.go`
  Verify detail payload includes usage/event slices when present.
- `backend/cmd/server/main.go`
  Wire new services/handlers.
- `ae-cli/internal/client/client.go`
  Add backend client methods for `session-usage-events` and `session-events`.
- `ae-cli/internal/client/client_test.go`
  Verify new client requests.
- `ae-cli/internal/session/runtime.go`
  Extend runtime bundle with proxy port/token/process metadata and provider creds.
- `ae-cli/internal/session/runtime_test.go`
  Verify new runtime fields and cleanup.
- `ae-cli/internal/session/manager.go`
  Start/stop proxy process, fetch provider data, stage tool config, rollback on failure.
- `ae-cli/internal/session/session_test.go`
  Cover proxy lifecycle within start/stop.
- `ae-cli/cmd/start.go`
  Call proxy-aware manager behavior and preserve existing session UX.
- `ae-cli/cmd/stop.go`
  Ensure stop tears down proxy runtime.
- `ae-cli/cmd/shell.go`
  Inject local proxy env into shell session.
- `ae-cli/internal/hooks/handler.go`
  Post git hook events to local proxy ingress before backend fallback queue.
- `ae-cli/internal/hooks/handler_test.go`
  Verify local proxy ingress path and fallback behavior.
- `ae-cli/cmd/hook.go`
  Reuse local proxy-aware hook uploader/bootstrap path.
- `ae-cli/cmd/flush.go`
  Flush both proxy-generated and hook-generated queues through backend.

### Existing files to read before implementation

- `ae-cli/internal/session/manager.go`
- `ae-cli/internal/session/runtime.go`
- `ae-cli/internal/hooks/handler.go`
- `ae-cli/internal/client/client.go`
- `backend/internal/checkpoint/service.go`
- `backend/internal/attribution/service.go`
- `backend/internal/handler/router.go`
- `backend/internal/handler/provider.go`
- `docs/superpowers/specs/2026-04-02-local-session-proxy-design.md`

---

### Task 1: Backend Usage/Event Schema And Ingest Contracts

**Files:**
- Create: `backend/ent/schema/session_usage_event.go`
- Create: `backend/ent/schema/session_event.go`
- Create: `backend/internal/sessionusage/service.go`
- Create: `backend/internal/sessionusage/service_test.go`
- Create: `backend/internal/sessionevent/service.go`
- Create: `backend/internal/sessionevent/service_test.go`
- Create: `backend/internal/handler/session_usage.go`
- Create: `backend/internal/handler/session_usage_test.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/cmd/server/main.go`
- Test: `backend/internal/sessionusage/service_test.go`
- Test: `backend/internal/sessionevent/service_test.go`
- Test: `backend/internal/handler/session_usage_test.go`

- [ ] **Step 1: Write the failing backend ingest tests**

```go
func TestSessionUsageIngest_CreatesUsageEvent(t *testing.T) {
	env := setupFullTestEnv(t)

	w := doFullRequest(env, http.MethodPost, "/api/v1/session-usage-events", map[string]any{
		"event_id":      "usage-evt-1",
		"session_id":    "11111111-1111-1111-1111-111111111111",
		"workspace_id":  "ws-1",
		"request_id":    "req-1",
		"provider_name": "sub2api",
		"model":         "gpt-5.4",
		"started_at":    "2026-04-02T10:00:00Z",
		"finished_at":   "2026-04-02T10:00:03Z",
		"input_tokens":  100,
		"output_tokens": 40,
		"total_tokens":  140,
		"status":        "completed",
	})

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
}

func TestSessionEventIngest_CreatesPromptEvent(t *testing.T) {
	env := setupFullTestEnv(t)

	w := doFullRequest(env, http.MethodPost, "/api/v1/session-events", map[string]any{
		"event_id":      "event-1",
		"session_id":    "11111111-1111-1111-1111-111111111111",
		"workspace_id":  "ws-1",
		"event_type":    "user_prompt_submit",
		"source":        "codex_hook",
		"captured_at":   "2026-04-02T10:00:00Z",
		"raw_payload":   map[string]any{"prompt": "explain the diff"},
	})

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/handler -run 'TestSession(UsageIngest_CreatesUsageEvent|EventIngest_CreatesPromptEvent)$' -v
```

Expected:

```text
FAIL ... 404/route missing or handler unresolved
```

- [ ] **Step 3: Add Ent schemas and generate code**

```go
// backend/ent/schema/session_usage_event.go
type SessionUsageEvent struct{ ent.Schema }

func (SessionUsageEvent) Fields() []ent.Field {
	return []ent.Field{
		field.String("event_id").Unique().NotEmpty(),
		field.UUID("session_id", uuid.UUID{}),
		field.String("workspace_id").NotEmpty(),
		field.String("request_id").NotEmpty(),
		field.String("provider_name").NotEmpty(),
		field.String("model").NotEmpty(),
		field.Time("started_at"),
		field.Time("finished_at"),
		field.Int64("input_tokens").Default(0),
		field.Int64("output_tokens").Default(0),
		field.Int64("total_tokens").Default(0),
		field.String("status").NotEmpty(),
		field.JSON("raw_metadata", map[string]any{}).Optional(),
		field.Time("created_at").Default(timeNow),
	}
}

// backend/ent/schema/session_event.go
type SessionEvent struct{ ent.Schema }

func (SessionEvent) Fields() []ent.Field {
	return []ent.Field{
		field.String("event_id").Unique().NotEmpty(),
		field.UUID("session_id", uuid.UUID{}),
		field.String("workspace_id").NotEmpty(),
		field.String("event_type").NotEmpty(),
		field.String("source").NotEmpty(),
		field.Time("captured_at"),
		field.JSON("raw_payload", map[string]any{}).Optional(),
		field.Time("created_at").Default(timeNow),
	}
}
```

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go generate ./ent
```

Expected:

```text
exit 0
```

- [ ] **Step 4: Implement minimal services and HTTP handlers**

```go
// backend/internal/sessionusage/service.go
type CreateUsageEventRequest struct {
	EventID      string         `json:"event_id" binding:"required"`
	SessionID    string         `json:"session_id" binding:"required"`
	WorkspaceID  string         `json:"workspace_id" binding:"required"`
	RequestID    string         `json:"request_id" binding:"required"`
	ProviderName string         `json:"provider_name" binding:"required"`
	Model        string         `json:"model" binding:"required"`
	StartedAt    time.Time      `json:"started_at" binding:"required"`
	FinishedAt   time.Time      `json:"finished_at" binding:"required"`
	InputTokens  int64          `json:"input_tokens"`
	OutputTokens int64          `json:"output_tokens"`
	TotalTokens  int64          `json:"total_tokens"`
	Status       string         `json:"status" binding:"required"`
	RawMetadata  map[string]any `json:"raw_metadata"`
}

func (s *Service) Create(ctx context.Context, req CreateUsageEventRequest) error {
	sessionID, err := uuid.Parse(req.SessionID)
	if err != nil {
		return fmt.Errorf("parse session_id: %w", err)
	}
	return s.entClient.SessionUsageEvent.Create().
		SetEventID(req.EventID).
		SetSessionID(sessionID).
		SetWorkspaceID(req.WorkspaceID).
		SetRequestID(req.RequestID).
		SetProviderName(req.ProviderName).
		SetModel(req.Model).
		SetStartedAt(req.StartedAt.UTC()).
		SetFinishedAt(req.FinishedAt.UTC()).
		SetInputTokens(req.InputTokens).
		SetOutputTokens(req.OutputTokens).
		SetTotalTokens(req.TotalTokens).
		SetStatus(req.Status).
		SetRawMetadata(req.RawMetadata).
		Exec(ctx)
}
```

```go
// backend/internal/handler/session_usage.go
func (h *SessionUsageHandler) CreateUsage(c *gin.Context) {
	var req sessionusage.CreateUsageEventRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.usageService.Create(c.Request.Context(), req); err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Created(c, gin.H{"event_id": req.EventID})
}
```

```go
// backend/internal/handler/router.go
sessionUsageGroup := protected.Group("/session-usage-events")
sessionUsageGroup.POST("", sessionUsageHandler.CreateUsage)

sessionEventGroup := protected.Group("/session-events")
sessionEventGroup.POST("", sessionUsageHandler.CreateEvent)
```

- [ ] **Step 5: Re-run targeted tests**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/sessionusage ./internal/sessionevent ./internal/handler -run 'TestSession(UsageIngest_CreatesUsageEvent|EventIngest_CreatesPromptEvent)$' -v
```

Expected:

```text
PASS
```

- [ ] **Step 6: Commit**

```bash
git add backend/ent/schema/session_usage_event.go \
        backend/ent/schema/session_event.go \
        backend/internal/sessionusage/service.go \
        backend/internal/sessionusage/service_test.go \
        backend/internal/sessionevent/service.go \
        backend/internal/sessionevent/service_test.go \
        backend/internal/handler/session_usage.go \
        backend/internal/handler/session_usage_test.go \
        backend/internal/handler/router.go \
        backend/cmd/server/main.go \
        backend/ent/
git commit -m "feat(backend): add session usage and event ingest"
```

### Task 2: Backend Attribution Reads Local Usage First

**Files:**
- Modify: `backend/internal/attribution/service.go`
- Modify: `backend/internal/attribution/service_test.go`
- Modify: `backend/internal/handler/session.go`
- Modify: `backend/internal/handler/session_detail_http_test.go`
- Test: `backend/internal/attribution/service_test.go`
- Test: `backend/internal/handler/session_detail_http_test.go`

- [ ] **Step 1: Write the failing attribution test**

```go
func TestSettlePR_PrefersSessionUsageEventsOverRelayLedger(t *testing.T) {
	ctx := context.Background()
	// Seed one matched checkpoint and one session_usage_event in its interval.
	// fakeRelayProvider.ListUsageLogsByAPIKeyExact must panic if called.
	// Expect result.PrimaryTokenCount == 140 from local usage event.
}
```

- [ ] **Step 2: Run the attribution test and verify it fails**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/attribution -run 'TestSettlePR_PrefersSessionUsageEventsOverRelayLedger$' -v
```

Expected:

```text
FAIL with relay-ledger path still used or token count mismatch
```

- [ ] **Step 3: Implement local-usage-first interval loading**

```go
func (s *Service) loadIntervalUsage(ctx context.Context, sessionID uuid.UUID, from, to time.Time, fallbackAPIKeyID int64) (int64, float64, map[string]any, error) {
	events, err := s.entClient.SessionUsageEvent.Query().
		Where(
			sessionusageevent.SessionIDEQ(sessionID),
			sessionusageevent.StartedAtGTE(from),
			sessionusageevent.FinishedAtLTE(to),
			sessionusageevent.StatusEQ("completed"),
		).
		All(ctx)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("query session usage events: %w", err)
	}
	if len(events) > 0 {
		var totalTokens int64
		for _, ev := range events {
			totalTokens += ev.TotalTokens
		}
		return totalTokens, 0, map[string]any{
			"source":      "local_proxy",
			"event_count": len(events),
		}, nil
	}
	return s.loadRelayLedgerFallback(ctx, fallbackAPIKeyID, from, to)
}
```

- [ ] **Step 4: Add session detail edges for usage/events**

```go
// backend/internal/handler/session.go
usageEvents, _ := sessionEntity.QuerySessionUsageEvents().
	Order(ent.Desc(sessionusageevent.FieldStartedAt)).
	Limit(100).
	All(c.Request.Context())

sessionEvents, _ := sessionEntity.QuerySessionEvents().
	Order(ent.Desc(sessionevent.FieldCapturedAt)).
	Limit(100).
	All(c.Request.Context())

edges["session_usage_events"] = usageEvents
edges["session_events"] = sessionEvents
```

- [ ] **Step 5: Re-run targeted tests**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/attribution ./internal/handler -run 'Test(SettlePR_PrefersSessionUsageEventsOverRelayLedger|SessionDetail)' -v
```

Expected:

```text
PASS
```

- [ ] **Step 6: Commit**

```bash
git add backend/internal/attribution/service.go \
        backend/internal/attribution/service_test.go \
        backend/internal/handler/session.go \
        backend/internal/handler/session_detail_http_test.go
git commit -m "feat(backend): prefer local session usage during attribution"
```

### Task 3: Proxy Runtime Bundle And Process Lifecycle

**Files:**
- Create: `ae-cli/internal/proxy/config.go`
- Create: `ae-cli/internal/proxy/server.go`
- Create: `ae-cli/internal/proxy/server_test.go`
- Modify: `ae-cli/internal/session/runtime.go`
- Modify: `ae-cli/internal/session/runtime_test.go`
- Modify: `ae-cli/internal/session/manager.go`
- Modify: `ae-cli/internal/session/session_test.go`
- Modify: `ae-cli/cmd/start.go`
- Modify: `ae-cli/cmd/stop.go`
- Test: `ae-cli/internal/proxy/server_test.go`
- Test: `ae-cli/internal/session/session_test.go`

- [ ] **Step 1: Write the failing proxy lifecycle test**

```go
func TestManagerStartLaunchesLocalProxyAndStoresRuntimeMetadata(t *testing.T) {
	state, rt := startSessionWithFakeBootstrap(t)

	if rt.Proxy == nil {
		t.Fatal("expected runtime bundle to contain proxy metadata")
	}
	if rt.Proxy.ListenAddr == "" || rt.Proxy.AuthToken == "" || rt.Proxy.PID == 0 {
		t.Fatalf("unexpected proxy metadata: %+v", rt.Proxy)
	}
	if state.ID == "" {
		t.Fatal("expected non-empty session id")
	}
}
```

- [ ] **Step 2: Run the lifecycle test and verify it fails**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/session -run 'TestManagerStartLaunchesLocalProxyAndStoresRuntimeMetadata$' -v
```

Expected:

```text
FAIL because runtime bundle has no proxy metadata and no process is started
```

- [ ] **Step 3: Add runtime bundle fields and proxy child-process launcher**

```go
// ae-cli/internal/proxy/config.go
type RuntimeConfig struct {
	SessionID   string            `json:"session_id"`
	ListenAddr  string            `json:"listen_addr"`
	AuthToken   string            `json:"auth_token"`
	ProviderURL string            `json:"provider_url"`
	ProviderKey string            `json:"provider_key"`
	Headers     map[string]string `json:"headers,omitempty"`
}

// ae-cli/internal/session/runtime.go
type ProxyRuntime struct {
	PID        int    `json:"pid,omitempty"`
	ListenAddr string `json:"listen_addr,omitempty"`
	AuthToken  string `json:"auth_token,omitempty"`
}

type RuntimeBundle struct {
	// existing fields...
	Proxy *ProxyRuntime `json:"proxy,omitempty"`
}
```

```go
// ae-cli/internal/session/manager.go
func (m *Manager) startLocalProxy(rt *RuntimeBundle, provider providerConfig) error {
	cfg := proxy.RuntimeConfig{
		SessionID:   rt.SessionID,
		ListenAddr:  "127.0.0.1:0",
		AuthToken:   randomToken(),
		ProviderURL: provider.BaseURL,
		ProviderKey: provider.APIKey,
	}
	pid, listenAddr, err := proxy.Spawn(cfg)
	if err != nil {
		return err
	}
	rt.Proxy = &session.ProxyRuntime{PID: pid, ListenAddr: listenAddr, AuthToken: cfg.AuthToken}
	return nil
}
```

- [ ] **Step 4: Verify start/stop behavior**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/proxy ./internal/session -run 'Test(ManagerStartLaunchesLocalProxyAndStoresRuntimeMetadata|ManagerStopRemovesProxyRuntime)$' -v
```

Expected:

```text
PASS
```

- [ ] **Step 5: Commit**

```bash
git add ae-cli/internal/proxy/config.go \
        ae-cli/internal/proxy/server.go \
        ae-cli/internal/proxy/server_test.go \
        ae-cli/internal/session/runtime.go \
        ae-cli/internal/session/runtime_test.go \
        ae-cli/internal/session/manager.go \
        ae-cli/internal/session/session_test.go \
        ae-cli/cmd/start.go \
        ae-cli/cmd/stop.go
git commit -m "feat(ae-cli): add session-bound local proxy runtime"
```

### Task 4: OpenAI-Compatible Proxy For Codex

**Files:**
- Create: `ae-cli/internal/proxy/openai.go`
- Modify: `ae-cli/internal/proxy/server.go`
- Modify: `ae-cli/internal/proxy/server_test.go`
- Test: `ae-cli/internal/proxy/server_test.go`

- [ ] **Step 1: Write the failing Codex proxy test**

```go
func TestProxyOpenAIResponses_ForwardsRequestAndRecordsUsage(t *testing.T) {
	srv, recorder := startProxyWithFakeOpenAIUpstream(t)

	resp, err := http.Post(
		"http://"+srv.ListenAddr+"/openai/v1/chat/completions",
		"application/json",
		strings.NewReader(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}]}`),
	)
	if err != nil {
		t.Fatalf("post proxy: %v", err)
	}
	defer resp.Body.Close()

	if recorder.LastUsage.TotalTokens != 140 {
		t.Fatalf("total_tokens = %d, want 140", recorder.LastUsage.TotalTokens)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/proxy -run 'TestProxyOpenAIResponses_ForwardsRequestAndRecordsUsage$' -v
```

Expected:

```text
FAIL because /openai/v1/chat/completions is unhandled
```

- [ ] **Step 3: Implement OpenAI-compatible forwarding and usage extraction**

```go
func (s *Server) handleOpenAIChatCompletions(w http.ResponseWriter, r *http.Request) {
	reqID := newRequestID()
	startedAt := time.Now().UTC()

	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.cfg.ProviderURL+"/chat/completions", r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	copyJSONHeaders(upstreamReq.Header, r.Header)
	upstreamReq.Header.Set("Authorization", "Bearer "+s.cfg.ProviderKey)

	resp, err := s.httpClient.Do(upstreamReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	usage := parseOpenAIUsage(body)
	s.recorder.RecordUsage(UsageEvent{
		RequestID:    reqID,
		ProviderName: "sub2api",
		Model:        usage.Model,
		StartedAt:    startedAt,
		FinishedAt:   time.Now().UTC(),
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		TotalTokens:  usage.TotalTokens,
		Status:       "completed",
	})

	copyResponse(w, resp.StatusCode, resp.Header, body)
}
```

- [ ] **Step 4: Re-run the OpenAI proxy test**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/proxy -run 'TestProxyOpenAIResponses_ForwardsRequestAndRecordsUsage$' -v
```

Expected:

```text
PASS
```

- [ ] **Step 5: Commit**

```bash
git add ae-cli/internal/proxy/openai.go \
        ae-cli/internal/proxy/server.go \
        ae-cli/internal/proxy/server_test.go
git commit -m "feat(ae-cli): proxy codex requests through local openai endpoint"
```

### Task 5: Anthropic-Compatible Proxy For Claude

**Files:**
- Create: `ae-cli/internal/proxy/anthropic.go`
- Modify: `ae-cli/internal/proxy/server.go`
- Modify: `ae-cli/internal/proxy/server_test.go`
- Test: `ae-cli/internal/proxy/server_test.go`

- [ ] **Step 1: Write the failing Claude proxy test**

```go
func TestProxyAnthropicMessages_ForwardsRequestAndRecordsUsage(t *testing.T) {
	srv, recorder := startProxyWithFakeAnthropicUpstream(t)

	reqBody := `{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"hello"}],"max_tokens":64}`
	resp, err := http.Post("http://"+srv.ListenAddr+"/anthropic/v1/messages", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post proxy: %v", err)
	}
	defer resp.Body.Close()

	if recorder.LastUsage.TotalTokens != 210 {
		t.Fatalf("total_tokens = %d, want 210", recorder.LastUsage.TotalTokens)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/proxy -run 'TestProxyAnthropicMessages_ForwardsRequestAndRecordsUsage$' -v
```

Expected:

```text
FAIL because /anthropic/v1/messages is unhandled
```

- [ ] **Step 3: Implement Anthropic-compatible forwarding**

```go
func (s *Server) handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	reqID := newRequestID()
	startedAt := time.Now().UTC()

	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.cfg.ProviderURL+"/v1/messages", r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	copyJSONHeaders(upstreamReq.Header, r.Header)
	upstreamReq.Header.Set("x-api-key", s.cfg.ProviderKey)
	upstreamReq.Header.Set("anthropic-version", firstNonEmptyHeader(r.Header.Get("anthropic-version"), "2023-06-01"))

	resp, err := s.httpClient.Do(upstreamReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	usage := parseAnthropicUsage(body)
	s.recorder.RecordUsage(UsageEvent{
		RequestID:    reqID,
		ProviderName: "sub2api",
		Model:        usage.Model,
		StartedAt:    startedAt,
		FinishedAt:   time.Now().UTC(),
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		TotalTokens:  usage.TotalTokens,
		Status:       "completed",
	})

	copyResponse(w, resp.StatusCode, resp.Header, body)
}
```

- [ ] **Step 4: Re-run the Anthropic proxy test**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/proxy -run 'TestProxyAnthropicMessages_ForwardsRequestAndRecordsUsage$' -v
```

Expected:

```text
PASS
```

- [ ] **Step 5: Commit**

```bash
git add ae-cli/internal/proxy/anthropic.go \
        ae-cli/internal/proxy/server.go \
        ae-cli/internal/proxy/server_test.go
git commit -m "feat(ae-cli): proxy claude requests through local anthropic endpoint"
```

### Task 6: Route Hooks And Local Events Through The Proxy

**Files:**
- Create: `ae-cli/internal/proxy/events.go`
- Modify: `ae-cli/internal/hooks/handler.go`
- Modify: `ae-cli/internal/hooks/handler_test.go`
- Modify: `ae-cli/internal/client/client.go`
- Modify: `ae-cli/internal/client/client_test.go`
- Test: `ae-cli/internal/hooks/handler_test.go`
- Test: `ae-cli/internal/client/client_test.go`

- [ ] **Step 1: Write the failing hook-to-proxy test**

```go
func TestPostCommitSendsEventToLocalProxyBeforeQueueFallback(t *testing.T) {
	repo := initRepoWithCommit2(t)
	proxy := startFakeProxy(t)
	writeRuntimeWithProxy(t, "sess-1", proxy.ListenAddr, proxy.AuthToken)
	writeMarker(t, repo, "sess-1")

	h := NewHandler(nil)
	if err := h.PostCommit(context.Background(), repo); err != nil {
		t.Fatalf("PostCommit: %v", err)
	}

	if proxy.LastEvent.EventType != "post_commit" {
		t.Fatalf("event_type = %q, want %q", proxy.LastEvent.EventType, "post_commit")
	}
}
```

- [ ] **Step 2: Run the hook test to verify it fails**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/hooks -run 'TestPostCommitSendsEventToLocalProxyBeforeQueueFallback$' -v
```

Expected:

```text
FAIL because hooks still only enqueue/backend-upload directly
```

- [ ] **Step 3: Add event ingress client and proxy-first hook path**

```go
// ae-cli/internal/client/client.go
func (c *Client) SendSessionEvent(ctx context.Context, req SessionEventRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal session event: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/session-events", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create session event request: %w", err)
	}
	c.setHeaders(httpReq)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send session event: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected session event status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
```

```go
// ae-cli/internal/hooks/handler.go
if rt, err := session.ReadRuntimeBundle(sessionID); err == nil && rt.Proxy != nil {
	if err := postLocalProxyEvent(rt.Proxy.ListenAddr, rt.Proxy.AuthToken, eventEnvelope{
		EventType: "post_commit",
		SessionID: sessionID,
		Payload:   ev,
	}); err == nil {
		return nil
	}
}
```

- [ ] **Step 4: Re-run hook and client tests**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/hooks ./internal/client -run 'Test(PostCommitSendsEventToLocalProxyBeforeQueueFallback|SendSessionEvent)$' -v
```

Expected:

```text
PASS
```

- [ ] **Step 5: Commit**

```bash
git add ae-cli/internal/proxy/events.go \
        ae-cli/internal/hooks/handler.go \
        ae-cli/internal/hooks/handler_test.go \
        ae-cli/internal/client/client.go \
        ae-cli/internal/client/client_test.go
git commit -m "feat(ae-cli): send local hook events through session proxy"
```

### Task 7: Auto-Configure Codex And Claude During Session Start

**Files:**
- Create: `ae-cli/internal/toolconfig/codex.go`
- Create: `ae-cli/internal/toolconfig/claude.go`
- Create: `ae-cli/internal/toolconfig/toolconfig_test.go`
- Modify: `ae-cli/internal/session/manager.go`
- Modify: `ae-cli/cmd/shell.go`
- Test: `ae-cli/internal/toolconfig/toolconfig_test.go`
- Test: `ae-cli/internal/session/session_test.go`

- [ ] **Step 1: Write the failing tool configuration tests**

```go
func TestWriteCodexSessionConfig(t *testing.T) {
	dir := t.TempDir()
	err := WriteCodexSessionConfig(dir, CodexConfig{
		BaseURL:  "http://127.0.0.1:43123/openai/v1",
		TokenEnv: "AE_LOCAL_PROXY_TOKEN",
		Model:    "gpt-5.4",
	})
	if err != nil {
		t.Fatalf("WriteCodexSessionConfig: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".codex", "config.toml"))
	if !strings.Contains(string(data), "model_provider = \"ae_local_proxy\"") {
		t.Fatalf("missing model_provider in config: %s", string(data))
	}
}

func TestWriteClaudeSessionEnv(t *testing.T) {
	env := ClaudeEnv{
		BaseURL: "http://127.0.0.1:43123/anthropic",
		Token:   "proxy-token-1",
	}
	got := BuildClaudeEnv(env)
	if got["ANTHROPIC_BASE_URL"] == "" || got["ANTHROPIC_AUTH_TOKEN"] == "" {
		t.Fatalf("unexpected claude env: %+v", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/toolconfig -run 'Test(WriteCodexSessionConfig|WriteClaudeSessionEnv)$' -v
```

Expected:

```text
FAIL because toolconfig package does not exist yet
```

- [ ] **Step 3: Implement session-local Codex/Claude config generation**

```go
// ae-cli/internal/toolconfig/codex.go
func WriteCodexSessionConfig(workspaceRoot string, cfg CodexConfig) error {
	content := fmt.Sprintf(`model = %q
model_provider = "ae_local_proxy"

[model_providers.ae_local_proxy]
name = "AI Efficiency Local Proxy"
base_url = %q
env_key = %q
wire_api = "responses"
supports_websockets = false
`, cfg.Model, cfg.BaseURL, cfg.TokenEnv)
	configPath := filepath.Join(workspaceRoot, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return err
	}
	return os.WriteFile(configPath, []byte(content), 0o600)
}
```

```go
// ae-cli/internal/toolconfig/claude.go
func BuildClaudeEnv(cfg ClaudeEnv) map[string]string {
	return map[string]string{
		"ANTHROPIC_BASE_URL":   cfg.BaseURL,
		"ANTHROPIC_AUTH_TOKEN": cfg.Token,
	}
}
```

- [ ] **Step 4: Wire tool config generation into start/shell**

```go
// ae-cli/internal/session/manager.go
if err := toolconfig.WriteCodexSessionConfig(gc.workspaceRoot, toolconfig.CodexConfig{
	BaseURL:  "http://" + rt.Proxy.ListenAddr + "/openai/v1",
	TokenEnv: "AE_LOCAL_PROXY_TOKEN",
	Model:    "gpt-5.4",
}); err != nil {
	return nil, rollback(fmt.Errorf("writing codex config: %w", err))
}
rt.EnvBundle["AE_LOCAL_PROXY_TOKEN"] = rt.Proxy.AuthToken
rt.EnvBundle["ANTHROPIC_BASE_URL"] = "http://" + rt.Proxy.ListenAddr + "/anthropic"
rt.EnvBundle["ANTHROPIC_AUTH_TOKEN"] = rt.Proxy.AuthToken
```

- [ ] **Step 5: Re-run tool config and session tests**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/toolconfig ./internal/session -run 'Test(WriteCodexSessionConfig|WriteClaudeSessionEnv|ManagerStart)' -v
```

Expected:

```text
PASS
```

- [ ] **Step 6: Commit**

```bash
git add ae-cli/internal/toolconfig/codex.go \
        ae-cli/internal/toolconfig/claude.go \
        ae-cli/internal/toolconfig/toolconfig_test.go \
        ae-cli/internal/session/manager.go \
        ae-cli/cmd/shell.go
git commit -m "feat(ae-cli): auto-configure codex and claude for local proxy"
```

### Task 8: End-To-End Verification And Docs

**Files:**
- Modify: `docs/ae-cli/session-pr-attribution.md`
- Modify: `docs/superpowers/specs/2026-04-02-local-session-proxy-design.md` (only if implementation reveals required deltas)
- Test: manual verification commands only

- [ ] **Step 1: Write the end-to-end verification checklist**

```md
1. Start backend on a clean local SQLite DB
2. Run `ae-cli start`
3. Verify local proxy is listening
4. Verify Codex config points to `127.0.0.1:<port>/openai/v1`
5. Verify Claude env points to `127.0.0.1:<port>/anthropic`
6. Send one Codex request and one Claude request through proxy
7. Create an empty git commit
8. Confirm backend has one `session_usage_event`, one `session_event`, and one `commit_checkpoint`
9. Stop session and confirm proxy exits
```

- [ ] **Step 2: Run automated verification**

Run:

```bash
cd /Users/admin/ai-efficiency/backend && go test ./...
cd /Users/admin/ai-efficiency/ae-cli && go test ./...
cd /Users/admin/ai-efficiency/frontend && pnpm test
cd /Users/admin/ai-efficiency/frontend && pnpm build
```

Expected:

```text
all commands exit 0
```

- [ ] **Step 3: Run manual session-proxy verification**

Run:

```bash
cd /Users/admin/ai-efficiency
AE_DEV_LOGIN_ENABLED=true go run ./backend/cmd/server
```

In another shell:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go run . login
go run . start
git -C /Users/admin/ai-efficiency commit --allow-empty -m "proxy e2e"
go run . stop
```

Expected:

```text
- local proxy listens on 127.0.0.1 dynamic port
- backend receives session-usage-events/session-events/checkpoints
- no upstream provider key appears in Codex/Claude user-facing config
```

- [ ] **Step 4: Update operator docs**

```md
## Local Session Proxy

- `ae-cli start` now launches a loopback-only local proxy.
- Codex uses the local OpenAI-compatible endpoint.
- Claude uses the local Anthropic-compatible endpoint.
- Upstream provider keys stay inside the local runtime bundle and are not exposed to tool configs.
- Use `ae-cli stop` to flush events and tear down the proxy.
```

- [ ] **Step 5: Commit**

```bash
git add docs/ae-cli/session-pr-attribution.md \
        docs/superpowers/specs/2026-04-02-local-session-proxy-design.md
git commit -m "docs: document local session proxy rollout"
```

## Self-Review

### Spec coverage

- Local session-bound proxy lifecycle: Task 3
- OpenAI-compatible Codex path: Task 4
- Anthropic-compatible Claude path: Task 5
- Local event ingress and hook aggregation: Task 6
- Automatic tool configuration on `ae-cli start`: Task 7
- Backend ingestion and persistence: Task 1
- Attribution using local usage: Task 2
- Verification and docs: Task 8

### Placeholder scan

- No `TODO`, `TBD`, or “similar to Task N” references remain.
- All commands use exact repository paths and test targets.
- All code steps include concrete function/type names and file paths.

### Type consistency

- `session_usage_events` and `session_events` are the only new backend ingest nouns.
- `ProxyRuntime`, `RuntimeConfig`, and `UsageEvent` are used consistently across proxy runtime tasks.
- `AE_LOCAL_PROXY_TOKEN`, `ANTHROPIC_BASE_URL`, and `ANTHROPIC_AUTH_TOKEN` are the only environment variables injected by this plan for tool routing.

