package relay_test

import (
	"reflect"
	"testing"

	"github.com/ai-efficiency/backend/internal/relay"
)

func TestStableProtocolCapabilities(t *testing.T) {
	tests := []struct {
		name        string
		platform    string
		allowMsg    bool
		wantSupport []string
		wantRec     string
	}{
		{
			name:        "openai",
			platform:    "openai",
			wantSupport: []string{relay.ProtocolResponses, relay.ProtocolChatCompletions},
			wantRec:     relay.ProtocolResponses,
		},
		{
			name:        "openai with message dispatch",
			platform:    "openai",
			allowMsg:    true,
			wantSupport: []string{relay.ProtocolResponses, relay.ProtocolChatCompletions, relay.ProtocolMessages},
			wantRec:     relay.ProtocolResponses,
		},
		{
			name:        "deepseek uses the openai gateway protocol set",
			platform:    "deepseek",
			wantSupport: []string{relay.ProtocolResponses, relay.ProtocolChatCompletions},
			wantRec:     relay.ProtocolResponses,
		},
		{
			name:        "deepseek with message dispatch",
			platform:    "deepseek",
			allowMsg:    true,
			wantSupport: []string{relay.ProtocolResponses, relay.ProtocolChatCompletions, relay.ProtocolMessages},
			wantRec:     relay.ProtocolResponses,
		},
		{
			name:        "deepseek platform is matched case-insensitively",
			platform:    "  DeepSeek ",
			wantSupport: []string{relay.ProtocolResponses, relay.ProtocolChatCompletions},
			wantRec:     relay.ProtocolResponses,
		},
		{
			name:        "anthropic",
			platform:    "anthropic",
			wantSupport: []string{relay.ProtocolMessages, relay.ProtocolResponses, relay.ProtocolChatCompletions},
			wantRec:     relay.ProtocolMessages,
		},
		{
			name:        "gemini",
			platform:    "gemini",
			wantSupport: []string{relay.ProtocolGenerateContent, relay.ProtocolChatCompletions},
			wantRec:     relay.ProtocolGenerateContent,
		},
		{
			name:        "unknown platform exposes no stable test protocol",
			platform:    "some-unknown-platform",
			wantSupport: nil,
			wantRec:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			group := relay.Group{
				ID:                    1,
				Name:                  "Group Alpha",
				Platform:              tt.platform,
				AllowMessagesDispatch: tt.allowMsg,
			}

			got := relay.StableProtocolCapabilities(group)

			if !reflect.DeepEqual(got.Supported, tt.wantSupport) {
				t.Errorf("Supported = %v, want %v", got.Supported, tt.wantSupport)
			}
			if got.Recommended != tt.wantRec {
				t.Errorf("Recommended = %q, want %q", got.Recommended, tt.wantRec)
			}
		})
	}
}
