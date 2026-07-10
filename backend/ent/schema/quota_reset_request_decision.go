package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type QuotaResetRequestDecision struct{ ent.Schema }

func (QuotaResetRequestDecision) Fields() []ent.Field {
	return []ent.Field{
		field.Int("request_id").Immutable(),
		field.Int("request_node_id").Immutable(),
		field.Int("actor_user_id").Immutable(),
		field.String("actor_display_name").Default("").Immutable(),
		field.Enum("decision").Values("approve", "reject").Immutable(),
		field.String("comment").NotEmpty().Immutable(),
		field.Bool("admin_override").Default(false).Immutable(),
		field.Time("created_at").Default(timeNow).Immutable(),
	}
}

func (QuotaResetRequestDecision) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("request_node_id").Unique(),
		index.Fields("request_id", "created_at"),
		index.Fields("actor_user_id", "created_at"),
	}
}
