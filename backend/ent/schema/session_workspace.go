package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"

	"github.com/google/uuid"
)

type SessionWorkspace struct {
	ent.Schema
}

func (SessionWorkspace) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("session_id", uuid.UUID{}),
		field.String("workspace_id").
			NotEmpty(),
		field.String("workspace_root").
			NotEmpty(),
		field.String("git_dir").
			NotEmpty(),
		field.String("git_common_dir").
			NotEmpty(),
		field.Time("first_seen_at").
			Default(timeNow),
		field.Time("last_seen_at").
			Default(timeNow).
			UpdateDefault(timeNow),
		field.Enum("binding_source").
			Values("marker", "env_bootstrap", "manual"),
	}
}

func (SessionWorkspace) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("session", Session.Type).
			Ref("session_workspaces").
			Field("session_id").
			Unique().
			Required(),
	}
}

