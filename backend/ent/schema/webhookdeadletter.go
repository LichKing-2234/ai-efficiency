package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// WebhookDeadLetter holds the schema definition for the WebhookDeadLetter entity.
type WebhookDeadLetter struct {
	ent.Schema
}

// Fields of the WebhookDeadLetter.
func (WebhookDeadLetter) Fields() []ent.Field {
	return []ent.Field{
		field.String("delivery_id").
			NotEmpty(),
		field.String("event_type").
			NotEmpty(),
		field.JSON("payload", map[string]interface{}{}),
		field.String("error_message"),
		field.Int("retry_count").
			Default(0),
		field.Int("max_retries").
			Default(3),
		field.Enum("status").
			Values("pending", "retrying", "failed", "resolved").
			Default("pending"),
		field.Time("created_at").
			Immutable().
			Default(timeNow),
		field.Time("resolved_at").
			Optional().
			Nillable(),
	}
}

// Edges of the WebhookDeadLetter.
func (WebhookDeadLetter) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("repo_config", RepoConfig.Type).
			Ref("webhook_dead_letters").
			Unique().
			Required(),
	}
}
