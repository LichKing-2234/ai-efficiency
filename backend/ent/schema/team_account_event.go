package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TeamAccountEvent is the append-only audit trail of a team account. Payloads
// never contain plaintext credentials or full API keys.
type TeamAccountEvent struct {
	ent.Schema
}

// Fields of the TeamAccountEvent.
func (TeamAccountEvent) Fields() []ent.Field {
	return []ent.Field{
		field.Int("team_account_id"),
		field.Int("actor_user_id").
			Optional().
			Nillable(),
		field.String("action").
			NotEmpty(),
		field.JSON("payload", map[string]any{}).
			Optional(),
		field.Time("created_at").
			Immutable().
			Default(timeNow),
	}
}

// Indexes of the TeamAccountEvent.
func (TeamAccountEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("team_account_id", "created_at"),
		index.Fields("action"),
	}
}
