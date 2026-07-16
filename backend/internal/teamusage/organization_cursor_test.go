package teamusage

import (
	"errors"
	"reflect"
	"testing"
)

func TestOrganizationCursorCodecRoundTripsAndSeparatesCollections(t *testing.T) {
	codec := newOrganizationCursorCodec(testMemberCursorSecret)
	payload := organizationCursorPayload{
		Version: organizationCursorVersion, Collection: organizationCursorDepartments,
		ActorUserID: 42, ScopeVersion: "scope-version-1", SnapshotID: "organization-snapshot-1",
		StartDate: "2026-07-01", EndDate: "2026-07-07", Granularity: "day", Timezone: "Asia/Shanghai",
		ParentDepartmentExternalID: "department-root", Offset: 25,
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
	if _, err := codec.Decode("A" + encoded[1:]); !errors.Is(err, ErrInvalidOrganizationCursor) {
		t.Fatalf("Decode(tampered) error = %v, want ErrInvalidOrganizationCursor", err)
	}

	memberPayload := payload
	memberPayload.Collection = organizationCursorMembers
	if encodedMembers, err := codec.Encode(memberPayload); err != nil || encodedMembers == encoded {
		t.Fatalf("member cursor = %q, error = %v, want distinct valid cursor", encodedMembers, err)
	}
}
