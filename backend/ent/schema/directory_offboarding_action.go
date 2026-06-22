package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type DirectoryOffboardingAction struct {
	ent.Schema
}

func (DirectoryOffboardingAction) Fields() []ent.Field {
	return []ent.Field{
		field.Int("source_id"),
		field.Int("user_id"),
		field.Int("relay_user_id"),
		field.Int("directory_run_id"),
		field.Enum("action").
			Values("disable_relay_user"),
		field.Enum("status").
			Values("running", "succeeded", "failed", "partial_failed").
			Default("running"),
		field.String("reason"),
		field.String("error_message").Optional().Nillable(),
		field.Int("performed_by_user_id"),
		field.Time("created_at").Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (DirectoryOffboardingAction) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("source_id", "user_id", "action").Unique(),
		index.Fields("source_id", "status"),
		index.Fields("user_id"),
	}
}
