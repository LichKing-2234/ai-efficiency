package relay_test

import (
	"testing"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/google/go-cmp/cmp"
)

func TestCompareAccountConfigurationClassifiesEveryDifference(t *testing.T) {
	baseline := relay.AccountConfiguration{
		ModelMapping: map[string]string{"gpt-5.5": "gpt-6.1-sol"},
		Extra: map[string]any{
			"privacy_mode":   "training_off",
			"codex_cli_only": true,
		},
	}

	t.Run("identical configuration matches", func(t *testing.T) {
		got := relay.CompareAccountConfiguration(baseline, relay.AccountConfiguration{
			ModelMapping: map[string]string{"gpt-5.5": "gpt-6.1-sol"},
			Extra: map[string]any{
				"privacy_mode":   "training_off",
				"codex_cli_only": true,
			},
		})
		if !got.Matches {
			t.Fatalf("Matches = false, want true; diff = %+v", got)
		}
		if len(got.MissingKeys)+len(got.DifferingKeys)+len(got.AdditionalKeys) != 0 {
			t.Fatalf("matched configuration reported differences: %+v", got)
		}
	})

	t.Run("absent configuration is missing", func(t *testing.T) {
		got := relay.CompareAccountConfiguration(baseline, relay.AccountConfiguration{})
		if got.Matches {
			t.Fatalf("Matches = true, want false")
		}
		want := []string{"extra.codex_cli_only", "extra.privacy_mode", "model_mapping"}
		if diff := cmp.Diff(want, got.MissingKeys); diff != "" {
			t.Fatalf("missing keys mismatch (-want +got):\n%s", diff)
		}
		if len(got.DifferingKeys)+len(got.AdditionalKeys) != 0 {
			t.Fatalf("unexpected other differences: %+v", got)
		}
	})

	t.Run("present but unequal values differ", func(t *testing.T) {
		got := relay.CompareAccountConfiguration(baseline, relay.AccountConfiguration{
			ModelMapping: map[string]string{"gpt-5.5": "gpt-6-luna"},
			Extra: map[string]any{
				"privacy_mode":   "training_on",
				"codex_cli_only": true,
			},
		})
		if got.Matches {
			t.Fatalf("Matches = true, want false")
		}
		want := []string{"extra.privacy_mode", "model_mapping"}
		if diff := cmp.Diff(want, got.DifferingKeys); diff != "" {
			t.Fatalf("differing keys mismatch (-want +got):\n%s", diff)
		}
		if len(got.MissingKeys)+len(got.AdditionalKeys) != 0 {
			t.Fatalf("unexpected other differences: %+v", got)
		}
	})

	t.Run("a target-only allow-listed key is additional", func(t *testing.T) {
		got := relay.CompareAccountConfiguration(baseline, relay.AccountConfiguration{
			ModelMapping: map[string]string{"gpt-5.5": "gpt-6.1-sol"},
			Extra: map[string]any{
				"privacy_mode":           "training_off",
				"codex_cli_only":         true,
				"codex_fingerprint_mode": "device",
			},
		})
		if got.Matches {
			t.Fatalf("Matches = true, want false; a copy would not converge the target")
		}
		want := []string{"extra.codex_fingerprint_mode"}
		if diff := cmp.Diff(want, got.AdditionalKeys); diff != "" {
			t.Fatalf("additional keys mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("false and zero are values, not absences", func(t *testing.T) {
		got := relay.CompareAccountConfiguration(
			relay.AccountConfiguration{Extra: map[string]any{"codex_cli_only": false, "auto_reset_credit_7d_threshold": 1}},
			relay.AccountConfiguration{Extra: map[string]any{"codex_cli_only": false, "auto_reset_credit_7d_threshold": 1}},
		)
		if !got.Matches {
			t.Fatalf("Matches = false, want true; diff = %+v", got)
		}
	})

	t.Run("a target-only model mapping is additional", func(t *testing.T) {
		got := relay.CompareAccountConfiguration(
			relay.AccountConfiguration{Extra: map[string]any{"privacy_mode": "training_off"}},
			relay.AccountConfiguration{
				ModelMapping: map[string]string{"gpt-5.5": "gpt-6.1-sol"},
				Extra:        map[string]any{"privacy_mode": "training_off"},
			},
		)
		if got.Matches {
			t.Fatalf("Matches = true, want false; the baseline carries no mapping, so a copy cannot remove the target's")
		}
		want := []string{"model_mapping"}
		if diff := cmp.Diff(want, got.AdditionalKeys); diff != "" {
			t.Fatalf("additional keys mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a nil and an empty mapping are the same absence", func(t *testing.T) {
		got := relay.CompareAccountConfiguration(
			relay.AccountConfiguration{ModelMapping: nil},
			relay.AccountConfiguration{ModelMapping: map[string]string{}},
		)
		if !got.Matches {
			t.Fatalf("Matches = false, want true; diff = %+v", got)
		}
	})

	t.Run("an absent key the provider defaults to the baseline value is not missing", func(t *testing.T) {
		got := relay.CompareAccountConfiguration(
			relay.AccountConfiguration{Extra: map[string]any{
				"privacy_mode":                   "training_off",
				"auto_reset_credit_enabled":      false,
				"auto_reset_credit_5h_threshold": float64(1),
				"auto_reset_credit_7d_threshold": float64(1),
			}},
			relay.AccountConfiguration{Extra: map[string]any{"privacy_mode": "training_off"}},
		)
		if !got.Matches {
			t.Fatalf("Matches = false, want true; an absent key the provider defaults to the same value is not a difference; diff = %+v", got)
		}
	})

	t.Run("an absent key whose baseline value is not the provider default is still missing", func(t *testing.T) {
		got := relay.CompareAccountConfiguration(
			relay.AccountConfiguration{Extra: map[string]any{"auto_reset_credit_enabled": true}},
			relay.AccountConfiguration{Extra: map[string]any{}},
		)
		want := []string{"extra.auto_reset_credit_enabled"}
		if diff := cmp.Diff(want, got.MissingKeys); diff != "" {
			t.Fatalf("missing keys mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a target-only key the provider defaults to is not additional", func(t *testing.T) {
		got := relay.CompareAccountConfiguration(
			relay.AccountConfiguration{Extra: map[string]any{"privacy_mode": "training_off"}},
			relay.AccountConfiguration{Extra: map[string]any{
				"privacy_mode":                   "training_off",
				"auto_reset_credit_5h_threshold": float64(1),
			}},
		)
		if !got.Matches {
			t.Fatalf("Matches = false, want true; a target storing the provider default is not a difference; diff = %+v", got)
		}
	})

	t.Run("a target-only key that differs from the provider default is additional", func(t *testing.T) {
		got := relay.CompareAccountConfiguration(
			relay.AccountConfiguration{Extra: map[string]any{"privacy_mode": "training_off"}},
			relay.AccountConfiguration{Extra: map[string]any{
				"privacy_mode":                   "training_off",
				"auto_reset_credit_5h_threshold": float64(5),
			}},
		)
		want := []string{"extra.auto_reset_credit_5h_threshold"}
		if diff := cmp.Diff(want, got.AdditionalKeys); diff != "" {
			t.Fatalf("additional keys mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a key the provider materialises at its own default is not a difference", func(t *testing.T) {
		// The baseline never carried openai_compact_mode. Applying a copy writes
		// extra, and Sub2API's bulk update then stores openai_compact_mode="auto"
		// on the target, so a raw presence comparison would fail the very copy that
		// converged it.
		got := relay.CompareAccountConfiguration(
			relay.AccountConfiguration{Extra: map[string]any{"privacy_mode": "training_off"}},
			relay.AccountConfiguration{Extra: map[string]any{
				"privacy_mode":        "training_off",
				"openai_compact_mode": "auto",
			}},
		)
		if !got.Matches {
			t.Fatalf("Matches = false, want true; a stored provider default is not a difference; diff = %+v", got)
		}
	})

	t.Run("a target-only compact mode the provider does not default to is additional", func(t *testing.T) {
		got := relay.CompareAccountConfiguration(
			relay.AccountConfiguration{Extra: map[string]any{"privacy_mode": "training_off"}},
			relay.AccountConfiguration{Extra: map[string]any{
				"privacy_mode":        "training_off",
				"openai_compact_mode": "force_on",
			}},
		)
		want := []string{"extra.openai_compact_mode"}
		if diff := cmp.Diff(want, got.AdditionalKeys); diff != "" {
			t.Fatalf("additional keys mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a baseline compact mode at the provider default is not missing", func(t *testing.T) {
		got := relay.CompareAccountConfiguration(
			relay.AccountConfiguration{Extra: map[string]any{
				"privacy_mode":        "training_off",
				"openai_compact_mode": "auto",
			}},
			relay.AccountConfiguration{Extra: map[string]any{"privacy_mode": "training_off"}},
		)
		if !got.Matches {
			t.Fatalf("Matches = false, want true; an absent key the provider defaults to the same value is not a difference; diff = %+v", got)
		}
	})
}
