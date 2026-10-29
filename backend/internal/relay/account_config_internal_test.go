package relay

import (
	"encoding/json"
	"testing"
)

// TestAccountConfigurationAllowListMatchesItsWireStruct keeps the exhaustive
// allow-list and the narrowed wire struct from drifting apart. A field declared
// without a list entry would silently never project; a list entry without a
// declared field would silently project nothing. The display-only keys are part
// of the same wire struct on purpose, so they are asserted here as well and must
// stay outside the copy allow-list.
func TestAccountConfigurationAllowListMatchesItsWireStruct(t *testing.T) {
	raw, err := json.Marshal(sub2apiAccountConfigExtra{})
	if err != nil {
		t.Fatalf("marshal wire struct: %v", err)
	}
	var declared map[string]json.RawMessage
	if err := json.Unmarshal(raw, &declared); err != nil {
		t.Fatalf("unmarshal wire struct: %v", err)
	}
	if want := len(accountConfigurationExtraKeys) + len(accountConfigurationDisplayOnlyExtraKeys); len(declared) != want {
		t.Fatalf("wire struct declares %d keys, want %d copy keys plus %d display-only keys", len(declared), len(accountConfigurationExtraKeys), len(accountConfigurationDisplayOnlyExtraKeys))
	}
	for _, key := range accountConfigurationExtraKeys {
		if _, ok := declared[key]; !ok {
			t.Fatalf("copy allow-list key %q is not declared on sub2apiAccountConfigExtra", key)
		}
	}
	for _, key := range accountConfigurationDisplayOnlyExtraKeys {
		if _, ok := declared[key]; !ok {
			t.Fatalf("display-only key %q is not declared on sub2apiAccountConfigExtra", key)
		}
	}
}

// TestAccountConfigurationDisplayOnlyKeysStayOutOfTheCopyAllowList is the
// guard that keeps a display observation from becoming copyable configuration.
// Copying a cached reset-credit snapshot onto another account would fabricate
// observed upstream state, so the key may be read and must never be projected
// into AccountConfiguration.
func TestAccountConfigurationDisplayOnlyKeysStayOutOfTheCopyAllowList(t *testing.T) {
	copyKeys := make(map[string]struct{}, len(accountConfigurationExtraKeys))
	for _, key := range accountConfigurationExtraKeys {
		copyKeys[key] = struct{}{}
	}
	for _, key := range accountConfigurationDisplayOnlyExtraKeys {
		if _, ok := copyKeys[key]; ok {
			t.Fatalf("display-only key %q is also in the copy allow-list", key)
		}
	}

	item := sub2apiAccountConfigItem{}
	item.Extra.ResetCreditsSnapshot = json.RawMessage(`{"available_count":2,"credits":[{"expires_at":"2099-01-01T00:00:00Z"}]}`)
	config, err := projectAccountConfiguration(item)
	if err != nil {
		t.Fatalf("projectAccountConfiguration() error = %v", err)
	}
	if _, ok := config.Extra[accountResetCreditCountKey]; ok {
		t.Fatalf("display-only key %q projected into the copyable configuration", accountResetCreditCountKey)
	}
	if len(config.Extra) != 0 {
		t.Fatalf("configuration extra = %v, want none", config.Extra)
	}
}

// TestAccountConfigurationDefaultsStayInsideTheAllowList keeps a documented
// provider default from surviving the removal of the key it describes. A default
// for a key that may no longer leave this package would compare nothing, so the
// normalization would silently stop applying.
func TestAccountConfigurationDefaultsStayInsideTheAllowList(t *testing.T) {
	allowed := make(map[string]struct{}, len(accountConfigurationExtraKeys))
	for _, key := range accountConfigurationExtraKeys {
		allowed[key] = struct{}{}
	}
	for key := range accountConfigurationExtraDefaults {
		if _, ok := allowed[key]; !ok {
			t.Fatalf("default declared for %q, which is not in the allow-list", key)
		}
	}
}

// TestAccountConfigurationInertThresholdsStayInsideTheAllowList keeps the copy
// normalization and the comparison exemption anchored to the same list and the
// same recorded defaults as the rest of the projection.
func TestAccountConfigurationInertThresholdsStayInsideTheAllowList(t *testing.T) {
	allowed := make(map[string]struct{}, len(accountConfigurationExtraKeys))
	for _, key := range accountConfigurationExtraKeys {
		allowed[key] = struct{}{}
	}
	for key := range accountConfigurationInertThresholdKeys {
		if _, ok := allowed[key]; !ok {
			t.Fatalf("inert threshold %q is not in the allow-list", key)
		}
		if _, ok := accountConfigurationExtraDefaults[key]; !ok {
			t.Fatalf("inert threshold %q has no recorded provider default", key)
		}
	}
	if _, ok := allowed[accountConfigurationAutoResetSwitchKey]; !ok {
		t.Fatalf("auto-reset switch %q is not in the allow-list", accountConfigurationAutoResetSwitchKey)
	}
}
