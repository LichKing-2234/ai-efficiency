# Session Raw Response Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist original upstream response bodies for new `session_usage_events` and expose them in the Sessions UI alongside existing normalized usage fields.

**Architecture:** Keep normalized request-level fields and `raw_metadata` unchanged, and add a separate nullable `raw_response` field on `session_usage_events` for original upstream response bodies. Frontend continues rendering normalized columns by default, but each `Session Usage` row can expand `Raw Response`, while `Agent Usage Snapshots` reuses existing `raw_payload` as `Raw Event`.

**Tech Stack:** Go (`ent`, `gin`, `ae-cli` proxy tests), Vue 3 (`<script setup>`, Vitest), existing session detail API and Ent-generated models.

**Status:** Core implementation and verification complete; commit steps intentionally pending

**Source Of Truth:** `docs/superpowers/specs/2026-04-16-session-raw-response-design.md`

---

## File Structure

### Modify

- `backend/ent/schema/session_usage_event.go`
  Add nullable `raw_response` JSON field to the schema.
- `backend/internal/sessionusage/service.go`
  Accept and persist `raw_response` on usage ingest.
- `backend/internal/sessionusage/service_test.go`
  Cover persistence of `raw_response`.
- `backend/internal/handler/session_usage_test.go`
  Cover HTTP ingest of `raw_response`.
- `ae-cli/internal/client/client.go`
  Add `RawResponse` to `SessionUsageEventRequest`.
- `ae-cli/internal/client/client_test.go`
  Verify request payload includes `raw_response`.
- `ae-cli/internal/proxy/openai.go`
  Persist non-stream OpenAI upstream JSON body into `raw_response`; keep stream path empty.
- `ae-cli/internal/proxy/anthropic.go`
  Persist non-stream Anthropic upstream JSON body into `raw_response`; keep stream path empty.
- `ae-cli/internal/proxy/server_test.go`
  Cover OpenAI/Anthropic non-stream `raw_response` persistence and stream omission.
- `frontend/src/types/index.ts`
  Add `raw_response` to `SessionUsageEvent` and ensure `raw_payload` stays typed on `AgentMetadataEvent`.
- `frontend/src/views/sessions/SessionDetailView.vue`
  Replace generic `Raw` affordance with `Raw Response` for `Session Usage` and `Raw Event` for `Agent Usage Snapshots`; render `raw_response` / `raw_payload`.
- `frontend/src/__tests__/session-detail-view.test.ts`
  Cover expanding `Raw Response` and `Raw Event`, including empty states.
- `docs/ae-cli/session-pr-attribution.md`
  Document that request-level raw view now comes from `session_usage_events.raw_response`.
- `docs/superpowers/specs/2026-04-02-local-session-proxy-design.md`
  Document `raw_response` as the original upstream body for new usage rows.

### Existing files to read before implementation

- `docs/superpowers/specs/2026-04-16-session-raw-response-design.md`
- `backend/internal/sessionusage/service.go`
- `backend/internal/handler/session_usage.go`
- `ae-cli/internal/proxy/openai.go`
- `ae-cli/internal/proxy/anthropic.go`
- `frontend/src/views/sessions/SessionDetailView.vue`

---

### Task 1: Add `raw_response` To Backend Usage Contract

**Files:**
- Modify: `backend/ent/schema/session_usage_event.go`
- Modify: `backend/internal/sessionusage/service.go`
- Modify: `backend/internal/sessionusage/service_test.go`
- Modify: `backend/internal/handler/session_usage_test.go`
- Test: `backend/internal/sessionusage/service_test.go`
- Test: `backend/internal/handler/session_usage_test.go`

- [x] **Step 1: Write the failing service test for `raw_response` persistence**

Add this test in `backend/internal/sessionusage/service_test.go` near the existing create/persist tests:

