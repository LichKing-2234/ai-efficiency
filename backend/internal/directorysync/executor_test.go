package directorysync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type staticCredentialResolver map[string]string

func (r staticCredentialResolver) ResolveCredential(_ context.Context, ref string) (string, bool, error) {
	value, ok := r[ref]
	return value, ok, nil
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
	if len(result.Warnings) != 2 {
		t.Fatalf("warnings = %#v, want invalid email and duplicate email", result.Warnings)
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

func TestExecutorMapsFirstDepartmentIDFromMemberArray(t *testing.T) {
	raw := strings.ReplaceAll(validDirectoryDSL, `department_external_id: "{{ item.external_id }}"`, "department_external_id: $.department_ids[0]")
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
	raw = strings.ReplaceAll(raw, `department_external_id: "{{ item.external_id }}"`, "department_external_id: $.departmentIds[0]")
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
