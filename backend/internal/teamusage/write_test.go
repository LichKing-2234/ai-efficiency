package teamusage

import (
	"errors"
	"math"
	"testing"

	"github.com/ai-efficiency/backend/internal/relay"
)

func TestValidateMultiplierRejectsBelowInheritedDefault(t *testing.T) {
	_, err := ValidateSetMultiplier(0.5, 1.0, 10)
	if !errors.Is(err, ErrMultiplierBelowInherited) {
		t.Fatalf("ValidateSetMultiplier() error = %v, want ErrMultiplierBelowInherited", err)
	}
}

func TestValidateMultiplierRejectsNonFiniteAndOverPrecision(t *testing.T) {
	if _, err := ValidateSetMultiplier(math.Inf(1), 1.0, 10); !errors.Is(err, ErrInvalidMultiplier) {
		t.Fatalf("infinite multiplier error = %v, want ErrInvalidMultiplier", err)
	}
	if _, err := ValidateSetMultiplier(1.12345, 1.0, 10); !errors.Is(err, ErrInvalidMultiplierPrecision) {
		t.Fatalf("precision error = %v, want ErrInvalidMultiplierPrecision", err)
	}
}

func TestMergeRateEntriesPreservesNonTargetRPMOverride(t *testing.T) {
	merged := MergeRateEntries([]relay.UserGroupRateEntry{
		{UserID: 1, RateMultiplier: floatPtr(2.0), RPMOverride: intPtr(120)},
		{UserID: 2, RateMultiplier: floatPtr(1.5), RPMOverride: intPtr(60)},
	}, 1, floatPtr(3.0))
	if merged[1].RPMOverride == nil || *merged[1].RPMOverride != 60 {
		t.Fatalf("non-target rpm override was not preserved: %#v", merged)
	}
}

func TestMergeRateEntriesResetKeepsTargetRPMOverride(t *testing.T) {
	merged := MergeRateEntries([]relay.UserGroupRateEntry{
		{UserID: 1, RateMultiplier: floatPtr(2.0), RPMOverride: intPtr(120)},
		{UserID: 2, RateMultiplier: floatPtr(1.5), RPMOverride: intPtr(60)},
	}, 1, nil)
	if merged[0].RateMultiplier != nil {
		t.Fatalf("target rate multiplier = %#v, want nil reset", merged[0].RateMultiplier)
	}
	if merged[0].RPMOverride == nil || *merged[0].RPMOverride != 120 {
		t.Fatalf("target rpm override was not preserved: %#v", merged[0])
	}
}

func TestNoOpWriteSkipsRelayReplacement(t *testing.T) {
	current := floatPtr(2.0)
	requested := floatPtr(2.0000)
	if !SameMultiplierState(current, requested) {
		t.Fatal("SameMultiplierState() = false, want true for equivalent normalized multipliers")
	}
}
