package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"

	"github.com/google/uuid"
)

type AgentMetadataEvent struct {
	ent.Schema
}

func (AgentMetadataEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("session_id", uuid.UUID{}),
		field.String("workspace_id").
			Optional().
			Nillable(),
		field.Enum("source").
			Values("codex", "claude", "kiro"),
		field.String("source_session_id").
			Optional().
			Nillable(),
		field.Enum("usage_unit").
			Values("token", "credit", "unknown").
			Default("unknown"),
		field.Int64("input_tokens").
			Default(0),
		field.Int64("output_tokens").
			Default(0),
		field.Int64("cached_input_tokens").
			Default(0),
		field.Int64("reasoning_tokens").
			Default(0),
		field.Float("credit_usage").
			Default(0),
		field.Float("context_usage_pct").
			Default(0),
		field.JSON("raw_payload", map[string]any{}).
			Optional(),
		field.Time("observed_at").
			Default(timeNow),
	}
}

func (AgentMetadataEvent) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("session", Session.Type).
			Ref("agent_metadata_events").
			Field("session_id").
			Unique().
			Required(),
	}
}

