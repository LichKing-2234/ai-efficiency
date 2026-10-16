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
