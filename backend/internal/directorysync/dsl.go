package directorysync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	maxDepartmentOverrides               = 100
	maxMemberOverrides                   = 100
	maxMetadataOperationValues           = 100
	representativeExternalIDsMetadataKey = "representative_external_ids"
	leaderDepartmentIDsMetadataKey       = "leader_department_ids"
)

type DSL struct {
	Version   int            `json:"version" yaml:"version"`
	Scope     string         `json:"scope" yaml:"scope"`
	Auth      AuthConfig     `json:"auth" yaml:"auth"`
	Limits    Limits         `json:"limits" yaml:"limits"`
	Steps     []StepConfig   `json:"steps" yaml:"steps"`
	Overrides OverrideConfig `json:"overrides" yaml:"overrides"`
}

type OverrideConfig struct {
	Departments []DepartmentOverride `json:"departments" yaml:"departments"`
	Members     []MemberOverride     `json:"members" yaml:"members"`
}

type DepartmentOverride struct {
	ExternalID string                               `json:"external_id" yaml:"external_id"`
	Metadata   map[string]MetadataOverrideOperation `json:"metadata" yaml:"metadata"`
}

type MemberOverride struct {
	ExternalID string                               `json:"external_id" yaml:"external_id"`
	Metadata   map[string]MetadataOverrideOperation `json:"metadata" yaml:"metadata"`
}

type MetadataOverrideOperation struct {
	Append []string `json:"append" yaml:"append"`
	Remove []string `json:"remove" yaml:"remove"`
}

type AuthConfig struct {
	Type          string `json:"type" yaml:"type"`
	Header        string `json:"header" yaml:"header"`
	CredentialRef string `json:"credential_ref" yaml:"credential_ref"`
}

type Limits struct {
	TimeoutSeconds  int `json:"timeout_seconds" yaml:"timeout_seconds"`
	MaxResponseSize int `json:"max_response_bytes" yaml:"max_response_bytes"`
	MaxItems        int `json:"max_items" yaml:"max_items"`
}

type StepConfig struct {
	ID      string        `json:"id" yaml:"id"`
	Foreach string        `json:"foreach" yaml:"foreach"`
	Request RequestConfig `json:"request" yaml:"request"`
	Extract ExtractConfig `json:"extract" yaml:"extract"`
	Map     MapConfig     `json:"map" yaml:"map"`
}

type RequestConfig struct {
	Method  string            `json:"method" yaml:"method"`
	URL     string            `json:"url" yaml:"url"`
	Headers map[string]string `json:"headers" yaml:"headers"`
	Query   map[string]string `json:"query" yaml:"query"`
}

type ExtractConfig struct {
	Items string `json:"items" yaml:"items"`
}

type MapConfig struct {
	Department *DepartmentMapping `json:"department" yaml:"department"`
	Member     *MemberMapping     `json:"member" yaml:"member"`
}

type DepartmentMapping struct {
	ExternalID       string            `json:"external_id" yaml:"external_id"`
	ParentExternalID string            `json:"parent_external_id" yaml:"parent_external_id"`
	Name             string            `json:"name" yaml:"name"`
	Path             string            `json:"path" yaml:"path"`
	Metadata         map[string]string `json:"metadata" yaml:"metadata"`
}

type MemberMapping struct {
	ExternalID            string            `json:"external_id" yaml:"external_id"`
	Email                 string            `json:"email" yaml:"email"`
	DisplayName           string            `json:"display_name" yaml:"display_name"`
	DepartmentExternalID  string            `json:"department_external_id" yaml:"department_external_id"`
	DepartmentExternalIDs string            `json:"department_external_ids" yaml:"department_external_ids"`
	Status                string            `json:"status" yaml:"status"`
	Metadata              map[string]string `json:"metadata" yaml:"metadata"`
}

type ValidationIssue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

func ParseDSL(raw string) (*DSL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("dsl is required")
	}

	var cfg DSL
	if strings.HasPrefix(raw, "{") {
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			return nil, fmt.Errorf("parse json dsl: %w", err)
		}
		return &cfg, nil
	}
	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("parse yaml dsl: %w", err)
	}
	return &cfg, nil
}

