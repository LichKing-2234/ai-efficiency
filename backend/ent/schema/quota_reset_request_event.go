package schema

import (
	"context"
	"errors"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

var errQuotaResetRequestEventAppendOnly = errors.New("quota reset request events are append-only")

type QuotaResetRequestEvent struct {
	ent.Schema
}

func (QuotaResetRequestEvent) Fields() []ent.Field {
	return []ent.Field{
		field.Int("request_id").Immutable(),
		field.Int("actor_user_id").Optional().Nillable().Immutable(),
		field.Enum("event_type").Values(
			"created",
			"approver_resolved",
			"notification_sent",
			"notification_failed",
			"approved",
			"reset_started",
			"reset_succeeded",
			"reset_failed",
			"rejected",
			"cancelled",
			"reset_retried",
			"workflow_snapshotted",
			"node_activated",
			"node_approved",
			"node_satisfied_by_prior_approval",
			"node_skipped_no_approver",
			"admin_fallback_activated",
		).Immutable(),
		field.JSON("metadata", map[string]any{}).Optional().Immutable(),
		field.String("error_message").Default("").Immutable(),
		field.Time("created_at").Default(timeNow).Immutable(),
	}
}

func quotaResetRequestEventCreateShape(mutation ent.Mutation) bool {
	if !mutation.Op().Is(ent.OpCreate) {
		return false
	}
	if idMutation, ok := mutation.(interface{ ID() (int, bool) }); ok {
		if _, exists := idMutation.ID(); exists {
			return false
		}
	}
	for _, fieldName := range [...]string{"request_id", "event_type", "error_message", "created_at"} {
		if _, exists := mutation.Field(fieldName); !exists {
			return false
		}
	}
	return true
}

func (QuotaResetRequestEvent) Hooks() []ent.Hook {
	return []ent.Hook{
		func(next ent.Mutator) ent.Mutator {
			return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
				if !quotaResetRequestEventCreateShape(mutation) {
					return nil, errQuotaResetRequestEventAppendOnly
				}
				return next.Mutate(ctx, mutation)
			})
		},
	}
}

func (QuotaResetRequestEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("request_id", "created_at"),
		index.Fields("event_type", "created_at"),
		index.Fields("actor_user_id", "created_at"),
	}
}
