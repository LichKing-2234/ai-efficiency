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
		field.Enum("status").Values("pending", "reconciled", "owner_mismatch", "ambiguous", "provider_unavailable", "source_expired").Default("pending"),
		field.Time("expires_at"),
		field.Time("created_at").Immutable().Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (AttributionRequestClaim) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("relay_provider_id", "request_id").Unique(),
		index.Fields("claim_group_id", "created_at"),
		index.Fields("status", "expires_at"),
	}
}
