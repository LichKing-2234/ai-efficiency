package relayplanning

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ai-efficiency/backend/internal/relay"
)

// MaxAccountConfigurationCopyTargets bounds one synchronous copy so a single
// request cannot fan out without limit. The upstream merge is a single
// statement, so this bounds request scope rather than upstream capacity.
const MaxAccountConfigurationCopyTargets = 100

// AccountConfigurationItem is one account with its allow-listed configuration
// and, when a baseline was chosen, its distance from that baseline.
type AccountConfigurationItem struct {
	Account       relay.Account                   `json:"account"`
	Configuration relay.AccountConfiguration      `json:"configuration"`
	Diff          *relay.AccountConfigurationDiff `json:"diff,omitempty"`
}

// AccountConfigurationPage is the read-only view an administrator uses to decide
// which accounts still need configuration copied onto them.
type AccountConfigurationPage struct {
	BaselineAccountID int64                       `json:"baseline_account_id"`
	Baseline          *relay.AccountConfiguration `json:"baseline_configuration,omitempty"`
	Items             []AccountConfigurationItem  `json:"items"`
	Total             int                         `json:"total"`
}

type ListAccountConfigurationsRequest struct {
	ProviderID        int    `json:"provider_id"`
	Platform          string `json:"platform"`
	BaselineAccountID int64  `json:"baseline_account_id"`
}

type ApplyAccountConfigurationRequest struct {
	ProviderID        int     `json:"provider_id"`
	Platform          string  `json:"platform"`
	BaselineAccountID int64   `json:"baseline_account_id"`
	TargetAccountIDs  []int64 `json:"target_account_ids"`
}

// AccountConfigurationApplyResult is one target's outcome. Applied is decided by
// the read-back, never by the write response.
type AccountConfigurationApplyResult struct {
	AccountID int64                          `json:"account_id"`
	Applied   bool                           `json:"applied"`
	Error     string                         `json:"error,omitempty"`
	Diff      relay.AccountConfigurationDiff `json:"diff"`
}

type AccountConfigurationApplyResponse struct {
	BaselineAccountID int64                             `json:"baseline_account_id"`
	Results           []AccountConfigurationApplyResult `json:"results"`
}

func (s *Service) ListAccountConfigurations(ctx context.Context, req ListAccountConfigurationsRequest) (*AccountConfigurationPage, error) {
	if req.ProviderID <= 0 || strings.TrimSpace(req.Platform) == "" {
		return nil, fmt.Errorf("provider_id and platform are required")
	}
	provider, err := s.resolver.Resolve(ctx, req.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("resolve relay provider: %w", err)
	}
	accountReader, ok := provider.(relay.AccountRelationshipReader)
	if !ok {
		return nil, fmt.Errorf("relay provider does not support account relationship reading")
	}
	configReader, ok := provider.(relay.AccountConfigurationReader)
	if !ok {
		return nil, fmt.Errorf("relay provider does not support account configuration reading")
	}
	accounts, err := accountReader.ListAccountsForPlatform(ctx, req.Platform)
	if err != nil {
		return nil, fmt.Errorf("list relay accounts: %w", err)
	}
	configurations, err := configReader.ReadAccountConfigurations(ctx, req.Platform)
	if err != nil {
		return nil, fmt.Errorf("read relay account configurations: %w", err)
	}

	page := &AccountConfigurationPage{BaselineAccountID: req.BaselineAccountID, Items: make([]AccountConfigurationItem, 0, len(accounts))}
	var baseline relay.AccountConfiguration
	hasBaseline := false
	if req.BaselineAccountID > 0 {
		baseline, hasBaseline = configurations[req.BaselineAccountID]
		if !hasBaseline {
			return nil, fmt.Errorf("baseline account %d was not found on platform %s", req.BaselineAccountID, req.Platform)
		}
		page.Baseline = &baseline
	}

	for _, account := range accounts {
		if !strings.EqualFold(strings.TrimSpace(account.Platform), strings.TrimSpace(req.Platform)) {
			continue
		}
		item := AccountConfigurationItem{Account: account, Configuration: configurations[account.ID]}
		if hasBaseline {
			diff := relay.CompareAccountConfiguration(baseline, item.Configuration)
			item.Diff = &diff
		}
		page.Items = append(page.Items, item)
	}
	sort.SliceStable(page.Items, func(i, j int) bool {
		left, right := strings.ToLower(page.Items[i].Account.Name), strings.ToLower(page.Items[j].Account.Name)
		if left == right {
			return page.Items[i].Account.ID < page.Items[j].Account.ID
		}
		return left < right
	})
	page.Total = len(page.Items)
	return page, nil
}

