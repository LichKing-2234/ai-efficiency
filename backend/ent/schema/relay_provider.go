package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// RelayProvider holds the schema definition for the RelayProvider entity.
type RelayProvider struct {
	ent.Schema
}

func (RelayProvider) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			Unique().
			NotEmpty(),
		field.String("display_name").
			NotEmpty(),
		field.String("base_url").
			NotEmpty(),
		field.String("admin_url").
			NotEmpty(),
		field.String("relay_type").
			Default("sub2api"),
		field.String("admin_api_key").
			Sensitive(),
		field.String("default_model").
			Default("claude-sonnet-4-20250514"),
		field.Bool("is_primary").
			Default(false),
		field.Bool("enabled").
			Default(true),
		field.Time("created_at").
			Immutable().
			Default(timeNow),
		field.Time("updated_at").
			Default(timeNow).
			UpdateDefault(timeNow),
	}
}
