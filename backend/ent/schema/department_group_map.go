package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// DepartmentGroupMap records which Relay access groups a directory department
// may use. Nothing else in the platform links departments to Relay groups, so
// this table is the only source for team account provisioning.
type DepartmentGroupMap struct {
	ent.Schema
}

// Fields of the DepartmentGroupMap.
func (DepartmentGroupMap) Fields() []ent.Field {
	return []ent.Field{
		field.String("department_external_id").
			NotEmpty(),
		field.Int("relay_provider_id"),
		field.Int64("relay_group_id"),
		field.Int("created_by_user_id").
			Optional().
			Nillable(),
		field.Time("created_at").
			Immutable().
			Default(timeNow),
	}
}

// Indexes of the DepartmentGroupMap.
func (DepartmentGroupMap) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("department_external_id", "relay_provider_id", "relay_group_id").Unique(),
		index.Fields("department_external_id"),
	}
}
