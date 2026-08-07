package directorysync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type staticCredentialResolver map[string]string

func (r staticCredentialResolver) ResolveCredential(_ context.Context, ref string) (string, bool, error) {
	value, ok := r[ref]
	return value, ok, nil
}

func TestExecutorUsesInjectedHTTPClient(t *testing.T) {
	var calls int
	injected := &http.Client{
		Timeout: 23 * time.Second,
		Transport: directoryRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{
					"data":{"departments":[{"id":"dept-alpha","name":"Department Alpha","path":"Department Alpha"}]}
				}`)),
				Request: req,
			}, nil
		}),
	}
	cfg, err := ParseDSL(validDirectoryDSL)
	if err != nil {
		t.Fatalf("ParseDSL() error = %v", err)
	}
	cfg.Steps = cfg.Steps[:1]

	executor := NewExecutor(ExecutorOptions{HTTPClient: injected})
	result, err := executor.Execute(context.Background(), cfg, staticCredentialResolver{"directory_api_key": "test-directory-secret"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if calls != 1 || result.HTTPRequestCount != 1 {
		t.Fatalf("HTTP calls = %d, result count = %d; want one injected request", calls, result.HTTPRequestCount)
	}
	if executor.client != injected || executor.client.Timeout != 23*time.Second {
		t.Fatal("NewExecutor() did not retain the injected HTTP client")
	}
}

type directoryRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn directoryRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestExecutorAppliesDepartmentMetadataAppendOverrideToExactTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, []map[string]any{
			{"id": "department-root", "name": "Department Root", "leader_ids": []string{"member-existing"}},
			{"id": "department-sibling", "name": "Department Sibling", "leader_ids": []string{"member-sibling"}},
		})
	}))
	defer server.Close()

	raw := `
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
      url: ` + server.URL + `
    extract:
      items: $
    map:
      department:
        external_id: $.id
        name: $.name
        metadata:
          representative_external_ids: $.leader_ids
overrides:
  departments:
    - external_id: department-root
      metadata:
        representative_external_ids:
          append:
            - member-existing
            - member-added
`
	cfg, err := ParseDSL(raw)
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}
	result, err := NewExecutor(ExecutorOptions{AllowHTTP: true}).Execute(
		context.Background(),
		cfg,
		staticCredentialResolver{"directory_api_key": "test-directory-secret"},
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := fmt.Sprint(result.Departments[0].Metadata["representative_external_ids"]), "[member-existing member-added]"; got != want {
		t.Fatalf("root representatives = %s, want %s", got, want)
	}
	if got, want := fmt.Sprint(result.Departments[1].Metadata["representative_external_ids"]), "[member-sibling]"; got != want {
		t.Fatalf("sibling representatives = %s, want %s", got, want)
	}
}

func TestExecutorAppliesRepresentativeMetadataRemoveAndAppendOverrides(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/departments":
			writeJSON(t, w, []map[string]any{
				{"id": "department-root", "name": "Department Root", "leader_ids": []string{" member-keep ", "member-remove", "member-keep"}},
				{"id": "department-sibling", "name": "Department Sibling", "leader_ids": []string{"member-sibling"}},
			})
		case "/members":
			writeJSON(t, w, []map[string]any{
				{"id": "member-actor", "email": "actor@example.com", "leader_department_ids": []string{" department-keep ", "department-remove", "department-keep"}},
				{"id": "member-sibling", "email": "sibling@example.org", "leader_department_ids": []string{"department-sibling"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	raw := `
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
      url: ` + server.URL + `/departments
    extract:
      items: $
    map:
      department:
        external_id: $.id
        name: $.name
        metadata:
          representative_external_ids: $.leader_ids
  - id: members
    request:
      method: GET
      url: ` + server.URL + `/members
    extract:
      items: $
    map:
      member:
        external_id: $.id
        email: $.email
        metadata:
          leader_department_ids: $.leader_department_ids
overrides:
  departments:
    - external_id: department-root
      metadata:
        representative_external_ids:
          remove:
            - member-remove
            - member-absent
          append:
            - " member-added "
  members:
    - external_id: member-actor
      metadata:
        leader_department_ids:
          remove:
            - department-remove
            - department-absent
          append:
            - " department-added "
`
	cfg, err := ParseDSL(raw)
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}

	result, err := NewExecutor(ExecutorOptions{AllowHTTP: true}).Execute(
		context.Background(),
		cfg,
		staticCredentialResolver{"directory_api_key": "test-directory-secret"},
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := fmt.Sprint(result.Departments[0].Metadata["representative_external_ids"]), "[member-keep member-added]"; got != want {
		t.Fatalf("root representatives = %s, want %s", got, want)
	}
	if got, want := fmt.Sprint(result.Departments[1].Metadata["representative_external_ids"]), "[member-sibling]"; got != want {
		t.Fatalf("sibling representatives = %s, want %s", got, want)
	}
	if got, want := fmt.Sprint(result.Members[0].Metadata["leader_department_ids"]), "[department-keep department-added]"; got != want {
		t.Fatalf("actor leader departments = %s, want %s", got, want)
	}
	if got, want := fmt.Sprint(result.Members[1].Metadata["leader_department_ids"]), "[department-sibling]"; got != want {
		t.Fatalf("sibling leader departments = %s, want %s", got, want)
	}
}

func TestExecutorRejectsDepartmentMetadataOverrideForMissingTarget(t *testing.T) {
	injected := &http.Client{Transport: directoryRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"data":{"departments":[{"id":"department-root","name":"Department Root"}]}}`)),
			Request:    req,
		}, nil
	})}
	cfg, err := ParseDSL(validDirectoryDSL)
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}
	cfg.Steps = cfg.Steps[:1]
	cfg.Overrides.Departments = []DepartmentOverride{{
		ExternalID: "department-missing",
		Metadata: map[string]MetadataOverrideOperation{
			"representative_external_ids": {Append: []string{"member-added"}},
		},
	}}

	_, err = NewExecutor(ExecutorOptions{HTTPClient: injected}).Execute(
		context.Background(),
		cfg,
		staticCredentialResolver{"directory_api_key": "test-directory-secret"},
	)
	if err == nil || !strings.Contains(err.Error(), `department metadata override target "department-missing" was not mapped`) {
		t.Fatalf("Execute error = %v, want missing override target", err)
	}
}

