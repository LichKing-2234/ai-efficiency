package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const DefaultErrorBodyLimit int64 = 4096

type Options struct {
	Headers        http.Header
	ErrorBodyLimit int64
}

type StatusError struct {
	Method     string
	URL        string
	Status     string
	StatusCode int
	Summary    string
	Body       string
}

func (e *StatusError) Error() string {
	if e == nil {
		return ""
	}
	status := strings.TrimSpace(e.Status)
	if status == "" && e.StatusCode != 0 {
		status = fmt.Sprintf("%d", e.StatusCode)
	}
	summary := strings.TrimSpace(e.Summary)
	if summary == "" {
		summary = "empty response body"
	}
	method := strings.TrimSpace(e.Method)
	target := strings.TrimSpace(e.URL)
	if method == "" {
		method = "request"
	}
	if target == "" {
		return fmt.Sprintf("%s failed (HTTP %s): %s", method, status, summary)
	}
	return fmt.Sprintf("%s %s failed (HTTP %s): %s", method, target, status, summary)
}

func DoJSON(ctx context.Context, client *http.Client, method, requestURL string, in any, out any, opts Options) error {
	var body io.Reader
	if in != nil {
		payload, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("marshal JSON request: %w", err)
		}
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	applyHeaders(req, opts.Headers)
	return do(req, client, out, opts)
}

func DoForm(ctx context.Context, client *http.Client, method, requestURL string, form url.Values, out any, opts Options) error {
	req, err := http.NewRequestWithContext(ctx, method, requestURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	applyHeaders(req, opts.Headers)
	return do(req, client, out, opts)
}

func do(req *http.Request, client *http.Client, out any, opts Options) error {
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send %s %s: %w", req.Method, req.URL.String(), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return readStatusError(req, resp, opts)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func readStatusError(req *http.Request, resp *http.Response, opts Options) error {
	limit := opts.ErrorBodyLimit
	if limit <= 0 {
		limit = DefaultErrorBodyLimit
	}
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return fmt.Errorf("read error response: %w", err)
	}
	body := strings.TrimSpace(string(bodyBytes))
	return &StatusError{
		Method:     req.Method,
		URL:        req.URL.String(),
		Status:     resp.Status,
		StatusCode: resp.StatusCode,
		Summary:    summarizeErrorBody(body),
		Body:       body,
	}
}

func summarizeErrorBody(body string) string {
	if body == "" {
		return "empty response body"
	}
	var payload struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
		Message          string `json:"message"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err == nil {
		switch {
		case payload.Error != "" && payload.ErrorDescription != "":
			return payload.Error + ": " + payload.ErrorDescription
		case payload.ErrorDescription != "":
			return payload.ErrorDescription
		case payload.Error != "":
			return payload.Error
		case payload.Message != "":
			return payload.Message
		}
	}
	return body
}

func applyHeaders(req *http.Request, headers http.Header) {
	for key, values := range headers {
		req.Header.Del(key)
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
}
