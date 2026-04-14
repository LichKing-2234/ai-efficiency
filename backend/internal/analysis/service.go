package analysis

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/aiscanresult"
	entcredential "github.com/ai-efficiency/backend/ent/credential"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	entscmprovider "github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/internal/analysis/llm"
	"github.com/ai-efficiency/backend/internal/analysis/rules"
	"github.com/ai-efficiency/backend/internal/credential"
	"github.com/ai-efficiency/backend/internal/pkg"
	scminternal "github.com/ai-efficiency/backend/internal/scm"
	scmbitbucket "github.com/ai-efficiency/backend/internal/scm/bitbucket"
	scmgithub "github.com/ai-efficiency/backend/internal/scm/github"
	"go.uber.org/zap"
)

// Service coordinates the analysis workflow.
type Service struct {
	entClient     *ent.Client
	cloner        *Cloner
	staticScanner *StaticScanner
	llmAnalyzer   *llm.Analyzer
	encryptionKey string
	logger        *zap.Logger
}

// NewService creates a new analysis service.
func NewService(entClient *ent.Client, cloner *Cloner, llmAnalyzer *llm.Analyzer, logger *zap.Logger, encryptionKeys ...string) *Service {
	encryptionKey := ""
	if len(encryptionKeys) > 0 {
		encryptionKey = encryptionKeys[0]
	}
	return &Service{
		entClient:     entClient,
		cloner:        cloner,
		staticScanner: NewStaticScanner(),
		llmAnalyzer:   llmAnalyzer,
		encryptionKey: encryptionKey,
		logger:        logger,
	}
}

// RunScan clones/updates the repo and runs analysis (static + optional LLM).
func (s *Service) RunScan(ctx context.Context, repoConfigID int) (*ent.AiScanResult, error) {
	// Get repo config
	rc, err := s.entClient.RepoConfig.Query().
		Where(repoconfig.IDEQ(repoConfigID)).
		WithScmProvider(func(query *ent.ScmProviderQuery) {
			query.WithAPICredential()
			query.WithCloneCredential()
		}).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("get repo config: %w", err)
	}

	cloneReq, err := s.buildCloneRequest(ctx, rc)
	if err != nil {
		return nil, fmt.Errorf("build clone request: %w", err)
	}

	// Clone or update
	repoPath, err := s.cloner.CloneOrUpdateWithAuth(cloneReq)
	if err != nil {
		return nil, fmt.Errorf("clone repo: %w", err)
	}

	// Run static scan
	staticResult, err := s.staticScanner.Scan(ctx, repoPath)
	if err != nil {
		return nil, fmt.Errorf("static scan: %w", err)
	}

	allDims := staticResult.Dimensions
	allSuggestions := staticResult.Suggestions
	scanType := aiscanresult.ScanTypeStatic
	totalScore := float64(staticResult.Score)

	// Build scan prompt override from repo config
	var scanOverride *llm.ScanPromptOverride
	if rc.ScanPromptOverride != nil {
		sp, _ := rc.ScanPromptOverride["system_prompt"]
		up, _ := rc.ScanPromptOverride["user_prompt_template"]
		if sp != "" || up != "" {
			scanOverride = &llm.ScanPromptOverride{
				SystemPrompt:       sp,
				UserPromptTemplate: up,
			}
		}
	}

	// Try LLM analysis if configured
	if s.llmAnalyzer != nil && s.llmAnalyzer.Enabled() {
		llmResult, err := s.llmAnalyzer.Analyze(ctx, repoPath, scanOverride)
		if err != nil {
			s.logger.Warn("LLM analysis failed, using static only", zap.Error(err))
		} else {
			allDims = append(allDims, llmResult.Dimensions...)
			allSuggestions = append(allSuggestions, llmResult.Suggestions...)
			totalScore += llmResult.TotalScore
			scanType = aiscanresult.ScanTypeFull
		}
	}

	finalScore := int(math.Round(totalScore))
	if finalScore > 100 {
		finalScore = 100
	}

	// Save scan result
	scanResult, err := s.saveScanResult(ctx, repoConfigID, finalScore, allDims, allSuggestions, scanType)
	if err != nil {
		return nil, err
	}

	// Update repo's ai_score and last_scan_at
	now := time.Now()
	if err := s.entClient.RepoConfig.UpdateOneID(repoConfigID).
		SetAiScore(finalScore).
		SetLastScanAt(now).
		Exec(ctx); err != nil {
		s.logger.Warn("failed to update repo ai_score", zap.Error(err))
	}

	return scanResult, nil
}

func (s *Service) buildCloneRequest(ctx context.Context, rc *ent.RepoConfig) (CloneRequest, error) {
	req := CloneRequest{
		RepoConfigID: rc.ID,
	}

	provider := rc.Edges.ScmProvider
	if provider == nil {
		return req, nil
	}
	req.ProviderID = provider.ID

	apiPayload, err := s.resolveCredentialPayload(ctx, provider.Edges.APICredential, provider.APICredentialID, provider.Credentials, true)
	if err != nil {
		return CloneRequest{}, err
	}

	var clonePayload credential.Payload
	if provider.CloneCredentialID != nil && *provider.CloneCredentialID != 0 {
		clonePayload, err = s.resolveCredentialPayload(ctx, provider.Edges.CloneCredential, *provider.CloneCredentialID, "", false)
		if err != nil {
			return CloneRequest{}, err
		}
	}

	cloneProtocol := provider.CloneProtocol.String()
	if cloneProtocol == "" {
		cloneProtocol = "https"
	}
	req.CloneURL = rc.CloneURL
	if cloneProtocol == "https" {
		cloneURL, err := s.resolveHTTPSCloneURL(ctx, provider, apiPayload, rc)
		if err != nil {
			return CloneRequest{}, err
		}
		req.CloneURL = cloneURL
	}
	authCfg, err := scminternal.BuildCloneAuthConfig(provider.Type, cloneProtocol, apiPayload, clonePayload)
	if err != nil {
		return CloneRequest{}, err
	}
	req.Auth = authCfg
	return req, nil
}

