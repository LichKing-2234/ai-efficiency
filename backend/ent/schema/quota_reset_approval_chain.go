package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type QuotaResetApprovalChain struct{ ent.Schema }

func (QuotaResetApprovalChain) Fields() []ent.Field {
	return []ent.Field{
		field.Int("provider_id"),
		field.String("group_id").NotEmpty(),
		field.String("group_name").Default(""),
		field.Bool("enabled").Default(true),
		field.Int("created_by_user_id").Default(0),
		field.Int("updated_by_user_id").Default(0),
		field.Time("created_at").Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (QuotaResetApprovalChain) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("provider_id", "group_id").Unique(),
		index.Fields("enabled", "updated_at"),
	}
}
