package teamusage

import (
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestNewServiceRequiresProductionCacheAndCursorSecret(t *testing.T) {
	client := testdb.Open(t)
	scopeResolver := fakeScopeResolver{}
	providerResolver := fakeProviderResolver{}

	service, err := NewService(client, scopeResolver, providerResolver, nil, ServiceOptions{})
	if err == nil || !strings.Contains(err.Error(), "snapshot cache") {
		t.Fatalf("NewService() service=%v error=%v, want snapshot cache error", service, err)
	}

	service, err = NewService(client, scopeResolver, providerResolver, nil, ServiceOptions{
		SnapshotCache: &SnapshotCache{},
	})
	if err == nil || !strings.Contains(err.Error(), "origin cache") {
		t.Fatalf("NewService() service=%v error=%v, want origin cache error", service, err)
	}

	service, err = NewService(client, scopeResolver, providerResolver, nil, ServiceOptions{
		SnapshotCache: &SnapshotCache{},
		OriginCache:   &OriginCache{},
	})
	if err == nil || !strings.Contains(err.Error(), "cursor secret") {
		t.Fatalf("NewService() service=%v error=%v, want cursor secret error", service, err)
	}

	service, err = NewService(client, scopeResolver, providerResolver, nil, ServiceOptions{
		SnapshotCache: &SnapshotCache{},
		OriginCache:   &OriginCache{},
		CursorSecret:  "test-cursor-secret",
	})
	if err != nil || service == nil || service.snapshotCache == nil || service.originCache == nil || service.memberCursorCodec == nil || service.organizationCursorCodec == nil {
		t.Fatalf("NewService() service=%v error=%v, want explicit production dependencies", service, err)
	}

	uncached := newUncachedServiceForTest(client, scopeResolver, providerResolver, nil)
	if uncached == nil || uncached.snapshotCache != nil || uncached.originCache != nil || uncached.memberCursorCodec != nil || uncached.organizationCursorCodec != nil {
		t.Fatalf("newUncachedServiceForTest() = %#v, want explicit uncached service", uncached)
	}
}
