package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PrRecord holds the schema definition for the PrRecord entity.
type PrRecord struct {
	ent.Schema
}

// Fields of the PrRecord.
func (PrRecord) Fields() []ent.Field {
	return []ent.Field{
		field.Int("scm_pr_id"),
		field.String("scm_pr_url").
			Optional(),
		field.String("author").
			Optional(),
		field.String("title").
			Optional(),
		field.String("source_branch").
			Optional(),
		field.String("target_branch").
			Optional(),
		field.Enum("status").
			Values("open", "merged", "closed").
			Default("open"),
		field.JSON("labels", []string{}).
			Optional(),
		field.Int("lines_added").
			Default(0),
		field.Int("lines_deleted").
			Default(0),
		field.JSON("changed_files", []string{}).
			Optional(),
		field.JSON("session_ids", []string{}).
			Optional(),
		field.Float("token_cost").
			Default(0),
		field.Float("ai_ratio").
			Default(0),
		field.Enum("ai_label").
			Values("ai_via_sub2api", "no_ai_detected", "pending").
			Default("pending"),
		field.Time("merged_at").
			Optional().
			Nillable(),
		field.Float("cycle_time_hours").
			Default(0),
		field.Time("created_at").
			Default(timeNow),
		field.Time("updated_at").
			Default(timeNow).
			UpdateDefault(timeNow),
	}
}

// Edges of the PrRecord.
func (PrRecord) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("repo_config", RepoConfig.Type).
			Ref("pr_records").
			Unique().
			Required(),
	}
}

// Indexes of the PrRecord.
func (PrRecord) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("scm_pr_id").
			Edges("repo_config").
			Unique(),
	}
}
