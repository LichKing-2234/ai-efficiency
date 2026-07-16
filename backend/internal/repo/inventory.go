package repo

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/ent/scmprovider"
)

type inventoryAggregateRow struct {
	ProviderID         *int   `sql:"provider_id"`
	ProviderName       string `sql:"provider_name"`
	ProviderType       string `sql:"provider_type"`
	ProviderBaseURL    string `sql:"provider_base_url"`
	Scope              string `sql:"scope"`
	TotalRepos         int    `sql:"total_repos"`
	BoundRepos         int    `sql:"bound_repos"`
	UnboundRepos       int    `sql:"unbound_repos"`
	ActiveRepos        int    `sql:"active_repos"`
	WebhookFailedRepos int    `sql:"webhook_failed_repos"`
}

// loadInventory is the authoritative bounded inventory read. It returns one
// row per provider and repository scope instead of materializing repositories.
func (s *Service) loadInventory(ctx context.Context) ([]InventoryProviderSummary, error) {
	providerTable := sql.Table(scmprovider.Table)
	var scopeExpression string
	rows := make([]inventoryAggregateRow, 0)
	err := s.entClient.RepoConfig.Query().
		Aggregate(
			func(selector *sql.Selector) string {
				selector.LeftJoin(providerTable).
					On(selector.C(repoconfig.ScmProviderColumn), providerTable.C(scmprovider.FieldID))
				scopeExpression = inventoryScopeExpression(selector.Dialect(), selector.C(repoconfig.FieldFullName))
				selector.GroupBy(
					providerTable.C(scmprovider.FieldID),
					providerTable.C(scmprovider.FieldName),
					providerTable.C(scmprovider.FieldType),
					providerTable.C(scmprovider.FieldBaseURL),
					scopeExpression,
				)
				return sql.As(providerTable.C(scmprovider.FieldID), "provider_id")
			},
			func(*sql.Selector) string {
				return sql.As(fmt.Sprintf("COALESCE(%s, '')", providerTable.C(scmprovider.FieldName)), "provider_name")
			},
			func(*sql.Selector) string {
				return sql.As(fmt.Sprintf("COALESCE(%s, '')", providerTable.C(scmprovider.FieldType)), "provider_type")
			},
			func(*sql.Selector) string {
				return sql.As(fmt.Sprintf("COALESCE(%s, '')", providerTable.C(scmprovider.FieldBaseURL)), "provider_base_url")
			},
			func(*sql.Selector) string {
				return sql.As(scopeExpression, "scope")
			},
			func(selector *sql.Selector) string {
				return sql.As(sql.Count(selector.C(repoconfig.FieldID)), "total_repos")
			},
			func(*sql.Selector) string {
				return inventorySumCase(providerTable.C(scmprovider.FieldID)+" IS NOT NULL", "bound_repos")
			},
			func(*sql.Selector) string {
				return inventorySumCase(providerTable.C(scmprovider.FieldID)+" IS NULL", "unbound_repos")
			},
			func(selector *sql.Selector) string {
				condition := fmt.Sprintf("%s = '%s'", selector.C(repoconfig.FieldStatus), repoconfig.StatusActive)
				return inventorySumCase(condition, "active_repos")
			},
			func(selector *sql.Selector) string {
				condition := fmt.Sprintf("%s = '%s'", selector.C(repoconfig.FieldStatus), repoconfig.StatusWebhookFailed)
				return inventorySumCase(condition, "webhook_failed_repos")
			},
		).
		Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("aggregate repo inventory: %w", err)
	}

	return foldInventoryRows(rows), nil
}

func inventoryScopeExpression(dialectName, fullNameColumn string) string {
	trimmed := fmt.Sprintf("TRIM(%s)", fullNameColumn)
	switch dialectName {
	case dialect.Postgres:
		return fmt.Sprintf(
			"CASE WHEN %s = '' THEN 'unknown' WHEN STRPOS(%s, '/') > 1 THEN SPLIT_PART(%s, '/', 1) ELSE %s END",
			trimmed, trimmed, trimmed, trimmed,
		)
	case dialect.MySQL:
		return fmt.Sprintf(
			"CASE WHEN %s = '' THEN 'unknown' WHEN LOCATE('/', %s) > 1 THEN SUBSTRING_INDEX(%s, '/', 1) ELSE %s END",
			trimmed, trimmed, trimmed, trimmed,
		)
	default:
		return fmt.Sprintf(
			"CASE WHEN %s = '' THEN 'unknown' WHEN INSTR(%s, '/') > 1 THEN SUBSTR(%s, 1, INSTR(%s, '/') - 1) ELSE %s END",
			trimmed, trimmed, trimmed, trimmed, trimmed,
		)
	}
}

