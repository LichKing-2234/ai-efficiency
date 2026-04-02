package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"

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
