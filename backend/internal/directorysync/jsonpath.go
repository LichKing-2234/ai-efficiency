package directorysync

import (
	"fmt"
	"strconv"
	"strings"
)

func EvaluateJSONPath(root any, path string) (any, error) {
	if err := validateJSONPath(path); err != nil {
		return nil, err
	}
	if strings.TrimSpace(path) == "$" {
		return root, nil
	}
	current := root
	for _, part := range strings.Split(strings.TrimPrefix(path, "$."), ".") {
		value, err := evaluateJSONPathSegment(current, part)
		if err != nil {
			return nil, err
		}
		current = value
	}
	return current, nil
}

func evaluateJSONPathSegment(current any, part string) (any, error) {
	if part == "" {
		return nil, fmt.Errorf("json path contains empty segment")
	}
	field := part
	index := -1
	if strings.Contains(part, "[") || strings.Contains(part, "]") {
		open := strings.Index(part, "[")
		close := strings.LastIndex(part, "]")
		if open <= 0 || close != len(part)-1 || close <= open+1 {
			return nil, fmt.Errorf("json path segment %q has unsupported array index", part)
		}
		field = part[:open]
		parsed, err := strconv.Atoi(part[open+1 : close])
		if err != nil || parsed < 0 {
			return nil, fmt.Errorf("json path segment %q has unsupported array index", part)
		}
		index = parsed
	}

	object, ok := current.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("json path segment %q is not an object", part)
	}
	value, ok := object[field]
	if !ok {
		return nil, fmt.Errorf("json path segment %q not found", field)
	}
	if index < 0 {
		return value, nil
	}
	list, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("json path segment %q is not an array", field)
	}
	if index >= len(list) {
		return nil, fmt.Errorf("json path segment %q index out of range", field)
	}
	return list[index], nil
}

func validateJSONPath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("json path is required")
	}
	if path == "$" {
		return nil
	}
	if !strings.HasPrefix(path, "$.") {
		return fmt.Errorf("json path must start with $.")
	}
	if strings.ContainsAny(path, "*?") {
		return fmt.Errorf("json path uses unsupported operators")
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "$."), ".") {
		if strings.Contains(part, "[") || strings.Contains(part, "]") {
			open := strings.Index(part, "[")
			close := strings.LastIndex(part, "]")
			if open <= 0 || close != len(part)-1 || close <= open+1 {
				return fmt.Errorf("json path segment %q has unsupported array index", part)
			}
			if _, err := strconv.Atoi(part[open+1 : close]); err != nil {
				return fmt.Errorf("json path segment %q has unsupported array index", part)
			}
		}
	}
	return nil
}