```go
func TestCreateUsageEventPersistsRawResponse(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()

	ctx := t.Context()
	u := client.User.Create().
		SetUsername("u1").
		SetEmail("u1@example.com").
		SetAuthSource(user.AuthSourceLdap).
		SaveX(ctx)
	sp := client.ScmProvider.Create().
		SetName("github-test").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)
	repo := client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("demo").
		SetFullName("org/demo").
		SetCloneURL("https://github.com/org/demo.git").
		SetDefaultBranch("main").
		SaveX(ctx)
	sess := client.Session.Create().
		SetRepoConfigID(repo.ID).
		SetUserID(u.ID).
		SetBranch("main").
		SaveX(ctx)

	svc := NewService(client)
	started := time.Now().UTC().Truncate(time.Second)
	finished := started.Add(2 * time.Second)
	rawResponse := map[string]any{
		"id":    "resp_1",
		"model": "gpt-5.4",
		"usage": map[string]any{
			"input_tokens": 27,
			"output_tokens": 10,
			"total_tokens": 37,
		},
	}

	err := svc.Create(ctx, u.ID, CreateUsageEventRequest{
		EventID:      "usage-raw-response-1",
		SessionID:    sess.ID.String(),
		WorkspaceID:  "ws-1",
		RequestID:    "req-1",
		ProviderName: "sub2api",
		Model:        "gpt-5.4",
		StartedAt:    started,
		FinishedAt:   finished,
		InputTokens:  27,
		OutputTokens: 10,
		TotalTokens:  37,
		Status:       "completed",
		RawResponse:  rawResponse,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	ev := client.SessionUsageEvent.Query().
		Where(sessionusageevent.EventIDEQ("usage-raw-response-1")).
		OnlyX(ctx)
	if ev.RawResponse == nil || ev.RawResponse["id"] != "resp_1" {
		t.Fatalf("raw_response = %+v, want id=resp_1", ev.RawResponse)
	}
}
```

- [x] **Step 2: Run the focused service test and verify it fails**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/sessionusage -run 'TestCreateUsageEventPersistsRawResponse$'
```

Expected:

```text
FAIL ... unknown field RawResponse / ev.RawResponse undefined
```

- [x] **Step 3: Add `raw_response` to the Ent schema and request contract**

Update `backend/ent/schema/session_usage_event.go`:

```go
field.JSON("raw_response", map[string]any{}).
	Optional(),
```

Update `backend/internal/sessionusage/service.go` request type and persistence:

```go
type CreateUsageEventRequest struct {
	// ...
	RawMetadata map[string]any `json:"raw_metadata"`
	RawResponse map[string]any `json:"raw_response"`
}
```

```go
if req.RawMetadata != nil {
	create.SetRawMetadata(req.RawMetadata)
}
if req.RawResponse != nil {
	create.SetRawResponse(req.RawResponse)
}
```

- [x] **Step 4: Add the failing HTTP test for ingesting `raw_response`**

Add this test in `backend/internal/handler/session_usage_test.go`:

```go
func TestSessionUsageIngestStoresRawResponse(t *testing.T) {
	env := setupFullTestEnv(t)

	w := doFullRequest(env, http.MethodPost, "/api/v1/session-usage-events", map[string]any{
		"event_id":      "usage-raw-http-1",
		"session_id":    "11111111-1111-1111-1111-111111111111",
		"workspace_id":  "ws-1",
		"request_id":    "req-raw-1",
		"provider_name": "sub2api",
		"model":         "gpt-5.4",
		"started_at":    "2026-04-16T10:00:00Z",
		"finished_at":   "2026-04-16T10:00:01Z",
		"input_tokens":  27,
		"output_tokens": 10,
		"total_tokens":  37,
		"status":        "completed",
		"raw_response": map[string]any{
			"id": "resp_1",
		},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusCreated, w.Body.String())
	}

	ev := env.client.SessionUsageEvent.Query().
		Where(sessionusageevent.EventIDEQ("usage-raw-http-1")).
		OnlyX(context.Background())
	if ev.RawResponse["id"] != "resp_1" {
		t.Fatalf("raw_response = %+v, want id=resp_1", ev.RawResponse)
	}
}
```

- [x] **Step 5: Run the focused HTTP test and verify it fails before the service contract is wired through**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/handler -run 'TestSessionUsageIngestStoresRawResponse$'
```

Expected:

```text
FAIL ... raw_response missing / request struct does not accept raw_response
```

- [x] **Step 6: Wire `raw_response` through the HTTP/service boundary**

