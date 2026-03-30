package schema

import (
	"context"
	"fmt"

	entgo "entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"

	"github.com/ai-efficiency/backend/ent"
	genhook "github.com/ai-efficiency/backend/ent/hook"
	"github.com/ai-efficiency/backend/ent/prattributionrun"
)

type PrAttributionRun struct {
	entgo.Schema
}

func (PrAttributionRun) Fields() []entgo.Field {
	return []entgo.Field{
		field.Int("pr_record_id"),
		field.Enum("trigger_mode").
			Values("manual").
			Default("manual"),
		field.String("triggered_by").
			NotEmpty(),
		field.Enum("status").
			Values("completed", "failed"),
		field.Enum("result_classification").
			Values("clear", "ambiguous").
			Optional().
			Nillable(),
		field.JSON("matched_commit_shas", []string{}).
			Default([]string{}),
		field.JSON("matched_session_ids", []string{}).
			Default([]string{}),
		field.JSON("primary_usage_summary", map[string]any{}).
			Optional(),
		field.JSON("metadata_summary", map[string]any{}).
			Optional(),
		field.JSON("validation_summary", map[string]any{}).
			Optional(),
		field.String("error_message").
			Optional().
			Nillable(),
		field.Time("created_at").
			Default(timeNow),
	}
}

func (PrAttributionRun) Edges() []entgo.Edge {
	return []entgo.Edge{
		edge.From("pr_record", PrRecord.Type).
			Ref("attribution_runs").
			Field("pr_record_id").
			Unique().
			Required(),
	}
}

func (PrAttributionRun) Hooks() []ent.Hook {
	return []ent.Hook{
		func(next ent.Mutator) ent.Mutator {
			return genhook.PrAttributionRunFunc(func(ctx context.Context, m *ent.PrAttributionRunMutation) (ent.Value, error) {
				if !m.Op().Is(ent.OpCreate | ent.OpUpdateOne | ent.OpUpdate) {
					return next.Mutate(ctx, m)
				}
				// Guardrail: Ent cannot validate per-row invariants on bulk update.
				if m.Op().Is(ent.OpUpdate) {
					if _, ok := m.Status(); ok {
						return nil, fmt.Errorf("prattributionrun: bulk update of status is not supported")
					}
					if _, ok := m.ResultClassification(); ok || m.ResultClassificationCleared() {
						return nil, fmt.Errorf("prattributionrun: bulk update of result_classification is not supported")
					}
					return next.Mutate(ctx, m)
				}

				// Determine the effective status.
				status, ok := m.Status()
				if !ok {
					switch {
					case m.Op().Is(ent.OpUpdateOne):
						old, err := m.OldStatus(ctx)
						if err != nil {
							return nil, fmt.Errorf("prattributionrun: read old status: %w", err)
						}
						status = old
					case m.Op().Is(ent.OpCreate):
						// Create should always have status set, but keep this explicit.
						return nil, fmt.Errorf("prattributionrun: status is required")
					}
				}

				if status != prattributionrun.StatusCompleted {
					return next.Mutate(ctx, m)
				}

				// Completed runs must have a classification. Failed runs may omit it.
				hasClassification := false
				switch {
				case m.ResultClassificationCleared():
					hasClassification = false
				case func() bool { _, ok := m.ResultClassification(); return ok }():
					hasClassification = true
				case m.Op().Is(ent.OpUpdateOne):
					old, err := m.OldResultClassification(ctx)
					if err != nil {
						return nil, fmt.Errorf("prattributionrun: read old result_classification: %w", err)
					}
					hasClassification = old != nil
				default:
					hasClassification = false
				}
				if !hasClassification {
					return nil, fmt.Errorf("prattributionrun: completed runs require result_classification")
				}

				return next.Mutate(ctx, m)
			})
		},
	}
}
