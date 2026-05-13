package attributionlocal

import (
	"math"
	"testing"
)

func TestParseKiroJSON_UsesCreditAndConversationID(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "kiro.json", `{"session_id":"root-1","cwd":"/tmp/repo","session_state":{"conversation_metadata":{"user_turn_metadatas":[{"total_request_count":2,"context_usage_percentage":4.2,"metering_usage":[{"value":0.1,"unit":"credit"},{"value":0.2,"unit":"credit"}]}]},"rts_model_state":{"conversation_id":"conv-1"}}}`)

	events, err := ParseKiroJSON(path, "/tmp/repo")
	if err != nil {
		t.Fatalf("ParseKiroJSON: %v", err)
	}
	if len(events) != 1 || events[0].ToolSessionID != "conv-1" || math.Abs(events[0].CreditUsage-0.3) > 1e-9 {
		t.Fatalf("events = %+v", events)
	}
}
