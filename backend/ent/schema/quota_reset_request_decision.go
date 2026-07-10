package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type QuotaResetRequestDecision struct{ ent.Schema }

func (QuotaResetRequestDecision) Fields() []ent.Field {
	return []ent.Field{
		field.Int("request_id"),
		field.Int("request_node_id"),
		field.Int("actor_user_id"),
		field.String("actor_display_name").Default(""),
		field.Enum("decision").Values("approve", "reject"),
		field.String("comment").NotEmpty(),
		field.Bool("admin_override").Default(false),
		field.Time("created_at").Default(timeNow),
	}
}

func (QuotaResetRequestDecision) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("request_node_id").Unique(),
		index.Fields("request_id", "created_at"),
		index.Fields("actor_user_id", "created_at"),
	}
}
