package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// AiScanResult holds the schema definition for the AiScanResult entity.
type AiScanResult struct {
	ent.Schema
}

// Fields of the AiScanResult.
func (AiScanResult) Fields() []ent.Field {
	return []ent.Field{
		field.Int("score").
			Min(0).
			Max(100).
			Default(0),
		field.JSON("dimensions", map[string]interface{}{}).
			Optional(),
		field.JSON("suggestions", []map[string]interface{}{}).
			Optional(),
		field.Enum("scan_type").
			Values("static", "llm", "full").
			Default("static"),
		field.String("commit_sha").
			Optional().
			Nillable(),
		field.Time("created_at").
			Immutable().
			Default(timeNow),
	}
}

// Edges of the AiScanResult.
func (AiScanResult) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("repo_config", RepoConfig.Type).
			Ref("ai_scan_results").
			Unique().
			Required(),
	}
}
