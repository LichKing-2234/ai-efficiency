package directorysync

import (
	"context"
	"fmt"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorysource"
	"github.com/ai-efficiency/backend/ent/directorysyncrun"
)

func CurrentSourceID(ctx context.Context, client *ent.Client) (int, bool, error) {
	if client == nil {
		return 0, false, fmt.Errorf("directory source resolver is not configured")
	}
	sources, err := client.DirectorySource.Query().
		Where(
			directorysource.DeletedEQ(false),
			directorysource.ScopeEQ(directorysource.ScopeFullCompany),
			directorysource.LastSuccessfulRunIDNotNil(),
		).
		All(ctx)
	if err != nil {
		return 0, false, fmt.Errorf("list directory sources with successful sync: %w", err)
	}
	if len(sources) == 0 {
		return 0, false, nil
	}

	sourceByRunID := make(map[int]int, len(sources))
	runIDs := make([]int, 0, len(sources))
	for _, source := range sources {
		if source.LastSuccessfulRunID == nil {
			continue
		}
		runID := *source.LastSuccessfulRunID
		sourceByRunID[runID] = source.ID
		runIDs = append(runIDs, runID)
	}
	if len(runIDs) == 0 {
		return 0, false, nil
	}

	run, err := client.DirectorySyncRun.Query().
		Where(
			directorysyncrun.IDIn(runIDs...),
			directorysyncrun.ModeEQ(directorysyncrun.ModeApply),
			directorysyncrun.StatusIn(directorysyncrun.StatusCompleted, directorysyncrun.StatusCompletedWithWarnings),
			directorysyncrun.CompletedAtNotNil(),
		).
		Order(ent.Desc(directorysyncrun.FieldCompletedAt), ent.Desc(directorysyncrun.FieldID)).
		First(ctx)
	if ent.IsNotFound(err) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("resolve latest successful directory sync run: %w", err)
	}
	sourceID, ok := sourceByRunID[run.ID]
	if !ok {
		return 0, false, nil
	}
	return sourceID, true, nil
}
