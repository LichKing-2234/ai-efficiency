package relayruntime

import (
	"bytes"
	"testing"
)

func TestProviderInvalidationPayloadContainsOnlyVersionIdentity(t *testing.T) {
	raw, err := EncodeInvalidation(InvalidationEvent{
		SchemaVersion:        1,
		ProviderID:           17,
		ConfigurationVersion: 4,
	})
	if err != nil {
		t.Fatalf("encode invalidation: %v", err)
	}
	if got, want := string(raw), `{"schema_version":1,"provider_id":17,"configuration_version":4}`; got != want {
		t.Fatalf("payload = %s, want %s", got, want)
	}
	for _, secret := range [][]byte{[]byte("api_key"), []byte("password"), []byte("base_url"), []byte("display_name")} {
		if bytes.Contains(raw, secret) {
			t.Fatalf("payload contains forbidden field %q: %s", secret, raw)
		}
	}
}

func TestProviderInvalidationRejectsMalformedOrUnknownPayloads(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte(`{"schema_version":2,"provider_id":17,"configuration_version":4}`),
		[]byte(`{"schema_version":1,"provider_id":0,"configuration_version":4}`),
		[]byte(`{"schema_version":1,"provider_id":17,"configuration_version":0}`),
		[]byte(`{"schema_version":1,"provider_id":17,"configuration_version":-1}`),
		[]byte(`{"schema_version":1,"provider_id":17,"configuration_version":4,"api_key":"secret"}`),
		[]byte(`not-json`),
	} {
		if _, err := DecodeInvalidation(raw); err == nil {
			t.Fatalf("DecodeInvalidation(%q) succeeded", raw)
		}
	}
}
