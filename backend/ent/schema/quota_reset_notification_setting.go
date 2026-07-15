package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type QuotaResetNotificationSetting struct {
	ent.Schema
}

func (QuotaResetNotificationSetting) Fields() []ent.Field {
	return []ent.Field{
		field.Bool("enabled").Default(false),
		field.String("url").Default(""),
		field.Enum("auth_type").Values("none", "bearer_token").Default("none"),
		field.Int("credential_id").Optional().Nillable(),
		field.Int("created_by_user_id").Default(0),
		field.Int("updated_by_user_id").Default(0),
		field.Time("created_at").Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}
