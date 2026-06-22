package directorysync

import (
	"fmt"
	"strings"
)

func EvaluateJSONPath(root any, path string) (any, error) {
	if err := validateJSONPath(path); err != nil {
		return nil, err
	}
	current := root
	for _, part := range strings.Split(strings.TrimPrefix(path, "$."), ".") {
		if part == "" {
			return nil, fmt.Errorf("json path contains empty segment")
		}
		object, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("json path segment %q is not an object", part)
		}
		value, ok := object[part]
		if !ok {
			return nil, fmt.Errorf("json path segment %q not found", part)
		}
		current = value
	}
	return current, nil
}

func validateJSONPath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("json path is required")
	}
	if !strings.HasPrefix(path, "$.") {
		return fmt.Errorf("json path must start with $.")
	}
	if strings.ContainsAny(path, "[]*?") {
		return fmt.Errorf("json path uses unsupported operators")
	}
	return nil
}
