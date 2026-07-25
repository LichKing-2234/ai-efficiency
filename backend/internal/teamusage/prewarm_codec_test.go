package teamusage

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/klauspost/compress/zstd"
)

func TestPrewarmStoredJSONRoundTripAndCorruption(t *testing.T) {
	value := testPrewarmSegment(t, testPrewarmIdentity(), testPrewarmGeneratedAt(), SegmentHistory29d, "a")
	encoded, err := encodePrewarmStoredJSON(value, prewarmSegmentMaxBytes, prewarmSegmentMaxBytes)
	if err != nil {
		t.Fatalf("encodePrewarmStoredJSON() error = %v", err)
	}
	if !bytes.HasPrefix(encoded, []byte{0x28, 0xb5, 0x2f, 0xfd}) || bytes.Contains(encoded, []byte(`"points"`)) {
		t.Fatal("stored value is not an opaque zstd frame")
	}
	var decoded PrewarmTrendSegment
	if err := decodePrewarmStoredJSON(encoded, prewarmSegmentMaxBytes, &decoded); err != nil {
		t.Fatalf("decodePrewarmStoredJSON() error = %v", err)
	}
	if diff := cmp.Diff(value, decoded); diff != "" {
		t.Fatalf("round trip mismatch (-want +got):\n%s", diff)
	}

	checksumCorrupted := append([]byte(nil), encoded...)
	checksumCorrupted[len(checksumCorrupted)-1] ^= 0xff
	emptyEncoder, err := zstd.NewWriter(nil, zstd.WithZeroFrames(true))
	if err != nil {
		t.Fatalf("create empty-frame encoder error = %v", err)
	}
	t.Cleanup(func() { emptyEncoder.Close() })
	emptyFrame := emptyEncoder.EncodeAll(nil, nil)
	skippableFrame := []byte{0x50, 0x2a, 0x4d, 0x18, 0, 0, 0, 0}
	corrupted := []struct {
		name  string
		value []byte
	}{
		{name: "checksum", value: checksumCorrupted},
		{name: "truncated", value: encoded[:len(encoded)-1]},
		{name: "appended", value: append(append([]byte(nil), encoded...), 0xff)},
		{name: "appended empty frame", value: append(append([]byte(nil), encoded...), emptyFrame...)},
		{name: "appended skippable frame", value: append(append([]byte(nil), encoded...), skippableFrame...)},
	}
	for _, test := range corrupted {
		t.Run(test.name, func(t *testing.T) {
			var destination PrewarmTrendSegment
			if err := decodePrewarmStoredJSON(test.value, prewarmSegmentMaxBytes, &destination); err == nil {
				t.Fatal("corrupt frame decoded without error")
			}
		})
	}
}

func TestPrewarmStoredJSONRejectsStrictBoundaries(t *testing.T) {
	t.Run("raw JSON exactly at decoded limit", func(t *testing.T) {
		for _, limit := range []int{prewarmCurrentStatsMaxBytes, prewarmSegmentMaxBytes} {
			value := strings.Repeat("x", limit-2)
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if len(raw) != limit {
				t.Fatalf("raw size = %d, want exact limit %d", len(raw), limit)
			}
			if _, err := encodePrewarmStoredJSON(value, limit, limit); err == nil {
				t.Fatalf("encodePrewarmStoredJSON(decoded limit %d) error = nil", limit)
			}
		}
	})

	t.Run("compressed output exactly at stored limit", func(t *testing.T) {
		value := strings.Repeat("compressible-value-", 32)
		encoded, err := encodePrewarmStoredJSON(value, 4096, 4096)
		if err != nil {
			t.Fatalf("encodePrewarmStoredJSON() error = %v", err)
		}
		if _, err := encodePrewarmStoredJSON(value, 4096, len(encoded)); err == nil {
			t.Fatalf("encodePrewarmStoredJSON(stored limit %d) error = nil", len(encoded))
		}
	})

	t.Run("expanded output exactly at decoded limit", func(t *testing.T) {
		value := strings.Repeat("expanded-value-", 32)
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		encoded, err := encodePrewarmStoredJSON(value, len(raw)+1, 4096)
		if err != nil {
			t.Fatalf("encodePrewarmStoredJSON() error = %v", err)
		}
		var decoded string
		if err := decodePrewarmStoredJSON(encoded, len(raw), &decoded); err == nil {
			t.Fatalf("decodePrewarmStoredJSON(decoded limit %d) error = nil", len(raw))
		}
	})

	t.Run("empty and stored width", func(t *testing.T) {
		var decoded string
		for _, encoded := range [][]byte{nil, make([]byte, prewarmCurrentStatsMaxBytes)} {
			if err := decodePrewarmStoredJSON(encoded, prewarmCurrentStatsMaxBytes, &decoded); err == nil {
				t.Fatalf("decodePrewarmStoredJSON(%d stored bytes) error = nil", len(encoded))
			}
		}
	})
}
