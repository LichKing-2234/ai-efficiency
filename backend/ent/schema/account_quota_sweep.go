package schema

import (
	"entgo.io/ent"
	entsql "entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AccountQuotaSweep records one administrator-triggered refresh of OpenAI OAuth
// account quota facts (5h/7d usage windows, Codex credits and banked reset
// credits). Each row is a durable async job: the API returns it immediately, the
// detached worker advances the counters, and the page polls it. The sweep is a
// read-only refresh upstream — it never consumes a reset credit.
type AccountQuotaSweep struct{ ent.Schema }

func (AccountQuotaSweep) Fields() []ent.Field {
	return []ent.Field{
		field.Enum("status").
			Values("queued", "running", "completed", "failed", "abandoned").
			Default("queued"),
		field.Int("provider_id").Immutable(),
		field.String("platform").Default("openai"),
		// scope all = every eligible account of the provider; selected = the
		// explicit account_ids the operator picked.
		field.Enum("scope").
			Values("all", "selected"),
		// include_auto_reset records the operator's explicit opt-in to refresh
		// accounts whose Sub2API auto reset-credit worker is enabled. Without it
		// those accounts are skipped because a refresh can lead the upstream to
		// spend a credit asynchronously.
		field.Bool("include_auto_reset").Default(false),
		field.Int("total_count").Default(0),
		field.Int("processed_count").Default(0),
		field.Int("success_count").Default(0),
		field.Int("skipped_count").Default(0),
		field.Int("failed_count").Default(0),
		field.JSON("results", []map[string]any{}).Optional(),
		field.String("last_error").Optional().Nillable(),
		field.Time("started_at").Optional().Nillable(),
		field.Time("finished_at").Optional().Nillable(),
		field.Time("created_at").Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (AccountQuotaSweep) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status", "created_at"),
		index.Fields("provider_id", "created_at"),
		// At most one in-flight sweep per provider, so two administrators (or two
		// platform replicas) cannot fan out the same pool twice. Terminal rows are
		// outside the predicate.
		index.Fields("provider_id").
			Unique().
			StorageKey("accountquotasweep_running_unique").
			Annotations(entsql.IndexWhere("status IN ('queued', 'running')")),
	}
}
