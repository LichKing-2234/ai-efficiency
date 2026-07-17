package schema

import (
	"entgo.io/ent"
	entsql "entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type DirectorySyncRun struct {
	ent.Schema
}

func (DirectorySyncRun) Fields() []ent.Field {
	return []ent.Field{
		field.Int("source_id"),
		field.Enum("mode").
			Values("validate", "preview", "apply"),
		field.Enum("trigger").
			Values("manual", "schedule").
			Default("manual"),
		field.Enum("status").
			Values("queued", "running", "completed", "completed_with_warnings", "failed").
			Default("queued"),
		field.Enum("phase").
			Values("validating", "executing", "normalizing", "applying", "completed", "failed").
			Default("validating"),
		field.Time("started_at").Optional().Nillable(),
		field.Time("completed_at").Optional().Nillable(),
		field.Int("http_request_count").Default(0),
		field.Int("department_count").Default(0),
		field.Int("member_count").Default(0),
		field.Int("invalid_member_count").Default(0),
		field.Int("warning_count").Default(0),
		field.String("error_message").Optional().Nillable(),
		field.JSON("warnings", []map[string]any{}).Optional(),
		field.JSON("summary", map[string]any{}).Optional(),
		field.JSON("preview_diff", map[string]any{}).Optional(),
		field.Time("created_at").Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (DirectorySyncRun) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("source_id", "created_at"),
		index.Fields("source_id", "status"),
		index.Fields("status", "created_at"),
		index.Fields("source_id", "started_at", "id").
			Annotations(entsql.DescColumns("started_at", "id")),
		index.Fields("source_id", "started_at", "id").
			StorageKey("directory_sync_runs_active_started_id").
			Annotations(
				entsql.DescColumns("started_at", "id"),
				entsql.IndexWhere("mode IN ('preview', 'apply') AND status IN ('queued', 'running')"),
			),
	}
}
