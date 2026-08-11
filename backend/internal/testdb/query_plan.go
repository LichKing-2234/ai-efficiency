package testdb

import (
	"encoding/json"
	"fmt"
)

type explainNode struct {
	ActualRows          float64       `json:"Actual Rows"`
	ActualLoops         float64       `json:"Actual Loops"`
	RowsRemovedByFilter float64       `json:"Rows Removed by Filter"`
	Plans               []explainNode `json:"Plans"`
}

// ExplainScannedRows returns the total rows visited by leaf scan nodes in a
// PostgreSQL EXPLAIN (ANALYZE, FORMAT JSON) document.
func ExplainScannedRows(raw []byte) (int64, error) {
	var documents []struct {
		Plan explainNode `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &documents); err != nil {
		return 0, fmt.Errorf("decode EXPLAIN JSON: %w", err)
	}
	if len(documents) != 1 {
		return 0, fmt.Errorf("EXPLAIN documents = %d, want 1", len(documents))
	}
	return scannedRows(documents[0].Plan), nil
}

func scannedRows(node explainNode) int64 {
	if len(node.Plans) == 0 {
		loops := node.ActualLoops
		if loops < 1 {
			loops = 1
		}
		return int64((node.ActualRows + node.RowsRemovedByFilter) * loops)
	}
	var total int64
	for _, child := range node.Plans {
		total += scannedRows(child)
	}
	return total
}
