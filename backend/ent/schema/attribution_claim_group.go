package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AttributionClaimGroup stores short-lived v2 commit proof in the shadow
// epoch. It deliberately stores digests, never local source payloads.
type AttributionClaimGroup struct{ ent.Schema }

func (AttributionClaimGroup) Fields() []ent.Field {
	return []ent.Field{
		field.String("group_id").NotEmpty().Unique(),
		field.Int("installation_id"),
		field.Int("user_id"),
		field.Int("relay_provider_id"),
		field.Int("repo_config_id"),
		field.Int("checkpoint_id"),
		field.Int("schema_version"),
		field.String("ledger_epoch").Default("shadow_v2"),
		field.String("thread_id").NotEmpty(),
		field.String("turn_id").NotEmpty(),
		field.String("evidence_digest").NotEmpty(),
		field.String("calibration_digest").Optional(),
		field.Int("request_count"),
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
