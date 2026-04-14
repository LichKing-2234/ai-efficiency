package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// ScmProvider holds the schema definition for the ScmProvider entity.
type ScmProvider struct {
	ent.Schema
}

// Fields of the ScmProvider.
func (ScmProvider) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			NotEmpty(),
		field.Enum("type").
			Values("github", "bitbucket_server"),
		field.String("base_url").
			NotEmpty(),
		field.String("credentials").
			Sensitive().
			Optional(),
		field.Int("api_credential_id").
			Optional(),
		field.Int("clone_credential_id").
			Optional().
			Nillable(),
		field.Enum("clone_protocol").
			Values("https", "ssh").
			Default("https"),
		field.Enum("status").
			Values("active", "inactive", "error").
			Default("active"),
		field.Time("created_at").
			Immutable().
			Default(timeNow),
		field.Time("updated_at").
			Default(timeNow).
			UpdateDefault(timeNow),
	}
}

// Edges of the ScmProvider.
func (ScmProvider) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("api_credential", Credential.Type).
			Ref("api_scm_providers").
			Field("api_credential_id").
			Unique(),
		edge.From("clone_credential", Credential.Type).
			Ref("clone_scm_providers").
			Field("clone_credential_id").
			Unique(),
		edge.To("repo_configs", RepoConfig.Type),
	}
}