func inventorySumCase(condition, alias string) string {
	return sql.As(fmt.Sprintf("SUM(CASE WHEN %s THEN 1 ELSE 0 END)", condition), alias)
}

func foldInventoryRows(rows []inventoryAggregateRow) []InventoryProviderSummary {
	type providerAccumulator struct {
		summary InventoryProviderSummary
		scopes  []InventoryScopeSummary
	}
	providers := make(map[string]*providerAccumulator)

	for _, row := range rows {
		key := "unbound"
		provider := InventoryProviderSummary{
			ProviderKey: "unbound",
			Name:        "Needs platform binding",
			Type:        "unbound",
		}
		if row.ProviderID != nil {
			providerID := *row.ProviderID
			key = inventoryProviderKey(providerID)
			provider = InventoryProviderSummary{
				ProviderKey: key,
				ProviderID:  &providerID,
				Name:        row.ProviderName,
				Type:        row.ProviderType,
				BaseURL:     row.ProviderBaseURL,
			}
		}

		acc, ok := providers[key]
		if !ok {
			acc = &providerAccumulator{summary: provider}
			providers[key] = acc
		}
		acc.summary.TotalRepos += row.TotalRepos
		acc.summary.BoundRepos += row.BoundRepos
		acc.summary.UnboundRepos += row.UnboundRepos
		acc.summary.ActiveRepos += row.ActiveRepos
		acc.summary.WebhookFailedRepos += row.WebhookFailedRepos
		acc.scopes = append(acc.scopes, InventoryScopeSummary{
			Scope:              row.Scope,
			TotalRepos:         row.TotalRepos,
			BoundRepos:         row.BoundRepos,
			UnboundRepos:       row.UnboundRepos,
			ActiveRepos:        row.ActiveRepos,
			WebhookFailedRepos: row.WebhookFailedRepos,
		})
	}

	items := make([]InventoryProviderSummary, 0, len(providers))
	for _, acc := range providers {
		sort.Slice(acc.scopes, func(i, j int) bool {
			return acc.scopes[i].Scope < acc.scopes[j].Scope
		})
		acc.summary.Scopes = acc.scopes
		items = append(items, acc.summary)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].ProviderKey == "unbound" {
			return false
		}
		if items[j].ProviderKey == "unbound" {
			return true
		}
		if items[i].Name == items[j].Name {
			return items[i].ProviderKey < items[j].ProviderKey
		}
		return items[i].Name < items[j].Name
	})
	return items
}

func inventoryProviderKey(providerID int) string {
	return fmt.Sprintf("scm_provider:%d", providerID)
}

func hasExplicitInventorySelection(opts ListOpts) bool {
	return opts.SCMProviderID > 0 || strings.TrimSpace(opts.Scope) != "" || strings.TrimSpace(opts.BindingState) != ""
}

func defaultListSelection(inventory []InventoryProviderSummary) *ListSelection {
	bound := make([]InventoryProviderSummary, 0, len(inventory))
	var unbound *InventoryProviderSummary
	for i := range inventory {
		provider := inventory[i]
		if provider.ProviderKey == "unbound" {
			unbound = &provider
			continue
		}
		if provider.ProviderID != nil && provider.BoundRepos > 0 {
			bound = append(bound, provider)
		}
	}
	sort.Slice(bound, func(i, j int) bool {
		leftRank := defaultProviderTypeRank(bound[i].Type)
		rightRank := defaultProviderTypeRank(bound[j].Type)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if bound[i].Name != bound[j].Name {
			return bound[i].Name < bound[j].Name
		}
		return *bound[i].ProviderID < *bound[j].ProviderID
	})
	for _, provider := range bound {
		for _, scope := range provider.Scopes {
			if scope.BoundRepos == 0 {
				continue
			}
			providerID := *provider.ProviderID
			return &ListSelection{
				ProviderKey:  provider.ProviderKey,
				ProviderID:   &providerID,
				ProviderName: provider.Name,
				ProviderType: provider.Type,
				Scope:        scope.Scope,
				BindingState: "bound",
			}
		}
	}
	if unbound != nil {
		for _, scope := range unbound.Scopes {
			if scope.UnboundRepos == 0 {
				continue
			}
			return &ListSelection{
				ProviderKey:  "unbound",
				ProviderName: unbound.Name,
				ProviderType: unbound.Type,
				Scope:        scope.Scope,
				BindingState: "unbound",
			}
		}
	}
	return nil
}

func defaultProviderTypeRank(providerType string) int {
	switch strings.ToLower(strings.TrimSpace(providerType)) {
	case "github":
		return 0
	case "bitbucket_server":
		return 1
	default:
		return 2
	}
}
