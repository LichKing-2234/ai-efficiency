package directorysync

import (
	"context"
	"strings"
	"testing"
)

const validDirectoryDSL = `
version: 1
scope: full_company
auth:
  type: header
  header: X-Directory-API-Key
  credential_ref: directory_api_key
limits:
  timeout_seconds: 5
  max_response_bytes: 1048576
  max_items: 100
steps:
  - id: departments
    request:
      method: GET
      url: https://directory.example.com/api/departments
    extract:
      items: $.data.departments
    map:
      department:
        external_id: $.id
        parent_external_id: $.parent_id
        name: $.name
        path: $.path
  - id: members
    foreach: departments.items
    request:
      method: GET
      url: https://directory.example.com/api/users
      query:
        department_id: "{{ item.external_id }}"
    extract:
      items: $.data.users
    map:
      member:
        external_id: $.id
        email: $.email
        display_name: $.name
        department_external_id: "{{ item.external_id }}"
        status: $.status
`

func TestParseDSLAcceptsSafeYAMLTemplate(t *testing.T) {
	cfg, err := ParseDSL(validDirectoryDSL)
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}
	if cfg.Version != 1 || cfg.Scope != "full_company" {
		t.Fatalf("version/scope = %d/%q, want 1/full_company", cfg.Version, cfg.Scope)
	}
	if cfg.Auth.CredentialRef != "directory_api_key" {
		t.Fatalf("credential_ref = %q", cfg.Auth.CredentialRef)
	}
	if len(cfg.Steps) != 2 || cfg.Steps[1].Foreach != "departments.items" {
		t.Fatalf("steps = %#v", cfg.Steps)
	}
}

func TestValidateDSLAcceptsRootArrayExtractPath(t *testing.T) {
	raw := strings.ReplaceAll(validDirectoryDSL, "items: $.data.departments", "items: $")
	raw = strings.ReplaceAll(raw, "items: $.data.users", "items: $")
	cfg, err := ParseDSL(raw)
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}
	issues := ValidateDSL(context.Background(), cfg, func(_ context.Context, ref string) bool {
		return ref == "directory_api_key"
	})
	if len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
}

func TestValidateDSLRejectsUnsupportedFeatures(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantIssue string
	}{
		{
			name:      "non https url",
			raw:       strings.Replace(validDirectoryDSL, "https://directory.example.com/api/departments", "http://directory.example.com/api/departments", 1),
			wantIssue: "steps[0].request.url",
		},
		{
			name:      "unsupported method",
			raw:       strings.Replace(validDirectoryDSL, "method: GET", "method: POST", 1),
			wantIssue: "steps[0].request.method",
		},
		{
			name:      "duplicate step id",
			raw:       strings.Replace(validDirectoryDSL, "id: members", "id: departments", 1),
			wantIssue: "steps[1].id",
		},
		{
			name:      "missing credential ref",
			raw:       strings.Replace(validDirectoryDSL, "credential_ref: directory_api_key", "credential_ref: missing_key", 1),
			wantIssue: "auth.credential_ref",
		},
		{
			name:      "missing member email mapping",
			raw:       strings.Replace(validDirectoryDSL, "        email: $.email\n", "", 1),
			wantIssue: "steps[1].map.member.email",
		},
		{
			name:      "missing department name mapping",
			raw:       strings.Replace(validDirectoryDSL, "        name: $.name\n", "", 1),
			wantIssue: "steps[0].map.department.name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseDSL(tt.raw)
			if err != nil {
				t.Fatalf("ParseDSL: %v", err)
			}
			issues := ValidateDSL(context.Background(), cfg, func(_ context.Context, ref string) bool {
				return ref == "directory_api_key"
			})
			if !containsIssuePath(issues, tt.wantIssue) {
				t.Fatalf("issues = %#v, want path %q", issues, tt.wantIssue)
			}
		})
	}
}

func containsIssuePath(issues []ValidationIssue, path string) bool {
	for _, issue := range issues {
		if issue.Path == path {
			return true
		}
	}
	return false
}
