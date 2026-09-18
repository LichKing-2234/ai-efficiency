package directorysync

import (
	"context"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/directoryfacts"
)

type CurrentDirectorySnapshot = directoryfacts.Snapshot

func CurrentSourceID(ctx context.Context, client *ent.Client) (int, bool, error) {
	snapshot, ok, err := CurrentSnapshot(ctx, client)
	if err != nil || !ok {
		return 0, ok, err
	}
	return snapshot.SourceID, true, nil
}

func CurrentSnapshot(ctx context.Context, client *ent.Client) (CurrentDirectorySnapshot, bool, error) {
	view, ok, err := directoryfacts.New(client).Current(ctx)
	if err != nil {
		return CurrentDirectorySnapshot{}, false, err
	}
	if !ok {
		return CurrentDirectorySnapshot{}, false, nil
	}
	return view.Snapshot(), true, nil
}
