package relay

import (
	"encoding/json"
	"testing"
)

// TestAccountConfigurationAllowListMatchesItsWireStruct keeps the exhaustive
// allow-list and the narrowed wire struct from drifting apart. A field declared
// without a list entry would silently never project; a list entry without a
// declared field would silently project nothing.
func TestAccountConfigurationAllowListMatchesItsWireStruct(t *testing.T) {
	raw, err := json.Marshal(sub2apiAccountConfigExtra{})
	if err != nil {
		t.Fatalf("marshal wire struct: %v", err)
	}
	var declared map[string]json.RawMessage
	if err := json.Unmarshal(raw, &declared); err != nil {
		t.Fatalf("unmarshal wire struct: %v", err)
	}
	if len(declared) != len(accountConfigurationExtraKeys) {
		t.Fatalf("wire struct declares %d keys, allow-list has %d", len(declared), len(accountConfigurationExtraKeys))
	}
	for _, key := range accountConfigurationExtraKeys {
		if _, ok := declared[key]; !ok {
			t.Fatalf("allow-list key %q is not declared on sub2apiAccountConfigExtra", key)
		}
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
