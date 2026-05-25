package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	oauthCodeKeyPrefix     = "ai-efficiency:oauth:code:"
	oauthDeviceKeyPrefix   = "ai-efficiency:oauth:device:"
	oauthUserCodeKeyPrefix = "ai-efficiency:oauth:user_code:"
)

var ErrOAuthStateNotFound = errors.New("oauth state not found")

type AuthorizationCodeSession struct {
	Code          string    `json:"code"`
	ClientID      string    `json:"client_id"`
	RedirectURI   string    `json:"redirect_uri"`
	CodeChallenge string    `json:"code_challenge"`
	UserID        int       `json:"user_id"`
	Username      string    `json:"username"`
	Role          string    `json:"role"`
	State         string    `json:"state"`
	CreatedAt     time.Time `json:"created_at"`
}

type DeviceSession struct {
	DeviceCode      string    `json:"device_code"`
	UserCode        string    `json:"user_code"`
	NormalizedCode  string    `json:"normalized_code"`
	ClientID        string    `json:"client_id"`
	Status          string    `json:"status"`
	UserID          int       `json:"user_id"`
	Username        string    `json:"username"`
	Role            string    `json:"role"`
	CreatedAt       time.Time `json:"created_at"`
	ExpiresAt       time.Time `json:"expires_at"`
	LastPolledAt    time.Time `json:"last_polled_at"`
	PollIntervalSec int       `json:"poll_interval_sec"`
}

type OAuthStateStore interface {
	StoreAuthorizationCode(ctx context.Context, session *AuthorizationCodeSession, ttl time.Duration) error
	ConsumeAuthorizationCode(ctx context.Context, code string) (*AuthorizationCodeSession, error)
	StoreDeviceSession(ctx context.Context, session *DeviceSession, ttl time.Duration) error
	GetDeviceSession(ctx context.Context, deviceCode string) (*DeviceSession, error)
	GetDeviceSessionByUserCode(ctx context.Context, userCode string) (*DeviceSession, error)
	UpdateDeviceSession(ctx context.Context, session *DeviceSession, ttl time.Duration) error
	ConsumeDeviceSession(ctx context.Context, deviceCode string) (*DeviceSession, error)
	UserCodeExists(ctx context.Context, normalizedUserCode string) (bool, error)
}

type RedisStateStore struct {
	rdb *redis.Client
}

func NewRedisStateStore(rdb *redis.Client) *RedisStateStore {
	return &RedisStateStore{rdb: rdb}
}

func oauthHash(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func oauthCodeKey(code string) string {
	return oauthCodeKeyPrefix + oauthHash(code)
}

func oauthDeviceKey(deviceCode string) string {
	return oauthDeviceKeyPrefix + oauthHash(deviceCode)
}

func oauthUserCodeKey(normalizedUserCode string) string {
	return oauthUserCodeKeyPrefix + normalizeUserCode(normalizedUserCode)
}

func (s *RedisStateStore) StoreAuthorizationCode(ctx context.Context, session *AuthorizationCodeSession, ttl time.Duration) error {
	if s == nil || s.rdb == nil {
		return errors.New("oauth state store is not configured")
	}
	if session == nil {
		return errors.New("authorization code session is nil")
	}
	payload, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshal authorization code: %w", err)
	}
	if err := s.rdb.Set(ctx, oauthCodeKey(session.Code), payload, ttl).Err(); err != nil {
		return fmt.Errorf("store authorization code: %w", err)
	}
	return nil
}

func (s *RedisStateStore) ConsumeAuthorizationCode(ctx context.Context, code string) (*AuthorizationCodeSession, error) {
	if s == nil || s.rdb == nil {
		return nil, errors.New("oauth state store is not configured")
	}
	payload, err := s.rdb.GetDel(ctx, oauthCodeKey(code)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrOAuthStateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("consume authorization code: %w", err)
	}
	var session AuthorizationCodeSession
	if err := json.Unmarshal(payload, &session); err != nil {
		return nil, fmt.Errorf("unmarshal authorization code: %w", err)
	}
	return &session, nil
}

func (s *RedisStateStore) StoreDeviceSession(ctx context.Context, session *DeviceSession, ttl time.Duration) error {
	if s == nil || s.rdb == nil {
		return errors.New("oauth state store is not configured")
	}
	if session == nil {
		return errors.New("device session is nil")
	}
	if session.NormalizedCode == "" {
		session.NormalizedCode = normalizeUserCode(session.UserCode)
	}
	payload, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshal device session: %w", err)
	}
	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, oauthDeviceKey(session.DeviceCode), payload, ttl)
	pipe.Set(ctx, oauthUserCodeKey(session.NormalizedCode), session.DeviceCode, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("store device session: %w", err)
	}
	return nil
}

func (s *RedisStateStore) GetDeviceSession(ctx context.Context, deviceCode string) (*DeviceSession, error) {
	if s == nil || s.rdb == nil {
		return nil, errors.New("oauth state store is not configured")
	}
	payload, err := s.rdb.Get(ctx, oauthDeviceKey(deviceCode)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrOAuthStateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get device session: %w", err)
	}
	var session DeviceSession
	if err := json.Unmarshal(payload, &session); err != nil {
		return nil, fmt.Errorf("unmarshal device session: %w", err)
	}
	return &session, nil
}

func (s *RedisStateStore) GetDeviceSessionByUserCode(ctx context.Context, userCode string) (*DeviceSession, error) {
	if s == nil || s.rdb == nil {
		return nil, errors.New("oauth state store is not configured")
	}
	deviceCode, err := s.rdb.Get(ctx, oauthUserCodeKey(userCode)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, ErrOAuthStateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get device code by user code: %w", err)
	}
	return s.GetDeviceSession(ctx, deviceCode)
}

func (s *RedisStateStore) UpdateDeviceSession(ctx context.Context, session *DeviceSession, ttl time.Duration) error {
	return s.StoreDeviceSession(ctx, session, ttl)
}

func (s *RedisStateStore) ConsumeDeviceSession(ctx context.Context, deviceCode string) (*DeviceSession, error) {
	if s == nil || s.rdb == nil {
		return nil, errors.New("oauth state store is not configured")
	}
	session, err := s.GetDeviceSession(ctx, deviceCode)
	if err != nil {
		return nil, err
	}
	pipe := s.rdb.Pipeline()
	pipe.Del(ctx, oauthDeviceKey(deviceCode))
	pipe.Del(ctx, oauthUserCodeKey(session.NormalizedCode))
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("consume device session: %w", err)
	}
	return session, nil
}

func (s *RedisStateStore) UserCodeExists(ctx context.Context, normalizedUserCode string) (bool, error) {
	if s == nil || s.rdb == nil {
		return false, errors.New("oauth state store is not configured")
	}
	count, err := s.rdb.Exists(ctx, oauthUserCodeKey(normalizedUserCode)).Result()
	if err != nil {
		return false, fmt.Errorf("check user code: %w", err)
	}
	return count > 0, nil
}
