package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AccountQuotaObservation is the last successful quota observation of one
// upstream account: the 5h/7d usage windows, the Codex credit balance and the
// banked reset-credit count/expiries returned by a sweep. It is a display fact
// with an explicit observation time, never an authority for a mutation, and it
// carries no credential, token or raw upstream error text. Only successful
// observations are written, so a failed attempt cannot erase the last good facts.
type AccountQuotaObservation struct{ ent.Schema }

func (AccountQuotaObservation) Fields() []ent.Field {
	return []ent.Field{
		field.Int("provider_id").Immutable(),
		field.Int64("account_id").Immutable(),
		// sweep_id is the sweep that produced this observation, for traceability.
		field.Int("sweep_id"),
		field.String("plan_type").Optional().Nillable(),
		// windows keeps the upstream window list as returned: used_percent,
		// limit_window_seconds and reset time. The 5h and 7d windows are
		// identified by window_seconds (18000 and 604800), not by position.
		field.JSON("windows", []map[string]any{}).Optional(),
		field.JSON("credits", map[string]any{}).Optional(),
		field.JSON("reset_credit_snapshot", map[string]any{}).Optional(),
		field.Time("observed_at"),
		field.Time("created_at").Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (AccountQuotaObservation) Indexes() []ent.Index {
	return []ent.Index{
		// One current observation per provider account; sweeps upsert it.
		index.Fields("provider_id", "account_id").Unique(),
		index.Fields("provider_id", "observed_at"),
	}
}
