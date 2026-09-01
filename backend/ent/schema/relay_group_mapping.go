package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// RelayGroupMapping stores the explicit admin-owned relationship between a
// directory department and the relay groups created for one platform.
// Group IDs are authoritative; names are retained only for display snapshots.
type RelayGroupMapping struct {
	ent.Schema
}

func (RelayGroupMapping) Fields() []ent.Field {
	return []ent.Field{
		field.Int("provider_id"),
		field.String("department_external_id").NotEmpty(),
		field.String("department_name").Default(""),
		field.String("platform").NotEmpty(),
		field.Int64("template_group_id").Default(0),
		field.String("template_group_name").Default(""),
		field.Int64("source_group_id").Default(0),
		field.String("source_group_name").Default(""),
		field.JSON("group_ids", []int64{}),
		field.JSON("member_assignments", map[string]int64{}).Default(map[string]int64{}),
		field.JSON("member_sources", map[string]int64{}).Default(map[string]int64{}),
		field.Bool("account_management_initialized").Default(false),
		field.JSON("desired_accounts", map[string][]map[string]int64{}).Default(map[string][]map[string]int64{}),
		field.JSON("operation_state", map[string]map[string]string{}).Default(map[string]map[string]string{}),
		field.Int64("baseline_revision").Default(1).Positive(),
		field.String("status").Default("active"),
		field.Float("weekly_cost_target").Default(0),
		field.Time("created_at").Immutable().Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (RelayGroupMapping) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("relationship_operation_mappings", RelationshipOperationMapping.Type),
	}
}

func (RelayGroupMapping) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("provider_id", "department_external_id", "platform").Unique(),
		index.Fields("provider_id", "source_group_id"),
	}
}
