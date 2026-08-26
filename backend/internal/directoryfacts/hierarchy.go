package directoryfacts

import (
	"sort"
	"strings"
)

type Hierarchy struct {
	byID             map[string]*Department
	childrenByParent map[string][]*Department
	depthByID        map[string]int
	displayPathByID  map[string]string
	ordered          []*Department
	subtreeIDsByID   map[string][]string
}

func newHierarchy(departments []Department) *Hierarchy {
	hierarchy := &Hierarchy{
		byID:             make(map[string]*Department, len(departments)),
		childrenByParent: make(map[string][]*Department, len(departments)),
		depthByID:        make(map[string]int, len(departments)),
		displayPathByID:  make(map[string]string, len(departments)),
		subtreeIDsByID:   make(map[string][]string, len(departments)),
	}
	for index := range departments {
		department := &departments[index]
		if id := strings.TrimSpace(department.ExternalID); id != "" {
			hierarchy.byID[id] = department
		}
	}

	roots := make([]*Department, 0)
	for _, department := range hierarchy.byID {
		parentID := effectiveParentID(department)
		if parentID == "" || hierarchy.byID[parentID] == nil {
			roots = append(roots, department)
			continue
		}
		hierarchy.childrenByParent[parentID] = append(hierarchy.childrenByParent[parentID], department)
	}
	sortDepartments(roots)
	for parentID := range hierarchy.childrenByParent {
		sortDepartments(hierarchy.childrenByParent[parentID])
	}

	visited := make(map[string]bool, len(hierarchy.byID))
	for _, root := range roots {
		hierarchy.visit(root, 0, "", visited)
	}
	if len(visited) < len(hierarchy.byID) {
		remaining := make([]*Department, 0, len(hierarchy.byID)-len(visited))
		for externalID, department := range hierarchy.byID {
			if !visited[externalID] {
				remaining = append(remaining, department)
			}
		}
		sortDepartments(remaining)
		for _, department := range remaining {
			hierarchy.visit(department, 0, "", visited)
		}
	}
	return hierarchy
}

func (h *Hierarchy) Department(externalID string) *Department {
	if h == nil {
		return nil
	}
	return h.byID[strings.TrimSpace(externalID)]
}

func (h *Hierarchy) Ordered() []Department {
	if h == nil {
		return nil
	}
	out := make([]Department, 0, len(h.ordered))
	for _, department := range h.ordered {
		out = append(out, *department)
	}
	return out
}

func (h *Hierarchy) ParentID(externalID string) string {
	return effectiveParentID(h.Department(externalID))
}

func (h *Hierarchy) Depth(externalID string) int {
	if h == nil {
		return 0
	}
	return h.depthByID[strings.TrimSpace(externalID)]
}

func (h *Hierarchy) ChildCount(externalID string) int {
	if h == nil {
		return 0
	}
	return len(h.childrenByParent[strings.TrimSpace(externalID)])
}

func (h *Hierarchy) DisplayPath(externalID string) string {
	if h == nil {
		return ""
	}
	return h.displayPathByID[strings.TrimSpace(externalID)]
}

func (h *Hierarchy) SubtreeIDs(externalID string) []string {
	externalID = strings.TrimSpace(externalID)
	if h == nil {
		return compactStrings([]string{externalID})
	}
	ids := h.subtreeIDsByID[externalID]
	if len(ids) == 0 {
		return compactStrings([]string{externalID})
	}
	return append([]string(nil), ids...)
}

func (h *Hierarchy) visit(department *Department, depth int, parentDisplayPath string, visited map[string]bool) []string {
	if department == nil || visited[department.ExternalID] {
		if department == nil {
			return nil
		}
		return h.subtreeIDsByID[department.ExternalID]
	}
	visited[department.ExternalID] = true
	h.depthByID[department.ExternalID] = depth
	displayPath := firstNonBlank(department.Name, department.ExternalID)
	if parentDisplayPath != "" && displayPath != "" {
		displayPath = parentDisplayPath + " / " + displayPath
	}
	h.displayPathByID[department.ExternalID] = displayPath
	h.ordered = append(h.ordered, department)

	subtreeIDs := []string{department.ExternalID}
	for _, child := range h.childrenByParent[department.ExternalID] {
		subtreeIDs = append(subtreeIDs, h.visit(child, depth+1, displayPath, visited)...)
	}
	h.subtreeIDsByID[department.ExternalID] = compactStringsPreservingOrder(subtreeIDs)
	return h.subtreeIDsByID[department.ExternalID]
}

func effectiveParentID(department *Department) string {
	if department == nil || department.EffectiveParentExternalID == nil {
		return ""
	}
	return strings.TrimSpace(*department.EffectiveParentExternalID)
}

func sortDepartments(departments []*Department) {
	sort.SliceStable(departments, func(i, j int) bool {
		leftName := strings.ToLower(strings.TrimSpace(departments[i].Name))
		rightName := strings.ToLower(strings.TrimSpace(departments[j].Name))
		if leftName != rightName {
			return leftName < rightName
		}
		return departments[i].ExternalID < departments[j].ExternalID
	})
}
