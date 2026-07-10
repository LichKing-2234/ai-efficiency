package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type QuotaResetRequestNodeApprover struct{ ent.Schema }

func (QuotaResetRequestNodeApprover) Fields() []ent.Field {
	return []ent.Field{
		field.Int("request_node_id").Immutable(),
		field.Int("user_id").Immutable(),
		field.String("display_name").Default("").Immutable(),
		field.String("email").Default("").Immutable(),
		field.Enum("source").Values("configured", "directory_representative").Immutable(),
		field.JSON("source_department_external_ids", []string{}).Optional().Immutable(),
		field.JSON("notification_ids", map[string]string{}).Optional().Immutable(),
		field.Time("created_at").Default(timeNow).Immutable(),
	}
}

func (QuotaResetRequestNodeApprover) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("request_node_id", "user_id").Unique(),
		index.Fields("user_id", "request_node_id"),
	}
}
