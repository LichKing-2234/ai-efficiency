package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ReportingInstallation is a machine-scoped reporting identity. Legacy OTLP
// columns remain during the rolling Phase 2 deployment and are removed later.
type ReportingInstallation struct {
	ent.Schema
}

func (ReportingInstallation) Fields() []ent.Field {
	return []ent.Field{
		field.String("installation_id").NotEmpty().Unique(),
		field.Int("user_id"),
		field.String("label").Optional(),
		field.String("client_version").Optional(),
		field.String("reporter_token_hash").NotEmpty().Unique().Sensitive(),
		field.String("otlp_token_hash").NotEmpty().Unique().Sensitive(),
		field.Bool("reporting_enabled").Default(false),
		field.Bool("otel_enabled").Default(false),
		field.Enum("status").Values("active", "revoked").Default("active"),
		field.Time("last_seen_at").Optional().Nillable(),
		field.Time("created_at").Immutable().Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (ReportingInstallation) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("reporting_installations").
			Field("user_id").
			Unique().
			Required(),
		edge.To("usage_buckets", AttributionUsageBucket.Type),
	}
}

func (ReportingInstallation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "status"),
	}
}