// ApplyAccountConfiguration copies one account's allow-listed configuration onto
// the selected targets and reports each target's outcome. The upstream endpoint
// reports success for account IDs that do not exist and does not report partial
// application, so every target is verified by a read-back before it is called
// applied.
func (s *Service) ApplyAccountConfiguration(ctx context.Context, req ApplyAccountConfigurationRequest) (*AccountConfigurationApplyResponse, error) {
	if req.ProviderID <= 0 || strings.TrimSpace(req.Platform) == "" {
		return nil, fmt.Errorf("provider_id and platform are required")
	}
	if req.BaselineAccountID <= 0 {
		return nil, fmt.Errorf("baseline_account_id is required")
	}
	targets := uniquePositiveIDs(req.TargetAccountIDs)
	if len(targets) == 0 {
		return nil, fmt.Errorf("at least one target account is required")
	}
	if len(targets) > MaxAccountConfigurationCopyTargets {
		return nil, fmt.Errorf("at most %d target accounts may be copied to at once", MaxAccountConfigurationCopyTargets)
	}
	for _, id := range targets {
		if id == req.BaselineAccountID {
			return nil, fmt.Errorf("the baseline account cannot also be a target")
		}
	}

	provider, err := s.resolver.Resolve(ctx, req.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("resolve relay provider: %w", err)
	}
	reader, ok := provider.(relay.AccountConfigurationReader)
	if !ok {
		return nil, fmt.Errorf("relay provider does not support account configuration reading")
	}
	applier, ok := provider.(relay.AccountConfigurationApplier)
	if !ok {
		return nil, fmt.Errorf("relay provider does not support account configuration application")
	}

	configurations, err := reader.ReadAccountConfigurations(ctx, req.Platform)
	if err != nil {
		return nil, fmt.Errorf("read relay account configurations: %w", err)
	}
	baseline, present := configurations[req.BaselineAccountID]
	if !present {
		return nil, fmt.Errorf("baseline account %d was not found on platform %s", req.BaselineAccountID, req.Platform)
	}
	if len(baseline.ModelMapping) == 0 && len(baseline.Extra) == 0 {
		return nil, fmt.Errorf("baseline account %d has no allow-listed configuration to copy", req.BaselineAccountID)
	}
	if err := applier.ApplyAccountConfiguration(ctx, targets, baseline); err != nil {
		return nil, fmt.Errorf("apply relay account configuration: %w", err)
	}

	verified, err := reader.ReadAccountConfigurations(ctx, req.Platform)
	if err != nil {
		return nil, fmt.Errorf("verify relay account configurations: %w", err)
	}
	response := &AccountConfigurationApplyResponse{
		BaselineAccountID: req.BaselineAccountID,
		Results:           make([]AccountConfigurationApplyResult, 0, len(targets)),
	}
	for _, id := range targets {
		result := AccountConfigurationApplyResult{AccountID: id}
		after, present := verified[id]
		if !present {
			result.Error = "account was not found when the copy was verified"
			response.Results = append(response.Results, result)
			continue
		}
		result.Diff = relay.CompareAccountConfiguration(baseline, after)
		result.Applied = result.Diff.Matches
		if !result.Applied {
			result.Error = "configuration did not converge on the baseline"
		}
		response.Results = append(response.Results, result)
	}
	return response, nil
}

func uniquePositiveIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
