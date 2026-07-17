package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type DirectoryMember struct {
	ent.Schema
}

func (DirectoryMember) Fields() []ent.Field {
	return []ent.Field{
		field.Int("source_id"),
		field.String("external_id").Default(""),
		field.String("email_normalized").NotEmpty(),
		field.String("display_name").Default(""),
		field.String("department_external_id").Default(""),
		field.String("status").Default("active"),
		field.JSON("metadata", map[string]any{}).Optional(),
		field.Int("matched_user_id").Optional().Nillable(),
		field.Int("last_seen_run_id"),
		field.Time("created_at").Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (DirectoryMember) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("source_id", "email_normalized").Unique(),
		index.Fields("source_id", "department_external_id"),
		index.Fields("source_id", "matched_user_id"),
		index.Fields("matched_user_id"),
		index.Fields("last_seen_run_id"),
	}
}
