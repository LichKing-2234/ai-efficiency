package directorysync

import (
	"encoding/json"
	"testing"
)

func TestEvaluateJSONPathSubset(t *testing.T) {
	var doc any
	if err := json.Unmarshal([]byte(`{
		"data": {
			"departments": [
				{"id": "dept-alpha", "name": "Department Alpha"},
				{"id": "dept-beta", "name": "Department Beta"}
			],
				"user": {"email": "alice@example.com", "department_ids": ["dept-alpha", "dept-beta"]}
		}
	}`), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	items, err := EvaluateJSONPath(doc, "$.data.departments")
	if err != nil {
		t.Fatalf("EvaluateJSONPath departments: %v", err)
	}
	if list, ok := items.([]any); !ok || len(list) != 2 {
		t.Fatalf("departments = %#v, want two items", items)
	}

	email, err := EvaluateJSONPath(doc, "$.data.user.email")
	if err != nil {
		t.Fatalf("EvaluateJSONPath email: %v", err)
	}
	if email != "alice@example.com" {
		t.Fatalf("email = %#v", email)
	}

	departmentID, err := EvaluateJSONPath(doc, "$.data.user.department_ids[0]")
	if err != nil {
		t.Fatalf("EvaluateJSONPath department id: %v", err)
	}
	if departmentID != "dept-alpha" {
		t.Fatalf("department id = %#v", departmentID)
	}
}

func TestEvaluateJSONPathAllowsRootDocument(t *testing.T) {
	root := []any{
		map[string]any{"id": "dept-alpha", "name": "Department Alpha"},
		map[string]any{"id": "dept-beta", "name": "Department Beta"},
	}

	value, err := EvaluateJSONPath(root, "$")
	if err != nil {
		t.Fatalf("EvaluateJSONPath root: %v", err)
	}
	if len(value.([]any)) != 2 {
		t.Fatalf("root value = %#v, want two items", value)
	}
}

func TestEvaluateJSONPathRejectsUnsupportedExpressions(t *testing.T) {
	if _, err := EvaluateJSONPath(map[string]any{"items": []any{}}, "data.items"); err == nil {
		t.Fatal("expected missing root prefix to fail")
	}
	if _, err := EvaluateJSONPath(map[string]any{"items": []any{}}, "$.items[*]"); err == nil {
		t.Fatal("expected wildcard expression to fail")
	}
}
