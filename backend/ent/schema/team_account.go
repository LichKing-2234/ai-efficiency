package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TeamAccount is an AI account owned by a directory department. The owner is a
// role, not an identity: handovers replace the owner row and never migrate the
// Relay user or its usage.
type TeamAccount struct {
	ent.Schema
}

// Fields of the TeamAccount.
func (TeamAccount) Fields() []ent.Field {
	return []ent.Field{
		field.String("department_external_id").
			Unique().
			NotEmpty().
			Comment("Directory department stable id; the account is owned by this department."),
		field.String("department_display_path").
			Optional().
			Default(""),
		field.String("name").
			NotEmpty(),
		field.Int("relay_provider_id"),
		field.Int64("relay_user_id").
			Unique(),
		field.String("relay_username").
			NotEmpty(),
		field.String("relay_email").
			Optional().
			Default(""),
		field.String("relay_auth_password").
			Optional().
			Nillable().
			Sensitive().
			Comment("Encrypted Relay password used to mint team API keys; never returned in listings."),
		field.Int("owner_user_id"),
		field.Enum("status").
			Values("active", "suspended", "pending_claim", "orphaned").
			Default("active"),
		field.Int("created_by_user_id").
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

// Indexes of the TeamAccount.
func (TeamAccount) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("owner_user_id"),
		index.Fields("status"),
	}
}
