package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"go.uber.org/zap"
)

const (
	WebhookRepairRegistered        = "registered"
	WebhookRepairAlreadyRegistered = "already_registered"
	WebhookRepairFailed            = "failed"
)

var ErrRepoInactive = errors.New("repo is inactive")

type RepairWebhookRequest struct {
	Force bool `json:"force"`
}

type RepairWebhookSummary struct {
	Scanned           int `json:"scanned"`
	Repaired          int `json:"repaired"`
	AlreadyRegistered int `json:"already_registered"`
	Failed            int `json:"failed"`
}

type RepairWebhookBatchResult struct {
	Summary RepairWebhookSummary  `json:"summary"`
	Items   []RepairWebhookResult `json:"items"`
}

type RepairWebhookResult struct {
	RepoConfigID   int    `json:"repo_config_id"`
	FullName       string `json:"full_name"`
	PreviousStatus string `json:"previous_status"`
	Status         string `json:"status"`
	WebhookStatus  string `json:"webhook_status"`
	WebhookID      string `json:"webhook_id,omitempty"`
	CallbackURL    string `json:"callback_url,omitempty"`
	Error          string `json:"error,omitempty"`
}

func (s *Service) RepairWebhook(ctx context.Context, repoID int, req RepairWebhookRequest) (RepairWebhookResult, error) {
	rc, err := s.entClient.RepoConfig.Query().
		Where(repoconfig.IDEQ(repoID)).
		WithScmProvider(func(query *ent.ScmProviderQuery) {
			query.WithAPICredential()
		}).
		Only(ctx)
	if err != nil {
		return RepairWebhookResult{}, fmt.Errorf("repair webhook: load repo: %w", err)
	}

	result := RepairWebhookResult{
		RepoConfigID:   rc.ID,
		FullName:       rc.FullName,
		PreviousStatus: string(rc.Status),
		Status:         string(rc.Status),
		WebhookStatus:  WebhookRepairFailed,
	}

	provider := rc.Edges.ScmProvider
	if provider == nil {
		return result, ErrRepoUnbound
	}
	if rc.Status == repoconfig.StatusInactive {
		return result, ErrRepoInactive
	}
	if rc.WebhookID != nil && *rc.WebhookID != "" && !req.Force && rc.Status == repoconfig.StatusActive {
		result.WebhookStatus = WebhookRepairAlreadyRegistered
		result.WebhookID = *rc.WebhookID
		return result, nil
	}

	callbackURL, err := s.webhookCallbackURL(string(provider.Type), true)
	if err != nil {
		return result, err
	}
	result.CallbackURL = callbackURL

	apiPayload, err := s.resolveAPICredentialPayload(ctx, provider)
	if err != nil {
		return result, fmt.Errorf("repair webhook: resolve api credential: %w", err)
	}

	scmProvider, err := s.newSCMProviderWithCallback(string(provider.Type), provider.BaseURL, apiPayload, callbackURL)
	if err != nil {
		return result, fmt.Errorf("repair webhook: create scm provider: %w", err)
	}

	repoInfo, err := scmProvider.GetRepo(ctx, rc.FullName)
	if err != nil {
		result.Error = err.Error()
		saveErr := s.mutateInventory(ctx, "save webhook verification failure", func(tx *ent.Tx) error {
			_, updateErr := tx.RepoConfig.UpdateOneID(rc.ID).SetStatus(repoconfig.StatusWebhookFailed).Save(ctx)
			return updateErr
		})
		if saveErr != nil {
			return result, fmt.Errorf("repair webhook: verify repo: %v; save webhook_failed: %w", err, saveErr)
		}
		result.Status = string(repoconfig.StatusWebhookFailed)
		return result, nil
	}

	deletedOldWebhook := false
	if req.Force && rc.WebhookID != nil && *rc.WebhookID != "" {
		if err := scmProvider.DeleteWebhook(ctx, rc.FullName, *rc.WebhookID); err != nil && s.logger != nil {
			s.logger.Warn("failed to delete old webhook before repair", zap.Int("repo_config_id", rc.ID), zap.Error(err))
		} else if err == nil {
			deletedOldWebhook = true
		}
	}

	secret, err := generateSecret(32)
	if err != nil {
		return result, fmt.Errorf("repair webhook: generate secret: %w", err)
	}

	webhookID, err := scmProvider.RegisterWebhook(ctx, repoInfo.FullName, []string{"pull_request", "push"}, secret)
	if err != nil {
		result.Error = err.Error()
		saveErr := s.mutateInventory(ctx, "save webhook registration failure", func(tx *ent.Tx) error {
			update := tx.RepoConfig.UpdateOneID(rc.ID).SetStatus(repoconfig.StatusWebhookFailed)
			if deletedOldWebhook {
				update.ClearWebhookID().ClearWebhookSecret()
			}
			_, updateErr := update.Save(ctx)
			return updateErr
		})
		if saveErr != nil {
			return result, fmt.Errorf("repair webhook: register webhook: %v; save webhook_failed: %w", err, saveErr)
		}
		result.Status = string(repoconfig.StatusWebhookFailed)
		return result, nil
	}

	if err := s.mutateInventory(ctx, "save repaired webhook metadata", func(tx *ent.Tx) error {
		_, saveErr := tx.RepoConfig.UpdateOneID(rc.ID).
			SetName(repoInfo.Name).
			SetFullName(repoInfo.FullName).
			SetCloneURL(repoInfo.CloneURL).
			SetDefaultBranch(repoInfo.DefaultBranch).
			SetWebhookID(webhookID).
			SetWebhookSecret(secret).
			SetStatus(repoconfig.StatusActive).
			Save(ctx)
		return saveErr
	}); err != nil {
		return result, fmt.Errorf("repair webhook: save repo metadata: %w", err)
	}

	result.Status = string(repoconfig.StatusActive)
	result.WebhookStatus = WebhookRepairRegistered
	result.WebhookID = webhookID
	return result, nil
}

func (s *Service) RepairFailedWebhooks(ctx context.Context, req RepairWebhookRequest) (*RepairWebhookBatchResult, error) {
	repos, err := s.entClient.RepoConfig.Query().
		Where(
			repoconfig.HasScmProvider(),
			repoconfig.StatusEQ(repoconfig.StatusWebhookFailed),
		).
		Order(ent.Asc(repoconfig.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("repair failed webhooks: list repos: %w", err)
	}

	batch := &RepairWebhookBatchResult{Items: make([]RepairWebhookResult, 0, len(repos))}
	for _, rc := range repos {
		item, err := s.RepairWebhook(ctx, rc.ID, req)
		if err != nil {
			item = RepairWebhookResult{
				RepoConfigID:   rc.ID,
				FullName:       rc.FullName,
				PreviousStatus: string(rc.Status),
				Status:         string(rc.Status),
				WebhookStatus:  WebhookRepairFailed,
				Error:          err.Error(),
			}
		}
		item.addToSummary(&batch.Summary)
		batch.Items = append(batch.Items, item)
	}
	return batch, nil
}

func (r RepairWebhookResult) addToSummary(summary *RepairWebhookSummary) {
	summary.Scanned++
	switch r.WebhookStatus {
	case WebhookRepairRegistered:
		summary.Repaired++
	case WebhookRepairAlreadyRegistered:
		summary.AlreadyRegistered++
	case WebhookRepairFailed:
		summary.Failed++
	}
}
