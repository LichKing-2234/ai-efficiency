package handler

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// updateYAMLSection reads a YAML file, locates a nested section by key path
// using yaml.Node for parsing, then splices only the target section's value
// in the raw text, preserving the original formatting everywhere else.
func updateYAMLSection(filePath string, keys []string, value interface{}) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}

	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return fmt.Errorf("invalid YAML document")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("root is not a mapping")
	}

	// Navigate to the target key and value nodes.
	current := root
	for i, key := range keys {
		idx := findMappingKey(current, key)
		if idx < 0 {
			// Key doesn't exist — append to end of file.
			return appendYAMLSection(filePath, data, keys, value)
		}
		if i < len(keys)-1 {
			current = current.Content[idx+1]
			if current.Kind != yaml.MappingNode {
				return fmt.Errorf("expected mapping at key %q", key)
			}
		}
	}

	lastKey := keys[len(keys)-1]
	idx := findMappingKey(current, lastKey)
	keyNode := current.Content[idx]
	valNode := current.Content[idx+1]

	lines := strings.Split(string(data), "\n")

	// startLine: the line where the value starts (0-indexed).
	startLine := valNode.Line - 1 // yaml.Node.Line is 1-based

	// Find the end of this value block: scan forward from the line after the
	// value start until we find a line with indentation <= the key's column.
	keyCol := keyNode.Column - 1 // 0-based
	endLine := len(lines)
	for i := startLine + 1; i < len(lines); i++ {
		line := lines[i]
		if line == "" {
			continue
		}
		if countIndent(line) <= keyCol {
			endLine = i
			break
		}
	}

	// Detect the indentation used in the original file for this value.
	valCol := valNode.Column - 1
	indent := strings.Repeat(" ", valCol)

	// Render the replacement value lines.
	replacement := renderMapValue(value, indent)

	// Splice: keep lines [0, startLine) + replacement + lines [endLine, end).
	var buf bytes.Buffer
	for i := 0; i < startLine; i++ {
		buf.WriteString(lines[i])
		buf.WriteByte('\n')
	}
	buf.WriteString(replacement)
	for i := endLine; i < len(lines); i++ {
		buf.WriteString(lines[i])
		if i < len(lines)-1 {
			buf.WriteByte('\n')
		}
	}

	return os.WriteFile(filePath, buf.Bytes(), 0o644)
}

// renderMapValue renders a map as YAML key-value lines with stable key order.
func renderMapValue(value interface{}, indent string) string {
	m, ok := value.(map[string]interface{})
	if !ok {
		return fmt.Sprintf("%s%v\n", indent, value)
	}

	// Sort keys for stable output.
	sortedKeys := make([]string, 0, len(m))
	for k := range m {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	var buf bytes.Buffer
	for _, k := range sortedKeys {
		v := m[k]
		switch tv := v.(type) {
		case string:
			if strings.Contains(tv, "\n") {
				buf.WriteString(fmt.Sprintf("%s%s: |-\n", indent, k))
				for _, line := range strings.Split(tv, "\n") {
					if line == "" {
						buf.WriteByte('\n')
					} else {
						buf.WriteString(indent + "  " + line + "\n")
					}
				}
			} else if tv == "" {
				buf.WriteString(fmt.Sprintf("%s%s: \"\"\n", indent, k))
			} else {
				buf.WriteString(fmt.Sprintf("%s%s: %s\n", indent, k, tv))
			}
		case bool:
			buf.WriteString(fmt.Sprintf("%s%s: %t\n", indent, k, tv))
		case int:
			buf.WriteString(fmt.Sprintf("%s%s: %d\n", indent, k, tv))
		case float64:
			if tv == float64(int(tv)) {
				buf.WriteString(fmt.Sprintf("%s%s: %d\n", indent, k, int(tv)))
			} else {
				buf.WriteString(fmt.Sprintf("%s%s: %g\n", indent, k, tv))
			}
		default:
			buf.WriteString(fmt.Sprintf("%s%s: %v\n", indent, k, v))
		}
	}
	return buf.String()
}

// appendYAMLSection appends a new section to the end of the file when the key doesn't exist.
func appendYAMLSection(filePath string, data []byte, keys []string, value interface{}) error {
	content := string(data)
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += "\n"

	// Build nested YAML manually.
	for i, key := range keys {
		indent := strings.Repeat("  ", i)
		content += fmt.Sprintf("%s%s:\n", indent, key)
	}

	baseIndent := strings.Repeat("  ", len(keys))
	content += renderMapValue(value, baseIndent)

	return os.WriteFile(filePath, []byte(content), 0o644)
}

// findMappingKey returns the index of the key node in a mapping, or -1 if not found.
func findMappingKey(mapping *yaml.Node, key string) int {
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == key {
			return i
		}
	}
	return -1
}

// countIndent returns the number of leading spaces in a line.
func countIndent(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}