No new handler logic should be needed beyond the updated request struct, but make sure `session_usage.go` still binds into `CreateUsageEventRequest` and that generated Ent code is refreshed if required by the repo workflow.

- [x] **Step 7: Re-run the focused backend tests and verify they pass**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/sessionusage -run 'TestCreateUsageEventPersistsRawResponse$'
go test ./internal/handler -run 'TestSessionUsageIngestStoresRawResponse$'
```

Expected:

```text
ok  	github.com/ai-efficiency/backend/internal/sessionusage	...
ok  	github.com/ai-efficiency/backend/internal/handler	...
```

- [ ] **Step 8: Commit backend contract changes**

```bash
git add backend/ent/schema/session_usage_event.go \
        backend/internal/sessionusage/service.go \
        backend/internal/sessionusage/service_test.go \
        backend/internal/handler/session_usage_test.go
git commit -m "feat(backend): persist session usage raw responses"
```

---

### Task 2: Persist Raw Upstream Bodies In `ae-cli` Proxy

**Files:**
- Modify: `ae-cli/internal/client/client.go`
- Modify: `ae-cli/internal/client/client_test.go`
- Modify: `ae-cli/internal/proxy/openai.go`
- Modify: `ae-cli/internal/proxy/anthropic.go`
- Modify: `ae-cli/internal/proxy/server_test.go`
- Test: `ae-cli/internal/client/client_test.go`
- Test: `ae-cli/internal/proxy/server_test.go`

- [x] **Step 1: Write the failing client test for `raw_response`**

Extend `TestSendSessionUsageEvent` in `ae-cli/internal/client/client_test.go` with:

```go
if req.RawResponse["id"] != "resp_1" {
	t.Fatalf("raw_response = %+v, want id=resp_1", req.RawResponse)
}
```

And send:

```go
RawResponse: map[string]any{"id": "resp_1"},
```

- [x] **Step 2: Run the focused client test and verify it fails**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/client -run 'TestSendSessionUsageEvent$'
```

Expected:

```text
FAIL ... RawResponse undefined
```

- [x] **Step 3: Add `RawResponse` to `SessionUsageEventRequest`**

Update `ae-cli/internal/client/client.go`:

```go
type SessionUsageEventRequest struct {
	// ...
	RawMetadata map[string]any `json:"raw_metadata,omitempty"`
	RawResponse map[string]any `json:"raw_response,omitempty"`
}
```

- [x] **Step 4: Write the failing proxy tests for raw response persistence**

Extend `ae-cli/internal/proxy/server_test.go` with these cases:

```go
func TestProxyOpenAIUsageUploadIncludesRawResponse(t *testing.T) {
	// upstream returns JSON body with id/model/usage
	// backend usage ingest receives req.RawResponse["id"] == "resp-1"
}

func TestProxyAnthropicUsageUploadIncludesRawResponse(t *testing.T) {
	// upstream returns Anthropic JSON body with id/model/usage
	// backend usage ingest receives req.RawResponse["id"] == "msg_1"
}

func TestProxyOpenAIStreamingUsageUploadLeavesRawResponseEmpty(t *testing.T) {
	// SSE path still uploads usage, but req.RawResponse must be nil
}

func TestProxyAnthropicStreamingUsageUploadLeavesRawResponseEmpty(t *testing.T) {
	// Anthropic SSE path still uploads usage, but req.RawResponse must be nil
}
```

For the backend assertion payloads, check:

```go
if usageReq.RawResponse["id"] != "resp-1" { ... }
if usageReq.RawResponse != nil { ... } // stream cases
```

