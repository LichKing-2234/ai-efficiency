package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AttributionClaimGroup stores short-lived v2 commit proof and bounded local
// Token aggregates. It never stores raw local source payloads.
type AttributionClaimGroup struct{ ent.Schema }

func (AttributionClaimGroup) Fields() []ent.Field {
	return []ent.Field{
		field.String("group_id").NotEmpty().Unique(),
		field.Int("installation_id"),
		field.Int("user_id"),
		field.Int("relay_provider_id"),
		field.Int("schema_version"),
		field.String("ledger_epoch").Default("shadow_v2"),
		field.String("thread_id").Optional(),
		field.String("turn_id").Optional(),
		field.String("evidence_digest").Optional(),
		field.String("calibration_digest").Optional(),
		field.Int64("calibration_input_tokens").Default(0),
		field.Int64("calibration_output_tokens").Default(0),
		field.Int64("calibration_cache_creation_tokens").Default(0),
		field.Int64("calibration_cache_read_tokens").Default(0),
		field.Int64("calibration_total_tokens").Default(0),
		field.JSON("local_usage", []map[string]any{}).Optional(),
		field.JSON("commit_allocations", []map[string]any{}),
		field.Int("request_count"),
		field.Time("finalized_at").Optional().Nillable(),
		field.Int("finalization_attempt_count").Default(0),
		field.Time("finalization_next_attempt_at").Default(timeNow),
		field.String("finalization_last_error_code").Optional(),
		field.Time("expires_at"),
		field.Time("created_at").Immutable().Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (AttributionClaimGroup) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "ledger_epoch", "expires_at"),
		index.Fields("installation_id", "created_at"),
	}
}
