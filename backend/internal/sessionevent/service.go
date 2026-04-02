package sessionevent

import (
	"context"
	"fmt"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/google/uuid"
)

type CreateSessionEventRequest struct {
	EventID     string         `json:"event_id" binding:"required"`
	SessionID   string         `json:"session_id" binding:"required"`
	WorkspaceID string         `json:"workspace_id" binding:"required"`
	EventType   string         `json:"event_type" binding:"required"`
	Source      string         `json:"source" binding:"required"`
	CapturedAt  time.Time      `json:"captured_at" binding:"required"`
	RawPayload  map[string]any `json:"raw_payload"`
}

type Service struct {
	entClient *ent.Client
}

func NewService(entClient *ent.Client) *Service {
	return &Service{entClient: entClient}
}

func (s *Service) Create(ctx context.Context, req CreateSessionEventRequest) error {
	if s.entClient == nil {
		return fmt.Errorf("create session event: ent client is required")
	}

	sessionID, err := uuid.Parse(req.SessionID)
	if err != nil {
		return fmt.Errorf("parse session_id: %w", err)
	}

	create := s.entClient.SessionEvent.Create().
		SetEventID(req.EventID).
		SetSessionID(sessionID).
		SetWorkspaceID(req.WorkspaceID).
		SetEventType(req.EventType).
		SetSource(req.Source).
		SetCapturedAt(req.CapturedAt.UTC())

	if req.RawPayload != nil {
		create.SetRawPayload(req.RawPayload)
	}

	if err := create.Exec(ctx); err != nil {
		return fmt.Errorf("create session event: %w", err)
	}

	return nil
}
