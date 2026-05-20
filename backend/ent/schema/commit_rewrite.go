package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type CommitRewrite struct {
	ent.Schema
}

func (CommitRewrite) Fields() []ent.Field {
	return []ent.Field{
		field.String("event_id").
			Unique(),
		field.Int("user_id").
			Optional().
			Nillable(),
		field.String("workspace_id").
			NotEmpty(),
		field.Int("repo_config_id"),
		field.Enum("rewrite_type").
			Values("amend", "rebase", "squash", "unknown").
			Default("unknown"),
		field.String("old_commit_sha").
			NotEmpty(),
		field.String("new_commit_sha").
			NotEmpty(),
		field.Enum("binding_source").
			Values("marker", "env_bootstrap", "manual", "unbound"),
		field.Time("captured_at").
			Default(timeNow),
	}
}

func (CommitRewrite) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("commit_rewrites").
			Field("user_id").
			Unique(),
		edge.From("repo_config", RepoConfig.Type).
			Ref("commit_rewrites").
			Field("repo_config_id").
			Unique().
			Required(),
	}
}

func (CommitRewrite) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("repo_config_id", "old_commit_sha", "new_commit_sha", "rewrite_type").
			Unique(),
	}
}
