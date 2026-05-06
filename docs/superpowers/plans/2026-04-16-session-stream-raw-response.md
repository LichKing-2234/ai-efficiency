# Session Stream Raw Response Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Preserve original upstream streaming responses in `session_usage_events.raw_response` using a stable envelope so `Raw Response` works for normal Claude and other stream-first flows.

**Architecture:** Reuse the existing nullable `raw_response` JSON field, but normalize its shape for all new rows. Non-stream requests will store `{kind:"json", body:{...}}`; stream requests will store `{kind:"sse", events:[...]}` built from parsed upstream SSE events. The frontend raw viewer remains schema-agnostic and simply pretty-prints the stored envelope.

**Tech Stack:** Go (`ae-cli` proxy, existing proxy tests), Vue 3 (`SessionDetailView`, Vitest), existing backend `session_usage_events` contract, existing docs/spec workflow.

**Status:** Core implementation and verification complete; commit steps intentionally pending

**Source Of Truth:** `docs/superpowers/specs/2026-04-16-session-stream-raw-response-design.md`

---

## File Structure

### Modify

- `ae-cli/internal/proxy/openai.go`
  Wrap non-stream raw responses as `{kind:"json", body:...}` and capture parsed SSE events as `{kind:"sse", events:[...]}`.
- `ae-cli/internal/proxy/anthropic.go`
  Mirror the same raw-response envelope for Anthropic non-stream and SSE paths.
- `ae-cli/internal/proxy/server_test.go`
  Update raw-response tests to assert the wrapped JSON shape and add Anthropic/OpenAI streaming envelope coverage.
- `frontend/src/__tests__/session-detail-view.test.ts`
  Update raw-response fixtures/assertions to expect wrapped JSON/SSE envelopes.
- `docs/ae-cli/session-pr-attribution.md`
  Document that `raw_response` now uses `{kind:"json"|"sse"}` envelopes.
- `docs/superpowers/specs/2026-04-02-local-session-proxy-design.md`
  Record that streaming request rows now preserve parsed SSE event arrays in `raw_response`.

### Existing files to read before implementation

- `docs/superpowers/specs/2026-04-16-session-stream-raw-response-design.md`
- `docs/superpowers/specs/2026-04-16-session-raw-response-design.md`
- `ae-cli/internal/proxy/openai.go`
- `ae-cli/internal/proxy/anthropic.go`
- `ae-cli/internal/proxy/server_test.go`
- `frontend/src/views/sessions/SessionDetailView.vue`

---

### Task 1: Add Streaming Raw Response Coverage For OpenAI And Anthropic

**Files:**
- Modify: `ae-cli/internal/proxy/server_test.go`
- Test: `ae-cli/internal/proxy/server_test.go`

- [x] **Step 1: Write the failing OpenAI stream raw-response envelope assertion**

Update the existing OpenAI streaming raw-response test in `ae-cli/internal/proxy/server_test.go` so it expects:

```go
if usageReq.RawResponse["kind"] != "sse" {
	t.Fatalf("raw_response.kind = %v, want sse", usageReq.RawResponse["kind"])
}
events, ok := usageReq.RawResponse["events"].([]any)
if !ok || len(events) != 2 {
	t.Fatalf("raw_response.events = %#v, want 2 parsed events", usageReq.RawResponse["events"])
}
first, _ := events[0].(map[string]any)
if first["event"] != "response.created" {
	t.Fatalf("first event = %v, want response.created", first["event"])
}
last, _ := events[len(events)-1].(map[string]any)
if last["event"] != "response.completed" {
	t.Fatalf("last event = %v, want response.completed", last["event"])
}
```

This replaces the old expectation that streaming `RawResponse` is `nil`.

- [x] **Step 2: Write the failing Anthropic stream raw-response envelope assertion**

Update the existing Anthropic streaming raw-response test in `ae-cli/internal/proxy/server_test.go` so it expects:

```go
if usageReq.RawResponse["kind"] != "sse" {
	t.Fatalf("raw_response.kind = %v, want sse", usageReq.RawResponse["kind"])
}
events, ok := usageReq.RawResponse["events"].([]any)
if !ok || len(events) != 2 {
	t.Fatalf("raw_response.events = %#v, want 2 parsed events", usageReq.RawResponse["events"])
}
first, _ := events[0].(map[string]any)
if first["event"] != "message_start" {
	t.Fatalf("first event = %v, want message_start", first["event"])
}
last, _ := events[len(events)-1].(map[string]any)
if last["event"] != "message_delta" {
	t.Fatalf("last event = %v, want message_delta", last["event"])
}
```

- [x] **Step 3: Write the failing non-stream envelope assertions**

Update the non-stream raw-response tests so they expect wrapped JSON instead of a bare object:

```go
if usageReq.RawResponse["kind"] != "json" {
	t.Fatalf("raw_response.kind = %v, want json", usageReq.RawResponse["kind"])
}
body, ok := usageReq.RawResponse["body"].(map[string]any)
if !ok || body["id"] != "resp-1" {
	t.Fatalf("raw_response.body = %#v, want id=resp-1", usageReq.RawResponse["body"])
}
```

