package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// RelationshipOperation stores one immutable reviewed relationship intent and
// its mutable recovery lifecycle independently from the Mapping baseline.
type RelationshipOperation struct {
	ent.Schema
}

func (RelationshipOperation) Fields() []ent.Field {
	return []ent.Field{
		field.String("operation_key").NotEmpty().Immutable(),
		field.Int("provider_id").Positive().Immutable(),
		field.String("platform").NotEmpty().Immutable(),
		field.Enum("lifecycle").Values("applying", "interrupted", "resuming", "restoring", "applied", "restored", "blocked_external").Default("applying"),
		field.JSON("baseline_snapshot", map[string]any{}).Immutable(),
		field.JSON("target_snapshot", map[string]any{}).Immutable(),
		field.String("baseline_fingerprint").NotEmpty().Immutable(),
		field.String("target_fingerprint").NotEmpty().Immutable(),
		field.JSON("supported_directions", []string{}).Immutable(),
		field.Int("initiated_by_user_id").Positive().Immutable(),
		field.JSON("terminal_result", map[string]any{}).Optional(),
		field.JSON("external_blocker", map[string]any{}).Optional(),
		field.Time("created_at").Immutable().Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
		field.Time("completed_at").Optional().Nillable(),
	}
}

func (RelationshipOperation) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("mappings", RelationshipOperationMapping.Type),
		edge.To("steps", RelationshipOperationStep.Type),
		edge.To("attempts", RelationshipOperationAttempt.Type),
	}
}

func (RelationshipOperation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("operation_key").Unique(),
		index.Fields("lifecycle", "created_at"),
		index.Fields("provider_id", "platform", "created_at"),
	}
}