- [x] **Step 5: Run the focused proxy tests and verify they fail**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/proxy -run 'TestProxy(OpenAI|Anthropic).*(RawResponse|LeavesRawResponseEmpty)'
```

Expected:

```text
FAIL ... raw_response missing in backend request
```

- [x] **Step 6: Persist raw response in the OpenAI non-stream path**

In `ae-cli/internal/proxy/openai.go`, when non-stream `io.ReadAll(resp.Body)` succeeds and JSON parses, attach:

```go
rawResponse := map[string]any{}
if err := json.Unmarshal(body, &rawResponse); err == nil && len(rawResponse) > 0 {
	// attach to UsageEvent or pass through request construction
}
```

Keep streaming behavior empty in this phase:

```go
// SSE responses are not persisted as raw_response in this phase.
```

- [x] **Step 7: Persist raw response in the Anthropic non-stream path**

In `ae-cli/internal/proxy/anthropic.go`, mirror the same non-stream behavior:

```go
rawResponse := map[string]any{}
if err := json.Unmarshal(body, &rawResponse); err == nil && len(rawResponse) > 0 {
	// attach to UsageEvent or pass through request construction
}
```

Do not persist SSE chunk streams as `raw_response` yet.

- [x] **Step 8: Re-run focused client/proxy tests and verify they pass**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/client -run 'TestSendSessionUsageEvent$'
go test ./internal/proxy -run 'TestProxy(OpenAI|Anthropic).*(RawResponse|LeavesRawResponseEmpty)'
```

Expected:

```text
ok  	github.com/ai-efficiency/ae-cli/internal/client	...
ok  	github.com/ai-efficiency/ae-cli/internal/proxy	...
```

- [ ] **Step 9: Commit proxy/client changes**

```bash
git add ae-cli/internal/client/client.go \
        ae-cli/internal/client/client_test.go \
        ae-cli/internal/proxy/openai.go \
        ae-cli/internal/proxy/anthropic.go \
        ae-cli/internal/proxy/server_test.go
git commit -m "feat(ae-cli): store session usage raw responses"
```

---

### Task 3: Expose `raw_response` In Session Detail API And Types

**Files:**
- Modify: `frontend/src/types/index.ts`
- Test: `frontend/src/__tests__/session-detail-view.test.ts`

- [x] **Step 1: Write the failing frontend type/test usage for `raw_response`**

Update the test fixture in `frontend/src/__tests__/session-detail-view.test.ts` to include:

```ts
session_usage_events: [{
  // ...
  raw_response: {
    id: 'resp_1',
    usage: {
      input_tokens: 27,
      input_tokens_details: { cached_tokens: 9 },
    },
  },
}]
```

Add assertions later in Task 4 for the expanded raw response view.

- [x] **Step 2: Extend the `SessionUsageEvent` type**

Update `frontend/src/types/index.ts`:

```ts
raw_response?: Record<string, unknown>
```

- [ ] **Step 3: Commit the type update (or combine with Task 4 if preferred)**

```bash
git add frontend/src/types/index.ts frontend/src/__tests__/session-detail-view.test.ts
git commit -m "refactor(frontend): type session usage raw responses"
```

---

### Task 4: Add `Raw Response` / `Raw Event` Expanders In Session Detail View

**Files:**
- Modify: `frontend/src/views/sessions/SessionDetailView.vue`
- Modify: `frontend/src/__tests__/session-detail-view.test.ts`
- Test: `frontend/src/__tests__/session-detail-view.test.ts`

- [x] **Step 1: Write the failing UI test for expanded raw response / raw event**

Add a test in `frontend/src/__tests__/session-detail-view.test.ts`:

```ts
it('expands raw response and raw event payloads', async () => {
  // mount with one session_usage_event.raw_response and one agent_metadata_event.raw_payload
  // find buttons by text: "Raw Response" and "Raw Event"
  // click them and assert pretty JSON is visible
  // mount a second fixture with no raw_response/raw_payload and assert:
  // "No raw response." / "No raw event."
})
```

Include exact assertions:

```ts
expect(wrapper.text()).toContain('"cached_tokens": 9')
expect(wrapper.text()).toContain('"reasoning_output_tokens": 10')
expect(wrapper.text()).toContain('No raw response.')
expect(wrapper.text()).toContain('No raw event.')
```

- [x] **Step 2: Run the focused frontend test and verify it fails**

Run:

```bash
cd /Users/admin/ai-efficiency/frontend
pnpm test src/__tests__/session-detail-view.test.ts
```

Expected:

```text
FAIL ... no "Raw Response" / "Raw Event" controls found
```

- [x] **Step 3: Add raw-expansion state and JSON formatting helpers**

In `frontend/src/views/sessions/SessionDetailView.vue`, add:

