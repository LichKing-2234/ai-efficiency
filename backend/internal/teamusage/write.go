package teamusage

import (
	"errors"
	"math"

	"github.com/ai-efficiency/backend/internal/relay"
)

var (
	ErrInvalidMultiplier          = errors.New("invalid_multiplier")
	ErrInvalidMultiplierPrecision = errors.New("invalid_multiplier_precision")
	ErrMultiplierBelowInherited   = errors.New("multiplier_below_inherited_default")
	ErrMultiplierAboveMaximum     = errors.New("multiplier_above_maximum")
)

const defaultMaxMultiplier = 10.0

func NormalizeMultiplier(value float64) (float64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, ErrInvalidMultiplier
	}
	rounded := math.Round(value*10000) / 10000
	if math.Abs(value-rounded) > 0.000000001 {
		return 0, ErrInvalidMultiplierPrecision
	}
	return rounded, nil
}

func ValidateSetMultiplier(value, inheritedDefault float64, maxMultiplier float64) (float64, error) {
	normalized, err := NormalizeMultiplier(value)
	if err != nil {
		return 0, err
	}
	if normalized <= 0 {
		return 0, ErrInvalidMultiplier
	}
	if normalized < inheritedDefault {
		return 0, ErrMultiplierBelowInherited
	}
	if maxMultiplier <= 0 {
		maxMultiplier = defaultMaxMultiplier
	}
	if normalized > maxMultiplier {
		return 0, ErrMultiplierAboveMaximum
	}
	return normalized, nil
}

func SameMultiplierState(current *float64, requested *float64) bool {
	if current == nil || requested == nil {
		return current == nil && requested == nil
	}
	left, leftErr := NormalizeMultiplier(*current)
	right, rightErr := NormalizeMultiplier(*requested)
	return leftErr == nil && rightErr == nil && left == right
}

func MergeRateEntries(current []relay.UserGroupRateEntry, targetUserID int64, requested *float64) []relay.GroupRateMultiplierInput {
	out := make([]relay.GroupRateMultiplierInput, 0, len(current)+1)
	seenTarget := false
	for _, entry := range current {
		if entry.UserID == targetUserID {
			seenTarget = true
			if requested == nil && entry.RPMOverride == nil {
				continue
			}
			out = append(out, relay.GroupRateMultiplierInput{
				UserID:         entry.UserID,
				RateMultiplier: requested,
				RPMOverride:    entry.RPMOverride,
			})
			continue
		}
		if entry.RateMultiplier != nil || entry.RPMOverride != nil {
			out = append(out, relay.GroupRateMultiplierInput{
				UserID:         entry.UserID,
				RateMultiplier: entry.RateMultiplier,
				RPMOverride:    entry.RPMOverride,
			})
		}
	}
	if !seenTarget && requested != nil {
		out = append(out, relay.GroupRateMultiplierInput{
			UserID:         targetUserID,
			RateMultiplier: requested,
		})
	}
	return out
}
