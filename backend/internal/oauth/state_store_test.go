package oauth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestOAuthStateStore(t *testing.T) OAuthStateStore {
	t.Helper()
	s := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return NewRedisStateStore(rdb)
}

func TestRedisStateStoreConsumesAuthorizationCodeOnce(t *testing.T) {
	ctx := context.Background()
	store := newTestOAuthStateStore(t)
	session := &AuthorizationCodeSession{
		Code:          "code-1",
		ClientID:      "ae-cli",
		RedirectURI:   "http://localhost:18234/callback",
		CodeChallenge: "challenge",
		UserID:        7,
		Username:      "alice",
		Role:          "user",
		State:         "state-1",
		CreatedAt:     time.Now().UTC(),
	}

	if err := store.StoreAuthorizationCode(ctx, session, time.Minute); err != nil {
		t.Fatalf("StoreAuthorizationCode: %v", err)
	}
	got, err := store.ConsumeAuthorizationCode(ctx, "code-1")
	if err != nil {
		t.Fatalf("ConsumeAuthorizationCode: %v", err)
	}
	if got.UserID != 7 || got.Username != "alice" {
		t.Fatalf("session mismatch: %#v", got)
	}
	if _, err := store.ConsumeAuthorizationCode(ctx, "code-1"); !errors.Is(err, ErrOAuthStateNotFound) {
		t.Fatalf("second consume err=%v, want ErrOAuthStateNotFound", err)
	}
}

func TestRedisStateStoreDeviceFlowSharedAcrossInstances(t *testing.T) {
	ctx := context.Background()
	store := newTestOAuthStateStore(t)
	device := &DeviceSession{
		DeviceCode:      "device-1",
		UserCode:        "ABCD-EFGH",
		NormalizedCode:  "ABCDEFGH",
		ClientID:        "ae-cli",
		Status:          deviceStatusPending,
		CreatedAt:       time.Now().UTC(),
		ExpiresAt:       time.Now().Add(15 * time.Minute).UTC(),
		PollIntervalSec: 5,
	}

	if err := store.StoreDeviceSession(ctx, device, 15*time.Minute); err != nil {
		t.Fatalf("StoreDeviceSession: %v", err)
	}
	byUserCode, err := store.GetDeviceSessionByUserCode(ctx, "abcd-efgh")
	if err != nil {
		t.Fatalf("GetDeviceSessionByUserCode: %v", err)
	}
	byUserCode.Status = deviceStatusApproved
	byUserCode.UserID = 11
	byUserCode.Username = "bob"
	byUserCode.Role = "user"
	if err := store.UpdateDeviceSession(ctx, byUserCode, 15*time.Minute); err != nil {
		t.Fatalf("UpdateDeviceSession: %v", err)
	}

	byDevice, err := store.GetDeviceSession(ctx, "device-1")
	if err != nil {
		t.Fatalf("GetDeviceSession: %v", err)
	}
	if byDevice.Status != deviceStatusApproved || byDevice.UserID != 11 {
		t.Fatalf("device state mismatch: %#v", byDevice)
	}

	consumed, err := store.ConsumeDeviceSession(ctx, "device-1")
	if err != nil {
		t.Fatalf("ConsumeDeviceSession: %v", err)
	}
	if consumed.Username != "bob" {
		t.Fatalf("consumed username=%q", consumed.Username)
	}
	if _, err := store.GetDeviceSessionByUserCode(ctx, "ABCD-EFGH"); !errors.Is(err, ErrOAuthStateNotFound) {
		t.Fatalf("user code after consume err=%v, want ErrOAuthStateNotFound", err)
	}
}
