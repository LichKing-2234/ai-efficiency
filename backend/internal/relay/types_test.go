package relay_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/ai-efficiency/backend/internal/relay"
)

func TestStableProtocolCapabilities(t *testing.T) {
	tests := []struct {
		name                  string
		platform              string
		allowMessagesDispatch bool
		want                  relay.ProtocolCapabilities
	}{
		{
			name:     "openai",
			platform: "openai",
			want: relay.ProtocolCapabilities{
				Supported:   []string{relay.ProtocolResponses, relay.ProtocolChatCompletions},
				Recommended: relay.ProtocolResponses,
			},
		},
		{
			name:                  "openai with message dispatch",
			platform:              "openai",
			allowMessagesDispatch: true,
			want: relay.ProtocolCapabilities{
				Supported:   []string{relay.ProtocolResponses, relay.ProtocolChatCompletions, relay.ProtocolMessages},
				Recommended: relay.ProtocolResponses,
			},
		},
		{
			// The Relay serves DeepSeek through its OpenAI gateway, so DeepSeek
			// must expose the same stable probe protocols as OpenAI.
			name:     "deepseek uses the openai gateway protocol set",
			platform: "deepseek",
			want: relay.ProtocolCapabilities{
				Supported:   []string{relay.ProtocolResponses, relay.ProtocolChatCompletions},
				Recommended: relay.ProtocolResponses,
			},
		},
		{
			name:                  "deepseek with message dispatch",
			platform:              "deepseek",
			allowMessagesDispatch: true,
			want: relay.ProtocolCapabilities{
				Supported:   []string{relay.ProtocolResponses, relay.ProtocolChatCompletions, relay.ProtocolMessages},
				Recommended: relay.ProtocolResponses,
			},
		},
		{
			name:     "deepseek platform is matched case-insensitively",
			platform: "  DeepSeek ",
			want: relay.ProtocolCapabilities{
				Supported:   []string{relay.ProtocolResponses, relay.ProtocolChatCompletions},
				Recommended: relay.ProtocolResponses,
			},
		},
		{
			name:     "anthropic",
			platform: "anthropic",
			want: relay.ProtocolCapabilities{
				Supported:   []string{relay.ProtocolMessages, relay.ProtocolResponses, relay.ProtocolChatCompletions},
				Recommended: relay.ProtocolMessages,
			},
		},
		{
			name:     "claude alias",
			platform: " Claude ",
			want: relay.ProtocolCapabilities{
				Supported:   []string{relay.ProtocolMessages, relay.ProtocolResponses, relay.ProtocolChatCompletions},
				Recommended: relay.ProtocolMessages,
			},
		},
		{
			name:     "gemini",
			platform: "gemini",
			want: relay.ProtocolCapabilities{
				Supported:   []string{relay.ProtocolGenerateContent, relay.ProtocolChatCompletions},
				Recommended: relay.ProtocolGenerateContent,
			},
		},
		{
			name:     "antigravity",
			platform: "antigravity",
			want: relay.ProtocolCapabilities{
				Supported:   []string{relay.ProtocolMessages, relay.ProtocolAntigravityGenerateContent},
				Recommended: relay.ProtocolMessages,
			},
		},
		{
			name:     "grok",
			platform: "grok",
			want: relay.ProtocolCapabilities{
				Supported:   []string{relay.ProtocolResponses, relay.ProtocolChatCompletions, relay.ProtocolMessages},
				Recommended: relay.ProtocolResponses,
			},
		},
		{
			name:     "composite",
			platform: "composite",
			want: relay.ProtocolCapabilities{
				Supported:   []string{relay.ProtocolChatCompletions, relay.ProtocolResponses, relay.ProtocolMessages, relay.ProtocolGenerateContent},
				Recommended: relay.ProtocolChatCompletions,
			},
		},
		{
			name:     "unknown platform exposes no stable test protocol",
			platform: "some-unknown-platform",
			want:     relay.ProtocolCapabilities{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			group := relay.Group{
				ID:                    1,
				Name:                  "Group Alpha",
				Platform:              tt.platform,
				AllowMessagesDispatch: tt.allowMessagesDispatch,
			}

			got := relay.StableProtocolCapabilities(group)

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("StableProtocolCapabilities() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStableProtocolCapabilitiesRecommendsSupportedProtocol(t *testing.T) {
	platforms := []string{"openai", "deepseek", "anthropic", "claude", "gemini", "antigravity", "grok", "composite"}
	for _, platform := range platforms {
		t.Run(platform, func(t *testing.T) {
			capabilities := relay.StableProtocolCapabilities(relay.Group{ID: 1, Platform: platform})
			if capabilities.Recommended == "" {
				t.Fatalf("platform %q has no recommended protocol", platform)
			}
			for _, supported := range capabilities.Supported {
				if supported == capabilities.Recommended {
					return
				}
			}
			t.Fatalf("platform %q recommends %q outside %v", platform, capabilities.Recommended, capabilities.Supported)
		})
	}
}
