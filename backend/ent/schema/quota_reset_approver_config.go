package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type QuotaResetApproverConfig struct {
	ent.Schema
}

func (QuotaResetApproverConfig) Fields() []ent.Field {
	return []ent.Field{
		field.Int("directory_source_id"),
		field.String("department_external_id").NotEmpty(),
		field.String("department_display_path").Default(""),
		field.Int("approver_user_id"),
		field.Bool("enabled").Default(true),
		field.Int("created_by_user_id").Default(0),
		field.Int("updated_by_user_id").Default(0),
		field.Time("created_at").Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (QuotaResetApproverConfig) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("directory_source_id", "department_external_id", "enabled"),
		index.Fields("approver_user_id", "enabled"),
		index.Fields("directory_source_id", "department_external_id", "approver_user_id").Unique(),
	}
}
