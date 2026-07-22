package teamusage

import (
	"fmt"
	"sync"

	"github.com/klauspost/compress/zstd"
)

type prewarmStoredCodecPair struct {
	encoder *zstd.Encoder
	decoder *zstd.Decoder
}

var prewarmStoredCodec = sync.OnceValues(func() (*prewarmStoredCodecPair, error) {
	encoder, err := zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.SpeedFastest),
		zstd.WithEncoderConcurrency(1),
		zstd.WithEncoderCRC(true),
	)
	if err != nil {
		return nil, fmt.Errorf("create prewarm zstd encoder: %w", err)
	}
	decoder, err := zstd.NewReader(nil,
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderMaxMemory(uint64(prewarmSegmentMaxBytes)),
		zstd.WithDecoderMaxWindow(uint64(prewarmSegmentMaxBytes)),
		zstd.WithDecodeAllCapLimit(true),
	)
	if err != nil {
		return nil, fmt.Errorf("create prewarm zstd decoder: %w", err)
	}
	return &prewarmStoredCodecPair{encoder: encoder, decoder: decoder}, nil
})

func encodePrewarmStoredJSON(value any, decodedLimit, storedLimit int) ([]byte, error) {
	if decodedLimit <= 0 || storedLimit <= 0 {
		return nil, fmt.Errorf("prewarm stored JSON limits must be positive")
	}
	decoded, err := encodePrewarmJSON(value, decodedLimit)
	if err != nil {
		return nil, err
	}
	codec, err := prewarmStoredCodec()
	if err != nil {
		return nil, err
	}
	encoded := codec.encoder.EncodeAll(decoded, nil)
	if len(encoded) == 0 || len(encoded) >= storedLimit {
		return nil, fmt.Errorf("compressed value reached strict %d-byte limit", storedLimit)
	}
	return encoded, nil
}

func decodePrewarmStoredJSON(encoded []byte, decodedLimit int, destination any) error {
	if decodedLimit <= 0 {
		return fmt.Errorf("prewarm stored JSON decoded limit must be positive")
	}
	if len(encoded) == 0 || len(encoded) >= decodedLimit {
		return fmt.Errorf("compressed value is empty or reached strict %d-byte limit", decodedLimit)
	}
	codec, err := prewarmStoredCodec()
	if err != nil {
		return err
	}
	decoded, err := codec.decoder.DecodeAll(encoded, make([]byte, 0, decodedLimit))
	if err != nil {
		return fmt.Errorf("decode prewarm zstd frame: %w", err)
	}
	if len(decoded) >= decodedLimit {
		return fmt.Errorf("expanded value reached strict %d-byte limit", decodedLimit)
	}
	if err := decodePrewarmJSON(decoded, decodedLimit, destination); err != nil {
		return fmt.Errorf("decode prewarm stored JSON: %w", err)
	}
	return nil
}
