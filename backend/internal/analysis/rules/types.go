package rules

// DimensionScore represents a single scoring dimension.
type DimensionScore struct {
	Name     string  `json:"name"`
	Score    float64 `json:"score"`
	MaxScore float64 `json:"max_score"`
	Details  string  `json:"details"`
}

// Suggestion represents an improvement suggestion.
type Suggestion struct {
	Category string `json:"category"`
	Message  string `json:"message"`
	Priority string `json:"priority"` // high, medium, low
	AutoFix  bool   `json:"auto_fix"`
}
