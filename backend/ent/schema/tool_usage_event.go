package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type ToolUsageEvent struct {
	ent.Schema
}

func (ToolUsageEvent) Fields() []ent.Field {
	return []ent.Field{
		field.String("tool").
			NotEmpty(),
		field.String("workspace_id").
			NotEmpty(),
		field.Int("repo_config_id"),
		field.Int("user_id"),
		field.String("tool_session_id").
			NotEmpty(),
		field.String("tool_event_id").
			Optional().
			Nillable(),
		field.Time("observed_start_at"),
		field.Time("observed_end_at"),
		field.Int("request_count").
			Default(0),
		field.Enum("usage_unit").
			Values("token", "credit").
			Default("token"),
		field.Int64("input_tokens").
			Default(0),
		field.Int64("output_tokens").
			Default(0),
		field.Int64("cached_input_tokens").
			Default(0),
		field.Int64("reasoning_tokens").
			Default(0),
		field.Float("credit_usage").
			Default(0),
		field.Float("context_usage_pct").
			Default(0),
		field.Int("commit_checkpoint_id").
			Optional().
			Nillable(),
		field.String("dedupe_key").
			NotEmpty().
			Unique(),
		field.String("raw_source_path").
			Optional().
			Nillable(),
		field.String("raw_source_locator").
			Optional().
			Nillable(),
		field.JSON("raw_payload", map[string]any{}).
			Optional(),
		field.Time("created_at").
			Default(timeNow),
	}
}

func (ToolUsageEvent) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("repo_config", RepoConfig.Type).
			Ref("tool_usage_events").
			Field("repo_config_id").
			Unique().
			Required(),
		edge.From("user", User.Type).
			Ref("tool_usage_events").
			Field("user_id").
			Unique().
			Required(),
		edge.From("commit_checkpoint", CommitCheckpoint.Type).
			Ref("tool_usage_events").
			Field("commit_checkpoint_id").
			Unique(),
	}
}

func (ToolUsageEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("workspace_id", "observed_end_at"),
		index.Fields("commit_checkpoint_id"),
		index.Fields("tool", "tool_session_id"),
		index.Fields("observed_end_at", "id").
			Annotations(entsql.DescColumns("observed_end_at", "id")),
		index.Fields("user_id", "observed_end_at", "id").
			Annotations(entsql.DescColumns("observed_end_at", "id")),
		index.Fields("repo_config_id", "observed_end_at", "id").
			Annotations(entsql.DescColumns("observed_end_at", "id")),
		index.Fields("tool", "observed_end_at", "id").
			Annotations(entsql.DescColumns("observed_end_at", "id")),
	}
}
