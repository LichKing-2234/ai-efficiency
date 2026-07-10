package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type QuotaResetApprovalChainNode struct{ ent.Schema }

func (QuotaResetApprovalChainNode) Fields() []ent.Field {
	return []ent.Field{
		field.Int("chain_id"),
		field.Int("position").NonNegative(),
		field.Int("directory_source_id"),
		field.String("department_external_id").NotEmpty(),
		field.String("department_display_path").Default(""),
		field.Time("created_at").Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (QuotaResetApprovalChainNode) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("chain_id", "position").Unique(),
		index.Fields("chain_id", "department_external_id").Unique(),
		index.Fields("directory_source_id", "department_external_id"),
	}
}
