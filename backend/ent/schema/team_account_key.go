package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TeamAccountKey is one Relay API key issued for a team account. The plaintext
// secret is never stored; only a short preview is kept for the console.
type TeamAccountKey struct {
	ent.Schema
}

// Fields of the TeamAccountKey.
func (TeamAccountKey) Fields() []ent.Field {
	return []ent.Field{
		field.Int("team_account_id"),
		field.Int64("relay_api_key_id"),
		field.Int64("relay_group_id"),
		field.String("name").
			NotEmpty(),
		field.String("secret_preview").
			Optional().
			Default(""),
		field.Enum("status").
			Values("active", "revoked").
			Default("active"),
		field.Int("created_by_user_id").
			Optional().
			Nillable(),
		field.Time("created_at").
			Immutable().
			Default(timeNow),
		field.Time("revoked_at").
			Optional().
			Nillable(),
	}
}

// Indexes of the TeamAccountKey.
func (TeamAccountKey) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("team_account_id"),
		index.Fields("relay_api_key_id").Unique(),
	}
}
