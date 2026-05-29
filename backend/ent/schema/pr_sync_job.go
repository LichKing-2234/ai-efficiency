package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type PRSyncJob struct {
	ent.Schema
}

func (PRSyncJob) Fields() []ent.Field {
	return []ent.Field{
		field.Int("repo_config_id"),
		field.Enum("status").
			Values("queued", "running", "completed", "failed", "cancelled", "abandoned").
			Default("queued"),
		field.Enum("phase").
			Values("queued", "fetching_prs", "upserting_prs", "labeling", "refreshing_usage", "completed", "failed").
			Default("queued"),
		field.Int("page_size").Default(100),
		field.Int("current_page").Default(0),
		field.Int("fetched_prs").Default(0),
		field.Int("total_prs").Default(0),
		field.Int("processed_prs").Default(0),
		field.Int("created_prs").Default(0),
		field.Int("changed_prs").Default(0),
		field.Int("unchanged_prs").Default(0),
		field.Int("upsert_failed_prs").Default(0),
		field.Int("labeled_prs").Default(0),
		field.Int("label_failed_prs").Default(0),
		field.Int("usage_total_prs").Default(0),
		field.Int("usage_refreshed_prs").Default(0),
		field.Int("usage_skipped_prs").Default(0),
		field.Int("usage_failed_prs").Default(0),
		field.String("last_error").Optional().Nillable(),
		field.JSON("error_summary", []map[string]any{}).Optional(),
		field.Time("started_at").Optional().Nillable(),
		field.Time("completed_at").Optional().Nillable(),
		field.Time("created_at").Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (PRSyncJob) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("repo_config", RepoConfig.Type).
			Ref("pr_sync_jobs").
			Field("repo_config_id").
			Unique().
			Required(),
	}
}

func (PRSyncJob) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("repo_config_id", "status"),
		index.Fields("created_at"),
	}
}
