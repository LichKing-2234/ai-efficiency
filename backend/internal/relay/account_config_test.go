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
				"auto_reset_credit_5h_threshold": float64(0),
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

	t.Run("inert window thresholds are not compared while the switch is off", func(t *testing.T) {
		// The baseline stores the legacy 100% thresholds while the target never
		// carried the keys. Both accounts have the auto-reset switch off, so
		// neither value has an effect and there is nothing a copy could converge.
		got := relay.CompareAccountConfiguration(
			relay.AccountConfiguration{Extra: map[string]any{
				"auto_reset_credit_enabled":      false,
				"auto_reset_credit_5h_threshold": float64(1),
				"auto_reset_credit_7d_threshold": float64(1),
			}},
			relay.AccountConfiguration{Extra: map[string]any{"auto_reset_credit_enabled": false}},
		)
		if !got.Matches {
			t.Fatalf("Matches = false, want true; the switch makes both thresholds inert; diff = %+v", got)
		}
	})

	t.Run("differing inert window thresholds are not a difference", func(t *testing.T) {
		got := relay.CompareAccountConfiguration(
			relay.AccountConfiguration{Extra: map[string]any{
				"auto_reset_credit_enabled":      false,
				"auto_reset_credit_5h_threshold": float64(1),
			}},
			relay.AccountConfiguration{Extra: map[string]any{
				"auto_reset_credit_enabled":      false,
				"auto_reset_credit_5h_threshold": float64(0.5),
			}},
		)
		if !got.Matches {
			t.Fatalf("Matches = false, want true; neither account reads its thresholds; diff = %+v", got)
		}
	})

	t.Run("a target-only inert threshold is not additional", func(t *testing.T) {
		got := relay.CompareAccountConfiguration(
			relay.AccountConfiguration{Extra: map[string]any{"privacy_mode": "training_off"}},
			relay.AccountConfiguration{Extra: map[string]any{
				"privacy_mode":                   "training_off",
				"auto_reset_credit_5h_threshold": float64(5),
			}},
		)
		if !got.Matches {
			t.Fatalf("Matches = false, want true; a missing switch reads as off, so the threshold is inert; diff = %+v", got)
		}
	})

	t.Run("window thresholds are compared once both switches are on", func(t *testing.T) {
		got := relay.CompareAccountConfiguration(
			relay.AccountConfiguration{Extra: map[string]any{
				"auto_reset_credit_enabled":      true,
				"auto_reset_credit_5h_threshold": float64(0.8),
			}},
			relay.AccountConfiguration{Extra: map[string]any{
				"auto_reset_credit_enabled":      true,
				"auto_reset_credit_5h_threshold": float64(0.5),
			}},
		)
		want := []string{"extra.auto_reset_credit_5h_threshold"}
		if diff := cmp.Diff(want, got.DifferingKeys); diff != "" {
			t.Fatalf("differing keys mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a threshold is not compared while only one account has the switch on", func(t *testing.T) {
		got := relay.CompareAccountConfiguration(
			relay.AccountConfiguration{Extra: map[string]any{
				"auto_reset_credit_enabled":      false,
				"auto_reset_credit_5h_threshold": float64(1),
			}},
			relay.AccountConfiguration{Extra: map[string]any{
				"auto_reset_credit_enabled":      true,
				"auto_reset_credit_5h_threshold": float64(0.5),
			}},
		)
		want := []string{"extra.auto_reset_credit_enabled"}
		if diff := cmp.Diff(want, got.DifferingKeys); diff != "" {
			t.Fatalf("differing keys mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a target-only key the provider defaults to is not additional", func(t *testing.T) {
		got := relay.CompareAccountConfiguration(
			relay.AccountConfiguration{Extra: map[string]any{
				"privacy_mode":              "training_off",
				"auto_reset_credit_enabled": true,
			}},
			relay.AccountConfiguration{Extra: map[string]any{
				"privacy_mode":                   "training_off",
				"auto_reset_credit_enabled":      true,
				"auto_reset_credit_5h_threshold": float64(0),
			}},
		)
		if !got.Matches {
			t.Fatalf("Matches = false, want true; a target storing the provider default is not a difference; diff = %+v", got)
		}
	})

	t.Run("a target-only key that differs from the provider default is additional", func(t *testing.T) {
		got := relay.CompareAccountConfiguration(
			relay.AccountConfiguration{Extra: map[string]any{
				"privacy_mode":              "training_off",
				"auto_reset_credit_enabled": true,
			}},
			relay.AccountConfiguration{Extra: map[string]any{
				"privacy_mode":                   "training_off",
				"auto_reset_credit_enabled":      true,
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

	t.Run("an absent expiry auto-use pair at the provider defaults is not missing", func(t *testing.T) {
		// Sub2API reads a missing expiry pair as disabled with a ten-minute lead,
		// so an account that never carried it behaves exactly like one storing
		// those values; reporting the pair as missing would never converge.
		got := relay.CompareAccountConfiguration(
			relay.AccountConfiguration{Extra: map[string]any{
				"privacy_mode":                          "training_off",
				"auto_reset_credit_expiry_enabled":      false,
				"auto_reset_credit_expiry_lead_minutes": float64(10),
			}},
			relay.AccountConfiguration{Extra: map[string]any{"privacy_mode": "training_off"}},
		)
		if !got.Matches {
			t.Fatalf("Matches = false, want true; diff = %+v", got)
		}
	})

	t.Run("an enabled expiry auto-use pair the target does not carry is missing", func(t *testing.T) {
		got := relay.CompareAccountConfiguration(
			relay.AccountConfiguration{Extra: map[string]any{
				"privacy_mode":                          "training_off",
				"auto_reset_credit_expiry_enabled":      true,
				"auto_reset_credit_expiry_lead_minutes": float64(30),
			}},
			relay.AccountConfiguration{Extra: map[string]any{"privacy_mode": "training_off"}},
		)
		want := []string{
			"extra.auto_reset_credit_expiry_enabled",
			"extra.auto_reset_credit_expiry_lead_minutes",
		}
		if diff := cmp.Diff(want, got.MissingKeys); diff != "" {
			t.Fatalf("missing keys mismatch (-want +got):\n%s", diff)
		}
	})
}

func TestFilterAccountConfigurationDropsOnlyTheNamedAllowListedKeys(t *testing.T) {
	baseline := relay.AccountConfiguration{
		ModelMapping: map[string]string{"gpt-5.5": "gpt-6.1-sol"},
		Extra: map[string]any{
			"privacy_mode":   "training_off",
			"codex_cli_only": true,
		},
	}

	got := relay.FilterAccountConfiguration(baseline, []string{"model_mapping", " extra.codex_cli_only "})
	want := relay.AccountConfiguration{Extra: map[string]any{"privacy_mode": "training_off"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("filtered configuration mismatch (-want +got):\n%s", diff)
	}

	t.Run("no ignored keys leaves the configuration untouched", func(t *testing.T) {
		got := relay.FilterAccountConfiguration(baseline, nil)
		if diff := cmp.Diff(baseline, got); diff != "" {
			t.Fatalf("unfiltered configuration mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a name outside the allow-list is ignored", func(t *testing.T) {
		got := relay.FilterAccountConfiguration(baseline, []string{"credentials", "extra.not_allow_listed"})
		if diff := cmp.Diff(baseline, got); diff != "" {
			t.Fatalf("unknown ignore name changed the configuration (-want +got):\n%s", diff)
		}
	})

	t.Run("filtering both sides lets an ignored key converge", func(t *testing.T) {
		ignored := []string{"model_mapping"}
		target := relay.AccountConfiguration{Extra: map[string]any{"privacy_mode": "training_off", "codex_cli_only": true}, ModelMapping: map[string]string{"other": "mapping"}}
		diff := relay.CompareAccountConfiguration(relay.FilterAccountConfiguration(baseline, ignored), relay.FilterAccountConfiguration(target, ignored))
		if !diff.Matches {
			t.Fatalf("Matches = false, want true; the ignored mapping must not count as a difference; diff = %+v", diff)
		}
	})
}

// A copy must not materialise a threshold that the baseline's switch makes
// inert, but it must leave the stored baseline itself untouched.
func TestNormalizeAccountConfigurationForCopyWritesDefaultsForInertThresholds(t *testing.T) {
	t.Run("a switch-off baseline normalizes both thresholds", func(t *testing.T) {
		baseline := relay.AccountConfiguration{Extra: map[string]any{
			"privacy_mode":                   "training_off",
			"auto_reset_credit_enabled":      false,
			"auto_reset_credit_5h_threshold": float64(1),
			"auto_reset_credit_7d_threshold": float64(1),
		}}
		got := relay.NormalizeAccountConfigurationForCopy(baseline)
		want := map[string]any{
			"privacy_mode":                   "training_off",
			"auto_reset_credit_enabled":      false,
			"auto_reset_credit_5h_threshold": float64(0),
			"auto_reset_credit_7d_threshold": float64(1),
		}
		if diff := cmp.Diff(want, got.Extra); diff != "" {
			t.Fatalf("normalized extra mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(map[string]any{
			"privacy_mode":                   "training_off",
			"auto_reset_credit_enabled":      false,
			"auto_reset_credit_5h_threshold": float64(1),
			"auto_reset_credit_7d_threshold": float64(1),
		}, baseline.Extra); diff != "" {
			t.Fatalf("normalization mutated the baseline (-want +got):\n%s", diff)
		}
	})

	t.Run("a missing switch reads as off and normalizes the thresholds", func(t *testing.T) {
		got := relay.NormalizeAccountConfigurationForCopy(relay.AccountConfiguration{Extra: map[string]any{
			"auto_reset_credit_5h_threshold": float64(2),
			"auto_reset_credit_7d_threshold": float64(0.5),
		}})
		want := map[string]any{
			"auto_reset_credit_5h_threshold": float64(0),
			"auto_reset_credit_7d_threshold": float64(1),
		}
		if diff := cmp.Diff(want, got.Extra); diff != "" {
			t.Fatalf("normalized extra mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("an enabled switch keeps the stored thresholds", func(t *testing.T) {
		baseline := relay.AccountConfiguration{Extra: map[string]any{
			"auto_reset_credit_enabled":      true,
			"auto_reset_credit_5h_threshold": float64(0.8),
		}}
		if diff := cmp.Diff(baseline, relay.NormalizeAccountConfigurationForCopy(baseline)); diff != "" {
			t.Fatalf("enabled configuration changed (-want +got):\n%s", diff)
		}
	})

	t.Run("a baseline without auto-reset keys is returned unchanged", func(t *testing.T) {
		baseline := relay.AccountConfiguration{
			ModelMapping: map[string]string{"gpt-5.5": "gpt-6.1-sol"},
			Extra:        map[string]any{"privacy_mode": "training_off"},
		}
		if diff := cmp.Diff(baseline, relay.NormalizeAccountConfigurationForCopy(baseline)); diff != "" {
			t.Fatalf("configuration without auto-reset keys changed (-want +got):\n%s", diff)
		}
	})
}
