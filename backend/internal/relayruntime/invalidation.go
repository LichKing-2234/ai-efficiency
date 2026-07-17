package relayruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"

	redis "github.com/redis/go-redis/v9"
)

const invalidationSchemaVersion = 1

var namespacePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

type InvalidationEvent struct {
	SchemaVersion        int   `json:"schema_version"`
	ProviderID           int   `json:"provider_id"`
	ConfigurationVersion int64 `json:"configuration_version"`
}

type InvalidationBus interface {
	Publish(ctx context.Context, event InvalidationEvent) error
	Subscribe(ctx context.Context, handler func(InvalidationEvent)) error
}

func EncodeInvalidation(event InvalidationEvent) ([]byte, error) {
	if err := validateInvalidation(event); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("encode provider invalidation: %w", err)
	}
	return raw, nil
}

func DecodeInvalidation(raw []byte) (InvalidationEvent, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var event InvalidationEvent
	if err := decoder.Decode(&event); err != nil {
		return InvalidationEvent{}, fmt.Errorf("decode provider invalidation: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return InvalidationEvent{}, fmt.Errorf("decode provider invalidation: trailing content")
	}
	if err := validateInvalidation(event); err != nil {
		return InvalidationEvent{}, err
	}
	return event, nil
}

func validateInvalidation(event InvalidationEvent) error {
	if event.SchemaVersion != invalidationSchemaVersion {
		return fmt.Errorf("unsupported provider invalidation schema version %d", event.SchemaVersion)
	}
	if event.ProviderID <= 0 {
		return fmt.Errorf("provider invalidation ID must be positive")
	}
	if event.ConfigurationVersion <= 0 {
		return fmt.Errorf("provider invalidation configuration version must be positive")
	}
	return nil
}

type RedisInvalidationBus struct {
	client  redis.UniversalClient
	channel string
}

func NewRedisInvalidationBus(client redis.UniversalClient, namespace string) (*RedisInvalidationBus, error) {
	if client == nil {
		return nil, fmt.Errorf("provider invalidation Redis client is required")
	}
	if !namespacePattern.MatchString(namespace) {
		return nil, fmt.Errorf("invalid Redis namespace %q", namespace)
	}
	return &RedisInvalidationBus{
		client:  client,
		channel: fmt.Sprintf("%s:relay-provider-invalidation:v1", namespace),
	}, nil
}

func (b *RedisInvalidationBus) Publish(ctx context.Context, event InvalidationEvent) error {
	raw, err := EncodeInvalidation(event)
	if err != nil {
		return err
	}
	if err := b.client.Publish(ctx, b.channel, raw).Err(); err != nil {
		return fmt.Errorf("publish provider invalidation: %w", err)
	}
	return nil
}

func (b *RedisInvalidationBus) Subscribe(ctx context.Context, handler func(InvalidationEvent)) error {
	if handler == nil {
		return fmt.Errorf("provider invalidation handler is required")
	}
	subscription := b.client.Subscribe(ctx, b.channel)
	defer subscription.Close()
	if _, err := subscription.Receive(ctx); err != nil {
		return fmt.Errorf("subscribe provider invalidation: %w", err)
	}
	messages := subscription.Channel()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case message, ok := <-messages:
			if !ok {
				return fmt.Errorf("provider invalidation subscription closed")
			}
			event, err := DecodeInvalidation([]byte(message.Payload))
			if err != nil {
				continue
			}
			handler(event)
		}
	}
}
