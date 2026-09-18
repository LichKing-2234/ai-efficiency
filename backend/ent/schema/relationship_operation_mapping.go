package schema

import (
	"entgo.io/ent"
	entsql "entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// RelationshipOperationMapping owns one affected Mapping at its captured
// baseline revision until the Operation reaches a terminal readback state.
type RelationshipOperationMapping struct {
	ent.Schema
}

func (RelationshipOperationMapping) Fields() []ent.Field {
	return []ent.Field{
		field.Int("operation_id").Positive().Immutable(),
		field.Int("mapping_id").Positive().Immutable(),
		field.Enum("role").Values("primary", "source", "destination", "affected").Immutable(),
		field.Int64("baseline_revision").Positive().Immutable(),
		field.JSON("baseline_snapshot", map[string]any{}).Immutable(),
		field.Bool("active").Default(true),
		field.Time("created_at").Immutable().Default(timeNow),
		field.Time("released_at").Optional().Nillable(),
	}
}

func (RelationshipOperationMapping) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("operation", RelationshipOperation.Type).Ref("mappings").Field("operation_id").Unique().Required().Immutable(),
		edge.From("mapping", RelayGroupMapping.Type).Ref("relationship_operation_mappings").Field("mapping_id").Unique().Required().Immutable(),
	}
}

func (RelationshipOperationMapping) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("operation_id", "mapping_id").Unique(),
		index.Fields("mapping_id").Unique().StorageKey("relationshipoperationmapping_active_mapping_unique").Annotations(entsql.IndexWhere("active")),
		index.Fields("operation_id", "active"),
	}
}
