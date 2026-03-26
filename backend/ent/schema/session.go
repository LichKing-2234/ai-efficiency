package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"

	"github.com/google/uuid"
)

type Session struct {
	ent.Schema
}

func (Session) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("branch").
			NotEmpty(),
		field.Int("relay_user_id").
			Optional().
			Nillable(),
		field.Int("relay_api_key_id").
			Optional().
			Nillable(),
		field.String("provider_name").
			Optional().
			Nillable(),
		field.JSON("tool_configs", []map[string]interface{}{}).
			Optional().
			Default([]map[string]interface{}{}),
		field.Time("started_at").
			Default(timeNow),
		field.Time("ended_at").
			Optional().
			Nillable(),
		field.JSON("tool_invocations", []map[string]interface{}{}).
			Default([]map[string]interface{}{}),
		field.Enum("status").
			Values("active", "completed", "abandoned").
			Default("active"),
		field.Time("created_at").
			Immutable().
			Default(timeNow),
	}
}

func (Session) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("repo_config", RepoConfig.Type).
			Ref("sessions").
			Unique().
			Required(),
		edge.From("user", User.Type).
			Ref("sessions").
			Unique(),
	}
}
