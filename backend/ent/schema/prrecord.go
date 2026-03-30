package schema

import (
	"context"
	"fmt"

	entgo "entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/ai-efficiency/backend/ent"
	genhook "github.com/ai-efficiency/backend/ent/hook"
)

// PrRecord holds the schema definition for the PrRecord entity.
type PrRecord struct {
	entgo.Schema
}

// Fields of the PrRecord.
func (PrRecord) Fields() []entgo.Field {
	return []entgo.Field{
		field.Int("scm_pr_id"),
		field.String("scm_pr_url").
			Optional(),
		field.String("author").
			Optional(),
		field.String("title").
			Optional(),
		field.String("source_branch").
			Optional(),
		field.String("target_branch").
			Optional(),
		field.Enum("status").
			Values("open", "merged", "closed").
			Default("open"),
		field.JSON("labels", []string{}).
			Optional(),
		field.Int("lines_added").
			Default(0),
		field.Int("lines_deleted").
			Default(0),
		field.JSON("changed_files", []string{}).
			Optional(),
		field.JSON("session_ids", []string{}).
			Optional(),
		field.Float("token_cost").
			Default(0),
		field.Float("ai_ratio").
			Default(0),
		field.Enum("ai_label").
			Values("ai_via_sub2api", "no_ai_detected", "pending").
			Default("pending"),
		field.Enum("attribution_status").
			Values("not_run", "clear", "ambiguous", "failed").
			Default("not_run"),
		field.Enum("attribution_confidence").
			Values("high", "medium", "low").
			Optional().
			Nillable(),
		field.Int64("primary_token_count").
			Default(0),
		field.Float("primary_token_cost").
			Default(0),
		field.JSON("metadata_summary", map[string]any{}).
			Optional(),
		field.Time("last_attributed_at").
			Optional().
			Nillable(),
		field.Int("last_attribution_run_id").
			Optional().
			Nillable(),
		field.Time("merged_at").
			Optional().
			Nillable(),
		field.Float("cycle_time_hours").
			Default(0),
		field.Time("created_at").
			Default(timeNow),
		field.Time("updated_at").
			Default(timeNow).
			UpdateDefault(timeNow),
	}
}

// Edges of the PrRecord.
func (PrRecord) Edges() []entgo.Edge {
	return []entgo.Edge{
		edge.From("repo_config", RepoConfig.Type).
			Ref("pr_records").
			Unique().
			Required(),
		edge.To("attribution_runs", PrAttributionRun.Type),
		edge.To("last_attribution_run", PrAttributionRun.Type).
			Unique().
			Field("last_attribution_run_id"),
	}
}

// Indexes of the PrRecord.
func (PrRecord) Indexes() []entgo.Index {
	return []entgo.Index{
		index.Fields("scm_pr_id").
			Edges("repo_config").
			Unique(),
	}
}

func (PrRecord) Hooks() []ent.Hook {
	return []ent.Hook{
		func(next ent.Mutator) ent.Mutator {
			return genhook.PrRecordFunc(func(ctx context.Context, m *ent.PrRecordMutation) (ent.Value, error) {
				if !m.Op().Is(ent.OpUpdateOne | ent.OpUpdate | ent.OpCreate) {
					return next.Mutate(ctx, m)
				}
				if m.LastAttributionRunIDCleared() {
					return next.Mutate(ctx, m)
				}
				runID, ok := m.LastAttributionRunID()
				if !ok {
					return next.Mutate(ctx, m)
				}

				// We need the PR record ID to validate cross-record integrity. Disallow setting this
				// field on create/bulk updates, because Ent can't validate it without a per-row ID.
				switch {
				case m.Op().Is(ent.OpUpdateOne):
					prID, exists := m.ID()
					if !exists {
						return nil, fmt.Errorf("prrecord: missing ID for last_attribution_run_id validation")
					}
					run, err := m.Client().PrAttributionRun.Get(ctx, runID)
					if err != nil {
						return nil, fmt.Errorf("prrecord: last_attribution_run_id %d: %w", runID, err)
					}
					if run.PrRecordID != prID {
						return nil, fmt.Errorf("prrecord: last_attribution_run_id %d belongs to pr_record_id %d (expected %d)", runID, run.PrRecordID, prID)
					}
				default:
					return nil, fmt.Errorf("prrecord: setting last_attribution_run_id is only supported on UpdateOne")
				}

				return next.Mutate(ctx, m)
			})
		},
	}
}
