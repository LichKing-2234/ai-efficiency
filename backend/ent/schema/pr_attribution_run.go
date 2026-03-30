package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

type PrAttributionRun struct {
	ent.Schema
}

func (PrAttributionRun) Fields() []ent.Field {
	return []ent.Field{
		field.Int("pr_record_id"),
		field.Enum("trigger_mode").
			Values("manual").
			Default("manual"),
		field.String("triggered_by").
			NotEmpty(),
		field.Enum("status").
			Values("completed", "failed"),
		field.Enum("result_classification").
			Values("clear", "ambiguous").
			Optional().
			Nillable(),
		field.JSON("matched_commit_shas", []string{}).
			Default([]string{}),
		field.JSON("matched_session_ids", []string{}).
			Default([]string{}),
		field.JSON("primary_usage_summary", map[string]any{}).
			Optional(),
		field.JSON("metadata_summary", map[string]any{}).
			Optional(),
		field.JSON("validation_summary", map[string]any{}).
			Optional(),
		field.String("error_message").
			Optional().
			Nillable(),
		field.Time("created_at").
			Default(timeNow),
	}
}

func (PrAttributionRun) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("pr_record", PrRecord.Type).
			Ref("attribution_runs").
			Field("pr_record_id").
			Unique().
			Required(),
	}
}
