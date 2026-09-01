package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// RelationshipOperationAttempt preserves each initial, Resume, or Restore
// execution attempt without overwriting evidence from another direction.
type RelationshipOperationAttempt struct {
	ent.Schema
}

func (RelationshipOperationAttempt) Fields() []ent.Field {
	return []ent.Field{
		field.Int("operation_id").Positive().Immutable(),
		field.Int("attempt_number").Positive().Immutable(),
		field.Enum("direction").Values("initial", "resume", "restore").Immutable(),
		field.Enum("status").Values("planned", "running", "succeeded", "failed", "interrupted", "blocked_external").Default("planned"),
		field.Int("initiated_by_user_id").Positive().Immutable(),
		field.JSON("result", map[string]any{}).Optional(),
		field.String("error_message").Default(""),
		field.Time("created_at").Immutable().Default(timeNow),
		field.Time("started_at").Optional().Nillable(),
		field.Time("completed_at").Optional().Nillable(),
	}
}

func (RelationshipOperationAttempt) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("operation", RelationshipOperation.Type).Ref("attempts").Field("operation_id").Unique().Required().Immutable(),
	}
}

func (RelationshipOperationAttempt) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("operation_id", "attempt_number").Unique(),
		index.Fields("operation_id", "direction", "created_at"),
	}
}
