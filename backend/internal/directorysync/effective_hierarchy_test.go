package directorysync

import (
	"fmt"
	"reflect"
	"testing"
)

func TestResolveEffectiveParentsSemantics(t *testing.T) {
	tests := []struct {
		name        string
		departments []DepartmentRecord
		want        map[string]string
	}{
		{
			name: "acyclic and orphan",
			departments: []DepartmentRecord{
				{ExternalID: "dept-root", Name: "Root"},
				{ExternalID: "dept-child", ParentExternalID: "dept-root", Name: "Child"},
				{ExternalID: "dept-orphan", ParentExternalID: "dept-missing", Name: "Orphan"},
			},
			want: map[string]string{"dept-root": "", "dept-child": "dept-root", "dept-orphan": ""},
		},
		{
			name: "name first cycle anchor",
			departments: []DepartmentRecord{
				{ExternalID: "dept-a", ParentExternalID: "dept-b", Name: "Zulu"},
				{ExternalID: "dept-b", ParentExternalID: "dept-c", Name: "Alpha"},
				{ExternalID: "dept-c", ParentExternalID: "dept-a", Name: "Alpha"},
			},
			want: map[string]string{"dept-a": "dept-b", "dept-b": "", "dept-c": "dept-a"},
		},
		{
			name: "tail entering cycle cannot become anchor",
			departments: []DepartmentRecord{
				{ExternalID: "dept-tail", ParentExternalID: "dept-a", Name: "Aardvark"},
				{ExternalID: "dept-a", ParentExternalID: "dept-b", Name: "Zulu"},
				{ExternalID: "dept-b", ParentExternalID: "dept-c", Name: "Alpha"},
				{ExternalID: "dept-c", ParentExternalID: "dept-a", Name: "Alpha"},
			},
			want: map[string]string{"dept-tail": "dept-a", "dept-a": "dept-b", "dept-b": "", "dept-c": "dept-a"},
		},
		{
			name:        "self cycle",
			departments: []DepartmentRecord{{ExternalID: "dept-self", ParentExternalID: "dept-self", Name: "Self"}},
			want:        map[string]string{"dept-self": ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveEffectiveParents(tt.departments)
			if err != nil {
				t.Fatalf("resolveEffectiveParents() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("effective parents = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestResolveEffectiveParentsDeterministicAcrossInputOrder(t *testing.T) {
	departments := []DepartmentRecord{
		{ExternalID: "dept-root", Name: "Root"},
		{ExternalID: "dept-a", ParentExternalID: "dept-b", Name: "Zulu"},
		{ExternalID: "dept-b", ParentExternalID: "dept-c", Name: "Alpha"},
		{ExternalID: "dept-c", ParentExternalID: "dept-a", Name: "Alpha"},
	}
	want := map[string]string{"dept-root": "", "dept-a": "dept-b", "dept-b": "", "dept-c": "dept-a"}

	permutations := 0
	forEachDepartmentPermutation(departments, func(permutation []DepartmentRecord) {
		permutations++
		got, err := resolveEffectiveParents(permutation)
		if err != nil {
			t.Fatalf("resolveEffectiveParents(permutation %d) error = %v", permutations, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("permutation %d effective parents = %#v, want %#v", permutations, got, want)
		}
	})
	if permutations != 24 {
		t.Fatalf("permutations = %d, want 24", permutations)
	}
}

func TestResolveEffectiveParentsUsesLocaleIndependentCycleAnchor(t *testing.T) {
	departments := []DepartmentRecord{
		{ExternalID: "dept-zulu", ParentExternalID: "dept-dotted-i", Name: "  Zulu  "},
		{ExternalID: "dept-dotted-i", ParentExternalID: "dept-zulu", Name: "İstanbul"},
	}

	got, err := resolveEffectiveParents(departments)
	if err != nil {
		t.Fatalf("resolveEffectiveParents() error = %v", err)
	}
	want := map[string]string{"dept-zulu": "", "dept-dotted-i": "dept-zulu"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("effective parents = %#v, want locale-independent order %#v", got, want)
	}
}

func TestResolveEffectiveParentsRejectsInvalidIDs(t *testing.T) {
	tests := []struct {
		name        string
		departments []DepartmentRecord
	}{
		{name: "blank", departments: []DepartmentRecord{{ExternalID: "  ", Name: "Blank"}}},
		{name: "duplicate", departments: []DepartmentRecord{{ExternalID: "dept-a", Name: "Alpha"}, {ExternalID: "dept-a", Name: "Again"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := resolveEffectiveParents(tt.departments); err == nil {
				t.Fatal("resolveEffectiveParents() succeeded, want validation error")
			}
		})
	}
}

func TestResolveEffectiveParentsHandlesLargeChainWithoutRecursion(t *testing.T) {
	const departmentCount = 10_000
	departments := make([]DepartmentRecord, 0, departmentCount)
	for index := departmentCount - 1; index >= 0; index-- {
		externalID := fmt.Sprintf("dept-%05d", index)
		parentID := ""
		if index > 0 {
			parentID = fmt.Sprintf("dept-%05d", index-1)
		}
		departments = append(departments, DepartmentRecord{
			ExternalID: externalID, ParentExternalID: parentID, Name: fmt.Sprintf("Department %05d", index),
		})
	}

	got, err := resolveEffectiveParents(departments)
	if err != nil {
		t.Fatalf("resolveEffectiveParents() error = %v", err)
	}
	if len(got) != departmentCount {
		t.Fatalf("effective parent count = %d, want %d", len(got), departmentCount)
	}
	if got["dept-00000"] != "" || got["dept-09999"] != "dept-09998" {
		t.Fatalf("large chain endpoints = root:%q leaf:%q", got["dept-00000"], got["dept-09999"])
	}
}

func forEachDepartmentPermutation(departments []DepartmentRecord, visit func([]DepartmentRecord)) {
	values := append([]DepartmentRecord(nil), departments...)
	var permute func(int)
	permute = func(index int) {
		if index == len(values) {
			visit(append([]DepartmentRecord(nil), values...))
			return
		}
		for candidate := index; candidate < len(values); candidate++ {
			values[index], values[candidate] = values[candidate], values[index]
			permute(index + 1)
			values[index], values[candidate] = values[candidate], values[index]
		}
	}
	permute(0)
}
