package authstate

import (
	"context"
	"sync"
	"time"
)

// MemoryStore is the process-local Store implementation. It keeps local tests
// and single-process deployments working without Redis, but it cannot complete
// a login flow on a different process.
type MemoryStore struct {
	now func() time.Time

	mu      sync.Mutex
	entries map[string]memoryEntry
}

type memoryEntry struct {
	value   []byte
	expires time.Time
}

// NewMemoryStore builds a process-local store backed by a mutex and lazy expiry.
func NewMemoryStore() *MemoryStore {
	return newMemoryStore(time.Now)
}

func newMemoryStore(now func() time.Time) *MemoryStore {
	return &MemoryStore{
		now:     now,
		entries: make(map[string]memoryEntry),
	}
}

func (s *MemoryStore) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	if err := validateTTL(ttl); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	s.entries[key] = memoryEntry{value: append([]byte(nil), value...), expires: s.now().Add(ttl)}
	return nil
}

func (s *MemoryStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	entry, ok := s.entries[key]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), entry.value...), true, nil
}

func (s *MemoryStore) Take(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	entry, ok := s.entries[key]
	if !ok {
		return nil, false, nil
	}
	delete(s.entries, key)
	return append([]byte(nil), entry.value...), true, nil
}

func (s *MemoryStore) SetIfAbsent(_ context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	if err := validateTTL(ttl); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	if _, ok := s.entries[key]; ok {
		return false, nil
	}
	s.entries[key] = memoryEntry{value: append([]byte(nil), value...), expires: s.now().Add(ttl)}
	return true, nil
}

func (s *MemoryStore) Update(_ context.Context, key string, ttl time.Duration, mutate func(current []byte) ([]byte, error)) (bool, error) {
	if err := validateTTL(ttl); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	entry, ok := s.entries[key]
	if !ok {
		return false, nil
	}
	next, err := mutate(append([]byte(nil), entry.value...))
	if err != nil {
		return true, err
	}
	s.entries[key] = memoryEntry{value: append([]byte(nil), next...), expires: s.now().Add(ttl)}
	return true, nil
}

func (s *MemoryStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, key)
	return nil
}

func (s *MemoryStore) cleanupLocked() {
	now := s.now()
	for key, entry := range s.entries {
		if !now.Before(entry.expires) {
			delete(s.entries, key)
		}
	}
}
