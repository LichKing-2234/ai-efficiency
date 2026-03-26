package analysis

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/aiscanresult"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/internal/analysis/llm"
	"github.com/ai-efficiency/backend/internal/analysis/rules"
	"go.uber.org/zap"
)

// Service coordinates the analysis workflow.
type Service struct {
	entClient     *ent.Client
	cloner        *Cloner
	staticScanner *StaticScanner
	llmAnalyzer   *llm.Analyzer
	logger        *zap.Logger
}

// NewService creates a new analysis service.
func NewService(entClient *ent.Client, cloner *Cloner, llmAnalyzer *llm.Analyzer, logger *zap.Logger) *Service {
	return &Service{
		entClient:     entClient,
		cloner:        cloner,
		staticScanner: NewStaticScanner(),
		llmAnalyzer:   llmAnalyzer,
		logger:        logger,
	}
}

// RunScan clones/updates the repo and runs analysis (static + optional LLM).
func (s *Service) RunScan(ctx context.Context, repoConfigID int) (*ent.AiScanResult, error) {
	// Get repo config
	rc, err := s.entClient.RepoConfig.Get(ctx, repoConfigID)
	if err != nil {
		return nil, fmt.Errorf("get repo config: %w", err)
	}

	// Clone or update
	repoPath, err := s.cloner.CloneOrUpdate(rc.CloneURL, repoConfigID)
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