func ValidateDSL(ctx context.Context, cfg *DSL, credentialExists func(context.Context, string) bool) []ValidationIssue {
	var issues []ValidationIssue
	add := func(path, message string) {
		issues = append(issues, ValidationIssue{Path: path, Message: message})
	}

	if cfg == nil {
		return []ValidationIssue{{Path: "dsl", Message: "dsl is required"}}
	}
	if cfg.Version != 1 {
		add("version", "version must be 1")
	}
	if strings.TrimSpace(cfg.Scope) != "full_company" {
		add("scope", "scope must be full_company")
	}
	if strings.TrimSpace(cfg.Auth.Type) != "header" {
		add("auth.type", "auth.type must be header")
	}
	if strings.TrimSpace(cfg.Auth.Header) == "" {
		add("auth.header", "auth.header is required")
	}
	credentialRef := strings.TrimSpace(cfg.Auth.CredentialRef)
	if credentialRef == "" {
		add("auth.credential_ref", "auth.credential_ref is required")
	} else if credentialExists != nil && !credentialExists(ctx, credentialRef) {
		add("auth.credential_ref", "credential_ref does not exist")
	}

	seen := map[string]struct{}{}
	for i, step := range cfg.Steps {
		prefix := fmt.Sprintf("steps[%d]", i)
		id := strings.TrimSpace(step.ID)
		if id == "" {
			add(prefix+".id", "step id is required")
		} else if _, ok := seen[id]; ok {
			add(prefix+".id", "step id must be unique")
		}
		if id != "" {
			seen[id] = struct{}{}
		}
		if method := strings.ToUpper(strings.TrimSpace(step.Request.Method)); method != "GET" {
			add(prefix+".request.method", "only GET is supported")
		}
		if err := validateHTTPSURL(step.Request.URL); err != nil {
			add(prefix+".request.url", err.Error())
		}
		for _, issue := range validateRequestSecrets(prefix+".request", step.Request) {
			add(issue.Path, issue.Message)
		}
		if err := validateJSONPath(step.Extract.Items); err != nil {
			add(prefix+".extract.items", err.Error())
		}
		if step.Foreach != "" && !strings.HasSuffix(step.Foreach, ".items") {
			add(prefix+".foreach", "foreach must reference a previous step items collection")
		}
		if step.Map.Department == nil && step.Map.Member == nil {
			add(prefix+".map", "department or member mapping is required")
		}
		if step.Map.Department != nil {
			if strings.TrimSpace(step.Map.Department.ExternalID) == "" {
				add(prefix+".map.department.external_id", "department external_id mapping is required")
			}
			if strings.TrimSpace(step.Map.Department.Name) == "" {
				add(prefix+".map.department.name", "department name mapping is required")
			}
		}
		if step.Map.Member != nil {
			if strings.TrimSpace(step.Map.Member.Email) == "" {
				add(prefix+".map.member.email", "member email mapping is required")
			}
		}
	}
	if len(cfg.Overrides.Departments) > maxDepartmentOverrides {
		add("overrides.departments", fmt.Sprintf("must contain at most %d entries", maxDepartmentOverrides))
	}
	seenOverrideDepartments := map[string]struct{}{}
	for index, override := range cfg.Overrides.Departments {
		prefix := fmt.Sprintf("overrides.departments[%d]", index)
		externalID := strings.TrimSpace(override.ExternalID)
		if externalID == "" {
			add(prefix+".external_id", "external_id is required")
		} else if _, exists := seenOverrideDepartments[externalID]; exists {
			add(prefix+".external_id", "external_id must be unique within department overrides")
		} else {
			seenOverrideDepartments[externalID] = struct{}{}
		}
		if len(override.Metadata) == 0 {
			add(prefix+".metadata", "metadata operation is required")
			continue
		}
		for key, operation := range override.Metadata {
			keyPath := prefix + ".metadata." + key
			if key != representativeExternalIDsMetadataKey {
				add(keyPath, "only representative_external_ids append/remove overrides are supported")
				continue
			}
			validateMetadataOperation(keyPath, operation, add)
		}
	}
	if len(cfg.Overrides.Members) > maxMemberOverrides {
		add("overrides.members", fmt.Sprintf("must contain at most %d entries", maxMemberOverrides))
	}
	seenOverrideMembers := map[string]struct{}{}
	for index, override := range cfg.Overrides.Members {
		prefix := fmt.Sprintf("overrides.members[%d]", index)
		externalID := strings.TrimSpace(override.ExternalID)
		if externalID == "" {
			add(prefix+".external_id", "external_id is required")
		} else if _, exists := seenOverrideMembers[externalID]; exists {
			add(prefix+".external_id", "external_id must be unique within member overrides")
		} else {
			seenOverrideMembers[externalID] = struct{}{}
		}
		if len(override.Metadata) == 0 {
			add(prefix+".metadata", "metadata operation is required")
			continue
		}
		for key, operation := range override.Metadata {
			keyPath := prefix + ".metadata." + key
			if key != leaderDepartmentIDsMetadataKey {
				add(keyPath, "only leader_department_ids append/remove overrides are supported")
				continue
			}
			validateMetadataOperation(keyPath, operation, add)
		}
	}
	return issues
}

