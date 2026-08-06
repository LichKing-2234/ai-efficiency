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
			var existing []RequestEvidence
			payload, err := s.store.Get(ctx, s.key(installationID, conversationID, class))
			if err == nil {
				var envelope evidenceEnvelope
				_ = json.Unmarshal(payload, &envelope)
				existing = envelope.Items
			} else if !errors.Is(err, readcache.ErrMiss) {
				return fmt.Errorf("load request correlation: %w", err)
			}
			seen := map[string]struct{}{}
			merged := make([]RequestEvidence, 0, len(existing)+len(incoming))
			for _, item := range append(existing, incoming...) {
				if !withinEvidenceRetention(item, now, retentionForClass(class)) {
					continue
				}
				identity := fmt.Sprintf("%s\x00%s\x00%d\x00%s", item.RequestID, item.EventName, item.ObservedAt.UnixNano(), item.Transport)
				if _, ok := seen[identity]; ok {
					continue
				}
				seen[identity] = struct{}{}
				merged = append(merged, item)
			}
			sort.Slice(merged, func(i, j int) bool { return merged[i].ObservedAt.Before(merged[j].ObservedAt) })
			if len(merged) > maxEvidencePerConversation {
				merged = merged[len(merged)-maxEvidencePerConversation:]
			}
			payload, err = json.Marshal(evidenceEnvelope{Items: merged})
			if err != nil {
				return fmt.Errorf("marshal request correlation: %w", err)
			}
			ttl := retentionForClass(class)
			remaining := merged[len(merged)-1].ObservedAt.Add(ttl).Sub(now)
			if remaining < ttl {
				ttl = remaining
			}
			if err := s.store.Set(ctx, s.key(installationID, conversationID, class), payload, ttl); err != nil {
				return fmt.Errorf("store request correlation: %w", err)
			}
		}
	}
	return nil
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
			payload, err := s.store.Get(ctx, s.key(installationID, slice.ConversationID, class))
			if errors.Is(err, readcache.ErrMiss) {
				continue
			}
			if err != nil {
				return result, fmt.Errorf("load request correlation for matching: %w", err)
			}
			var envelope evidenceEnvelope
			if err := json.Unmarshal(payload, &envelope); err != nil {
				continue
			}
			for _, item := range envelope.Items {
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
