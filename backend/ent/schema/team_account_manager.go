package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TeamAccountManager grants one platform user the ability to manage a team
// account. Exactly one row per account carries role=owner; the owner is chosen
// by hand and can be replaced without touching the Relay identity.
type TeamAccountManager struct {
	ent.Schema
}

// Fields of the TeamAccountManager.
func (TeamAccountManager) Fields() []ent.Field {
	return []ent.Field{
		field.Int("team_account_id"),
		field.Int("user_id"),
		field.Enum("role").
			Values("owner", "manager").
			Default("manager"),
		field.Int("added_by_user_id").
			Optional().
			Nillable(),
		field.Time("created_at").
			Immutable().
			Default(timeNow),
	}
}

// Indexes of the TeamAccountManager.
func (TeamAccountManager) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("team_account_id", "user_id").Unique(),
		index.Fields("user_id"),
	}
}
