package directorysync

import (
	"context"
	"fmt"
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
        department_external_id: "{{ source.external_id }}"
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

func TestParseAndValidateDSLAcceptsDepartmentMetadataAppendOverride(t *testing.T) {
	cfg, err := ParseDSL(validDirectoryDSL + `
overrides:
  departments:
    - external_id: department-root
      metadata:
        representative_external_ids:
          append:
            - member-added
`)
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}
	issues := ValidateDSL(context.Background(), cfg, func(_ context.Context, ref string) bool {
		return ref == "directory_api_key"
	})
	if len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
	if got := cfg.Overrides.Departments[0].Metadata["representative_external_ids"].Append; len(got) != 1 || got[0] != "member-added" {
		t.Fatalf("override append = %#v, want member-added", got)
	}
}

func TestParseAndValidateDSLAcceptsDepartmentMetadataRemoveOverride(t *testing.T) {
	cfg, err := ParseDSL(validDirectoryDSL + `
overrides:
  departments:
    - external_id: department-root
      metadata:
        representative_external_ids:
          remove:
            - member-removed
`)
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

func TestParseAndValidateDSLAcceptsMemberMetadataRemoveOverride(t *testing.T) {
	cfg, err := ParseDSL(validDirectoryDSL + `
overrides:
  members:
    - external_id: member-alpha
      metadata:
        leader_department_ids:
          remove:
            - department-beta
`)
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}
	issues := ValidateDSL(context.Background(), cfg, func(_ context.Context, ref string) bool {
		return ref == "directory_api_key"
	})
	if len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
	if got := cfg.Overrides.Members[0].Metadata["leader_department_ids"].Remove; len(got) != 1 || got[0] != "department-beta" {
		t.Fatalf("override remove = %#v, want department-beta", got)
	}
}

