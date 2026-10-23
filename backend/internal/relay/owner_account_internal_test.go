package relay

import (
	"encoding/json"
	"testing"
)

// TestOwnerAccountExtraAllowListMatchesItsWireStruct keeps the exhaustive
// allow-list and the narrowed wire struct from drifting apart. A field declared
// without a list entry would silently never project; a list entry without a
// declared field would silently project nothing.
func TestOwnerAccountExtraAllowListMatchesItsWireStruct(t *testing.T) {
	raw, err := json.Marshal(sub2apiOwnerAccountExtra{})
	if err != nil {
		t.Fatalf("marshal wire struct: %v", err)
	}
	var declared map[string]json.RawMessage
	if err := json.Unmarshal(raw, &declared); err != nil {
		t.Fatalf("unmarshal wire struct: %v", err)
	}
	if len(declared) != len(ownerAccountExtraKeys) {
		t.Fatalf("wire struct declares %d keys, allow-list has %d", len(declared), len(ownerAccountExtraKeys))
	}
	for _, key := range ownerAccountExtraKeys {
		if _, ok := declared[key]; !ok {
			t.Fatalf("allow-list key %q is not declared on sub2apiOwnerAccountExtra", key)
		}
	}
}
