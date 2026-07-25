package teamusage

import (
	"fmt"
	"io"
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
	if err := validateSinglePrewarmZstdFrame(encoded); err != nil {
		return err
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

func validateSinglePrewarmZstdFrame(encoded []byte) error {
	var header zstd.Header
	if err := header.Decode(encoded); err != nil {
		return fmt.Errorf("decode prewarm zstd frame header: %w", err)
	}
	if header.Skippable {
		return fmt.Errorf("prewarm stored JSON must use one non-skippable zstd frame")
	}
	offset := header.HeaderSize
	for {
		if len(encoded)-offset < 3 {
			return fmt.Errorf("decode prewarm zstd frame blocks: %w", io.ErrUnexpectedEOF)
		}
		blockHeader := uint32(encoded[offset]) | uint32(encoded[offset+1])<<8 | uint32(encoded[offset+2])<<16
		offset += 3
		lastBlock := blockHeader&1 != 0
		blockType := (blockHeader >> 1) & 3
		blockSize := int(blockHeader >> 3)
		switch blockType {
		case 0, 2:
		case 1:
			blockSize = 1
		default:
			return fmt.Errorf("decode prewarm zstd frame blocks: reserved block type")
		}
		if blockSize > len(encoded)-offset {
			return fmt.Errorf("decode prewarm zstd frame blocks: %w", io.ErrUnexpectedEOF)
		}
		offset += blockSize
		if lastBlock {
			break
		}
	}
	if header.HasCheckSum {
		if len(encoded)-offset < 4 {
			return fmt.Errorf("decode prewarm zstd frame checksum: %w", io.ErrUnexpectedEOF)
		}
		offset += 4
	}
	if offset != len(encoded) {
		return fmt.Errorf("prewarm stored JSON contains more than one zstd frame")
	}
	return nil
}