Mirror the same for Anthropic:

```go
if usageReq.RawResponse["kind"] != "json" {
	t.Fatalf("raw_response.kind = %v, want json", usageReq.RawResponse["kind"])
}
body, ok := usageReq.RawResponse["body"].(map[string]any)
if !ok || body["id"] != "msg_1" {
	t.Fatalf("raw_response.body = %#v, want id=msg_1", usageReq.RawResponse["body"])
}
```

- [x] **Step 4: Run the focused proxy tests and verify they fail**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/proxy -run 'TestProxy(OpenAI.*RawResponse|OpenAIStreamingUsageUploadLeavesRawResponseEmpty|Anthropic.*RawResponse|AnthropicStreamingUsageUploadLeavesRawResponseEmpty)'
```

Expected:

```text
FAIL ... raw_response.kind missing / expected sse envelope but got nil
```

- [ ] **Step 5: Commit the red test changes only if you intentionally checkpoint test-first work**

```bash
git add ae-cli/internal/proxy/server_test.go
git commit -m "test(ae-cli): define stream raw response envelopes"
```

Leave unchecked if you do not make an intermediate commit.

---

### Task 2: Implement Wrapped `raw_response` Envelopes In OpenAI Proxy

**Files:**
- Modify: `ae-cli/internal/proxy/openai.go`
- Test: `ae-cli/internal/proxy/server_test.go`

- [x] **Step 1: Add helpers for wrapped raw responses**

In `ae-cli/internal/proxy/openai.go`, add helpers:

```go
func wrapJSONRawResponse(body []byte) map[string]any {
	obj := parseRawResponseObject(body)
	if obj == nil {
		return nil
	}
	return map[string]any{
		"kind": "json",
		"body": obj,
	}
}

func wrapSSERawResponse(events []map[string]any) map[string]any {
	if len(events) == 0 {
		return nil
	}
	items := make([]any, 0, len(events))
	for _, event := range events {
		items = append(items, event)
	}
	return map[string]any{
		"kind":   "sse",
		"events": items,
	}
}
```

- [x] **Step 2: Extend the OpenAI SSE accumulator to capture raw events**

Augment `openAISSEUsageAccumulator` with a `rawEvents []map[string]any` field and, inside `consumeLine`, capture parsed SSE events:

```go
eventName := ""
if strings.HasPrefix(line, "event:") {
	eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
	return
}
```

Instead of early-returning on non-`data:` lines only, preserve the most recent event name and, when a JSON `data:` payload parses successfully, append:

```go
a.rawEvents = append(a.rawEvents, map[string]any{
	"event": eventName,
	"data":  parsedMap,
})
```

Use a `map[string]any` parsed from the payload, not the typed struct, so the stored raw event remains close to upstream shape.

- [x] **Step 3: Persist wrapped non-stream OpenAI raw responses**

In the OpenAI non-stream success path, replace:

```go
RawResponse: parseRawResponseObject(body),
```

with:

```go
RawResponse: wrapJSONRawResponse(body),
```

- [x] **Step 4: Persist wrapped OpenAI streaming raw responses**

After stream completion, change `recordOpenAIUsage` call path so it receives:

```go
rawResponse := wrapSSERawResponse(acc.RawEvents())
```

and forwards that through `UsageEvent.RawResponse`.

Add a method:

```go
func (a *openAISSEUsageAccumulator) RawEvents() []map[string]any {
	return append([]map[string]any(nil), a.rawEvents...)
}
```

- [x] **Step 5: Re-run focused OpenAI proxy tests and verify they pass**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/proxy -run 'TestProxyOpenAI(UsageUploadsToBackend|ResponsesUsageUploadsCacheAndReasoningDetailsToRawMetadata|StreamingUsageUploadLeavesRawResponseEmpty)'
```

Expected:

```text
ok  	github.com/ai-efficiency/ae-cli/internal/proxy	...
```

---

### Task 3: Implement Wrapped `raw_response` Envelopes In Anthropic Proxy

**Files:**
- Modify: `ae-cli/internal/proxy/anthropic.go`
- Test: `ae-cli/internal/proxy/server_test.go`

- [x] **Step 1: Persist wrapped non-stream Anthropic raw responses**

In the Anthropic non-stream success path, replace:

```go
RawResponse: parseRawResponseObject(body),
```

with:

```go
RawResponse: wrapJSONRawResponse(body),
```

Reuse the helper introduced in `openai.go` by keeping it in the `proxy` package.

- [x] **Step 2: Extend the Anthropic SSE accumulator to capture raw events**

Mirror the OpenAI approach:

- keep the current usage accumulation logic
- add `rawEvents []map[string]any`
- track the current `event:` label
- append parsed JSON `data:` payloads as:

```go
map[string]any{
	"event": eventName,
	"data":  parsedMap,
}
```

- [x] **Step 3: Persist wrapped Anthropic streaming raw responses**

When recording Anthropic streaming usage, forward:

```go
RawResponse: wrapSSERawResponse(acc.RawEvents()),
```

instead of leaving the field empty.

