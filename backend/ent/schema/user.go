package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// User holds the schema definition for the User entity.
type User struct {
	ent.Schema
}

// Fields of the User.
func (User) Fields() []ent.Field {
	return []ent.Field{
		field.String("username").
			Unique().
			NotEmpty(),
		field.String("email").
			Unique().
			NotEmpty(),
		field.Enum("auth_source").
			Values("sub2api_sso", "relay_sso", "ldap"),
		field.Int("relay_user_id").
			Optional().
			Nillable(),
		field.String("relay_auth_password").
			Optional().
			Nillable().
			Sensitive(),
		field.String("ldap_dn").
			Optional().
			Nillable(),
		field.Enum("role").
			Values("admin", "user").
			Default("user"),
		field.Time("token_valid_after").
			Optional().
			Nillable(),
		field.Time("relay_disabled_at").
			Optional().
			Nillable(),
		field.Time("created_at").
			Immutable().
			Default(timeNow),
		field.Time("updated_at").
			Default(timeNow).
			UpdateDefault(timeNow),
	}
}

// Edges of the User.
func (User) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("commit_checkpoints", CommitCheckpoint.Type),
		edge.To("commit_rewrites", CommitRewrite.Type),
		edge.To("tool_usage_events", ToolUsageEvent.Type),
		edge.To("reporting_installations", ReportingInstallation.Type),
		edge.To("attribution_usage_buckets", AttributionUsageBucket.Type),
	}
}

// Indexes of the User.
func (User) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("relay_user_id"),
	}
}
