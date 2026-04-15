package schema

import (
	"context"
	"fmt"
	"strings"

	entgo "entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	genent "github.com/ai-efficiency/backend/ent"
	genhook "github.com/ai-efficiency/backend/ent/hook"
	repoidentity "github.com/ai-efficiency/backend/internal/repoidentity"
)

// RepoConfig holds the schema definition for the RepoConfig entity.
type RepoConfig struct {
	entgo.Schema
}

// Fields of the RepoConfig.
func (RepoConfig) Fields() []entgo.Field {
	return []entgo.Field{
		field.String("repo_key").
			Optional(),
		field.String("name").
			NotEmpty(),
		field.String("full_name").
			NotEmpty(),
		field.String("clone_url").
			NotEmpty(),
		field.String("default_branch").
			Default("main"),
		field.String("webhook_id").
			Optional().
			Nillable(),
		field.String("webhook_secret").
			Optional().
			Nillable().
			Sensitive(),
		field.Int("ai_score").
			Optional().
			Default(0),
		field.Time("last_scan_at").
			Optional().
			Nillable(),
		field.String("group_id").
			Optional().
			Nillable(),
		field.String("relay_provider_name").
			Optional().
			Nillable(),
		field.String("relay_group_id").
			Optional().
			Nillable(),
		field.Enum("status").
			Values("active", "webhook_failed", "inactive").
			Default("active"),
		field.Time("created_at").
			Immutable().
			Default(timeNow),
		field.Time("updated_at").
			Default(timeNow).
			UpdateDefault(timeNow),
		field.JSON("scan_prompt_override", map[string]string{}).
			Optional(),
	}
}

// Edges of the RepoConfig.
func (RepoConfig) Edges() []entgo.Edge {
	return []entgo.Edge{
		edge.From("scm_provider", ScmProvider.Type).
			Ref("repo_configs").
			Unique(),
		edge.To("sessions", Session.Type),
		edge.To("commit_checkpoints", CommitCheckpoint.Type),
		edge.To("commit_rewrites", CommitRewrite.Type),
		edge.To("webhook_dead_letters", WebhookDeadLetter.Type),
		edge.To("ai_scan_results", AiScanResult.Type),
		edge.To("pr_records", PrRecord.Type),
		edge.To("efficiency_metrics", EfficiencyMetric.Type),
	}
}

// Indexes of the RepoConfig.
func (RepoConfig) Indexes() []entgo.Index {
	return []entgo.Index{
		index.Fields("repo_key").
			Unique(),
		index.Fields("full_name").
			Edges("scm_provider").
			Unique(),
	}
}

func (RepoConfig) Hooks() []genent.Hook {
	return []genent.Hook{
		func(next genent.Mutator) genent.Mutator {
			return genhook.RepoConfigFunc(func(ctx context.Context, m *genent.RepoConfigMutation) (genent.Value, error) {
				if !m.Op().Is(genent.OpCreate | genent.OpUpdateOne | genent.OpUpdate) {
					return next.Mutate(ctx, m)
				}
				if repoKey, ok := m.RepoKey(); ok && strings.TrimSpace(repoKey) != "" {
					return next.Mutate(ctx, m)
				}

				cloneURL, _ := m.CloneURL()
				if strings.TrimSpace(cloneURL) == "" && m.Op().Is(genent.OpUpdateOne) {
					oldCloneURL, err := m.OldCloneURL(ctx)
					if err == nil {
						cloneURL = oldCloneURL
					}
				}

				fullName, _ := m.FullName()
				if strings.TrimSpace(fullName) == "" && m.Op().Is(genent.OpUpdateOne) {
					oldFullName, err := m.OldFullName(ctx)
					if err == nil {
						fullName = oldFullName
					}
				}

				identity, err := repoidentity.DeriveRepoIdentity(strings.TrimSpace(cloneURL))
				if err != nil {
					identity = repoidentity.FallbackRepoIdentity(cloneURL, fullName)
				}
				if strings.TrimSpace(identity.RepoKey) == "" {
					return nil, fmt.Errorf("repoconfig: repo_key is required")
				}
				m.SetRepoKey(identity.RepoKey)
				return next.Mutate(ctx, m)
			})
		},
	}
}
