package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AttributionUsagePool stores official reconciled Token once for a canonical
// user/model/usage-window/counting-commit set. It never stores Request IDs.
type AttributionUsagePool struct{ ent.Schema }

func (AttributionUsagePool) Fields() []ent.Field {
	return []ent.Field{
		field.String("canonical_pool_key").NotEmpty().Unique(),
		field.String("ledger_epoch").Default("shadow_v2"),
		// Zero is reserved for pre-migration shadow rows. Formal reads reject it.
		field.Int("relay_provider_id").Default(0),
		field.Int("user_id"),
		field.String("requested_model").NotEmpty(),
		field.Time("bucket_start_utc"),
		field.Int64("input_tokens").Default(0),
		field.Int64("output_tokens").Default(0),
		field.Int64("cache_creation_tokens").Default(0),
		field.Int64("cache_read_tokens").Default(0),
		field.Int64("total_tokens").Default(0),
		field.Int("request_count").Default(0),
		field.Int("coverage_gap_count").Default(0),
		field.Time("created_at").Immutable().Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (AttributionUsagePool) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("ledger_epoch", "relay_provider_id", "user_id", "bucket_start_utc"),
		index.Fields("ledger_epoch", "user_id", "bucket_start_utc"),
		index.Fields("ledger_epoch", "bucket_start_utc"),
	}
}
