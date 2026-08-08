package attributionledger

import "testing"

func TestExtractCodexRequestEvidenceCapturesHTTPAndLeavesWebSocketUnlinked(t *testing.T) {
	root := map[string]any{
		"resourceSpans": []any{map[string]any{
			"scopeSpans": []any{map[string]any{
				"spans": []any{
					map[string]any{
						"name": "codex.api_request", "startTimeUnixNano": "1785902400000000000",
						"attributes": []any{
							otlpTestAttribute("conversation.id", "conversation-a"),
							otlpTestAttribute("auth.request_id", "request-http-a"),
							otlpTestAttribute("http.response.status_code", "200"),
							otlpTestAttribute("gen_ai.prompt", "must-not-be-retained"),
						},
					},
					map[string]any{
						"name": "codex.websocket_request", "startTimeUnixNano": "1785902401000000000",
						"attributes": []any{
							otlpTestAttribute("conversation.id", "conversation-a"),
							otlpTestAttribute("http.response.status_code", "200"),
						},
					},
				},
			}},
		}},
	}
	evidence := ExtractCodexRequestEvidence(root)
	if len(evidence) != 2 {
		t.Fatalf("evidence count = %d, want 2: %+v", len(evidence), evidence)
	}
	if evidence[0].ConversationID != "conversation-a" || evidence[0].RequestID != "request-http-a" || evidence[0].Transport != "http" {
		t.Fatalf("HTTP evidence = %+v", evidence[0])
	}
	if evidence[1].ConversationID != "conversation-a" || evidence[1].RequestID != "" || evidence[1].Transport != "websocket" {
		t.Fatalf("WebSocket evidence = %+v", evidence[1])
	}
}

func otlpTestAttribute(key, value string) map[string]any {
	return map[string]any{"key": key, "value": map[string]any{"stringValue": value}}
}
