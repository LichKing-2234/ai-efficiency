package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type TeamUsageRateMultiplierAudit struct {
	ent.Schema
}

func (TeamUsageRateMultiplierAudit) Fields() []ent.Field {
	return []ent.Field{
		field.Int("actor_user_id"),
		field.Int("target_user_id").Optional().Nillable(),
		field.Int("provider_id").Optional().Nillable(),
		field.Int64("relay_user_id").Optional().Nillable(),
		field.String("group_id").Default(""),
		field.String("group_name").Default(""),
		field.Enum("action").
			Values("set_rate_multiplier", "reset_rate_multiplier"),
		field.Enum("status").
			Values("running", "succeeded", "failed", "partial_failed", "rejected"),
		field.Float("old_multiplier").Optional().Nillable(),
		field.Enum("old_multiplier_source").
			Values("user", "group", "system", "unknown").
			Default("unknown"),
		field.Float("new_multiplier").Optional().Nillable(),
		field.Enum("new_multiplier_source").
			Values("user", "group", "system", "unknown").
			Default("unknown"),
		field.Bool("changed").Default(false),
		field.JSON("old_effective_limits", map[string]any{}).Optional(),
		field.JSON("new_effective_limits", map[string]any{}).Optional(),
		field.JSON("scope_evidence", map[string]any{}).Optional(),
		field.Enum("rejection_reason").
			Values(
				"not_representative",
				"self_edit_forbidden",
				"not_upper_level_representative",
				"out_of_scope",
				"no_relay_mapping",
				"inactive_subscription",
				"policy_denied",
				"provider_unsupported",
			).
			Optional().
			Nillable(),
		field.JSON("request_metadata", map[string]any{}).Optional(),
		field.String("reason").Default(""),
		field.String("error_message").Default(""),
		field.Time("created_at").Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (TeamUsageRateMultiplierAudit) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("actor_user_id", "created_at"),
		index.Fields("target_user_id", "created_at"),
		index.Fields("provider_id", "group_id", "created_at"),
		index.Fields("status", "created_at"),
	}
}
