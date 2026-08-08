package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AttributionUsageBucket is an immutable, compact measurement fact. Raw local
// token atoms, prompts, paths, commands, tool output, and OTLP spans never enter
// this table.
type AttributionUsageBucket struct {
	ent.Schema
}

func (AttributionUsageBucket) Fields() []ent.Field {
	return []ent.Field{
		field.String("bucket_id").NotEmpty().Unique(),
		field.Int("schema_version").Default(1),
		field.Int("reporting_installation_id"),
		field.Int("user_id"),
		field.String("tool").NotEmpty(),
		field.String("model").Optional(),
		field.String("change_set_id").Optional(),
		field.JSON("session_slices", []map[string]any{}),
		field.Time("observed_start_at"),
		field.Time("observed_end_at"),
		field.Int64("fresh_input_tokens").Default(0),
		field.Int64("cache_read_tokens").Default(0),
		field.Int64("cache_write_tokens").Default(0),
		field.Int64("output_tokens").Default(0),
		field.Int64("reasoning_tokens").Default(0),
		field.Int64("provider_total_tokens").Default(0),
		field.Int64("processed_total_tokens").Default(0),
		field.Int("request_count").Default(0),
		field.Int("source_event_count").Default(0),
		field.String("source_digest").NotEmpty(),
		field.String("immutable_digest").NotEmpty(),
		field.String("extractor_version").NotEmpty(),
		field.Int("normalization_version").Default(1),
		field.Enum("token_quality").Values("measured", "historical_advisory", "invalid"),
		field.Enum("request_correlation_quality").Values("exact", "advisory", "unlinked").Default("unlinked"),
		field.Int("request_id_coverage_count").Default(0),
		field.String("request_set_digest").Optional(),
		field.Int("coverage_gap_count").Default(0),
		field.Time("created_at").Immutable().Default(timeNow),
		field.Time("correlation_updated_at").Optional().Nillable(),
	}
}

func (AttributionUsageBucket) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("reporting_installation", ReportingInstallation.Type).
			Ref("usage_buckets").
			Field("reporting_installation_id").
			Unique().
			Required(),
		edge.From("user", User.Type).
			Ref("attribution_usage_buckets").
			Field("user_id").
			Unique().
			Required(),
		edge.To("allocation_revisions", AttributionAllocationRevision.Type),
	}
}

func (AttributionUsageBucket) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "observed_end_at", "id").
			Annotations(entsql.DescColumns("observed_end_at", "id")),
		index.Fields("reporting_installation_id", "observed_end_at"),
	}
}
