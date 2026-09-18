package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// RelationshipOperationStep stores one immutable directional effect identity.
type RelationshipOperationStep struct {
	ent.Schema
}

func (RelationshipOperationStep) Fields() []ent.Field {
	return []ent.Field{
		field.Int("operation_id").Positive().Immutable(),
		field.String("step_key").NotEmpty().Immutable(),
		field.String("action").NotEmpty().Immutable(),
		field.String("relationship_type").NotEmpty().Immutable(),
		field.Enum("direction").Values("target", "baseline").Immutable(),
		field.Int("local_user_id").Optional().Nillable().Immutable(),
		field.Int64("relay_user_id").Optional().Nillable().Immutable(),
		field.Int64("source_group_id").Optional().Nillable().Immutable(),
		field.Int64("target_group_id").Optional().Nillable().Immutable(),
		field.JSON("reviewed_resource_ids", []int64{}).Immutable(),
		field.Int("reviewed_priority").Optional().Nillable().Immutable(),
		field.String("reviewed_status").Optional().Nillable().Immutable(),
		field.JSON("expected_result", map[string]any{}).Immutable(),
		field.Bool("resume_supported").Immutable(),
		field.Bool("restore_supported").Immutable(),
		field.Enum("lifecycle").Values("planned", "dispatched", "readback_verified", "failed", "blocked_external").Default("planned"),
		field.JSON("latest_verified_effect", map[string]any{}).Optional(),
		field.Time("created_at").Immutable().Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (RelationshipOperationStep) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("operation", RelationshipOperation.Type).Ref("steps").Field("operation_id").Unique().Required().Immutable(),
	}
}

func (RelationshipOperationStep) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("operation_id", "step_key").Unique(),
		index.Fields("operation_id", "lifecycle"),
	}
}
