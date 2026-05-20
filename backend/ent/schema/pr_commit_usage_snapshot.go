package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type PRCommitUsageSnapshot struct {
	ent.Schema
}

func (PRCommitUsageSnapshot) Fields() []ent.Field {
	return []ent.Field{
		field.Int("pr_record_id"),
		field.String("commit_sha").
			NotEmpty(),
		field.Int("commit_checkpoint_id").
			Optional().
			Nillable(),
		field.Time("captured_at").
			Optional().
			Nillable(),
		field.Int64("input_tokens").
			Default(0),
		field.Int64("output_tokens").
			Default(0),
		field.Int64("cached_input_tokens").
			Default(0),
		field.Int64("reasoning_tokens").
			Default(0),
		field.Float("credit_usage").
			Default(0),
		field.Int("request_count").
			Default(0),
		field.Int("sort_order").
			Default(0),
		field.Time("created_at").
			Default(timeNow),
		field.Time("updated_at").
			Default(timeNow).
			UpdateDefault(timeNow),
	}
}

func (PRCommitUsageSnapshot) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("pr_record", PrRecord.Type).
			Ref("pr_commit_usage_snapshots").
			Field("pr_record_id").
			Unique().
			Required(),
		edge.From("commit_checkpoint", CommitCheckpoint.Type).
			Ref("pr_commit_usage_snapshots").
			Field("commit_checkpoint_id").
			Unique(),
	}
}

func (PRCommitUsageSnapshot) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("pr_record_id", "commit_sha").
			Unique(),
	}
}
