package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// EfficiencyMetric holds the schema definition for the EfficiencyMetric entity.
type EfficiencyMetric struct {
	ent.Schema
}

// Fields of the EfficiencyMetric.
func (EfficiencyMetric) Fields() []ent.Field {
	return []ent.Field{
		field.Int("user_id").
			Optional().
			Nillable(),
		field.Enum("period_type").
			Values("daily", "weekly", "monthly"),
		field.Time("period_start"),
		field.Int("total_prs").
			Default(0),
		field.Int("ai_prs").
			Default(0),
		field.Int("human_prs").
			Default(0),
		field.Float("avg_cycle_time_hours").
			Default(0),
		field.Int("total_tokens").
			Default(0),
		field.Float("total_token_cost").
			Default(0),
		field.Float("ai_vs_human_ratio").
			Default(0),
		field.Time("created_at").
			Immutable().
			Default(timeNow),
	}
}

// Edges of the EfficiencyMetric.
func (EfficiencyMetric) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("repo_config", RepoConfig.Type).
			Ref("efficiency_metrics").
			Unique().
			Required(),
	}
}

// Indexes of the EfficiencyMetric.
func (EfficiencyMetric) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("period_type", "period_start").
			Edges("repo_config").
			Unique(),
	}
}