- [x] **Step 4: Re-run focused Anthropic proxy tests and verify they pass**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./internal/proxy -run 'TestProxyAnthropicMessages(BackendUploadPreservesCacheBreakdown|StreamingPassthroughAndRecordsUsage|AnthropicStreamingUsageUploadLeavesRawResponseEmpty)'
```

Expected:

```text
ok  	github.com/ai-efficiency/ae-cli/internal/proxy	...
```

---

### Task 4: Update Frontend Tests And Generic Raw Viewer Expectations

**Files:**
- Modify: `frontend/src/__tests__/session-detail-view.test.ts`
- Test: `frontend/src/__tests__/session-detail-view.test.ts`

- [x] **Step 1: Write the failing frontend test for wrapped raw response envelopes**

Update the `Session Usage` fixture in `frontend/src/__tests__/session-detail-view.test.ts` from:

```ts
raw_response: {
  id: 'resp_1',
  usage: { ... },
}
```

to:

```ts
raw_response: {
  kind: 'json',
  body: {
    id: 'resp_1',
    usage: {
      input_tokens: 100,
      input_tokens_details: { cached_tokens: 333 },
      output_tokens: 40,
      output_tokens_details: { reasoning_tokens: 444 },
      total_tokens: 140,
    },
  },
}
```

Add one streaming case:

```ts
raw_response: {
  kind: 'sse',
  events: [
    {
      event: 'response.created',
      data: { type: 'response.created' },
    },
    {
      event: 'response.completed',
      data: {
        type: 'response.completed',
        response: {
          usage: {
            input_tokens_details: { cached_tokens: 3 },
          },
        },
      },
    },
  ],
}
```

- [x] **Step 2: Run the focused frontend test and verify it fails**

Run:

```bash
cd /Users/admin/ai-efficiency/frontend
pnpm test src/__tests__/session-detail-view.test.ts
```

Expected:

```text
FAIL ... expected wrapped json/sse raw response structure
```

- [x] **Step 3: Keep the UI generic and pretty-print the stored envelope**

No UI restructuring should be needed if `Raw Response` already pretty-prints `usage.raw_response`.

If the current assertions fail only because fixtures changed, keep the view logic unchanged and update the tests to match the new wrapped shape.

- [x] **Step 4: Re-run the focused frontend test and verify it passes**

Run:

```bash
cd /Users/admin/ai-efficiency/frontend
pnpm test src/__tests__/session-detail-view.test.ts
```

Expected:

```text
ok / 1 file passed
```

- [x] **Step 5: Run frontend full verification**

Run:

```bash
cd /Users/admin/ai-efficiency/frontend
pnpm test
pnpm build
```

Expected:

```text
all tests pass
vite build succeeds
```

---

### Task 5: Update Specs And Operator Docs For Streaming Raw Responses

**Files:**
- Modify: `docs/superpowers/specs/2026-04-16-session-raw-response-design.md`
- Modify: `docs/ae-cli/session-pr-attribution.md`
- Modify: `docs/superpowers/specs/2026-04-02-local-session-proxy-design.md`

- [x] **Step 1: Update the earlier raw-response spec to point to the streaming follow-up**

In `docs/superpowers/specs/2026-04-16-session-raw-response-design.md`, add a note:

```md
This document describes the initial non-stream raw response contract. Streaming raw response preservation is extended by `2026-04-16-session-stream-raw-response-design.md`.
```

- [x] **Step 2: Document the new `{kind:"json"|"sse"}` envelope**

In the operator and proxy docs, add:

```md
- `session_usage_events.raw_response.kind == "json"` means a non-stream upstream body.
- `session_usage_events.raw_response.kind == "sse"` means a parsed SSE event array.
```

- [x] **Step 3: Run the docs consistency check**

Run:

```bash
rg -n "kind.: .json|kind.: .sse|Raw Response|raw_response" \
  docs/ae-cli/session-pr-attribution.md \
  docs/superpowers/specs/2026-04-02-local-session-proxy-design.md \
  docs/superpowers/specs/2026-04-16-session-raw-response-design.md \
  docs/superpowers/specs/2026-04-16-session-stream-raw-response-design.md
```

Expected:

```text
All four docs describe the same wrapped raw-response contract.
```

- [ ] **Step 4: Commit docs updates**

```bash
git add docs/ae-cli/session-pr-attribution.md \
        docs/superpowers/specs/2026-04-02-local-session-proxy-design.md \
        docs/superpowers/specs/2026-04-16-session-raw-response-design.md \
        docs/superpowers/specs/2026-04-16-session-stream-raw-response-design.md
git commit -m "docs(specs): document streaming raw response envelopes"
```

---

## Self-Review

- Spec coverage: OpenAI stream, Anthropic stream, wrapped `raw_response` envelopes, and frontend raw-view behavior are all covered.
- Placeholder scan: no TBD/TODO placeholders remain; each step names files, tests, and commands.
- Type consistency: `raw_response` remains request-level only; `raw_payload` remains agent-event only.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-04-16-session-stream-raw-response.md`. Two execution options:

1. Subagent-Driven (recommended) - I dispatch a fresh subagent per task, review between tasks, fast iteration

2. Inline Execution - Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?
