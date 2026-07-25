package teamusage

import (
	"strings"

	"github.com/ai-efficiency/backend/ent"
)

func newServiceWithSnapshotCacheForTest(client *ent.Client, scopeResolver ScopeResolver, providerResolver ProviderResolver, locker AdvisoryLocker, snapshotCache *SnapshotCache, cursorSecrets ...string) *Service {
	var cursorSecret string
	if len(cursorSecrets) > 0 {
		cursorSecret = strings.TrimSpace(cursorSecrets[0])
	}
	return newService(client, scopeResolver, providerResolver, locker, snapshotCache, nil, cursorSecret)
}

func newUncachedServiceForTest(client *ent.Client, scopeResolver ScopeResolver, providerResolver ProviderResolver, locker AdvisoryLocker) *Service {
	return newService(client, scopeResolver, providerResolver, locker, nil, nil, "")
}
