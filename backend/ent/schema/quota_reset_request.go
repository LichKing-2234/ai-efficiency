package schema

import (
	"context"
	"errors"

	"entgo.io/ent"
	entsql "entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type QuotaResetRequest struct {
	ent.Schema
}

var errQuotaResetRequestJSONSnapshotClear = errors.New("quotaresetrequest: JSON creation snapshots cannot be cleared")

func (QuotaResetRequest) Fields() []ent.Field {
	return []ent.Field{
		field.Int("requester_user_id").Immutable(),
		field.Int64("requester_relay_user_id").Immutable(),
		field.Int("provider_id").Immutable(),
		field.String("group_id").NotEmpty().Immutable(),
		field.String("group_name").Default("").Immutable(),
		field.String("group_platform").Default("").Immutable(),
		field.String("reason").NotEmpty().Immutable(),
		field.Int("workflow_version").Default(1).Immutable(),
		field.Int("current_node_id").Optional().Nillable(),
		field.Int("workflow_completed_by_decision_id").Optional().Nillable(),
		field.String("requester_display_name_snapshot").Default("").Immutable(),
		field.String("requester_email_snapshot").Default("").Immutable(),
		validatedQuotaResetJSONField(
			field.JSON("requester_department_paths", []string{}).Default(newQuotaResetSlice[string]).Optional().Immutable(),
			validateQuotaResetSlice[string],
		),
		validatedQuotaResetJSONField(
			field.JSON("requester_notification_ids", map[string]string{}).Default(newQuotaResetMap[string, string]).Optional().Immutable(),
			validateQuotaResetMap[string, string],
		),
		field.Enum("status").Values(
			"pending",
			"approved_resetting",
			"approved_reset_succeeded",
			"approved_reset_failed",
			"rejected",
			"cancelled",
		).Default("pending"),
		validatedQuotaResetJSONField(
			field.JSON("resolved_approver_user_ids", []int{}).Default(newQuotaResetSlice[int]).Optional().Immutable(),
			validateQuotaResetSlice[int],
		),
		validatedQuotaResetJSONField(
			field.JSON("matched_department_paths", []map[string]any{}).Default(newQuotaResetSlice[map[string]any]).Optional().Immutable(),
			validateQuotaResetMapSlice,
		),
		field.Int("approved_by_user_id").Optional().Nillable(),
		field.Int("rejected_by_user_id").Optional().Nillable(),
		field.String("decision_reason").Default(""),
		field.Time("decided_at").Optional().Nillable(),
		field.String("reset_error").Default(""),
		field.Time("reset_started_at").Optional().Nillable(),
		field.Time("reset_completed_at").Optional().Nillable(),
		field.Time("created_at").Default(timeNow).Immutable(),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (QuotaResetRequest) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("requester_user_id", "created_at"),
		index.Fields("status", "created_at"),
		index.Fields("workflow_version", "status", "created_at"),
		index.Fields("current_node_id"),
		index.Fields("provider_id", "group_id", "status"),
		index.Fields("requester_user_id", "provider_id", "group_id").
			Unique().
			Annotations(entsql.IndexWhere("status IN ('pending', 'approved_resetting', 'approved_reset_failed')")),
		index.Fields("updated_at"),
	}
}

func (QuotaResetRequest) Hooks() []ent.Hook {
	return []ent.Hook{
		func(next ent.Mutator) ent.Mutator {
			return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
				for _, fieldName := range [...]string{
					"requester_department_paths",
					"requester_notification_ids",
					"resolved_approver_user_ids",
					"matched_department_paths",
				} {
					if mutation.FieldCleared(fieldName) {
						return nil, errQuotaResetRequestJSONSnapshotClear
					}
				}
				return next.Mutate(ctx, mutation)
			})
		},
	}
}
