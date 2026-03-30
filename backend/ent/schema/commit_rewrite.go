package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"

	"github.com/google/uuid"
)

type CommitRewrite struct {
	ent.Schema
}

func (CommitRewrite) Fields() []ent.Field {
	return []ent.Field{
		field.String("event_id").
			Unique(),
		field.UUID("session_id", uuid.UUID{}).
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
		edge.From("session", Session.Type).
			Ref("commit_rewrites").
			Field("session_id").
			Unique(),
	}
}

