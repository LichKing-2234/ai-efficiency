package sessionevent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/session"
	"github.com/ai-efficiency/backend/ent/sessionevent"
	"github.com/ai-efficiency/backend/ent/user"
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

var (
	ErrInvalidRequest   = errors.New("invalid session event request")
	ErrSessionNotFound  = errors.New("session not found")
	ErrSessionForbidden = errors.New("session does not belong to authenticated user")
)

func NewService(entClient *ent.Client) *Service {
	return &Service{entClient: entClient}
}

func (s *Service) Create(ctx context.Context, userID int, req CreateSessionEventRequest) error {
	if s.entClient == nil {
		return fmt.Errorf("create session event: ent client is required")
	}
	if userID <= 0 {
		return fmt.Errorf("create session event: %w: user_id is required", ErrInvalidRequest)
	}

	eventID := strings.TrimSpace(req.EventID)
	if eventID == "" {
		return fmt.Errorf("create session event: %w: event_id is required", ErrInvalidRequest)
	}
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return fmt.Errorf("create session event: %w: workspace_id is required", ErrInvalidRequest)
	}
	eventType := strings.TrimSpace(req.EventType)
	if eventType == "" {
		return fmt.Errorf("create session event: %w: event_type is required", ErrInvalidRequest)
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		return fmt.Errorf("create session event: %w: source is required", ErrInvalidRequest)
	}
	if req.CapturedAt.IsZero() {
		return fmt.Errorf("create session event: %w: captured_at is required", ErrInvalidRequest)
	}

	sessionID, err := uuid.Parse(strings.TrimSpace(req.SessionID))
	if err != nil {
		return fmt.Errorf("parse session_id: %w", err)
	}

	exists, err := s.entClient.SessionEvent.Query().
		Where(sessionevent.EventIDEQ(eventID)).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("create session event: query event_id: %w", err)
	}
	if exists {
		return nil
	}

	sessionExists, err := s.entClient.Session.Query().
		Where(session.IDEQ(sessionID)).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("create session event: load session: %w", err)
	}
	if !sessionExists {
		return fmt.Errorf("create session event: %w", ErrSessionNotFound)
	}

	ownedByCaller, err := s.entClient.Session.Query().
		Where(
			session.IDEQ(sessionID),
			session.HasUserWith(user.IDEQ(userID)),
		).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("create session event: validate ownership: %w", err)
	}
	if !ownedByCaller {
		return fmt.Errorf("create session event: %w", ErrSessionForbidden)
	}

	create := s.entClient.SessionEvent.Create().
		SetEventID(eventID).
		SetSessionID(sessionID).
		SetWorkspaceID(workspaceID).
		SetEventType(eventType).
		SetSource(source).
		SetCapturedAt(req.CapturedAt.UTC())

	if req.RawPayload != nil {
		create.SetRawPayload(req.RawPayload)
	}

	if err := create.Exec(ctx); err != nil {
		if ent.IsConstraintError(err) {
			exists, qerr := s.entClient.SessionEvent.Query().
				Where(sessionevent.EventIDEQ(eventID)).
				Exist(ctx)
			if qerr == nil && exists {
				return nil
			}
		}
		return fmt.Errorf("create session event: %w", err)
	}

	return nil
}
