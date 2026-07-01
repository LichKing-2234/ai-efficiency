package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type DirectorySource struct {
	ent.Schema
}

func (DirectorySource) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty(),
		field.String("description").Default(""),
		field.Enum("scope").
			Values("full_company").
			Default("full_company"),
		field.Bool("enabled").Default(false),
		field.Bool("deleted").Default(false),
		field.Text("dsl"),
		field.Bool("schedule_enabled").Default(false),
		field.Enum("schedule_interval").
			Values("hourly", "daily", "weekly").
			Default("daily"),
		field.String("schedule_timezone").Default("UTC"),
		field.Int("last_successful_run_id").Optional().Nillable(),
		field.Int("last_run_id").Optional().Nillable(),
		field.Time("last_scheduled_at").Optional().Nillable(),
		field.Time("created_at").Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (DirectorySource) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("deleted", "enabled"),
		index.Fields("schedule_enabled", "enabled", "deleted"),
		index.Fields("created_at"),
	}
}
