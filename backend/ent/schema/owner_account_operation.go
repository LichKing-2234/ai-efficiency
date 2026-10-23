package schema

import (
	"entgo.io/ent"
	entsql "entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OwnerAccountOperation records one owner-initiated console action (or one
// read refresh) so that async polling, audit history, unknown-outcome blocks
// and daily caps have a durable local source of truth. Ownership, credentials
// and upstream account snapshots are never persisted here.
type OwnerAccountOperation struct{ ent.Schema }

func (OwnerAccountOperation) Fields() []ent.Field {
	return []ent.Field{
		field.Int("provider_id").Immutable(),
		field.Int64("account_id").Immutable(),
		field.Int("actor_user_id").Immutable(),
		field.Enum("action").
			Values("refresh_quota", "refresh_referral", "reset_credit", "send_invitation").
			Immutable(),
		// Refreshes are terminal-only; mutations start as running and are
		// advanced by the detached worker.
		field.Enum("status").
			Values("running", "succeeded", "failed", "unknown").
			Default("running"),
		field.String("reason").Optional().Nillable(),
		field.String("invite_email").Optional().Nillable(),
		// Upstream codes and results are sanitized before they are stored.
		field.String("upstream_code").Optional().Nillable(),
		field.String("warning_code").Optional().Nillable(),
		field.JSON("result", map[string]any{}).Optional(),
		// The worker renews the heartbeat while the upstream call is in flight;
		// a stale heartbeat marks a running row unknown.
		field.Time("heartbeat_at").Optional().Nillable(),
		field.Time("started_at").Optional().Nillable(),
		field.Time("finished_at").Optional().Nillable(),
		field.Time("created_at").Immutable().Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (OwnerAccountOperation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("provider_id", "account_id", "created_at"),
		index.Fields("actor_user_id", "created_at"),
		index.Fields("status", "created_at"),
		// At most one running mutation per upstream account, across both
		// mutation actions. Terminal rows are outside the predicate.
		index.Fields("provider_id", "account_id").
			Unique().
			StorageKey("owneraccountoperation_running_unique").
			Annotations(entsql.IndexWhere("status = 'running'")),
	}
}
