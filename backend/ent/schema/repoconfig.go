package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// RepoConfig holds the schema definition for the RepoConfig entity.
type RepoConfig struct {
	ent.Schema
}

// Fields of the RepoConfig.
func (RepoConfig) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			NotEmpty(),
		field.String("full_name").
			NotEmpty(),
		field.String("clone_url").
			NotEmpty(),
		field.String("default_branch").
			Default("main"),
		field.String("webhook_id").
			Optional().
			Nillable(),
		field.String("webhook_secret").
			Optional().
			Nillable().
			Sensitive(),
		field.Int("ai_score").
			Optional().
			Default(0),
		field.Time("last_scan_at").
			Optional().
			Nillable(),
		field.String("group_id").
			Optional().
			Nillable(),
		field.String("relay_provider_name").
			Optional().
			Nillable(),
		field.String("relay_group_id").
			Optional().
			Nillable(),
		field.Enum("status").
			Values("active", "webhook_failed", "inactive").
			Default("active"),
		field.Time("created_at").
			Immutable().
			Default(timeNow),
		field.Time("updated_at").
			Default(timeNow).
			UpdateDefault(timeNow),
		field.JSON("scan_prompt_override", map[string]string{}).
			Optional(),
	}
}

// Edges of the RepoConfig.
func (RepoConfig) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("scm_provider", ScmProvider.Type).
			Ref("repo_configs").
			Unique().
			Required(),
		edge.To("sessions", Session.Type),
		edge.To("commit_checkpoints", CommitCheckpoint.Type),
		edge.To("commit_rewrites", CommitRewrite.Type),
		edge.To("webhook_dead_letters", WebhookDeadLetter.Type),
		edge.To("ai_scan_results", AiScanResult.Type),
		edge.To("pr_records", PrRecord.Type),
		edge.To("efficiency_metrics", EfficiencyMetric.Type),
	}
}

// Indexes of the RepoConfig.
func (RepoConfig) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("full_name").
			Edges("scm_provider").
			Unique(),
	}
}
