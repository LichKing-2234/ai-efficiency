package directorysync

import (
	"fmt"
	"strings"
)

func resolveEffectiveParents(departments []DepartmentRecord) (map[string]string, error) {
	departmentByID := make(map[string]DepartmentRecord, len(departments))
	for _, department := range departments {
		externalID := department.ExternalID
		if strings.TrimSpace(externalID) == "" {
			return nil, fmt.Errorf("directory department external id is required")
		}
		if _, exists := departmentByID[externalID]; exists {
			return nil, fmt.Errorf("duplicate directory department external id %q", externalID)
		}
		departmentByID[externalID] = department
	}

	effectiveParents := make(map[string]string, len(departments))
	for _, department := range departments {
		parentID := strings.TrimSpace(department.ParentExternalID)
		if _, exists := departmentByID[parentID]; !exists {
			parentID = ""
		}
		effectiveParents[department.ExternalID] = parentID
	}

	completed := make(map[string]struct{}, len(departments))
	for _, department := range departments {
		startID := department.ExternalID
		if _, done := completed[startID]; done {
			continue
		}

		path := make([]string, 0)
		pathIndex := make(map[string]int)
		currentID := startID
		for currentID != "" {
			if _, done := completed[currentID]; done {
				break
			}
			if cycleStart, closesCycle := pathIndex[currentID]; closesCycle {
				anchorID := path[cycleStart]
				for _, cycleID := range path[cycleStart+1:] {
					if effectiveHierarchyLess(departmentByID[cycleID], departmentByID[anchorID]) {
						anchorID = cycleID
					}
				}
				effectiveParents[anchorID] = ""
				break
			}
			pathIndex[currentID] = len(path)
			path = append(path, currentID)
			currentID = effectiveParents[currentID]
		}
		for _, externalID := range path {
			completed[externalID] = struct{}{}
		}
	}

	return effectiveParents, nil
}

func effectiveHierarchyLess(left, right DepartmentRecord) bool {
	leftName := strings.ToLower(strings.TrimSpace(left.Name))
	rightName := strings.ToLower(strings.TrimSpace(right.Name))
	if leftName != rightName {
		return leftName < rightName
	}
	return left.ExternalID < right.ExternalID
}
