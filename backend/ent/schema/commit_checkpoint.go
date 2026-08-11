package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type CommitCheckpoint struct {
	ent.Schema
}

func (CommitCheckpoint) Fields() []ent.Field {
	return []ent.Field{
		field.String("event_id").
			Unique(),
		field.Int("user_id").
			Optional().
			Nillable(),
		field.String("workspace_id").
			NotEmpty(),
		field.Int("repo_config_id"),
		field.String("commit_sha").
			NotEmpty(),
		field.JSON("parent_shas", []string{}),
		field.String("branch_snapshot").
			Optional().
			Nillable(),
		field.String("head_snapshot").
			Optional().
			Nillable(),
		field.Enum("lineage_kind").Values("cherry_pick").Optional(),
		field.String("source_commit_sha").Optional(),
		field.String("commit_patch_id").Optional(),
		field.String("source_patch_id").Optional(),
		field.Enum("binding_source").
			Values("marker", "env_bootstrap", "manual", "unbound"),
		field.JSON("agent_snapshot", map[string]any{}).
			Optional(),
		field.Time("captured_at").
			Default(timeNow),
	}
}

func (CommitCheckpoint) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("commit_checkpoints").
			Field("user_id").
			Unique(),
		edge.From("repo_config", RepoConfig.Type).
			Ref("commit_checkpoints").
			Field("repo_config_id").
			Unique().
			Required(),
		edge.To("tool_usage_events", ToolUsageEvent.Type),
		edge.To("pr_commit_usage_snapshots", PRCommitUsageSnapshot.Type),
	}
}

func (CommitCheckpoint) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "workspace_id", "repo_config_id", "commit_sha").
			Unique(),
		index.Fields("repo_config_id", "commit_sha").StorageKey("commitcheckpoint_repo_commit_lookup_v2"),
		index.Fields("user_id", "repo_config_id", "lineage_kind"),
	}
}
