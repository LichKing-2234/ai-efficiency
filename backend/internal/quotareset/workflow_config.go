package quotareset

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorydepartment"
	"github.com/ai-efficiency/backend/ent/quotaresetapprovalchain"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	"github.com/ai-efficiency/backend/internal/directorysync"
	"github.com/ai-efficiency/backend/internal/directorytree"
	"github.com/ai-efficiency/backend/internal/relay"
)

const (
	maxApprovalChains           = 100
	maxApprovalChainDepartments = 20
)

type platformGroupLister interface {
	ListPlatformGroups(context.Context) ([]relay.Group, error)
}

func (s *Service) ListApprovalChains(ctx context.Context) (*ApprovalChainListResponse, error) {
	rows, err := s.client.QuotaResetApprovalChain.Query().
		Order(ent.Asc(quotaresetapprovalchain.FieldProviderID), ent.Asc(quotaresetapprovalchain.FieldGroupName)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list quota reset approval chains: %w", err)
	}
	items := make([]ApprovalChain, 0, len(rows))
	for _, row := range rows {
		departments, err := decodeChainDepartments(row.DepartmentChain)
		if err != nil {
			return nil, fmt.Errorf("decode quota reset approval chain %d: %w", row.ID, err)
		}
		items = append(items, ApprovalChain{
			ID:          row.ID,
			ProviderID:  row.ProviderID,
			GroupID:     row.GroupID,
			GroupName:   row.GroupName,
			Enabled:     row.Enabled,
			Departments: departments,
			UpdatedAt:   row.UpdatedAt,
		})
	}
	groups, err := s.approvalChainGroupOptions(ctx)
	if err != nil {
		return nil, err
	}
	return &ApprovalChainListResponse{Items: items, Groups: groups}, nil
}

func (s *Service) SaveApprovalChains(ctx context.Context, actorID int, inputs []ApprovalChainInput) (*ApprovalChainListResponse, error) {
	if len(inputs) > maxApprovalChains {
		return nil, fmt.Errorf("%w: at most %d approval chains are allowed", ErrInvalidApproverConfig, maxApprovalChains)
	}
	groups, err := s.approvalChainGroupOptions(ctx)
	if err != nil {
		return nil, err
	}
	groupByKey := make(map[string]ApprovalChainGroupOption, len(groups))
	for _, group := range groups {
		groupByKey[approvalChainKey(group.ProviderID, group.GroupID)] = group
	}
	sourceID, ok, err := directorysync.CurrentSourceID(ctx, s.client)
	if err != nil {
		return nil, fmt.Errorf("current directory source: %w", err)
	}
	if !ok && len(inputs) > 0 {
		return nil, ErrDirectoryUnavailable
	}
	departments, err := s.client.DirectoryDepartment.Query().Where(directorydepartment.SourceIDEQ(sourceID)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list approval chain departments: %w", err)
	}
	departmentsByID := make(map[string]*ent.DirectoryDepartment, len(departments))
	for _, department := range departments {
		departmentsByID[department.ExternalID] = department
	}
	tree := directorytree.New(departments)
	normalized := make([]ApprovalChainInput, 0, len(inputs))
	seenChains := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		input.GroupID = strings.TrimSpace(input.GroupID)
		key := approvalChainKey(input.ProviderID, input.GroupID)
		group, exists := groupByKey[key]
		if !exists {
			return nil, fmt.Errorf("%w: subscription group %s is not available", ErrInvalidApproverConfig, key)
		}
		if _, duplicate := seenChains[key]; duplicate {
			return nil, fmt.Errorf("%w: duplicate subscription group %s", ErrInvalidApproverConfig, key)
		}
		seenChains[key] = struct{}{}
		if len(input.Departments) > maxApprovalChainDepartments {
			return nil, fmt.Errorf("%w: group %s exceeds %d departments", ErrInvalidApproverConfig, input.GroupID, maxApprovalChainDepartments)
		}
		seenDepartments := make(map[string]struct{}, len(input.Departments))
		cleanDepartments := make([]ChainDepartmentInput, 0, len(input.Departments))
		for _, item := range input.Departments {
			departmentID := strings.TrimSpace(item.DepartmentExternalID)
			department := departmentsByID[departmentID]
			if item.DirectorySourceID != sourceID || department == nil {
				return nil, fmt.Errorf("%w: department %s is not in the current directory", ErrInvalidApproverConfig, departmentID)
			}
			if _, duplicate := seenDepartments[departmentID]; duplicate {
				return nil, fmt.Errorf("%w: duplicate department %s", ErrInvalidApproverConfig, departmentID)
			}
			seenDepartments[departmentID] = struct{}{}
			cleanDepartments = append(cleanDepartments, ChainDepartmentInput{
				DirectorySourceID:     sourceID,
				DepartmentExternalID:  departmentID,
				DepartmentDisplayPath: workflowDepartmentPath(tree, department),
			})
		}
		input.GroupName = group.GroupName
		input.Departments = cleanDepartments
		normalized = append(normalized, input)
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("start approval chain replacement: %w", err)
	}
	if _, err := tx.QuotaResetApprovalChain.Delete().Exec(ctx); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("replace quota reset approval chains: %w", err)
	}
	for _, input := range normalized {
		raw, err := encodeChainDepartments(input.Departments)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		if _, err := tx.QuotaResetApprovalChain.Create().
			SetProviderID(input.ProviderID).
			SetGroupID(input.GroupID).
			SetGroupName(input.GroupName).
			SetDepartmentChain(raw).
			SetEnabled(input.Enabled).
			SetCreatedByUserID(actorID).
			SetUpdatedByUserID(actorID).
			Save(ctx); err != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("create quota reset approval chain: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit quota reset approval chains: %w", err)
	}
	return s.ListApprovalChains(ctx)
}

