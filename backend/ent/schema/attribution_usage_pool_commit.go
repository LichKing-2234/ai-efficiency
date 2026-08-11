package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AttributionUsagePoolCommit projects a globally counted pool onto commits.
// Shared and inherited relations are deliberately non-additive projections.
type AttributionUsagePoolCommit struct{ ent.Schema }

func (AttributionUsagePoolCommit) Fields() []ent.Field {
	return []ent.Field{
		field.Int("pool_id"),
		field.Int("repo_config_id"),
		field.String("commit_sha").NotEmpty(),
		field.Enum("relation_kind").Values("direct", "shared", "inherited_non_counting"),
		field.Time("created_at").Immutable().Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (AttributionUsagePoolCommit) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("pool_id", "repo_config_id", "commit_sha").Unique(),
		index.Fields("repo_config_id", "commit_sha", "pool_id"),
	}
}
