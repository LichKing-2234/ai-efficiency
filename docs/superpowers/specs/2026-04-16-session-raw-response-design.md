# Session Raw Response Design

**Date:** 2026-04-16  
**Status:** Draft for Review  
**Scope:** `backend/`, `frontend/`, `docs/`  
**Related:**  
- [2026-04-02-local-session-proxy-design.md](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-04-02-local-session-proxy-design.md)  
- [2026-03-26-session-pr-attribution-design.md](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-03-26-session-pr-attribution-design.md)

## Summary

`Session Usage` currently mixes two concerns:

1. normalized request-level fields used for cross-provider display
2. the user's desire to inspect the real upstream payload shape without forcing OpenAI/Codex and Anthropic/Claude into one schema

This design keeps both concerns separate:

- `raw_metadata` remains the normalized metadata envelope for request-level UI fields
- `session_usage_events` gains a new `raw_response` JSON field for the original upstream response body
- frontend displays normalized columns by default, with a per-row `Raw Response` expansion for the original upstream body
- `Agent Usage Snapshots` continues to use the existing `raw_payload` as its `Raw Event` view

No historical backfill is attempted.

## Goals

1. Preserve normalized request-level fields already used by the current UI.
2. Let users inspect the original upstream response body for each stored usage event.
3. Avoid pretending OpenAI/Codex and Anthropic/Claude share the same raw schema.
4. Keep history stable: old rows may simply have no `raw_response`.

## Non-Goals

1. No backfill or migration of historical rows beyond adding a nullable field.
2. No attempt to reconstruct raw responses from transcripts or checkpoints.
3. No request-body storage in this change.
4. No change to attribution math in this change.

## Data Contract

### `session_usage_events`

Keep current fields:

- `input_tokens`
- `output_tokens`
- `total_tokens`
- `status`
- `raw_metadata`

Add:

- `raw_response` `JSON` `nullable`

Semantics:

- `raw_metadata` remains the normalized metadata envelope
- `raw_response` stores the original upstream response body when available
- historical rows may have `raw_response = null`

Storage rules:

- OpenAI non-stream success: persist the original JSON response body
- Anthropic non-stream success: persist the original JSON response body
- transport errors / body read failures: `raw_response` stays empty
- streaming responses: `raw_response` stays empty in this phase

### `agent_metadata_events`

No schema change.

- `raw_payload` continues to represent the original captured event/snapshot payload
- frontend will label this as `Raw Event`

## UI Behavior

### `Session Usage`

Keep the current normalized columns:

- `Input`
- `Cached Input`
- `Output`
- `Reasoning`
- `Total`
- `Status`
- `Started`

Add one per-row control:

- `Raw Response`

Behavior:

- clicking `Raw Response` expands a detail row below the usage row
- the detail row renders pretty-printed `raw_response`
- if `raw_response` is missing, render `No raw response.`

### `Agent Usage Snapshots`

Keep the current normalized columns.

Rename the per-row expansion affordance to:

- `Raw Event`

Behavior:

- clicking `Raw Event` expands a detail row below the snapshot row
- the detail row renders pretty-printed `raw_payload`
- if `raw_payload` is missing, render `No raw event.`

## API Behavior

Session detail continues returning `session_usage_events` and `agent_metadata_events` under `edges`.

Expected request-level response shape:

- `session_usage_events[*].raw_metadata` remains normalized
- `session_usage_events[*].raw_response` contains the original upstream response when available
- `agent_metadata_events[*].raw_payload` remains unchanged

## Testing

### Backend

Add coverage for:

1. OpenAI non-stream usage ingest stores `raw_response`
2. Anthropic non-stream usage ingest stores `raw_response`
3. streaming usage ingest keeps `raw_response` empty

### Frontend

Add coverage for:

1. `Session Usage` expands `Raw Response` and renders pretty JSON
2. `Session Usage` shows `No raw response.` when field is missing
3. `Agent Usage Snapshots` expands `Raw Event` and renders pretty JSON
4. `Agent Usage Snapshots` shows `No raw event.` when field is missing

## Rollout

1. Add nullable backend field and expose it through session detail reads.
2. Persist raw upstream response bodies for new non-stream usage events.
3. Update frontend to show `Raw Response` / `Raw Event`.
4. Leave historical rows untouched.