func (s *Service) approvalChainGroupOptions(ctx context.Context) ([]ApprovalChainGroupOption, error) {
	rows, err := s.client.RelayProvider.Query().
		Where(relayprovider.Enabled(true)).
		Order(ent.Desc(relayprovider.FieldIsPrimary), ent.Asc(relayprovider.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list relay providers for approval chains: %w", err)
	}
	options := make([]ApprovalChainGroupOption, 0)
	for _, row := range rows {
		if s.providerResolver == nil {
			return nil, fmt.Errorf("resolve relay provider %d: provider resolver is not configured", row.ID)
		}
		provider, err := s.providerResolver.Resolve(ctx, row.ID)
		if err != nil {
			return nil, fmt.Errorf("resolve relay provider %d: %w", row.ID, err)
		}
		lister, ok := provider.(platformGroupLister)
		if !ok {
			return nil, fmt.Errorf("relay provider %d does not support group listing", row.ID)
		}
		groups, err := lister.ListPlatformGroups(ctx)
		if err != nil {
			return nil, fmt.Errorf("list relay provider %d groups: %w", row.ID, err)
		}
		for _, group := range groups {
			if group.ID <= 0 || strings.TrimSpace(group.Platform) == "" {
				continue
			}
			if kind := strings.TrimSpace(group.SubscriptionType); !strings.EqualFold(kind, "subscription") {
				continue
			}
			groupID := strconv.FormatInt(group.ID, 10)
			groupName := strings.TrimSpace(group.Name)
			if groupName == "" {
				groupName = groupID
			}
			options = append(options, ApprovalChainGroupOption{
				ProviderID:   row.ID,
				ProviderName: row.DisplayName,
				GroupID:      groupID,
				GroupName:    groupName,
				Platform:     strings.TrimSpace(group.Platform),
			})
		}
	}
	sort.SliceStable(options, func(i, j int) bool {
		if options[i].ProviderName != options[j].ProviderName {
			return options[i].ProviderName < options[j].ProviderName
		}
		if options[i].GroupName != options[j].GroupName {
			return options[i].GroupName < options[j].GroupName
		}
		return options[i].GroupID < options[j].GroupID
	})
	return options, nil
}

func encodeChainDepartments(items []ChainDepartmentInput) ([]map[string]any, error) {
	raw, err := json.Marshal(items)
	if err != nil {
		return nil, fmt.Errorf("encode approval chain departments: %w", err)
	}
	var result []map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("normalize approval chain departments: %w", err)
	}
	return result, nil
}

func decodeChainDepartments(raw []map[string]any) ([]ChainDepartmentInput, error) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var result []ChainDepartmentInput
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, err
	}
	if result == nil {
		result = []ChainDepartmentInput{}
	}
	return result, nil
}

func approvalChainKey(providerID int, groupID string) string {
	return fmt.Sprintf("%d/%s", providerID, strings.TrimSpace(groupID))
}
