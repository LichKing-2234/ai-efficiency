package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type DirectoryMemberDepartment struct {
	ent.Schema
}

func (DirectoryMemberDepartment) Fields() []ent.Field {
	return []ent.Field{
		field.Int("source_id"),
		field.Int("directory_member_id"),
		field.String("member_external_id").Default(""),
		field.String("member_email_normalized").NotEmpty(),
		field.String("department_external_id").NotEmpty(),
		field.Int("last_seen_run_id"),
		field.Time("created_at").Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (DirectoryMemberDepartment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("source_id", "member_email_normalized", "department_external_id").Unique(),
		index.Fields("source_id", "department_external_id"),
		index.Fields("source_id", "directory_member_id", "department_external_id"),
		index.Fields("directory_member_id"),
		index.Fields("last_seen_run_id"),
	}
}
