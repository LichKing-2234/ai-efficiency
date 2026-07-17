package directorysync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/internal/httpclient"
)

const (
	defaultTimeoutSeconds  = 30
	defaultMaxResponseSize = 1048576
	defaultMaxItems        = 50000
)

type CredentialResolver interface {
	ResolveCredential(ctx context.Context, ref string) (value string, ok bool, err error)
}

type ExecutorOptions struct {
	HTTPClient *http.Client
	AllowHTTP  bool
}

type Executor struct {
	client    *http.Client
	allowHTTP bool
}

func NewExecutor(options ExecutorOptions) *Executor {
	client := options.HTTPClient
	if client == nil {
		client = httpclient.NewDefault(defaultTimeoutSeconds * time.Second)
	}
	return &Executor{client: client, allowHTTP: options.AllowHTTP}
}

type ExecutionResult struct {
	Departments      []DepartmentRecord `json:"departments"`
	Members          []MemberRecord     `json:"members"`
	Warnings         []ExecutionWarning `json:"warnings"`
	HTTPRequestCount int                `json:"http_request_count"`
}

type DepartmentRecord struct {
	ExternalID       string         `json:"external_id"`
	ParentExternalID string         `json:"parent_external_id,omitempty"`
	Name             string         `json:"name"`
	Path             string         `json:"path,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
}

type MemberRecord struct {
	ExternalID            string         `json:"external_id,omitempty"`
	EmailNormalized       string         `json:"email_normalized"`
	DisplayName           string         `json:"display_name,omitempty"`
	DepartmentExternalID  string         `json:"department_external_id,omitempty"`
	DepartmentExternalIDs []string       `json:"department_external_ids,omitempty"`
	Status                string         `json:"status,omitempty"`
	Metadata              map[string]any `json:"metadata,omitempty"`
}

type ExecutionWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	StepID  string `json:"step_id,omitempty"`
}

func (e *Executor) Execute(ctx context.Context, cfg *DSL, credentials CredentialResolver) (*ExecutionResult, error) {
	if cfg == nil {
		return nil, fmt.Errorf("dsl is required")
	}
	if e == nil {
		e = NewExecutor(ExecutorOptions{})
	}

	limits := normalizedLimits(cfg.Limits)
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(limits.TimeoutSeconds)*time.Second)
	defer cancel()

	credentialValue := ""
	if strings.TrimSpace(cfg.Auth.CredentialRef) != "" {
		if credentials == nil {
			return nil, fmt.Errorf("credential resolver is required")
		}
		value, ok, err := credentials.ResolveCredential(runCtx, strings.TrimSpace(cfg.Auth.CredentialRef))
		if err != nil {
			return nil, fmt.Errorf("resolve credential: %w", err)
		}
		if !ok {
			return nil, fmt.Errorf("credential_ref %q not found", cfg.Auth.CredentialRef)
		}
		credentialValue = value
	}

	result := &ExecutionResult{}
	stepItems := map[string][]map[string]any{}
	seenMemberships := map[string]struct{}{}
	memberIndexesByEmail := map[string]int{}
	totalItems := 0

	for _, step := range cfg.Steps {
		iterationItems, err := iterationItemsForStep(step, stepItems)
		if err != nil {
			return nil, err
		}
		var extractedForStep []map[string]any
		for _, iterationItem := range iterationItems {
			items, err := e.executeStep(runCtx, step, iterationItem, credentialValue, cfg.Auth.Header, limits.MaxResponseSize)
			if err != nil {
				return nil, err
			}
			result.HTTPRequestCount++
			totalItems += len(items)
			if totalItems > limits.MaxItems {
				return nil, fmt.Errorf("max_items exceeded")
			}
			for _, item := range items {
				stepItem := cloneMap(item)
				if step.Map.Department != nil {
					department := mapDepartment(step.Map.Department, item)
					if department.ExternalID != "" && department.Name != "" {
						result.Departments = append(result.Departments, department)
						stepItem["external_id"] = department.ExternalID
						stepItem["parent_external_id"] = department.ParentExternalID
						stepItem["name"] = department.Name
						stepItem["path"] = department.Path
					}
				}
				if step.Map.Member != nil {
					member, warnings := mapMember(step.ID, step.Map.Member, item, iterationItem, seenMemberships)
					result.Warnings = append(result.Warnings, warnings...)
					if member != nil {
						mergeMemberRecord(result, memberIndexesByEmail, *member)
					}
				}
				extractedForStep = append(extractedForStep, stepItem)
			}
		}
		stepItems[step.ID] = extractedForStep
	}

	return result, nil
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+4)
	for key, value := range in {
		out[key] = value
	}
	return out
}

func normalizedLimits(in Limits) Limits {
	out := in
	if out.TimeoutSeconds <= 0 {
		out.TimeoutSeconds = defaultTimeoutSeconds
	}
	if out.MaxResponseSize <= 0 {
		out.MaxResponseSize = defaultMaxResponseSize
	}
	if out.MaxItems <= 0 {
		out.MaxItems = defaultMaxItems
	}
	return out
}

func iterationItemsForStep(step StepConfig, stepItems map[string][]map[string]any) ([]map[string]any, error) {
	if strings.TrimSpace(step.Foreach) == "" {
		return []map[string]any{nil}, nil
	}
	parts := strings.Split(strings.TrimSpace(step.Foreach), ".")
	if len(parts) != 2 || parts[1] != "items" {
		return nil, fmt.Errorf("unsupported foreach reference %q", step.Foreach)
	}
	items, ok := stepItems[parts[0]]
	if !ok {
		return nil, fmt.Errorf("foreach references unknown step %q", parts[0])
	}
	return items, nil
}

func (e *Executor) executeStep(ctx context.Context, step StepConfig, iterationItem map[string]any, credentialValue, credentialHeader string, maxResponseSize int) ([]map[string]any, error) {
	requestURL, err := renderTemplate(step.Request.URL, iterationItem, nil)
	if err != nil {
		return nil, fmt.Errorf("render url for step %s: %w", step.ID, err)
	}
	parsed, err := url.Parse(requestURL)
	if err != nil {
		return nil, fmt.Errorf("parse url for step %s: %w", step.ID, err)
	}
	if parsed.Scheme != "https" && !(e.allowHTTP && parsed.Scheme == "http") {
		return nil, fmt.Errorf("url must use https")
	}
	query := parsed.Query()
	for key, value := range step.Request.Query {
		rendered, err := renderTemplate(value, iterationItem, nil)
		if err != nil {
			return nil, fmt.Errorf("render query %s for step %s: %w", key, step.ID, err)
		}
		query.Set(key, rendered)
	}
	parsed.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request for step %s: %w", step.ID, err)
	}
	if strings.TrimSpace(credentialHeader) != "" {
		req.Header.Set(strings.TrimSpace(credentialHeader), credentialValue)
	}
	for key, value := range step.Request.Headers {
		rendered, err := renderTemplate(value, iterationItem, nil)
		if err != nil {
			return nil, fmt.Errorf("render header %s for step %s: %w", key, step.ID, err)
		}
		req.Header.Set(key, rendered)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute step %s: %w", step.ID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("execute step %s: unexpected status %d", step.ID, resp.StatusCode)
	}
	body, err := readLimited(resp.Body, maxResponseSize)
	if err != nil {
		return nil, fmt.Errorf("execute step %s: %w", step.ID, err)
	}
	var document any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode step %s response: %w", step.ID, err)
	}
	rawItems, err := EvaluateJSONPath(document, step.Extract.Items)
	if err != nil {
		return nil, fmt.Errorf("extract step %s items: %w", step.ID, err)
	}
	list, ok := rawItems.([]any)
	if !ok {
		return nil, fmt.Errorf("extract step %s items: path did not resolve to an array", step.ID)
	}
	items := make([]map[string]any, 0, len(list))
	for _, raw := range list {
		item, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("extract step %s items: item is not an object", step.ID)
		}
		items = append(items, item)
	}
	return items, nil
}

func readLimited(reader io.Reader, maxBytes int) ([]byte, error) {
	var buf bytes.Buffer
	limited := io.LimitReader(reader, int64(maxBytes)+1)
	if _, err := io.Copy(&buf, limited); err != nil {
		return nil, err
	}
	if buf.Len() > maxBytes {
		return nil, fmt.Errorf("response too large")
	}
	return buf.Bytes(), nil
}

func mapDepartment(mapping *DepartmentMapping, item map[string]any) DepartmentRecord {
	return DepartmentRecord{
		ExternalID:       evaluateString(mapping.ExternalID, item, nil),
		ParentExternalID: evaluateString(mapping.ParentExternalID, item, nil),
		Name:             evaluateString(mapping.Name, item, nil),
		Path:             evaluateString(mapping.Path, item, nil),
		Metadata:         evaluateMetadata(mapping.Metadata, item, nil),
	}
}

func mapMember(stepID string, mapping *MemberMapping, item, source map[string]any, seenMemberships map[string]struct{}) (*MemberRecord, []ExecutionWarning) {
	email := normalizeEmail(evaluateString(mapping.Email, item, source))
	if email == "" || !validEmail(email) {
		return nil, []ExecutionWarning{{
			Code:    "invalid_member_email",
			Message: "member email is missing or invalid",
			StepID:  stepID,
		}}
	}
	departmentIDs := appendUniqueStrings(nil, evaluateStringList(mapping.DepartmentExternalID, item, source)...)
	departmentIDs = appendUniqueStrings(departmentIDs, evaluateStringList(mapping.DepartmentExternalIDs, item, source)...)
	newDepartmentIDs := make([]string, 0, len(departmentIDs))
	for _, departmentID := range departmentIDs {
		key := email + "\x00" + departmentID
		if _, exists := seenMemberships[key]; exists {
			continue
		}
		seenMemberships[key] = struct{}{}
		newDepartmentIDs = append(newDepartmentIDs, departmentID)
	}
	if len(newDepartmentIDs) == 0 && len(departmentIDs) > 0 {
		return nil, []ExecutionWarning{{
			Code:    "duplicate_member_membership",
			Message: "duplicate member department membership skipped",
			StepID:  stepID,
		}}
	}

	status := evaluateString(mapping.Status, item, source)
	if status == "" {
		status = "active"
	}
	primaryDepartmentID := ""
	if len(newDepartmentIDs) > 0 {
		primaryDepartmentID = newDepartmentIDs[0]
	}
	return &MemberRecord{
		ExternalID:            evaluateString(mapping.ExternalID, item, source),
		EmailNormalized:       email,
		DisplayName:           evaluateString(mapping.DisplayName, item, source),
		DepartmentExternalID:  primaryDepartmentID,
		DepartmentExternalIDs: newDepartmentIDs,
		Status:                status,
		Metadata:              evaluateMetadata(mapping.Metadata, item, source),
	}, nil
}

func mergeMemberRecord(result *ExecutionResult, indexesByEmail map[string]int, next MemberRecord) {
	if result == nil {
		return
	}
	if idx, ok := indexesByEmail[next.EmailNormalized]; ok {
		existing := result.Members[idx]
		if existing.ExternalID == "" {
			existing.ExternalID = next.ExternalID
		}
		if existing.DisplayName == "" {
			existing.DisplayName = next.DisplayName
		}
		if existing.DepartmentExternalID == "" {
			existing.DepartmentExternalID = next.DepartmentExternalID
		}
		if existing.Status == "" {
			existing.Status = next.Status
		}
		existing.DepartmentExternalIDs = appendUniqueStrings(existing.DepartmentExternalIDs, next.DepartmentExternalIDs...)
		if len(existing.DepartmentExternalIDs) == 0 && existing.DepartmentExternalID != "" {
			existing.DepartmentExternalIDs = []string{existing.DepartmentExternalID}
		}
		existing.Metadata = mergeMetadata(existing.Metadata, next.Metadata)
		result.Members[idx] = existing
		return
	}
	if len(next.DepartmentExternalIDs) == 0 && next.DepartmentExternalID != "" {
		next.DepartmentExternalIDs = []string{next.DepartmentExternalID}
	}
	indexesByEmail[next.EmailNormalized] = len(result.Members)
	result.Members = append(result.Members, next)
}

func mergeMetadata(current, next map[string]any) map[string]any {
	if len(current) == 0 {
		if len(next) == 0 {
			return map[string]any{}
		}
		merged := make(map[string]any, len(next))
		for key, value := range next {
			merged[key] = value
		}
		return merged
	}
	merged := make(map[string]any, len(current)+len(next))
	for key, value := range current {
		merged[key] = value
	}
	for key, value := range next {
		if _, exists := merged[key]; !exists {
			merged[key] = value
		}
	}
	return merged
}

func evaluateMetadata(mapping map[string]string, item, source map[string]any) map[string]any {
	if len(mapping) == 0 {
		return map[string]any{}
	}
	metadata := make(map[string]any, len(mapping))
	for key, expression := range mapping {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		value, err := evaluateValue(expression, item, source)
		if err != nil || emptyMetadataValue(value) {
			continue
		}
		metadata[key] = value
	}
	return metadata
}

func emptyMetadataValue(value any) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) == ""
	}
	if list, ok := value.([]any); ok {
		return len(list) == 0
	}
	return false
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validEmail(email string) bool {
	parsed, err := mail.ParseAddress(email)
	return err == nil && strings.EqualFold(parsed.Address, email)
}

func evaluateString(expression string, item, source map[string]any) string {
	value, err := evaluateValue(expression, item, source)
	if err != nil || value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func evaluateStringList(expression string, item, source map[string]any) []string {
	value, err := evaluateValue(expression, item, source)
	if err != nil || value == nil {
		return nil
	}
	return compactStringValues(value)
}

func compactStringValues(value any) []string {
	seen := map[string]struct{}{}
	out := []string{}
	var add func(any)
	add = func(item any) {
		switch typed := item.(type) {
		case nil:
			return
		case []any:
			for _, nested := range typed {
				add(nested)
			}
		case []string:
			for _, nested := range typed {
				add(nested)
			}
		default:
			text := strings.TrimSpace(fmt.Sprint(typed))
			if text == "" {
				return
			}
			if _, exists := seen[text]; exists {
				return
			}
			seen[text] = struct{}{}
			out = append(out, text)
		}
	}
	add(value)
	return out
}

func appendUniqueStrings(current []string, values ...string) []string {
	seen := make(map[string]struct{}, len(current)+len(values))
	out := make([]string, 0, len(current)+len(values))
	for _, value := range current {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func evaluateValue(expression string, item, source map[string]any) (any, error) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return "", nil
	}
	if strings.HasPrefix(expression, "$.") {
		value, err := EvaluateJSONPath(item, expression)
		if err != nil {
			return "", nil
		}
		return value, nil
	}
	return renderTemplate(expression, item, source)
}

func renderTemplate(value string, item, source map[string]any) (string, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "{{") || !strings.HasSuffix(value, "}}") {
		return value, nil
	}
	expr := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "{{"), "}}"))
	switch {
	case strings.HasPrefix(expr, "item."):
		return lookupTemplateValue(item, strings.TrimPrefix(expr, "item."))
	case strings.HasPrefix(expr, "source."):
		return lookupTemplateValue(source, strings.TrimPrefix(expr, "source."))
	default:
		return "", fmt.Errorf("unsupported template expression %q", expr)
	}
}

func lookupTemplateValue(root map[string]any, path string) (string, error) {
	if root == nil {
		return "", nil
	}
	current := any(root)
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return "", nil
		}
		next, ok := object[part]
		if !ok || next == nil {
			return "", nil
		}
		current = next
	}
	return strings.TrimSpace(fmt.Sprint(current)), nil
}
