# Session Stream Raw Response Design

**Date:** 2026-04-16  
**Status:** Draft for Review  
**Scope:** `backend/`, `ae-cli/`, `frontend/`, `docs/`  
**Related:**
- [2026-04-16-session-raw-response-design.md](./2026-04-16-session-raw-response-design.md)
- [2026-04-02-local-session-proxy-design.md](./2026-04-02-local-session-proxy-design.md)

## Summary

The previous `raw_response` contract only covered non-stream upstream bodies. That leaves normal Claude usage and many Codex/OpenAI flows with `No raw response.` even when the proxy did observe a real upstream stream.

This design extends `session_usage_events.raw_response` so both non-stream and stream requests can preserve their original upstream response shape:

- non-stream rows store the original upstream JSON response body
- stream rows store a parsed array of SSE events instead of dropping raw response data

The frontend continues to show normalized request-level columns by default, but `Raw Response` now renders either:

- `{ "kind": "json", "body": { ... } }`
- `{ "kind": "sse", "events": [ ... ] }`

Historical rows are not backfilled.

## Goals

1. Preserve raw upstream data for streaming request usage rows.
2. Avoid pretending stream payloads are normal JSON response bodies.
3. Keep one stable top-level shape for `raw_response`.
4. Preserve the current `raw_payload` semantics for `Agent Usage Snapshots`.

## Non-Goals

1. No request-body storage in this change.
2. No stream replay feature in the UI beyond pretty-printing stored events.
3. No historical backfill.
4. No change to attribution math.

## Data Contract

### `session_usage_events.raw_response`

Keep the nullable JSON field introduced by the earlier design, but change its shape to a stable envelope.

#### Non-stream

```json
{
  "kind": "json",
  "body": { ...original upstream response body... }
}
```

#### Stream

```json
{
  "kind": "sse",
  "events": [
    {
      "event": "response.created",
      "data": { ...parsed SSE JSON payload... }
    },
    {
      "event": "response.completed",
      "data": { ...parsed SSE JSON payload... }
    }
  ]
}
```

For Anthropic:

```json
{
  "kind": "sse",
  "events": [
    {
      "event": "message_start",
      "data": { ...parsed SSE JSON payload... }
    },
    {
      "event": "message_delta",
      "data": { ...parsed SSE JSON payload... }
    }
  ]
}
```

Storage rules:

- only persist parsed events actually observed from upstream
- ignore non-JSON `data:` payloads rather than fabricating event objects
- transport errors / upstream read failures still leave `raw_response` empty

### `agent_metadata_events.raw_payload`

No change.

- `Raw Event` in the UI continues to render `raw_payload`

## UI Behavior

### `Session Usage`

Keep the current normalized columns and `Raw Response` expander.

Expanded raw view behavior:

- if `raw_response.kind == "json"`, render the full wrapped object
- if `raw_response.kind == "sse"`, render the full wrapped object
- do not collapse or reinterpret SSE events into a fake single response
- if `raw_response` is missing, render `No raw response.`

### `Agent Usage Snapshots`

No behavior change.

- `Raw Event` continues to pretty-print `raw_payload`

## Testing

### Backend / ae-cli proxy

Add or update coverage for:

1. OpenAI non-stream stores `{kind:"json", body:...}`
2. OpenAI stream stores `{kind:"sse", events:[...]}`
3. Anthropic non-stream stores `{kind:"json", body:...}`
4. Anthropic stream stores `{kind:"sse", events:[...]}`
5. failed / incomplete upstream reads still leave `raw_response` empty

### Frontend

Add or update coverage for:

1. `Raw Response` renders wrapped JSON raw responses
2. `Raw Response` renders wrapped SSE event arrays
3. empty-state behavior remains unchanged

## Rollout

1. Update proxy-side raw response collection to wrap both non-stream and stream cases.
2. Keep the backend field unchanged; only the stored JSON shape changes for new rows.
3. Keep the frontend raw viewer generic and schema-agnostic.
4. Leave historical rows untouched.
