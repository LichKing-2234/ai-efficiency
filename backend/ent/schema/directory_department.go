package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type DirectoryDepartment struct {
	ent.Schema
}

func (DirectoryDepartment) Fields() []ent.Field {
	return []ent.Field{
		field.Int("source_id"),
		field.String("external_id").NotEmpty(),
		field.String("parent_external_id").Optional().Nillable(),
		field.String("effective_parent_external_id").Optional().Nillable(),
		field.String("name").NotEmpty(),
		field.String("path").Default(""),
		field.JSON("metadata", map[string]any{}).Optional(),
		field.Int("last_seen_run_id"),
		field.Time("created_at").Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (DirectoryDepartment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("source_id", "external_id").Unique(),
		index.Fields("source_id", "parent_external_id"),
		index.Fields("source_id", "effective_parent_external_id"),
		index.Fields("source_id", "name"),
	}
}
