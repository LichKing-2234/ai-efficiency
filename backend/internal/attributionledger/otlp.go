package attributionledger

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ExtractCodexRequestEvidence reduces an OTLP JSON object to the bounded
// correlation fields retained by the attribution ledger.
func ExtractCodexRequestEvidence(root any) []RequestEvidence {
	result := make([]RequestEvidence, 0)
	var visit func(any, map[string]string)
	visit = func(value any, inherited map[string]string) {
		switch node := value.(type) {
		case []any:
			for _, child := range node {
				visit(child, inherited)
			}
		case map[string]any:
			attributes := cloneStringMap(inherited)
			if raw, ok := node["attributes"].([]any); ok {
				for key, value := range otlpAttributes(raw) {
					attributes[key] = value
				}
			}
			eventName := firstString(attributes["event.name"], attributes["name"], otlpString(node["name"]), otlpString(node["body"]))
			conversationID := firstString(attributes["conversation.id"], attributes["thread.id"])
			observedAt := otlpTimestamp(node)
			if conversationID != "" && !observedAt.IsZero() && (strings.Contains(eventName, "codex.api_request") || strings.Contains(eventName, "codex.websocket")) {
				statusCode, _ := strconv.Atoi(firstString(attributes["http.response.status_code"], attributes["http.status_code"]))
				transport := "http"
				if strings.Contains(eventName, "websocket") {
					transport = "websocket"
				}
				requestID := firstString(attributes["auth.request_id"], attributes["x-request-id"])
				result = append(result, RequestEvidence{
					ConversationID: conversationID,
					RequestID:      requestID,
					ObservedAt:     observedAt,
					EventName:      eventName,
					Transport:      transport,
					StatusCode:     statusCode,
					ErrorCategory:  firstString(attributes["error.type"], attributes["error.category"]),
					Failed:         statusCode >= 400 || attributes["error.type"] != "" || attributes["error.category"] != "",
				})
			}
			for key, child := range node {
				if key == "attributes" {
					continue
				}
				visit(child, attributes)
			}
		}
	}
	visit(root, map[string]string{})
	return result
}

func otlpAttributes(raw []any) map[string]string {
	result := map[string]string{}
	for _, item := range raw {
		entry, _ := item.(map[string]any)
		key, _ := entry["key"].(string)
		if key != "" {
			result[key] = otlpString(entry["value"])
		}
	}
	return result
}

func otlpString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		for _, key := range []string{"stringValue", "intValue", "boolValue"} {
			if inner, ok := typed[key]; ok {
				return strings.TrimSpace(fmt.Sprint(inner))
			}
		}
	}
	return ""
}

func otlpTimestamp(node map[string]any) time.Time {
	for _, key := range []string{"timeUnixNano", "startTimeUnixNano", "observedTimeUnixNano"} {
		raw := strings.TrimSpace(fmt.Sprint(node[key]))
		if raw == "" || raw == "<nil>" {
			continue
		}
		nanos, err := strconv.ParseInt(raw, 10, 64)
		if err == nil && nanos > 0 {
			return time.Unix(0, nanos).UTC()
		}
	}
	return time.Time{}
}

func cloneStringMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func firstString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
