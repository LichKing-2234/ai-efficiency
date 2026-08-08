package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AttributionAllocationRevision is an append-only complete allocation vector
// for one immutable usage bucket. Most buckets have exactly one revision.
type AttributionAllocationRevision struct {
	ent.Schema
}

func (AttributionAllocationRevision) Fields() []ent.Field {
	return []ent.Field{
		field.String("revision_id").NotEmpty().Unique(),
		field.Int("usage_bucket_id"),
		field.Int("sequence"),
		field.String("reason").NotEmpty(),
		field.String("evidence_version").NotEmpty(),
		field.JSON("allocations", []map[string]any{}),
		field.Time("restated_at").Default(timeNow),
		field.Time("created_at").Immutable().Default(timeNow),
	}
}

func (AttributionAllocationRevision) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("usage_bucket", AttributionUsageBucket.Type).
			Ref("allocation_revisions").
			Field("usage_bucket_id").
			Unique().
			Required(),
	}
}

func (AttributionAllocationRevision) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("usage_bucket_id", "sequence").Unique(),
	}
}
