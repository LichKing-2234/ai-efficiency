package auth

import (
	"context"
	"crypto/rand"
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
	refreshTokenPrefix           = "rt_"
	refreshSessionKeyPrefix      = "ai-efficiency:auth:refresh:"
	userRefreshSessionsKeyPrefix = "ai-efficiency:auth:user:"
	refreshFamilyKeyPrefix       = "ai-efficiency:auth:family:"
	refreshSessionRandomBytes    = 32
	refreshFamilyIDRandomBytes   = 16
)

var ErrRefreshSessionNotFound = errors.New("refresh session not found")

type RefreshSession struct {
	UserID    int       `json:"user_id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	FamilyID  string    `json:"family_id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type RefreshSessionStore interface {
	StoreRefreshSession(ctx context.Context, tokenHash string, session *RefreshSession, ttl time.Duration) error
	GetRefreshSession(ctx context.Context, tokenHash string) (*RefreshSession, error)
	DeleteRefreshSession(ctx context.Context, tokenHash string) error
	DeleteUserRefreshSessions(ctx context.Context, userID int) error
	DeleteRefreshTokenFamily(ctx context.Context, familyID string) error
}

type RedisRefreshSessionStore struct {
	rdb *redis.Client
}

func NewRedisRefreshSessionStore(rdb *redis.Client) *RedisRefreshSessionStore {
	return &RedisRefreshSessionStore{rdb: rdb}
}

func refreshSessionKey(tokenHash string) string {
	return refreshSessionKeyPrefix + tokenHash
}

func userRefreshSessionsKey(userID int) string {
	return fmt.Sprintf("%s%d:refresh", userRefreshSessionsKeyPrefix, userID)
}

func refreshFamilyKey(familyID string) string {
	return refreshFamilyKeyPrefix + familyID + ":refresh"
}

func hashRefreshToken(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}

func generateOpaqueRefreshToken() (string, error) {
	b := make([]byte, refreshSessionRandomBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate refresh token: %w", err)
	}
	return refreshTokenPrefix + hex.EncodeToString(b), nil
}

func generateTokenFamilyID() (string, error) {
	b := make([]byte, refreshFamilyIDRandomBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token family: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func (s *RedisRefreshSessionStore) StoreRefreshSession(ctx context.Context, tokenHash string, session *RefreshSession, ttl time.Duration) error {
	if s == nil || s.rdb == nil {
		return errors.New("refresh session store is not configured")
	}
	if session == nil {
		return errors.New("refresh session is nil")
	}
	payload, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshal refresh session: %w", err)
	}

	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, refreshSessionKey(tokenHash), payload, ttl)
	pipe.SAdd(ctx, userRefreshSessionsKey(session.UserID), tokenHash)
	pipe.Expire(ctx, userRefreshSessionsKey(session.UserID), ttl)
	pipe.SAdd(ctx, refreshFamilyKey(session.FamilyID), tokenHash)
	pipe.Expire(ctx, refreshFamilyKey(session.FamilyID), ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("store refresh session: %w", err)
	}
	return nil
}

func (s *RedisRefreshSessionStore) GetRefreshSession(ctx context.Context, tokenHash string) (*RefreshSession, error) {
	if s == nil || s.rdb == nil {
		return nil, errors.New("refresh session store is not configured")
	}
	payload, err := s.rdb.Get(ctx, refreshSessionKey(tokenHash)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrRefreshSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get refresh session: %w", err)
	}

	var session RefreshSession
	if err := json.Unmarshal(payload, &session); err != nil {
		return nil, fmt.Errorf("unmarshal refresh session: %w", err)
	}
	return &session, nil
}

func (s *RedisRefreshSessionStore) DeleteRefreshSession(ctx context.Context, tokenHash string) error {
	if s == nil || s.rdb == nil {
		return errors.New("refresh session store is not configured")
	}
	session, err := s.GetRefreshSession(ctx, tokenHash)
	if err != nil && !errors.Is(err, ErrRefreshSessionNotFound) {
		return err
	}

	pipe := s.rdb.Pipeline()
	pipe.Del(ctx, refreshSessionKey(tokenHash))
	if session != nil {
		pipe.SRem(ctx, userRefreshSessionsKey(session.UserID), tokenHash)
		pipe.SRem(ctx, refreshFamilyKey(session.FamilyID), tokenHash)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("delete refresh session: %w", err)
	}
	return nil
}

func (s *RedisRefreshSessionStore) DeleteUserRefreshSessions(ctx context.Context, userID int) error {
	if s == nil || s.rdb == nil {
		return errors.New("refresh session store is not configured")
	}
	indexKey := userRefreshSessionsKey(userID)
	hashes, err := s.rdb.SMembers(ctx, indexKey).Result()
	if err != nil {
		return fmt.Errorf("list user refresh sessions: %w", err)
	}
	keys := make([]string, 0, len(hashes)+1)
	for _, hash := range hashes {
		keys = append(keys, refreshSessionKey(hash))
	}
	keys = append(keys, indexKey)
	if err := s.rdb.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("delete user refresh sessions: %w", err)
	}
	return nil
}

func (s *RedisRefreshSessionStore) DeleteRefreshTokenFamily(ctx context.Context, familyID string) error {
	if s == nil || s.rdb == nil {
		return errors.New("refresh session store is not configured")
	}
	indexKey := refreshFamilyKey(familyID)
	hashes, err := s.rdb.SMembers(ctx, indexKey).Result()
	if err != nil {
		return fmt.Errorf("list refresh token family: %w", err)
	}
	keys := make([]string, 0, len(hashes)+1)
	for _, hash := range hashes {
		keys = append(keys, refreshSessionKey(hash))
	}
	keys = append(keys, indexKey)
	if err := s.rdb.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("delete refresh token family: %w", err)
	}
	return nil
}
