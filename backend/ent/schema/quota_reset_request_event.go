package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type QuotaResetRequestEvent struct {
	ent.Schema
}

func (QuotaResetRequestEvent) Fields() []ent.Field {
	return []ent.Field{
		field.Int("request_id"),
		field.Int("actor_user_id").Optional().Nillable(),
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
		),
		field.JSON("metadata", map[string]any{}).Optional(),
		field.String("error_message").Default(""),
		field.Time("created_at").Default(timeNow),
	}
}

func (QuotaResetRequestEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("request_id", "created_at"),
		index.Fields("event_type", "created_at"),
		index.Fields("actor_user_id", "created_at"),
	}
}
