package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/google/uuid"
)

type SessionEvent struct {
	ent.Schema
}

func (SessionEvent) Fields() []ent.Field {
	return []ent.Field{
		field.String("event_id").
			Unique().
			NotEmpty(),
		field.UUID("session_id", uuid.UUID{}),
		field.String("workspace_id").
			NotEmpty(),
		field.String("event_type").
			NotEmpty(),
		field.String("source").
			NotEmpty(),
		field.Time("captured_at"),
		field.JSON("raw_payload", map[string]any{}).
			Optional(),
		field.Time("created_at").
			Default(timeNow),
	}
}

func (SessionEvent) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("session", Session.Type).
			Ref("session_events").
			Field("session_id").
			Unique().
			Required(),
	}
}

func (SessionEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id", "captured_at"),
	}
}
