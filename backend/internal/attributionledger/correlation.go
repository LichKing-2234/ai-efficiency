package attributionledger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
)

const (
	requestEvidenceTTL         = 24 * time.Hour
	failedRequestEvidenceTTL   = 30 * 24 * time.Hour
	maxEvidencePerConversation = 128
)

var ErrCorrelationStoreUnavailable = errors.New("correlation store is unavailable")

type RequestEvidence struct {
	ConversationID string    `json:"conversation_id"`
	RequestID      string    `json:"request_id,omitempty"`
	ObservedAt     time.Time `json:"observed_at"`
	EventName      string    `json:"event_name"`
	Transport      string    `json:"transport"`
	StatusCode     int       `json:"status_code,omitempty"`
	ErrorCategory  string    `json:"error_category,omitempty"`
	Failed         bool      `json:"failed,omitempty"`
}

type CorrelationSummary struct {
	Quality          CorrelationQuality `json:"quality"`
	RequestIDCount   int                `json:"request_id_count"`
	RequestSetDigest string             `json:"request_set_digest,omitempty"`
}

type evidenceEnvelope struct {
	Items []RequestEvidence `json:"items"`
}

type evidenceIndex struct {
	Version int                 `json:"version"`
	Items   []evidenceReference `json:"items"`
}

