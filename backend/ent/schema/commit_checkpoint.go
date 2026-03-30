package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"

	"github.com/google/uuid"
)

type CommitCheckpoint struct {
	ent.Schema
}

func (CommitCheckpoint) Fields() []ent.Field {
	return []ent.Field{
		field.String("event_id").
			Unique(),
		field.UUID("session_id", uuid.UUID{}).
			Optional().
			Nillable(),
		field.String("workspace_id").
			NotEmpty(),
		field.Int("repo_config_id"),
		field.String("commit_sha").
			NotEmpty(),
		field.JSON("parent_shas", []string{}),
		field.String("branch_snapshot").
			Optional().
			Nillable(),
		field.String("head_snapshot").
			Optional().
			Nillable(),
		field.Enum("binding_source").
			Values("marker", "env_bootstrap", "manual", "unbound"),
		field.JSON("agent_snapshot", map[string]any{}).
			Optional(),
		field.Time("captured_at").
			Default(timeNow),
	}
}

func (CommitCheckpoint) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("session", Session.Type).
			Ref("commit_checkpoints").
			Field("session_id").
			Unique(),
	}
}

