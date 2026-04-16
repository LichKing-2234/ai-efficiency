package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/google/uuid"
)

type SessionUsageEvent struct {
	ent.Schema
}

func (SessionUsageEvent) Fields() []ent.Field {
	return []ent.Field{
		field.String("event_id").
			Unique().
			NotEmpty(),
		field.UUID("session_id", uuid.UUID{}),
		field.String("workspace_id").
			NotEmpty(),
		field.String("request_id").
			NotEmpty(),
		field.String("provider_name").
			NotEmpty(),
		field.String("model").
			NotEmpty(),
		field.Time("started_at"),
		field.Time("finished_at"),
		field.Int64("input_tokens").
			Default(0),
		field.Int64("output_tokens").
			Default(0),
		field.Int64("total_tokens").
			Default(0),
		field.String("status").
			NotEmpty(),
		field.JSON("raw_metadata", map[string]any{}).
			Optional(),
		field.JSON("raw_response", map[string]any{}).
			Optional(),
		field.Time("created_at").
			Default(timeNow),
	}
}

func (SessionUsageEvent) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("session", Session.Type).
			Ref("session_usage_events").
			Field("session_id").
			Unique().
			Required(),
	}
}

func (SessionUsageEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id", "started_at"),
	}
}
