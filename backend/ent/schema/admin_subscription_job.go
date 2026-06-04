package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type AdminSubscriptionJob struct {
	ent.Schema
}

func (AdminSubscriptionJob) Fields() []ent.Field {
	return []ent.Field{
		field.Enum("status").
			Values("queued", "running", "completed", "failed", "abandoned").
			Default("queued"),
		field.Enum("phase").
			Values("queued", "resolving_targets", "processing", "completed", "failed").
			Default("queued"),
		field.Enum("scope").
			Values("selected", "current_filter", "all_mapped"),
		field.Enum("operation").
			Values("add", "extend", "remove"),
		field.Int("provider_id"),
		field.String("group_id"),
		field.Int("validity_days").Optional(),
		field.Int("days").Optional(),
		field.String("filter_query").Optional(),
		field.JSON("target_user_ids", []int{}).Optional(),
		field.JSON("target_snapshots", []map[string]any{}).Optional(),
		field.JSON("requested_user_ids", []int{}).Optional(),
		field.Int("total_count").Default(0),
		field.Int("processed_count").Default(0),
		field.Int("success_count").Default(0),
		field.Int("skipped_count").Default(0),
		field.Int("failed_count").Default(0),
		field.JSON("results", []map[string]any{}).Optional(),
		field.String("last_error").Optional().Nillable(),
		field.Time("started_at").Optional().Nillable(),
		field.Time("completed_at").Optional().Nillable(),
		field.Time("created_at").Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (AdminSubscriptionJob) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status", "created_at"),
		index.Fields("created_at"),
	}
}