func TestExecutorRejectsMemberMetadataOverrideForMissingTarget(t *testing.T) {
	injected := &http.Client{Transport: directoryRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"data":{"users":[{"id":"member-alpha","email":"alpha@example.com"}]}}`)),
			Request:    req,
		}, nil
	})}
	cfg, err := ParseDSL(validDirectoryDSL)
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}
	cfg.Steps = cfg.Steps[1:]
	cfg.Steps[0].Foreach = ""
	cfg.Overrides.Members = []MemberOverride{{
		ExternalID: "member-missing",
		Metadata: map[string]MetadataOverrideOperation{
			"leader_department_ids": {Remove: []string{"department-beta"}},
		},
	}}

	_, err = NewExecutor(ExecutorOptions{HTTPClient: injected}).Execute(
		context.Background(),
		cfg,
		staticCredentialResolver{"directory_api_key": "test-directory-secret"},
	)
	if err == nil || !strings.Contains(err.Error(), `member metadata override target "member-missing" was not mapped`) {
		t.Fatalf("Execute error = %v, want missing member override target", err)
	}
}

func TestExecutorRejectsAmbiguousMemberMetadataOverrideTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, []map[string]any{
			{"id": "member-shared", "email": "alice@example.com"},
			{"id": "member-shared", "email": "bob@example.org"},
		})
	}))
	defer server.Close()

	raw := `
version: 1
scope: full_company
auth:
  type: header
  header: X-Directory-API-Key
  credential_ref: directory_api_key
steps:
  - id: members
    request:
      method: GET
      url: ` + server.URL + `
    extract:
      items: $
    map:
      member:
        external_id: $.id
        email: $.email
overrides:
  members:
    - external_id: member-shared
      metadata:
        leader_department_ids:
          remove:
            - department-alpha
`
	cfg, err := ParseDSL(raw)
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}

	_, err = NewExecutor(ExecutorOptions{AllowHTTP: true}).Execute(
		context.Background(),
		cfg,
		staticCredentialResolver{"directory_api_key": "test-directory-secret"},
	)
	if err == nil || !strings.Contains(err.Error(), `member metadata override target "member-shared" was mapped more than once`) {
		t.Fatalf("Execute error = %v, want ambiguous exact member target", err)
	}
}

func TestExecutorRejectsAmbiguousDepartmentMetadataOverrideTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, []map[string]any{
			{"id": "department-shared", "name": "Department Alpha"},
			{"id": "department-shared", "name": "Department Beta"},
		})
	}))
	defer server.Close()

	raw := `
version: 1
scope: full_company
auth:
  type: header
  header: X-Directory-API-Key
  credential_ref: directory_api_key
steps:
  - id: departments
    request:
      method: GET
      url: ` + server.URL + `
    extract:
      items: $
    map:
      department:
        external_id: $.id
        name: $.name
overrides:
  departments:
    - external_id: department-shared
      metadata:
        representative_external_ids:
          remove:
            - member-alpha
`
	cfg, err := ParseDSL(raw)
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}

	_, err = NewExecutor(ExecutorOptions{AllowHTTP: true}).Execute(
		context.Background(),
		cfg,
		staticCredentialResolver{"directory_api_key": "test-directory-secret"},
	)
	if err == nil || !strings.Contains(err.Error(), `department metadata override target "department-shared" was mapped more than once`) {
		t.Fatalf("Execute error = %v, want ambiguous exact department target", err)
	}
}

func TestExecutorRunsForeachAndNormalizesMembers(t *testing.T) {
	var seenAuthHeaders []string
	var seenDepartmentQueries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuthHeaders = append(seenAuthHeaders, r.Header.Get("X-Directory-API-Key"))
		switch r.URL.Path {
		case "/departments":
			writeJSON(t, w, map[string]any{
				"data": map[string]any{
					"departments": []map[string]any{
						{"id": "dept-alpha", "name": "Department Alpha", "path": "Department Alpha"},
						{"id": "dept-beta", "name": "Department Beta", "path": "Department Beta"},
					},
				},
			})
		case "/users":
			departmentID := r.URL.Query().Get("department_id")
			seenDepartmentQueries = append(seenDepartmentQueries, departmentID)
			users := []map[string]any{
				{"id": "member-" + departmentID, "email": departmentID + "@example.com", "name": departmentID, "status": "active"},
			}
			if departmentID == "dept-beta" {
				users = append(users,
					map[string]any{"id": "bad-email", "email": "not-an-email", "name": "Bad Email", "status": "active"},
					map[string]any{"id": "duplicate", "email": " DEPT-ALPHA@example.com ", "name": "Duplicate Email", "status": "active"},
				)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{"users": users}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	raw := strings.ReplaceAll(validDirectoryDSL, "https://directory.example.com/api/departments", server.URL+"/departments")
	raw = strings.ReplaceAll(raw, "https://directory.example.com/api/users", server.URL+"/users")
	cfg, err := ParseDSL(raw)
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}

	executor := NewExecutor(ExecutorOptions{AllowHTTP: true})
	result, err := executor.Execute(context.Background(), cfg, staticCredentialResolver{"directory_api_key": "test-directory-secret"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if result.HTTPRequestCount != 3 {
		t.Fatalf("HTTPRequestCount = %d, want 3", result.HTTPRequestCount)
	}
	if len(result.Departments) != 2 {
		t.Fatalf("departments = %d, want 2", len(result.Departments))
	}
	if len(result.Members) != 2 {
		t.Fatalf("members = %#v, want two valid unique members", result.Members)
	}
	if result.Members[0].EmailNormalized != "dept-alpha@example.com" {
		t.Fatalf("first email = %q", result.Members[0].EmailNormalized)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("warnings = %#v, want invalid email only; duplicate email across departments is a membership", result.Warnings)
	}
	if fmt.Sprint(seenDepartmentQueries) != "[dept-alpha dept-beta]" {
		t.Fatalf("department queries = %v", seenDepartmentQueries)
	}
	for _, header := range seenAuthHeaders {
		if header != "test-directory-secret" {
			t.Fatalf("auth header = %q", header)
		}
	}
}

func TestExecutorCoalescesDuplicateEmailIntoMultipleDepartmentMemberships(t *testing.T) {
	var seenDepartmentQueries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/departments":
			writeJSON(t, w, map[string]any{
				"data": map[string]any{
					"departments": []map[string]any{
						{"id": "dept-alpha", "name": "Department Alpha", "path": "Department Alpha"},
						{"id": "dept-beta", "name": "Department Beta", "path": "Department Beta"},
					},
				},
			})
		case "/users":
			departmentID := r.URL.Query().Get("department_id")
			seenDepartmentQueries = append(seenDepartmentQueries, departmentID)
			writeJSON(t, w, map[string]any{"data": map[string]any{"users": []map[string]any{
				{"id": "member-alice", "email": "alice@example.com", "name": "Alice", "status": "active"},
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	raw := strings.ReplaceAll(validDirectoryDSL, "https://directory.example.com/api/departments", server.URL+"/departments")
	raw = strings.ReplaceAll(raw, "https://directory.example.com/api/users", server.URL+"/users")
	cfg, err := ParseDSL(raw)
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}

	executor := NewExecutor(ExecutorOptions{AllowHTTP: true})
	result, err := executor.Execute(context.Background(), cfg, staticCredentialResolver{"directory_api_key": "test-directory-secret"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if fmt.Sprint(seenDepartmentQueries) != "[dept-alpha dept-beta]" {
		t.Fatalf("department queries = %v", seenDepartmentQueries)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want no warning for one member in two departments", result.Warnings)
	}
	if len(result.Members) != 1 {
		t.Fatalf("members = %#v, want one canonical member", result.Members)
	}
	member := result.Members[0]
	if member.EmailNormalized != "alice@example.com" {
		t.Fatalf("email = %q, want alice@example.com", member.EmailNormalized)
	}
	if got, want := fmt.Sprint(member.DepartmentExternalIDs), "[dept-alpha dept-beta]"; got != want {
		t.Fatalf("department ids = %s, want %s", got, want)
	}
	if member.DepartmentExternalID != "dept-alpha" {
		t.Fatalf("primary department = %q, want first department dept-alpha", member.DepartmentExternalID)
	}
}

func TestExecutorMapsFirstDepartmentIDFromMemberArray(t *testing.T) {
	raw := strings.ReplaceAll(validDirectoryDSL, `department_external_id: "{{ source.external_id }}"`, "department_external_id: $.department_ids[0]")
	cfg, err := ParseDSL(raw)
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}
	member := map[string]any{
		"id":             "member-alpha",
		"email":          "alice@example.com",
		"name":           "Alice",
		"department_ids": []any{"dept-alpha", "dept-parent"},
	}

	mapped, warnings := mapMember("members", cfg.Steps[1].Map.Member, member, map[string]any{"external_id": "dept-parent"}, map[string]struct{}{})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if mapped == nil {
		t.Fatal("mapped member is nil")
	}
	if mapped.DepartmentExternalID != "dept-alpha" {
		t.Fatalf("department_external_id = %q, want dept-alpha", mapped.DepartmentExternalID)
	}
}

func TestExecutorUnionsPrimaryAndMultipleDepartmentMappings(t *testing.T) {
	raw := strings.ReplaceAll(validDirectoryDSL, `department_external_id: "{{ source.external_id }}"`, "department_external_id: $.primary_department_id\n        department_external_ids: $.department_ids")
	cfg, err := ParseDSL(raw)
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}
	member := map[string]any{
		"id":                    "member-alpha",
		"email":                 "alice@example.com",
		"name":                  "Alice",
		"primary_department_id": "dept-primary",
		"department_ids":        []any{"dept-secondary"},
	}

	mapped, warnings := mapMember("members", cfg.Steps[1].Map.Member, member, map[string]any{}, map[string]struct{}{})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if mapped == nil {
		t.Fatal("mapped member is nil")
	}
	if mapped.DepartmentExternalID != "dept-primary" {
		t.Fatalf("primary department = %q, want dept-primary", mapped.DepartmentExternalID)
	}
	if got, want := fmt.Sprint(mapped.DepartmentExternalIDs), "[dept-primary dept-secondary]"; got != want {
		t.Fatalf("department ids = %s, want %s", got, want)
	}
}

func TestExecutorMapsDepartmentAndMemberMetadata(t *testing.T) {
	raw := strings.ReplaceAll(validDirectoryDSL, "        path: $.path\n", "        path: $.path\n        metadata:\n          representative_external_ids: $.leader_ids\n")
	raw = strings.ReplaceAll(raw, "        status: $.status\n", "        status: $.status\n        metadata:\n          leader_department_ids: $.leader_department_ids\n")
	cfg, err := ParseDSL(raw)
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}

	department := mapDepartment(cfg.Steps[0].Map.Department, map[string]any{
		"id":         "dept-alpha",
		"name":       "Department Alpha",
		"path":       "Department Alpha",
		"leader_ids": []any{"member-alpha"},
	})
	if fmt.Sprint(department.Metadata["representative_external_ids"]) != "[member-alpha]" {
		t.Fatalf("department metadata = %#v, want representative_external_ids array", department.Metadata)
	}

	member, warnings := mapMember("members", cfg.Steps[1].Map.Member, map[string]any{
		"id":                    "member-alpha",
		"email":                 "alice@example.com",
		"name":                  "Alice Example",
		"leader_department_ids": []any{"dept-alpha"},
	}, map[string]any{"external_id": "dept-alpha"}, map[string]struct{}{})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if member == nil {
		t.Fatal("member is nil")
	}
	if fmt.Sprint(member.Metadata["leader_department_ids"]) != "[dept-alpha]" {
		t.Fatalf("member metadata = %#v, want leader_department_ids array", member.Metadata)
	}
}

func TestExecutorPreservesNumericIDsAsDecimalStrings(t *testing.T) {
	const departmentID = "123456789012345678"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/departments":
			_, _ = fmt.Fprintf(w, `[{"id":%s,"name":"Department Alpha","path":"Department Alpha"}]`, departmentID)
		case "/users":
			_, _ = fmt.Fprintf(w, `[{"id":"member-alpha","email":"alice@example.com","name":"Alice","departmentIds":[%s]}]`, departmentID)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	raw := strings.ReplaceAll(validDirectoryDSL, "https://directory.example.com/api/departments", server.URL+"/departments")
	raw = strings.ReplaceAll(raw, "https://directory.example.com/api/users", server.URL+"/users")
	raw = strings.ReplaceAll(raw, "items: $.data.departments", "items: $")
	raw = strings.ReplaceAll(raw, "items: $.data.users", "items: $")
	raw = strings.ReplaceAll(raw, `department_external_id: "{{ source.external_id }}"`, "department_external_id: $.departmentIds[0]")
	cfg, err := ParseDSL(raw)
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}

	executor := NewExecutor(ExecutorOptions{AllowHTTP: true})
	result, err := executor.Execute(context.Background(), cfg, staticCredentialResolver{"directory_api_key": "test-directory-secret"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Departments[0].ExternalID != departmentID {
		t.Fatalf("department external id = %q, want %s", result.Departments[0].ExternalID, departmentID)
	}
	if result.Members[0].DepartmentExternalID != departmentID {
		t.Fatalf("member department external id = %q, want %s", result.Members[0].DepartmentExternalID, departmentID)
	}
}

func TestExecutorExtractsRootArrayResponses(t *testing.T) {
	var seenDepartmentQueries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/departments":
			writeJSON(t, w, []map[string]any{
				{"id": "dept-alpha", "name": "Department Alpha", "path": "Department Alpha"},
				{"id": "dept-beta", "name": "Department Beta", "path": "Department Beta"},
			})
		case "/users":
			departmentID := r.URL.Query().Get("department_id")
			seenDepartmentQueries = append(seenDepartmentQueries, departmentID)
			writeJSON(t, w, []map[string]any{
				{"id": "member-" + departmentID, "email": departmentID + "@example.com", "name": departmentID},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	raw := strings.ReplaceAll(validDirectoryDSL, "https://directory.example.com/api/departments", server.URL+"/departments")
	raw = strings.ReplaceAll(raw, "https://directory.example.com/api/users", server.URL+"/users")
	raw = strings.ReplaceAll(raw, "items: $.data.departments", "items: $")
	raw = strings.ReplaceAll(raw, "items: $.data.users", "items: $")
	cfg, err := ParseDSL(raw)
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}

	executor := NewExecutor(ExecutorOptions{AllowHTTP: true})
	result, err := executor.Execute(context.Background(), cfg, staticCredentialResolver{"directory_api_key": "test-directory-secret"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(result.Departments) != 2 {
		t.Fatalf("departments = %#v, want two departments", result.Departments)
	}
	if len(result.Members) != 2 {
		t.Fatalf("members = %#v, want two members", result.Members)
	}
	if fmt.Sprint(seenDepartmentQueries) != "[dept-alpha dept-beta]" {
		t.Fatalf("department queries = %v", seenDepartmentQueries)
	}
}

func TestExecutorEnforcesLimits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"departments":[]}}`))
	}))
	defer server.Close()

	raw := strings.ReplaceAll(validDirectoryDSL, "https://directory.example.com/api/departments", server.URL+"/departments")
	raw = strings.ReplaceAll(raw, "https://directory.example.com/api/users", server.URL+"/users")
	raw = strings.Replace(raw, "max_response_bytes: 1048576", "max_response_bytes: 8", 1)
	cfg, err := ParseDSL(raw)
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}

	executor := NewExecutor(ExecutorOptions{AllowHTTP: true})
	_, err = executor.Execute(context.Background(), cfg, staticCredentialResolver{"directory_api_key": "test-directory-secret"})
	if err == nil {
		t.Fatal("expected response-size limit error")
	}
	if !strings.Contains(err.Error(), "response too large") {
		t.Fatalf("error = %v, want response too large", err)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, body any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
