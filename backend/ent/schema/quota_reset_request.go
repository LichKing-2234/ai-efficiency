package schema

import (
	"entgo.io/ent"
	entsql "entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type QuotaResetRequest struct {
	ent.Schema
}

func (QuotaResetRequest) Fields() []ent.Field {
	return []ent.Field{
		field.Int("requester_user_id"),
		field.Int64("requester_relay_user_id"),
		field.Int("provider_id"),
		field.String("group_id").NotEmpty(),
		field.String("group_name").Default(""),
		field.String("group_platform").Default(""),
		field.String("reason").NotEmpty(),
		field.Int("workflow_version").Default(1),
		field.JSON("workflow", map[string]any{}).Optional(),
		field.Int("workflow_revision").Default(0),
		field.Enum("status").Values(
			"pending",
			"workflow_pending",
			"approved_resetting",
			"approved_reset_succeeded",
			"approved_reset_failed",
			"rejected",
			"cancelled",
		).Default("pending"),
		field.JSON("resolved_approver_user_ids", []int{}).Optional(),
		field.JSON("matched_department_paths", []map[string]any{}).Optional(),
		field.Int("approved_by_user_id").Optional().Nillable(),
		field.Int("rejected_by_user_id").Optional().Nillable(),
		field.String("decision_reason").Default(""),
		field.Time("decided_at").Optional().Nillable(),
		field.String("reset_error").Default(""),
		field.Time("reset_started_at").Optional().Nillable(),
		field.Time("reset_completed_at").Optional().Nillable(),
		field.Time("created_at").Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (QuotaResetRequest) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("requester_user_id", "created_at"),
		index.Fields("status", "created_at"),
		index.Fields("provider_id", "group_id", "status"),
		index.Fields("resolved_approver_user_ids").
			Annotations(entsql.IndexType("GIN"), entsql.OpClass("jsonb_path_ops")),
		index.Fields("workflow").
			Annotations(entsql.IndexType("GIN"), entsql.OpClass("jsonb_path_ops")),
		// Keep the original index stable so older binaries do not rewrite it
		// during a rolling rollback. The second index spans both workflow versions.
		index.Fields("requester_user_id", "provider_id", "group_id").
			Unique().
			Annotations(entsql.IndexWhere("status IN ('pending', 'approved_resetting', 'approved_reset_failed')")),
		index.Fields("requester_user_id", "provider_id", "group_id").
			Unique().
			StorageKey("quotaresetrequest_workflow_active_unique").
			Annotations(entsql.IndexWhere("status IN ('pending', 'workflow_pending', 'approved_resetting', 'approved_reset_failed')")),
		index.Fields("updated_at"),
	}
}
