package proxy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
)

type HookForwardOptions struct {
	WorkspaceID string
	CapturedAt  time.Time
}

type HookForwardRequest struct {
	Tool            string
	LocalProxyURL   string
	LocalProxyToken string
	SessionID       string
	WorkspaceID     string
	CapturedAt      time.Time
}

func NormalizeHookEvent(tool string, raw map[string]any, opts HookForwardOptions) (EventEnvelope, error) {
	tool = strings.TrimSpace(tool)
	if tool == "" {
		return EventEnvelope{}, fmt.Errorf("tool is required")
	}
	if raw == nil {
		raw = map[string]any{}
	}

	hookEventName, _ := raw["hook_event_name"].(string)
	hookEventName = strings.TrimSpace(hookEventName)
	if hookEventName == "" {
		return EventEnvelope{}, fmt.Errorf("hook_event_name is required")
	}

	capturedAt := opts.CapturedAt.UTC()
	if capturedAt.IsZero() {
		capturedAt = time.Now().UTC()
	}

	payload := make(map[string]any, len(raw)+4)
	for k, v := range raw {
		payload[k] = v
	}
	if strings.TrimSpace(asString(payload["event_id"])) == "" {
		payload["event_id"] = newHookForwardEventID()
	}
	payload["workspace_id"] = strings.TrimSpace(opts.WorkspaceID)
	payload["source"] = tool + "_hook"
	payload["captured_at"] = capturedAt.Format(time.RFC3339)

	return EventEnvelope{
		EventType: normalizeHookEventName(hookEventName),
		SessionID: strings.TrimSpace(asString(raw["session_id"])),
		Payload:   payload,
	}, nil
}

func ForwardHookEvent(ctx context.Context, stdin io.Reader, req HookForwardRequest) error {
	if strings.TrimSpace(req.LocalProxyURL) == "" || strings.TrimSpace(req.LocalProxyToken) == "" {
		return nil
	}
	if stdin == nil {
		stdin = strings.NewReader("{}")
	}

	data, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("read hook stdin: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		data = []byte("{}")
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse hook stdin: %w", err)
	}

	event, err := NormalizeHookEvent(req.Tool, raw, HookForwardOptions{
		WorkspaceID: req.WorkspaceID,
		CapturedAt:  req.CapturedAt,
	})
	if err != nil {
		return err
	}

	if err := PostEvent(ctx, req.LocalProxyURL, req.LocalProxyToken, event); err != nil {
		if strings.TrimSpace(req.SessionID) != "" {
			_ = AppendDurableEvent(req.SessionID, event)
		}
		return nil
	}
	return nil
}

func normalizeHookEventName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	var b strings.Builder
	for i, r := range name {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		if r == '-' || r == ' ' {
			b.WriteByte('_')
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func newHookForwardEventID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("evt-%d", time.Now().UnixNano())
	}
	return "evt-" + hex.EncodeToString(buf[:])
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