type evidenceReference struct {
	Key        string    `json:"key"`
	ObservedAt time.Time `json:"observed_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type storedEvidence struct {
	Key  string
	Item RequestEvidence
}

type correlationDeleteStore interface {
	Delete(context.Context, string) error
}

type CorrelationStore struct {
	store     readcache.Store
	namespace string
	now       func() time.Time
}

func NewCorrelationStore(store readcache.Store, namespace string) *CorrelationStore {
	return &CorrelationStore{store: store, namespace: strings.TrimSpace(namespace), now: time.Now}
}

func (s *CorrelationStore) Put(ctx context.Context, installationID string, evidence []RequestEvidence) error {
	if s == nil || s.store == nil {
		return errors.New("correlation store is unavailable")
	}
	now := s.currentTime()
	grouped := map[string]map[string][]RequestEvidence{}
	for _, item := range evidence {
		item.ConversationID = strings.TrimSpace(item.ConversationID)
		item.RequestID = strings.TrimSpace(item.RequestID)
		item.EventName = strings.TrimSpace(item.EventName)
		item.Transport = strings.TrimSpace(item.Transport)
		item.ErrorCategory = strings.TrimSpace(item.ErrorCategory)
		if item.ConversationID == "" || item.ObservedAt.IsZero() {
			continue
		}
		class := "success"
		if item.Failed {
			class = "failed"
		}
		if !withinEvidenceRetention(item, now, retentionForClass(class)) {
			continue
		}
		if grouped[item.ConversationID] == nil {
			grouped[item.ConversationID] = map[string][]RequestEvidence{}
		}
		grouped[item.ConversationID][class] = append(grouped[item.ConversationID][class], item)
	}
	for conversationID, incomingByClass := range grouped {
		for _, class := range []string{"success", "failed"} {
			incoming := incomingByClass[class]
			if len(incoming) == 0 {
				// Do not rewrite the other retention class. In particular, a
				// failed request must not extend successful Request ID TTLs.
				continue
			}
			indexKey := s.key(installationID, conversationID, class)
			existing, existingKeys, err := s.loadStoredEvidence(ctx, indexKey)
			if err != nil {
				return err
			}
			retention := retentionForClass(class)
			merged := map[string]RequestEvidence{}
			for _, stored := range existing {
				if withinEvidenceRetention(stored.Item, now, retention) {
					merged[s.itemKey(indexKey, stored.Item)] = stored.Item
				}
			}
			for _, item := range incoming {
				if withinEvidenceRetention(item, now, retention) {
					merged[s.itemKey(indexKey, item)] = item
				}
			}
			items := make([]storedEvidence, 0, len(merged))
			for key, item := range merged {
				items = append(items, storedEvidence{Key: key, Item: item})
			}
			sort.Slice(items, func(i, j int) bool {
				if items[i].Item.ObservedAt.Equal(items[j].Item.ObservedAt) {
					return items[i].Key < items[j].Key
				}
				return items[i].Item.ObservedAt.Before(items[j].Item.ObservedAt)
			})
			if len(items) > maxEvidencePerConversation {
				items = items[len(items)-maxEvidencePerConversation:]
			}
			kept := map[string]struct{}{}
			index := evidenceIndex{Version: 2, Items: make([]evidenceReference, 0, len(items))}
			for _, stored := range items {
				expiresAt := stored.Item.ObservedAt.Add(retention)
				remaining := expiresAt.Sub(now)
				if remaining <= 0 {
					continue
				}
				if remaining > retention {
					remaining = retention
				}
				payload, err := json.Marshal(stored.Item)
				if err != nil {
					return fmt.Errorf("marshal request correlation item: %w", err)
				}
				if err := s.store.Set(ctx, stored.Key, payload, remaining); err != nil {
					return fmt.Errorf("store request correlation item: %w", err)
				}
				kept[stored.Key] = struct{}{}
				index.Items = append(index.Items, evidenceReference{Key: stored.Key, ObservedAt: stored.Item.ObservedAt, ExpiresAt: expiresAt})
			}
			if len(index.Items) == 0 {
				continue
			}
			if deleter, ok := s.store.(correlationDeleteStore); ok {
				for _, key := range existingKeys {
					if _, ok := kept[key]; !ok {
						if err := deleter.Delete(ctx, key); err != nil {
							return fmt.Errorf("delete expired request correlation item: %w", err)
						}
					}
				}
			}
			payload, err := json.Marshal(index)
			if err != nil {
				return fmt.Errorf("marshal request correlation index: %w", err)
			}
			indexTTL := index.Items[len(index.Items)-1].ExpiresAt.Sub(now)
			if indexTTL > retention {
				indexTTL = retention
			}
			if err := s.store.Set(ctx, indexKey, payload, indexTTL); err != nil {
				return fmt.Errorf("store request correlation index: %w", err)
			}
		}
	}
	return nil
}

func (s *CorrelationStore) loadStoredEvidence(ctx context.Context, indexKey string) ([]storedEvidence, []string, error) {
	payload, err := s.store.Get(ctx, indexKey)
	if errors.Is(err, readcache.ErrMiss) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("load request correlation: %w", err)
	}
	var index evidenceIndex
	if err := json.Unmarshal(payload, &index); err == nil && index.Version == 2 {
		items := make([]storedEvidence, 0, len(index.Items))
		keys := make([]string, 0, len(index.Items))
		for _, reference := range index.Items {
			key := strings.TrimSpace(reference.Key)
			if key == "" {
				continue
			}
			keys = append(keys, key)
			itemPayload, err := s.store.Get(ctx, key)
			if errors.Is(err, readcache.ErrMiss) {
				continue
			}
			if err != nil {
				return nil, nil, fmt.Errorf("load request correlation item: %w", err)
			}
			var item RequestEvidence
			if err := json.Unmarshal(itemPayload, &item); err != nil {
				continue
			}
			items = append(items, storedEvidence{Key: key, Item: item})
		}
		return items, keys, nil
	}
	var legacy evidenceEnvelope
	if err := json.Unmarshal(payload, &legacy); err != nil {
		return nil, nil, nil
	}
	items := make([]storedEvidence, 0, len(legacy.Items))
	for _, item := range legacy.Items {
		items = append(items, storedEvidence{Key: s.itemKey(indexKey, item), Item: item})
	}
	return items, nil, nil
}

func (s *CorrelationStore) itemKey(indexKey string, item RequestEvidence) string {
	identity := fmt.Sprintf("%s\x00%s\x00%d\x00%s", item.RequestID, item.EventName, item.ObservedAt.UnixNano(), item.Transport)
	sum := sha256.Sum256([]byte(identity))
	return indexKey + ":item:" + hex.EncodeToString(sum[:])
}

func (s *CorrelationStore) Match(ctx context.Context, installationID string, slices []SessionSlice) (CorrelationSummary, error) {
	result := CorrelationSummary{Quality: CorrelationQualityUnlinked}
	if s == nil || s.store == nil {
		return result, nil
	}
	now := s.currentTime()
	requestIDs := map[string]struct{}{}
	for _, slice := range slices {
		for _, class := range []string{"success", "failed"} {
			items, _, err := s.loadStoredEvidence(ctx, s.key(installationID, slice.ConversationID, class))
			if err != nil {
				return result, fmt.Errorf("load request correlation for matching: %w", err)
			}
			for _, stored := range items {
				item := stored.Item
				if !withinEvidenceRetention(item, now, retentionForClass(class)) {
					continue
				}
				if item.ObservedAt.Before(slice.ObservedStart.Add(-2*time.Minute)) || item.ObservedAt.After(slice.ObservedEnd.Add(2*time.Minute)) {
					continue
				}
				if item.RequestID != "" {
					requestIDs[item.RequestID] = struct{}{}
				}
			}
		}
	}
	if len(requestIDs) == 0 {
		return result, nil
	}
	ids := make([]string, 0, len(requestIDs))
	for id := range requestIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	sum := sha256.Sum256([]byte(strings.Join(ids, "\x00")))
	result.Quality = CorrelationQualityAdvisory
	result.RequestIDCount = len(ids)
	result.RequestSetDigest = hex.EncodeToString(sum[:])
	return result, nil
}

// Lookup returns only currently retained, time-matched Request ID evidence.
// Callers must apply their own owner/Admin authorization before invoking it.
func (s *CorrelationStore) Lookup(ctx context.Context, installationID string, slices []SessionSlice) ([]RequestEvidence, error) {
	if s == nil || s.store == nil {
		return nil, ErrCorrelationStoreUnavailable
	}
	now := s.currentTime()
	seen := map[string]struct{}{}
	result := []RequestEvidence{}
	for _, slice := range slices {
		for _, class := range []string{"success", "failed"} {
			items, _, err := s.loadStoredEvidence(ctx, s.key(installationID, slice.ConversationID, class))
			if err != nil {
				return nil, fmt.Errorf("load request correlation detail: %w", err)
			}
			for _, stored := range items {
				item := stored.Item
				if strings.TrimSpace(item.RequestID) == "" || !withinEvidenceRetention(item, now, retentionForClass(class)) {
					continue
				}
				if item.ObservedAt.Before(slice.ObservedStart.Add(-2*time.Minute)) || item.ObservedAt.After(slice.ObservedEnd.Add(2*time.Minute)) {
					continue
				}
				key := fmt.Sprintf("%s\x00%d\x00%s", item.RequestID, item.ObservedAt.UnixNano(), item.EventName)
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				result = append(result, item)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ObservedAt.Equal(result[j].ObservedAt) {
			return result[i].RequestID < result[j].RequestID
		}
		return result[i].ObservedAt.Before(result[j].ObservedAt)
	})
	return result, nil
}

func (s *CorrelationStore) currentTime() time.Time {
	if s.now == nil {
		return time.Now().UTC()
	}
	return s.now().UTC()
}

func retentionForClass(class string) time.Duration {
	if class == "failed" {
		return failedRequestEvidenceTTL
	}
	return requestEvidenceTTL
}

func withinEvidenceRetention(item RequestEvidence, now time.Time, retention time.Duration) bool {
	return item.ObservedAt.After(now.Add(-retention))
}

func (s *CorrelationStore) key(installationID, conversationID, class string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(installationID) + "\x00" + strings.TrimSpace(conversationID)))
	prefix := s.namespace
	if prefix != "" {
		prefix += ":"
	}
	return prefix + "attribution:codex-request-evidence:" + hex.EncodeToString(sum[:]) + ":" + class
}
