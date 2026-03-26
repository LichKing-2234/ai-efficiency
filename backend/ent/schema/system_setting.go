package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// SystemSetting holds the schema definition for the SystemSetting entity.
type SystemSetting struct {
	ent.Schema
}

func (SystemSetting) Fields() []ent.Field {
	return []ent.Field{
		field.String("key").
			Unique().
			NotEmpty(),
		field.Text("value").
			Default(""),
		field.Time("updated_at").
			Default(timeNow).
			UpdateDefault(timeNow),
	}
}