func (s *Service) resolveCredentialPayload(ctx context.Context, edge *ent.Credential, credentialID int, legacyEncrypted string, allowLegacy bool) (credential.Payload, error) {
	cred := edge
	if cred == nil && credentialID != 0 {
		row, err := s.entClient.Credential.Get(ctx, credentialID)
		if err != nil {
			return nil, fmt.Errorf("get credential %d: %w", credentialID, err)
		}
		cred = row
	}
	if cred == nil {
		if allowLegacy && legacyEncrypted != "" {
			raw, err := pkg.Decrypt(legacyEncrypted, s.encryptionKey)
			if err != nil {
				return nil, fmt.Errorf("decrypt legacy provider credentials: %w", err)
			}
			payload, err := credential.ParseLegacySCMProviderSecret(raw)
			if err != nil {
				return nil, fmt.Errorf("parse legacy provider credentials: %w", err)
			}
			return payload, nil
		}
		return nil, fmt.Errorf("missing credential")
	}

	raw, err := pkg.Decrypt(cred.Payload, s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt credential %d: %w", cred.ID, err)
	}
	payload, err := credential.ParsePayload(credential.Kind(cred.Kind), []byte(raw))
	if err != nil {
		return nil, fmt.Errorf("parse credential %d payload: %w", cred.ID, err)
	}
	if allowLegacy && cred.Kind == entcredential.KindSSHUsernameWithPrivateKey {
		return nil, fmt.Errorf("api credential cannot be ssh_username_with_private_key")
	}
	return payload, nil
}

func (s *Service) resolveHTTPSCloneURL(ctx context.Context, provider *ent.ScmProvider, apiPayload credential.Payload, rc *ent.RepoConfig) (string, error) {
	scmProvider, err := s.newSCMProviderForLookup(provider.Type, provider.BaseURL, apiPayload)
	if err != nil {
		return rc.CloneURL, nil
	}
	repoInfo, err := scmProvider.GetRepo(ctx, rc.FullName)
	if err != nil {
		return rc.CloneURL, nil
	}
	if repoInfo == nil || repoInfo.CloneURL == "" {
		return rc.CloneURL, nil
	}
	return repoInfo.CloneURL, nil
}

func (s *Service) newSCMProviderForLookup(providerType entscmprovider.Type, baseURL string, apiPayload credential.Payload) (scminternal.SCMProvider, error) {
	secret, err := credential.ResolveAPISecret(apiPayload)
	if err != nil {
		return nil, err
	}

	switch providerType {
	case "github":
		return scmgithub.New(baseURL, secret, s.logger)
	case "bitbucket_server":
		return scmbitbucket.New(baseURL, secret, s.logger)
	default:
		return nil, fmt.Errorf("unsupported provider type: %s", providerType)
	}
}

func (s *Service) saveScanResult(
	ctx context.Context,
	repoConfigID int,
	score int,
	dims []rules.DimensionScore,
	suggestions []rules.Suggestion,
	scanType aiscanresult.ScanType,
) (*ent.AiScanResult, error) {
	dimMap := make(map[string]interface{})
	for _, d := range dims {
		dimMap[d.Name] = map[string]interface{}{
			"score":     d.Score,
			"max_score": d.MaxScore,
			"details":   d.Details,
		}
	}

	sugList := make([]map[string]interface{}, len(suggestions))
	for i, sg := range suggestions {
		sugList[i] = map[string]interface{}{
			"category": sg.Category,
			"message":  sg.Message,
			"priority": sg.Priority,
			"auto_fix": sg.AutoFix,
		}
	}

	scanResult, err := s.entClient.AiScanResult.Create().
		SetRepoConfigID(repoConfigID).
		SetScore(score).
		SetDimensions(dimMap).
		SetSuggestions(sugList).
		SetScanType(scanType).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("save scan result: %w", err)
	}

	return scanResult, nil
}

// GetLatestScan returns the most recent scan result for a repo.
func (s *Service) GetLatestScan(ctx context.Context, repoConfigID int) (*ent.AiScanResult, error) {
	return s.entClient.AiScanResult.Query().
		Where(aiscanresult.HasRepoConfigWith(repoconfig.IDEQ(repoConfigID))).
		Order(ent.Desc(aiscanresult.FieldCreatedAt)).
		First(ctx)
}

// ListScans returns scan history for a repo.
func (s *Service) ListScans(ctx context.Context, repoConfigID int, limit int) ([]*ent.AiScanResult, error) {
	if limit <= 0 {
		limit = 20
	}
	return s.entClient.AiScanResult.Query().
		Where(aiscanresult.HasRepoConfigWith(repoconfig.IDEQ(repoConfigID))).
		Order(ent.Desc(aiscanresult.FieldCreatedAt)).
		Limit(limit).
		All(ctx)
}
