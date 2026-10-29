package relayplanning_test

import (
	"context"
	"testing"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/relayplanning"
	"github.com/google/go-cmp/cmp"
)

// accountConfigCopyProvider serves the allow-listed configuration of one platform
// and applies a copy the way the Relay provider does: a key-level merge per
// account.
type accountConfigCopyProvider struct {
	relay.Provider
	configurations map[int64]relay.AccountConfiguration
	appliedTargets []int64
	applied        relay.AccountConfiguration
}

func cloneAccountConfigCopyExtra(extra map[string]any) map[string]any {
	if extra == nil {
		return nil
	}
	out := make(map[string]any, len(extra))
	for key, value := range extra {
		out[key] = value
	}
	return out
}

func (p *accountConfigCopyProvider) ReadAccountConfigurations(context.Context, string) (map[int64]relay.AccountConfigurationRecord, error) {
	out := make(map[int64]relay.AccountConfigurationRecord, len(p.configurations))
	for id, configuration := range p.configurations {
		out[id] = relay.AccountConfigurationRecord{Configuration: relay.AccountConfiguration{
			ModelMapping: configuration.ModelMapping,
			Extra:        cloneAccountConfigCopyExtra(configuration.Extra),
		}}
	}
	return out, nil
}

func (p *accountConfigCopyProvider) ApplyAccountConfiguration(_ context.Context, accountIDs []int64, configuration relay.AccountConfiguration) error {
	p.appliedTargets = append([]int64(nil), accountIDs...)
	p.applied = relay.AccountConfiguration{
		ModelMapping: configuration.ModelMapping,
		Extra:        cloneAccountConfigCopyExtra(configuration.Extra),
	}
	for _, id := range accountIDs {
		merged := p.configurations[id]
		if configuration.ModelMapping != nil {
			merged.ModelMapping = configuration.ModelMapping
		}
		if merged.Extra == nil {
			merged.Extra = map[string]any{}
		}
		for key, value := range configuration.Extra {
			merged.Extra[key] = value
		}
		p.configurations[id] = merged
	}
	return nil
}

type accountConfigCopyResolver struct {
	provider relay.Provider
}

func (r accountConfigCopyResolver) Resolve(context.Context, int) (relay.Provider, error) {
	return r.provider, nil
}

// A copy from a switch-off baseline must write the provider threshold defaults
// rather than the baseline's stored values, and still verify as applied because
// the comparison ignores thresholds while the switch is off.
func TestApplyAccountConfigurationNormalizesInertThresholds(t *testing.T) {
	provider := &accountConfigCopyProvider{configurations: map[int64]relay.AccountConfiguration{
		11: {Extra: map[string]any{
			"privacy_mode":                   "training_off",
			"auto_reset_credit_enabled":      false,
			"auto_reset_credit_5h_threshold": float64(1),
			"auto_reset_credit_7d_threshold": float64(1),
		}},
		12: {Extra: map[string]any{"privacy_mode": "training_off"}},
	}}
	service := relayplanning.NewService(nil, accountConfigCopyResolver{provider: provider}, nil)

	got, err := service.ApplyAccountConfiguration(context.Background(), relayplanning.ApplyAccountConfigurationRequest{
		ProviderID:        7,
		Platform:          "openai",
		BaselineAccountID: 11,
		TargetAccountIDs:  []int64{12},
	})
	if err != nil {
		t.Fatalf("ApplyAccountConfiguration() error = %v", err)
	}
	if diff := cmp.Diff([]int64{12}, provider.appliedTargets); diff != "" {
		t.Fatalf("applied targets mismatch (-want +got):\n%s", diff)
	}
	wantApplied := map[string]any{
		"privacy_mode":                   "training_off",
		"auto_reset_credit_enabled":      false,
		"auto_reset_credit_5h_threshold": float64(0),
		"auto_reset_credit_7d_threshold": float64(1),
	}
	if diff := cmp.Diff(wantApplied, provider.applied.Extra); diff != "" {
		t.Fatalf("applied extra mismatch (-want +got):\n%s", diff)
	}
	if len(got.Results) != 1 || !got.Results[0].Applied {
		t.Fatalf("results = %+v, want one applied target", got.Results)
	}
}

// A baseline whose switch is on keeps its stored thresholds in the copied
// payload; they are meaningful for both the baseline and the targets.
func TestApplyAccountConfigurationKeepsThresholdsWhileTheSwitchIsOn(t *testing.T) {
	provider := &accountConfigCopyProvider{configurations: map[int64]relay.AccountConfiguration{
		21: {Extra: map[string]any{
			"auto_reset_credit_enabled":      true,
			"auto_reset_credit_5h_threshold": float64(0.8),
		}},
		22: {Extra: map[string]any{
			"auto_reset_credit_enabled":      true,
			"auto_reset_credit_5h_threshold": float64(0.5),
		}},
	}}
	service := relayplanning.NewService(nil, accountConfigCopyResolver{provider: provider}, nil)

	got, err := service.ApplyAccountConfiguration(context.Background(), relayplanning.ApplyAccountConfigurationRequest{
		ProviderID:        7,
		Platform:          "openai",
		BaselineAccountID: 21,
		TargetAccountIDs:  []int64{22},
	})
	if err != nil {
		t.Fatalf("ApplyAccountConfiguration() error = %v", err)
	}
	wantApplied := map[string]any{
		"auto_reset_credit_enabled":      true,
		"auto_reset_credit_5h_threshold": float64(0.8),
	}
	if diff := cmp.Diff(wantApplied, provider.applied.Extra); diff != "" {
		t.Fatalf("applied extra mismatch (-want +got):\n%s", diff)
	}
	if len(got.Results) != 1 || !got.Results[0].Applied {
		t.Fatalf("results = %+v, want one applied target", got.Results)
	}
}