func validateMetadataOperation(path string, operation MetadataOverrideOperation, add func(string, string)) {
	if len(operation.Append) == 0 && len(operation.Remove) == 0 {
		add(path, "append or remove must contain at least one value")
		return
	}

	appendValues := validateMetadataValues(path+".append", "append", operation.Append, add)
	validateMetadataValues(path+".remove", "remove", operation.Remove, func(issuePath, message string) {
		add(issuePath, message)
	})
	for index, value := range operation.Remove {
		normalized := strings.TrimSpace(value)
		if normalized == "" {
			continue
		}
		if _, exists := appendValues[normalized]; exists {
			add(fmt.Sprintf("%s.remove[%d]", path, index), "value must not also appear in append")
		}
	}
}

func validateMetadataValues(path, operationName string, values []string, add func(string, string)) map[string]struct{} {
	if len(values) > maxMetadataOperationValues {
		add(path, fmt.Sprintf("%s must contain at most %d values", operationName, maxMetadataOperationValues))
	}
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		normalized := strings.TrimSpace(value)
		valuePath := fmt.Sprintf("%s[%d]", path, index)
		if normalized == "" {
			add(valuePath, operationName+" value must not be blank")
			continue
		}
		if _, exists := seen[normalized]; exists {
			add(valuePath, operationName+" value must be unique")
			continue
		}
		seen[normalized] = struct{}{}
	}
	return seen
}

func validateHTTPSURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("url is invalid")
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("url must use https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("url host is required")
	}
	return nil
}

func validateRequestSecrets(prefix string, request RequestConfig) []ValidationIssue {
	var issues []ValidationIssue
	add := func(path, message string) {
		issues = append(issues, ValidationIssue{Path: path, Message: message})
	}

	if parsed, err := url.Parse(strings.TrimSpace(request.URL)); err == nil {
		for key, values := range parsed.Query() {
			if isSensitiveCredentialKey(key) {
				add(prefix+".url", "request url must not include credential query parameters; use auth.credential_ref")
				break
			}
			for _, value := range values {
				if looksLikeLiteralSecret(value) {
					add(prefix+".url", "request url must not include literal secrets; use auth.credential_ref")
					break
				}
			}
		}
	}

	for key, value := range request.Headers {
		path := prefix + ".headers." + key
		if isSensitiveCredentialKey(key) {
			add(path, "request headers must not include credential values; use auth.header with auth.credential_ref")
			continue
		}
		if looksLikeLiteralSecret(value) {
			add(path, "request header value looks like a literal secret; use auth.credential_ref")
		}
	}

	for key, value := range request.Query {
		path := prefix + ".query." + key
		if isSensitiveCredentialKey(key) {
			add(path, "request query must not include credential values; use auth.credential_ref")
			continue
		}
		if looksLikeLiteralSecret(value) {
			add(path, "request query value looks like a literal secret; use auth.credential_ref")
		}
	}
	return issues
}

func isSensitiveCredentialKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	sensitiveExact := map[string]struct{}{
		"authorization": {},
		"cookie":        {},
		"set-cookie":    {},
		"key":           {},
		"api-key":       {},
		"apikey":        {},
		"access-token":  {},
		"refresh-token": {},
	}
	if _, ok := sensitiveExact[normalized]; ok {
		return true
	}
	for _, marker := range []string{"token", "secret", "password", "credential"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return strings.HasSuffix(normalized, "-key")
}

func looksLikeLiteralSecret(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{"bearer ", "basic ", "token ", "apikey ", "api-key "} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	if strings.Contains(trimmed, "{{") {
		return false
	}
	if strings.HasPrefix(trimmed, "eyJ") && strings.Count(trimmed, ".") >= 2 {
		return true
	}
	for _, prefix := range []string{"sk-", "ghp_", "glpat-", "xoxb-", "xoxp-"} {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}
