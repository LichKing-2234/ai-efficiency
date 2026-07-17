package teamusage

import (
	"errors"
	"reflect"
	"testing"
)

func TestMemberCursorCodecRoundTripsAndRejectsTampering(t *testing.T) {
	codec := newMemberCursorCodec("test-member-cursor-secret")
	payload := memberCursorPayload{
		Version: 1, ActorUserID: 42, ScopeVersion: "scope-version-1", SnapshotID: "snapshot-1",
		StartDate: "2026-07-01", EndDate: "2026-07-07", Granularity: "day", Timezone: "Asia/Shanghai", Offset: 50,
	}

	encoded, err := codec.Encode(payload)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	decoded, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !reflect.DeepEqual(decoded, payload) {
		t.Fatalf("decoded payload = %+v, want %+v", decoded, payload)
	}

	tamperedPrefix := "A"
	if encoded[0] == 'A' {
		tamperedPrefix = "B"
	}
	tampered := tamperedPrefix + encoded[1:]
	if _, err := codec.Decode(tampered); !errors.Is(err, ErrInvalidMemberCursor) {
		t.Fatalf("Decode(tampered) error = %v, want ErrInvalidMemberCursor", err)
	}
	if _, err := codec.Decode("not-a-cursor"); !errors.Is(err, ErrInvalidMemberCursor) {
		t.Fatalf("Decode(invalid) error = %v, want ErrInvalidMemberCursor", err)
	}
}
