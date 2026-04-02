package sessionusage

import (
	"context"
	"fmt"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/google/uuid"
)

type CreateUsageEventRequest struct {
	EventID      string         `json:"event_id" binding:"required"`
	SessionID    string         `json:"session_id" binding:"required"`
	WorkspaceID  string         `json:"workspace_id" binding:"required"`
	RequestID    string         `json:"request_id" binding:"required"`
	ProviderName string         `json:"provider_name" binding:"required"`
	Model        string         `json:"model" binding:"required"`
	StartedAt    time.Time      `json:"started_at" binding:"required"`
	FinishedAt   time.Time      `json:"finished_at" binding:"required"`
	InputTokens  int64          `json:"input_tokens"`
	OutputTokens int64          `json:"output_tokens"`
	TotalTokens  int64          `json:"total_tokens"`
	Status       string         `json:"status" binding:"required"`
	RawMetadata  map[string]any `json:"raw_metadata"`
}

type Service struct {
	entClient *ent.Client
}

func NewService(entClient *ent.Client) *Service {
	return &Service{entClient: entClient}
}

func (s *Service) Create(ctx context.Context, req CreateUsageEventRequest) error {
	if s.entClient == nil {
		return fmt.Errorf("create usage event: ent client is required")
	}

	sessionID, err := uuid.Parse(req.SessionID)
	if err != nil {
		return fmt.Errorf("parse session_id: %w", err)
	}

	create := s.entClient.SessionUsageEvent.Create().
		SetEventID(req.EventID).
		SetSessionID(sessionID).
		SetWorkspaceID(req.WorkspaceID).
		SetRequestID(req.RequestID).
		SetProviderName(req.ProviderName).
		SetModel(req.Model).
		SetStartedAt(req.StartedAt.UTC()).
		SetFinishedAt(req.FinishedAt.UTC()).
		SetInputTokens(req.InputTokens).
		SetOutputTokens(req.OutputTokens).
		SetTotalTokens(req.TotalTokens).
		SetStatus(req.Status)

	if req.RawMetadata != nil {
		create.SetRawMetadata(req.RawMetadata)
	}

	if err := create.Exec(ctx); err != nil {
		return fmt.Errorf("create usage event: %w", err)
	}

	return nil
}
