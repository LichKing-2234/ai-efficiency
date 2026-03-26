package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Router uses an LLM to decide which tool should handle a message.
type Router struct {
	apiURL string
	apiKey string
	model  string
	tools  []string
}

// New creates a new Router.
func New(apiURL, apiKey, model string, tools []string) *Router {
	return &Router{
		apiURL: strings.TrimRight(apiURL, "/") + "/v1/chat/completions",
		apiKey: apiKey,
		model:  model,
		tools:  tools,
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Route asks the LLM which tool should handle the given message.
// Returns the tool name from the configured tools list.
func (r *Router) Route(message string) (string, error) {
	systemPrompt := fmt.Sprintf(
		`You are a routing assistant. Given a user message, decide which AI tool should handle it.
Available tools: %s

Rules:
- Reply with ONLY the tool name, nothing else.
- If the message is about code review, use kiro.
- If the message is about code generation, debugging, or general coding, use claude.
- If the message is about running commands or quick edits, use codex.
- If unsure, use claude as the default.

Reply with exactly one tool name from the list above.`,
		strings.Join(r.tools, ", "))

	reqBody := chatRequest{
		Model: r.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: message},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, r.apiURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+r.apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("calling LLM: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("LLM returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("no choices in LLM response")
	}

	toolName := strings.TrimSpace(strings.ToLower(chatResp.Choices[0].Message.Content))

	// Validate the tool name
	for _, t := range r.tools {
		if t == toolName {
			return toolName, nil
		}
	}

	// Default fallback
	if len(r.tools) > 0 {
		return r.tools[0], nil
	}
	return "", fmt.Errorf("no tools configured")
}
