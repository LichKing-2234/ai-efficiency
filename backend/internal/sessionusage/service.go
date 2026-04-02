package sessionusage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/session"
	"github.com/ai-efficiency/backend/ent/sessionusageevent"
	"github.com/ai-efficiency/backend/ent/user"
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

var (
	ErrInvalidRequest   = errors.New("invalid usage event request")
	ErrSessionNotFound  = errors.New("session not found")
	ErrSessionForbidden = errors.New("session does not belong to authenticated user")
)

func NewService(entClient *ent.Client) *Service {
	return &Service{entClient: entClient}
}

func (s *Service) Create(ctx context.Context, userID int, req CreateUsageEventRequest) error {
	if s.entClient == nil {
		return fmt.Errorf("create usage event: ent client is required")
	}
	if userID <= 0 {
		return fmt.Errorf("create usage event: %w: user_id is required", ErrInvalidRequest)
	}

	eventID := strings.TrimSpace(req.EventID)
	if eventID == "" {
		return fmt.Errorf("create usage event: %w: event_id is required", ErrInvalidRequest)
	}
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return fmt.Errorf("create usage event: %w: workspace_id is required", ErrInvalidRequest)
	}
	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" {
		return fmt.Errorf("create usage event: %w: request_id is required", ErrInvalidRequest)
	}
	providerName := strings.TrimSpace(req.ProviderName)
	if providerName == "" {
		return fmt.Errorf("create usage event: %w: provider_name is required", ErrInvalidRequest)
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		return fmt.Errorf("create usage event: %w: model is required", ErrInvalidRequest)
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		return fmt.Errorf("create usage event: %w: status is required", ErrInvalidRequest)
	}
	if req.StartedAt.IsZero() {
		return fmt.Errorf("create usage event: %w: started_at is required", ErrInvalidRequest)
	}
	if req.FinishedAt.IsZero() {
		return fmt.Errorf("create usage event: %w: finished_at is required", ErrInvalidRequest)
	}
	if req.FinishedAt.Before(req.StartedAt) {
		return fmt.Errorf("create usage event: %w: finished_at must be greater than or equal to started_at", ErrInvalidRequest)
	}
	if req.InputTokens < 0 || req.OutputTokens < 0 || req.TotalTokens < 0 {
		return fmt.Errorf("create usage event: %w: token counts must be non-negative", ErrInvalidRequest)
	}

	sessionID, err := uuid.Parse(strings.TrimSpace(req.SessionID))
	if err != nil {
		return fmt.Errorf("parse session_id: %w", err)
	}

	exists, err := s.entClient.SessionUsageEvent.Query().
		Where(sessionusageevent.EventIDEQ(eventID)).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("create usage event: query event_id: %w", err)
	}
	if exists {
		return nil
	}

	sessionExists, err := s.entClient.Session.Query().
		Where(session.IDEQ(sessionID)).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("create usage event: load session: %w", err)
	}
	if !sessionExists {
		return fmt.Errorf("create usage event: %w", ErrSessionNotFound)
	}

	ownedByCaller, err := s.entClient.Session.Query().
		Where(
			session.IDEQ(sessionID),
			session.HasUserWith(user.IDEQ(userID)),
		).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("create usage event: validate ownership: %w", err)
	}
	if !ownedByCaller {
		return fmt.Errorf("create usage event: %w", ErrSessionForbidden)
	}

	create := s.entClient.SessionUsageEvent.Create().
		SetEventID(eventID).
		SetSessionID(sessionID).
		SetWorkspaceID(workspaceID).
		SetRequestID(requestID).
		SetProviderName(providerName).
		SetModel(model).
		SetStartedAt(req.StartedAt.UTC()).
		SetFinishedAt(req.FinishedAt.UTC()).
		SetInputTokens(req.InputTokens).
		SetOutputTokens(req.OutputTokens).
		SetTotalTokens(req.TotalTokens).
		SetStatus(status)

	if req.RawMetadata != nil {
		create.SetRawMetadata(req.RawMetadata)
	}

	if err := create.Exec(ctx); err != nil {
		if ent.IsConstraintError(err) {
			exists, qerr := s.entClient.SessionUsageEvent.Query().
				Where(sessionusageevent.EventIDEQ(eventID)).
				Exist(ctx)
			if qerr == nil && exists {
				return nil
			}
		}
		return fmt.Errorf("create usage event: %w", err)
	}

	return nil
}
