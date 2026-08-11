package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AttributionRequestClaim is hot reconciliation identity. The provider is
// part of identity because upstream Request IDs are provider-scoped.
type AttributionRequestClaim struct{ ent.Schema }

func (AttributionRequestClaim) Fields() []ent.Field {
	return []ent.Field{
		field.Int("claim_group_id"),
		field.Int("relay_provider_id"),
		field.String("request_id").NotEmpty(),
		field.String("canonical_digest").NotEmpty(),
		field.Enum("status").Values("pending", "reconciled", "owner_mismatch", "ambiguous", "provider_unavailable", "invalid_usage", "source_expired").Default("pending"),
		field.Int("attempt_count").Default(0),
		field.Time("next_attempt_at").Default(timeNow),
		field.String("lease_token").Optional(),
		field.Time("lease_expires_at").Optional().Nillable(),
		field.String("last_error_code").Optional(),
		field.String("requested_model").Optional(),
		field.Time("usage_at").Optional().Nillable(),
		field.Int64("input_tokens").Default(0),
		field.Int64("output_tokens").Default(0),
		field.Int64("cache_creation_tokens").Default(0),
		field.Int64("cache_read_tokens").Default(0),
		field.Int64("total_tokens").Default(0),
		field.Time("reconciled_at").Optional().Nillable(),
		field.Int("materialized_pool_id").Optional().Nillable(),
		field.Time("materialized_at").Optional().Nillable(),
		field.Time("expires_at"),
		field.Time("created_at").Immutable().Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (AttributionRequestClaim) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("relay_provider_id", "request_id").Unique(),
		index.Fields("claim_group_id", "created_at"),
		index.Fields("status", "next_attempt_at", "lease_expires_at"),
		index.Fields("status", "expires_at"),
		index.Fields("materialized_pool_id"),
	}
}
