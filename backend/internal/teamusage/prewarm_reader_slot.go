package teamusage

import "sync/atomic"

// PrewarmReaderSource supplies the currently installed optional reader.
// A nil load means the caller must use the exact authoritative fallback.
type PrewarmReaderSource interface {
	Load() *PrewarmReader
}

type PrewarmReaderSlot struct {
	reader atomic.Pointer[PrewarmReader]
}

func NewPrewarmReaderSlot() *PrewarmReaderSlot {
	return &PrewarmReaderSlot{}
}

func (s *PrewarmReaderSlot) Load() *PrewarmReader {
	if s == nil {
		return nil
	}
	return s.reader.Load()
}

func (s *PrewarmReaderSlot) Store(reader *PrewarmReader) {
	if s == nil {
		return
	}
	s.reader.Store(reader)
}

func (s *PrewarmReaderSlot) Clear() {
	if s == nil {
		return
	}
	s.reader.Store(nil)
}

var _ PrewarmReaderSource = (*PrewarmReaderSlot)(nil)