```ts
const expandedUsageRaw = ref<string[]>([])
const expandedAgentRaw = ref<string[]>([])

function formatRawJSON(value?: unknown, emptyLabel = 'No raw data.') {
  if (value == null) return emptyLabel
  return JSON.stringify(value, null, 2)
}
```

Keep toggles keyed by:

```ts
function usageRawKey(usage: SessionUsageEvent) {
  return usage.event_id
}

function agentRawKey(event: AgentMetadataEvent) {
  return `${event.source}-${event.source_session_id || event.observed_at}`
}
```

- [x] **Step 4: Render `Raw Response` for session usage**

In the `Session Usage` table, replace the generic affordance with:

```vue
<button ...>Raw Response</button>
```

Expanded detail row:

```vue
<pre ...>{{ formatRawJSON(usage.raw_response, 'No raw response.') }}</pre>
```

- [x] **Step 5: Render `Raw Event` for agent snapshots**

In the `Agent Usage Snapshots` table, replace the generic affordance with:

```vue
<button ...>Raw Event</button>
```

Expanded detail row:

```vue
<pre ...>{{ formatRawJSON(event.raw_payload, 'No raw event.') }}</pre>
```

- [x] **Step 6: Re-run the focused frontend test and verify it passes**

Run:

```bash
cd /Users/admin/ai-efficiency/frontend
pnpm test src/__tests__/session-detail-view.test.ts
```

Expected:

```text
PASS ... expands raw response and raw event payloads
```

- [x] **Step 7: Run frontend full verification**

Run:

```bash
cd /Users/admin/ai-efficiency/frontend
pnpm test
pnpm build
```

Expected:

```text
16 passed / all tests pass
vite build succeeds
```

- [ ] **Step 8: Commit frontend view changes**

```bash
git add frontend/src/views/sessions/SessionDetailView.vue \
        frontend/src/__tests__/session-detail-view.test.ts \
        frontend/src/types/index.ts
git commit -m "feat(frontend): show raw session responses and raw events"
```

---

### Task 5: Update Documentation To Match The New Contract

**Files:**
- Modify: `docs/ae-cli/session-pr-attribution.md`
- Modify: `docs/superpowers/specs/2026-04-02-local-session-proxy-design.md`
- Test: documentation review only

- [x] **Step 1: Update the operator guide**

Add to `docs/ae-cli/session-pr-attribution.md`:

```md
- `Session Usage` raw view now renders `session_usage_events.raw_response` (original upstream response body) for newly ingested non-stream requests.
- `Agent Usage Snapshots` raw view renders `agent_metadata_events.raw_payload` (original captured event/snapshot payload).
```

Clarify that `raw_metadata` remains normalized metadata and historical rows may not have `raw_response`.

- [x] **Step 2: Update the proxy design spec**

Add to `docs/superpowers/specs/2026-04-02-local-session-proxy-design.md`:

```md
- `session_usage_events.raw_response` stores the original upstream response body for new non-stream OpenAI/Anthropic requests.
- `raw_metadata` remains a normalized metadata envelope rather than the original upstream payload.
```

- [x] **Step 3: Review docs for consistency**

Run:

```bash
rg -n "raw_response|raw_metadata|Raw Response|Raw Event" docs/ae-cli/session-pr-attribution.md docs/superpowers/specs/2026-04-02-local-session-proxy-design.md docs/superpowers/specs/2026-04-16-session-raw-response-design.md
```

Expected:

```text
All three documents describe the same contract without contradiction.
```

- [ ] **Step 4: Commit docs updates**

```bash
git add docs/ae-cli/session-pr-attribution.md \
        docs/superpowers/specs/2026-04-02-local-session-proxy-design.md
git commit -m "docs(specs): document session raw response contract"
```

---

## Self-Review

- Spec coverage: backend storage, proxy persistence, frontend raw display, and docs alignment are all covered by Tasks 1-5.
- Placeholder scan: no TBD/TODO placeholders remain; every task names exact files, tests, and commands.
- Type consistency: the plan uses `raw_response` only for `session_usage_events` and `raw_payload` only for `agent_metadata_events`, matching the approved spec.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-04-16-session-raw-response.md`. Two execution options:

1. Subagent-Driven (recommended) - I dispatch a fresh subagent per task, review between tasks, fast iteration

2. Inline Execution - Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?
