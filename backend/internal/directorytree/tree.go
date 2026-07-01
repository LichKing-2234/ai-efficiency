package directorytree

import (
	"sort"
	"strings"

	"github.com/ai-efficiency/backend/ent"
)

type Tree struct {
	childrenByParent map[string][]*ent.DirectoryDepartment
	depthByID        map[string]int
	displayPathByID  map[string]string
	ordered          []*ent.DirectoryDepartment
	subtreeIDsByID   map[string][]string
}

func New(departments []*ent.DirectoryDepartment) *Tree {
	t := &Tree{
		childrenByParent: make(map[string][]*ent.DirectoryDepartment, len(departments)),
		depthByID:        make(map[string]int, len(departments)),
		displayPathByID:  make(map[string]string, len(departments)),
		subtreeIDsByID:   make(map[string][]string, len(departments)),
	}
	byID := make(map[string]*ent.DirectoryDepartment, len(departments))
	for _, department := range departments {
		if department == nil {
			continue
		}
		byID[department.ExternalID] = department
	}
	roots := make([]*ent.DirectoryDepartment, 0)
	for _, department := range departments {
		if department == nil {
			continue
		}
		parentID := ParentExternalID(department)
		if parentID == "" || byID[parentID] == nil {
			roots = append(roots, department)
			continue
		}
		t.childrenByParent[parentID] = append(t.childrenByParent[parentID], department)
	}
	sortDepartments(roots)
	for parentID := range t.childrenByParent {
		sortDepartments(t.childrenByParent[parentID])
	}

	visited := make(map[string]bool, len(departments))
	for _, root := range roots {
		t.visit(root, 0, "", visited)
	}
	if len(visited) < len(byID) {
		remaining := make([]*ent.DirectoryDepartment, 0, len(byID)-len(visited))
		for externalID, department := range byID {
			if !visited[externalID] {
				remaining = append(remaining, department)
			}
		}
		sortDepartments(remaining)
		for _, department := range remaining {
			t.visit(department, 0, "", visited)
		}
	}
	return t
}

func ParentExternalID(department *ent.DirectoryDepartment) string {
	if department == nil || department.ParentExternalID == nil {
		return ""
	}
	return strings.TrimSpace(*department.ParentExternalID)
}

func (t *Tree) Ordered() []*ent.DirectoryDepartment {
	if t == nil {
		return nil
	}
	ordered := make([]*ent.DirectoryDepartment, len(t.ordered))
	copy(ordered, t.ordered)
	return ordered
}

func (t *Tree) Depth(externalID string) int {
	if t == nil {
		return 0
	}
	return t.depthByID[externalID]
}

func (t *Tree) ChildCount(externalID string) int {
	if t == nil {
		return 0
	}
	return len(t.childrenByParent[externalID])
}

func (t *Tree) DisplayPath(externalID string) string {
	if t == nil {
		return ""
	}
	return t.displayPathByID[externalID]
}

func (t *Tree) SubtreeIDs(externalID string) []string {
	if t == nil {
		return []string{externalID}
	}
	ids := t.subtreeIDsByID[externalID]
	if len(ids) == 0 {
		return []string{externalID}
	}
	copied := make([]string, len(ids))
	copy(copied, ids)
	return copied
}

func (t *Tree) visit(department *ent.DirectoryDepartment, depth int, parentDisplayPath string, visited map[string]bool) []string {
	if department == nil {
		return nil
	}
	if visited[department.ExternalID] {
		return t.subtreeIDsByID[department.ExternalID]
	}
	visited[department.ExternalID] = true
	t.depthByID[department.ExternalID] = depth
	displayPath := strings.TrimSpace(department.Name)
	if parentDisplayPath != "" && displayPath != "" {
		displayPath = parentDisplayPath + " / " + displayPath
	}
	if displayPath == "" {
		displayPath = department.ExternalID
	}
	t.displayPathByID[department.ExternalID] = displayPath
	t.ordered = append(t.ordered, department)

	subtreeIDs := []string{department.ExternalID}
	for _, child := range t.childrenByParent[department.ExternalID] {
		subtreeIDs = append(subtreeIDs, t.visit(child, depth+1, displayPath, visited)...)
	}
	t.subtreeIDsByID[department.ExternalID] = subtreeIDs
	return subtreeIDs
}

func sortDepartments(departments []*ent.DirectoryDepartment) {
	sort.SliceStable(departments, func(i, j int) bool {
		left := departments[i]
		right := departments[j]
		leftName := strings.ToLower(strings.TrimSpace(left.Name))
		rightName := strings.ToLower(strings.TrimSpace(right.Name))
		if leftName != rightName {
			return leftName < rightName
		}
		return left.ExternalID < right.ExternalID
	})
}