func TestValidateDSLRejectsInvalidDepartmentMetadataAppendOverrides(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*DSL)
		wantIssue string
	}{
		{
			name: "missing department external id",
			mutate: func(cfg *DSL) {
				cfg.Overrides.Departments[0].ExternalID = ""
			},
			wantIssue: "overrides.departments[0].external_id",
		},
		{
			name: "duplicate department target",
			mutate: func(cfg *DSL) {
				cfg.Overrides.Departments = append(cfg.Overrides.Departments, cfg.Overrides.Departments[0])
			},
			wantIssue: "overrides.departments[1].external_id",
		},
		{
			name: "missing metadata operation",
			mutate: func(cfg *DSL) {
				cfg.Overrides.Departments[0].Metadata = nil
			},
			wantIssue: "overrides.departments[0].metadata",
		},
		{
			name: "unsupported metadata key",
			mutate: func(cfg *DSL) {
				cfg.Overrides.Departments[0].Metadata = map[string]MetadataOverrideOperation{
					"unrecognized_flag": {Append: []string{"value"}},
				}
			},
			wantIssue: "overrides.departments[0].metadata.unrecognized_flag",
		},
		{
			name: "metadata key must be canonical",
			mutate: func(cfg *DSL) {
				cfg.Overrides.Departments[0].Metadata = map[string]MetadataOverrideOperation{
					" representative_external_ids ": {Remove: []string{"member-added"}},
				}
			},
			wantIssue: "overrides.departments[0].metadata. representative_external_ids ",
		},
		{
			name: "empty operation",
			mutate: func(cfg *DSL) {
				cfg.Overrides.Departments[0].Metadata["representative_external_ids"] = MetadataOverrideOperation{}
			},
			wantIssue: "overrides.departments[0].metadata.representative_external_ids",
		},
		{
			name: "blank append value",
			mutate: func(cfg *DSL) {
				cfg.Overrides.Departments[0].Metadata["representative_external_ids"] = MetadataOverrideOperation{Append: []string{" "}}
			},
			wantIssue: "overrides.departments[0].metadata.representative_external_ids.append[0]",
		},
		{
			name: "too many department overrides",
			mutate: func(cfg *DSL) {
				cfg.Overrides.Departments = make([]DepartmentOverride, 101)
				for index := range cfg.Overrides.Departments {
					cfg.Overrides.Departments[index] = DepartmentOverride{
						ExternalID: fmt.Sprintf("department-%d", index),
						Metadata: map[string]MetadataOverrideOperation{
							"representative_external_ids": {Append: []string{"member-added"}},
						},
					}
				}
			},
			wantIssue: "overrides.departments",
		},
		{
			name: "too many append values",
			mutate: func(cfg *DSL) {
				cfg.Overrides.Departments[0].Metadata["representative_external_ids"] = MetadataOverrideOperation{Append: make([]string, 101)}
				for index := range cfg.Overrides.Departments[0].Metadata["representative_external_ids"].Append {
					cfg.Overrides.Departments[0].Metadata["representative_external_ids"].Append[index] = fmt.Sprintf("member-%d", index)
				}
			},
			wantIssue: "overrides.departments[0].metadata.representative_external_ids.append",
		},
		{
			name: "duplicate normalized append value",
			mutate: func(cfg *DSL) {
				cfg.Overrides.Departments[0].Metadata["representative_external_ids"] = MetadataOverrideOperation{Append: []string{"member-added", " member-added "}}
			},
			wantIssue: "overrides.departments[0].metadata.representative_external_ids.append[1]",
		},
		{
			name: "duplicate normalized remove value",
			mutate: func(cfg *DSL) {
				cfg.Overrides.Departments[0].Metadata["representative_external_ids"] = MetadataOverrideOperation{Remove: []string{"member-removed", " member-removed "}}
			},
			wantIssue: "overrides.departments[0].metadata.representative_external_ids.remove[1]",
		},
		{
			name: "same normalized value in append and remove",
			mutate: func(cfg *DSL) {
				cfg.Overrides.Departments[0].Metadata["representative_external_ids"] = MetadataOverrideOperation{
					Append: []string{"member-shared"},
					Remove: []string{" member-shared "},
				}
			},
			wantIssue: "overrides.departments[0].metadata.representative_external_ids.remove[0]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseDSL(validDirectoryDSL + `
overrides:
  departments:
    - external_id: department-root
      metadata:
        representative_external_ids:
          append:
            - member-added
`)
			if err != nil {
				t.Fatalf("ParseDSL: %v", err)
			}
			tt.mutate(cfg)
			issues := ValidateDSL(context.Background(), cfg, func(_ context.Context, ref string) bool {
				return ref == "directory_api_key"
			})
			if !containsIssuePath(issues, tt.wantIssue) {
				t.Fatalf("issues = %#v, want path %q", issues, tt.wantIssue)
			}
		})
	}
}

func TestValidateDSLRejectsInvalidMemberMetadataOverrides(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*DSL)
		wantIssue string
	}{
		{
			name: "missing member external id",
			mutate: func(cfg *DSL) {
				cfg.Overrides.Members[0].ExternalID = " "
			},
			wantIssue: "overrides.members[0].external_id",
		},
		{
			name: "duplicate normalized member target",
			mutate: func(cfg *DSL) {
				duplicate := cfg.Overrides.Members[0]
				duplicate.ExternalID = " member-alpha "
				cfg.Overrides.Members = append(cfg.Overrides.Members, duplicate)
			},
			wantIssue: "overrides.members[1].external_id",
		},
		{
			name: "missing metadata operation",
			mutate: func(cfg *DSL) {
				cfg.Overrides.Members[0].Metadata = nil
			},
			wantIssue: "overrides.members[0].metadata",
		},
		{
			name: "unsupported metadata key",
			mutate: func(cfg *DSL) {
				cfg.Overrides.Members[0].Metadata = map[string]MetadataOverrideOperation{"unrecognized_flag": {Remove: []string{"value"}}}
			},
			wantIssue: "overrides.members[0].metadata.unrecognized_flag",
		},
		{
			name: "metadata key must be canonical",
			mutate: func(cfg *DSL) {
				cfg.Overrides.Members[0].Metadata = map[string]MetadataOverrideOperation{" leader_department_ids ": {Remove: []string{"department-beta"}}}
			},
			wantIssue: "overrides.members[0].metadata. leader_department_ids ",
		},
		{
			name: "empty operation",
			mutate: func(cfg *DSL) {
				cfg.Overrides.Members[0].Metadata["leader_department_ids"] = MetadataOverrideOperation{}
			},
			wantIssue: "overrides.members[0].metadata.leader_department_ids",
		},
		{
			name: "blank remove value",
			mutate: func(cfg *DSL) {
				cfg.Overrides.Members[0].Metadata["leader_department_ids"] = MetadataOverrideOperation{Remove: []string{" "}}
			},
			wantIssue: "overrides.members[0].metadata.leader_department_ids.remove[0]",
		},
		{
			name: "duplicate append value",
			mutate: func(cfg *DSL) {
				cfg.Overrides.Members[0].Metadata["leader_department_ids"] = MetadataOverrideOperation{Append: []string{"department-alpha", " department-alpha "}}
			},
			wantIssue: "overrides.members[0].metadata.leader_department_ids.append[1]",
		},
		{
			name: "same value in append and remove",
			mutate: func(cfg *DSL) {
				cfg.Overrides.Members[0].Metadata["leader_department_ids"] = MetadataOverrideOperation{Append: []string{"department-alpha"}, Remove: []string{" department-alpha "}}
			},
			wantIssue: "overrides.members[0].metadata.leader_department_ids.remove[0]",
		},
		{
			name: "too many member overrides",
			mutate: func(cfg *DSL) {
				cfg.Overrides.Members = make([]MemberOverride, 101)
				for index := range cfg.Overrides.Members {
					cfg.Overrides.Members[index] = MemberOverride{
						ExternalID: fmt.Sprintf("member-%d", index),
						Metadata: map[string]MetadataOverrideOperation{
							"leader_department_ids": {Remove: []string{"department-beta"}},
						},
					}
				}
			},
			wantIssue: "overrides.members",
		},
		{
			name: "too many remove values",
			mutate: func(cfg *DSL) {
				values := make([]string, 101)
				for index := range values {
					values[index] = fmt.Sprintf("department-%d", index)
				}
				cfg.Overrides.Members[0].Metadata["leader_department_ids"] = MetadataOverrideOperation{Remove: values}
			},
			wantIssue: "overrides.members[0].metadata.leader_department_ids.remove",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseDSL(validDirectoryDSL + `
overrides:
  members:
    - external_id: member-alpha
      metadata:
        leader_department_ids:
          remove:
            - department-beta
`)
			if err != nil {
				t.Fatalf("ParseDSL: %v", err)
			}
			tt.mutate(cfg)
			issues := ValidateDSL(context.Background(), cfg, func(_ context.Context, ref string) bool {
				return ref == "directory_api_key"
			})
			if !containsIssuePath(issues, tt.wantIssue) {
				t.Fatalf("issues = %#v, want path %q", issues, tt.wantIssue)
			}
		})
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
		{
			name:      "literal authorization header",
			raw:       strings.Replace(validDirectoryDSL, "      url: https://directory.example.com/api/departments\n", "      url: https://directory.example.com/api/departments\n      headers:\n        Authorization: Bearer test-token\n", 1),
			wantIssue: "steps[0].request.headers.Authorization",
		},
		{
			name:      "sensitive query parameter",
			raw:       strings.Replace(validDirectoryDSL, "      url: https://directory.example.com/api/departments\n", "      url: https://directory.example.com/api/departments\n      query:\n        access_token: test-token\n", 1),
			wantIssue: "steps[0].request.query.access_token",
		},
		{
			name:      "secret-looking url query",
			raw:       strings.Replace(validDirectoryDSL, "https://directory.example.com/api/departments", "https://directory.example.com/api/departments?token=test-token", 1),
			wantIssue: "steps[0].request.url",
		},
		{
			name:      "templated bearer header",
			raw:       strings.Replace(validDirectoryDSL, "      url: https://directory.example.com/api/departments\n", "      url: https://directory.example.com/api/departments\n      headers:\n        X-Directory-Session: Bearer {{ item.token }}\n", 1),
			wantIssue: "steps[0].request.headers.X-Directory-Session",
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
