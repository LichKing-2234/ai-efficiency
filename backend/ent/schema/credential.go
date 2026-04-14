package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Credential holds the schema definition for the Credential entity.
type Credential struct {
	ent.Schema
}

// Fields of the Credential.
func (Credential) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			NotEmpty(),
		field.String("description").
			Default(""),
		field.Enum("kind").
			Values("secret_text", "username_password", "ssh_username_with_private_key"),
		field.String("payload").
			Sensitive(),
		field.Time("created_at").
			Immutable().
			Default(timeNow),
		field.Time("updated_at").
			Default(timeNow).
			UpdateDefault(timeNow),
	}
}

// Edges of the Credential.
func (Credential) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("api_scm_providers", ScmProvider.Type),
		edge.To("clone_scm_providers", ScmProvider.Type),
	}
}
