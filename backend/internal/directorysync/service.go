package directorysync

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorydepartment"
	"github.com/ai-efficiency/backend/ent/directorymember"
	"github.com/ai-efficiency/backend/ent/directoryoffboardingaction"
	"github.com/ai-efficiency/backend/ent/directorysource"
	"github.com/ai-efficiency/backend/ent/directorysyncrun"
	"github.com/ai-efficiency/backend/ent/predicate"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/relay"
)

const offboardingReasonMissingFromDirectory = "missing_from_latest_full_company_directory"

type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

type ConflictError struct {
	Message string
}

func (e *ConflictError) Error() string { return e.Message }

type UpstreamError struct {
	Message string
	Err     error
}

func (e *UpstreamError) Error() string {
	if e.Err == nil {
		return e.Message
	}
	return e.Message + ": " + e.Err.Error()
}

func (e *UpstreamError) Unwrap() error { return e.Err }

type TokenRevoker interface {
	RevokeUserTokens(ctx context.Context, userID int, revokedAt time.Time) error
}

type RelayDisablerResolver interface {
	ResolveRelayDisabler(ctx context.Context, providerID int) (relay.UserDisabler, error)
}

type ServiceOptions struct {
	Executor       *Executor
	Credentials    CredentialResolver
	RelayDisablers RelayDisablerResolver
	TokenRevoker   TokenRevoker
	Now            func() time.Time
}

type Service struct {
	client         *ent.Client
	executor       *Executor
	credentials    CredentialResolver
	relayDisablers RelayDisablerResolver
	tokenRevoker   TokenRevoker
	now            func() time.Time
	runningMu      sync.Mutex
	runningSources map[int]struct{}
}

func NewService(client *ent.Client, options ServiceOptions) *Service {
	executor := options.Executor
	if executor == nil {
		executor = NewExecutor(ExecutorOptions{})
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		client:         client,
		executor:       executor,
		credentials:    options.Credentials,
		relayDisablers: options.RelayDisablers,
		tokenRevoker:   options.TokenRevoker,
		now:            now,
		runningSources: make(map[int]struct{}),
	}
}

type SourceInput struct {
	Name             string
	Description      string
	Scope            string
	Enabled          bool
	DSL              string
	ScheduleEnabled  bool
	ScheduleInterval string
	ScheduleTimezone string
}

func (s *Service) ListSources(ctx context.Context) ([]*ent.DirectorySource, error) {
	return s.client.DirectorySource.Query().
		Where(directorysource.DeletedEQ(false)).
		Order(ent.Asc(directorysource.FieldID)).
		All(ctx)
}

func (s *Service) CreateSource(ctx context.Context, input SourceInput) (*ent.DirectorySource, error) {
	input = normalizeSourceInput(input)
	if _, err := ParseDSL(input.DSL); err != nil {
		return nil, err
	}
	return s.client.DirectorySource.Create().
		SetName(input.Name).
		SetDescription(input.Description).
		SetScope(directorysource.Scope(input.Scope)).
		SetEnabled(input.Enabled).
		SetDsl(input.DSL).
		SetScheduleEnabled(input.ScheduleEnabled).
		SetScheduleInterval(directorysource.ScheduleInterval(input.ScheduleInterval)).
		SetScheduleTimezone(input.ScheduleTimezone).
		Save(ctx)
}

func (s *Service) UpdateSource(ctx context.Context, id int, input SourceInput) (*ent.DirectorySource, error) {
	input = normalizeSourceInput(input)
	if _, err := ParseDSL(input.DSL); err != nil {
		return nil, err
	}
	return s.client.DirectorySource.UpdateOneID(id).
		SetName(input.Name).
		SetDescription(input.Description).
		SetScope(directorysource.Scope(input.Scope)).
		SetEnabled(input.Enabled).
		SetDsl(input.DSL).
		SetScheduleEnabled(input.ScheduleEnabled).
		SetScheduleInterval(directorysource.ScheduleInterval(input.ScheduleInterval)).
		SetScheduleTimezone(input.ScheduleTimezone).
		Save(ctx)
}

func (s *Service) DeleteSource(ctx context.Context, id int) error {
	_, err := s.client.DirectorySource.UpdateOneID(id).
		SetDeleted(true).
		SetEnabled(false).
		SetScheduleEnabled(false).
		Save(ctx)
	return err
}

func normalizeSourceInput(input SourceInput) SourceInput {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.Scope = strings.TrimSpace(input.Scope)
	if input.Scope == "" {
		input.Scope = "full_company"
	}
	input.DSL = strings.TrimSpace(input.DSL)
	input.ScheduleInterval = strings.TrimSpace(input.ScheduleInterval)
	if input.ScheduleInterval == "" {
		input.ScheduleInterval = "daily"
	}
	input.ScheduleTimezone = strings.TrimSpace(input.ScheduleTimezone)
	if input.ScheduleTimezone == "" {
		input.ScheduleTimezone = "UTC"
	}
	return input
}

func (s *Service) ValidateSource(ctx context.Context, sourceID int) ([]ValidationIssue, error) {
	source, err := s.client.DirectorySource.Get(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	cfg, err := ParseDSL(source.Dsl)
	if err != nil {
		return nil, err
	}
	return s.validateConfig(ctx, cfg), nil
}

func (s *Service) RunSource(ctx context.Context, sourceID int, mode, trigger string) (*ent.DirectorySyncRun, error) {
	mode = strings.TrimSpace(mode)
	trigger = strings.TrimSpace(trigger)
	if mode == "" {
		mode = "apply"
	}
	if trigger == "" {
		trigger = "manual"
	}
	run, err := s.client.DirectorySyncRun.Create().
		SetSourceID(sourceID).
		SetMode(directorysyncrun.Mode(mode)).
		SetTrigger(directorysyncrun.Trigger(trigger)).
		SetStatus(directorysyncrun.StatusQueued).
		SetPhase(directorysyncrun.PhaseValidating).
		SetWarnings([]map[string]any{}).
		SetSummary(map[string]any{}).
		SetPreviewDiff(map[string]any{}).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return s.ExecuteRun(ctx, run.ID)
}

func (s *Service) ExecuteRun(ctx context.Context, runID int) (*ent.DirectorySyncRun, error) {
	run, err := s.client.DirectorySyncRun.Get(ctx, runID)
	if err != nil {
		return nil, err
	}
	source, err := s.client.DirectorySource.Get(ctx, run.SourceID)
	if err != nil {
		return s.failRun(ctx, run.ID, "load source: "+err.Error())
	}
	cfg, err := ParseDSL(source.Dsl)
	if err != nil {
		return s.failRun(ctx, run.ID, "parse dsl: "+err.Error())
	}
	if issues := s.validateConfig(ctx, cfg); len(issues) > 0 {
		return s.failRun(ctx, run.ID, validationIssuesMessage(issues))
	}

	startedAt := s.now()
	run, err = s.client.DirectorySyncRun.UpdateOneID(run.ID).
		SetStatus(directorysyncrun.StatusRunning).
		SetPhase(directorysyncrun.PhaseExecuting).
		SetStartedAt(startedAt).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	if run.Mode == directorysyncrun.ModeValidate {
		return s.completeRun(ctx, run.ID, nil, nil)
	}

	result, err := s.executor.Execute(ctx, cfg, s.credentials)
	if err != nil {
		return s.failRun(ctx, run.ID, err.Error())
	}
	if run.Mode == directorysyncrun.ModeApply {
		if _, err := s.client.DirectorySyncRun.UpdateOneID(run.ID).
			SetPhase(directorysyncrun.PhaseApplying).
			Save(ctx); err != nil {
			return nil, err
		}
		if err := s.replaceFacts(ctx, source.ID, run.ID, result); err != nil {
			return s.failRun(ctx, run.ID, "apply facts: "+err.Error())
		}
	}
	return s.completeRun(ctx, run.ID, source, result)
}

func (s *Service) validateConfig(ctx context.Context, cfg *DSL) []ValidationIssue {
	issues := ValidateDSL(ctx, cfg, func(ctx context.Context, ref string) bool {
		if s.credentials == nil {
			return false
		}
		_, ok, err := s.credentials.ResolveCredential(ctx, ref)
		return err == nil && ok
	})
	if s.executor != nil && s.executor.allowHTTP {
		filtered := issues[:0]
		for _, issue := range issues {
			if strings.HasSuffix(issue.Path, ".request.url") && strings.Contains(issue.Message, "https") {
				continue
			}
			filtered = append(filtered, issue)
		}
		issues = filtered
	}
	return issues
}

func validationIssuesMessage(issues []ValidationIssue) string {
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		parts = append(parts, issue.Path+": "+issue.Message)
	}
	return strings.Join(parts, "; ")
}

func (s *Service) failRun(ctx context.Context, runID int, message string) (*ent.DirectorySyncRun, error) {
	completedAt := s.now()
	run, err := s.client.DirectorySyncRun.UpdateOneID(runID).
		SetStatus(directorysyncrun.StatusFailed).
		SetPhase(directorysyncrun.PhaseFailed).
		SetErrorMessage(message).
		SetCompletedAt(completedAt).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return run, fmt.Errorf(message)
}

func (s *Service) completeRun(ctx context.Context, runID int, source *ent.DirectorySource, result *ExecutionResult) (*ent.DirectorySyncRun, error) {
	completedAt := s.now()
	update := s.client.DirectorySyncRun.UpdateOneID(runID).
		SetPhase(directorysyncrun.PhaseCompleted).
		SetCompletedAt(completedAt)
	if result == nil {
		update.SetStatus(directorysyncrun.StatusCompleted)
	} else {
		status := directorysyncrun.StatusCompleted
		if len(result.Warnings) > 0 {
			status = directorysyncrun.StatusCompletedWithWarnings
		}
		update.SetStatus(status).
			SetHTTPRequestCount(result.HTTPRequestCount).
			SetDepartmentCount(len(result.Departments)).
			SetMemberCount(len(result.Members)).
			SetWarningCount(len(result.Warnings)).
			SetWarnings(warningsToMaps(result.Warnings)).
			SetSummary(map[string]any{
				"departments": len(result.Departments),
				"members":     len(result.Members),
				"warnings":    len(result.Warnings),
			}).
			SetPreviewDiff(map[string]any{
				"departments": len(result.Departments),
				"members":     len(result.Members),
				"warnings":    len(result.Warnings),
			})
	}
	run, err := update.Save(ctx)
	if err != nil {
		return nil, err
	}
	if source != nil && run.Mode == directorysyncrun.ModeApply {
		_, err = s.client.DirectorySource.UpdateOneID(source.ID).
			SetLastRunID(run.ID).
			SetLastSuccessfulRunID(run.ID).
			Save(ctx)
		if err != nil {
			return nil, err
		}
	} else if source != nil {
		_, _ = s.client.DirectorySource.UpdateOneID(source.ID).SetLastRunID(run.ID).Save(ctx)
	}
	return run, nil
}

func warningsToMaps(warnings []ExecutionWarning) []map[string]any {
	out := make([]map[string]any, 0, len(warnings))
	for _, warning := range warnings {
		out = append(out, map[string]any{
			"code":    warning.Code,
			"message": warning.Message,
			"step_id": warning.StepID,
		})
	}
	return out
}

func (s *Service) replaceFacts(ctx context.Context, sourceID, runID int, result *ExecutionResult) error {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.DirectoryDepartment.Delete().Where(directorydepartment.SourceIDEQ(sourceID)).Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.DirectoryMember.Delete().Where(directorymember.SourceIDEQ(sourceID)).Exec(ctx); err != nil {
		return err
	}
	for _, department := range result.Departments {
		create := tx.DirectoryDepartment.Create().
			SetSourceID(sourceID).
			SetExternalID(department.ExternalID).
			SetName(department.Name).
			SetPath(department.Path).
			SetMetadata(department.Metadata).
			SetLastSeenRunID(runID)
		if strings.TrimSpace(department.ParentExternalID) != "" {
			create.SetParentExternalID(department.ParentExternalID)
		}
		if _, err := create.Save(ctx); err != nil {
			return err
		}
	}
	for _, member := range result.Members {
		create := tx.DirectoryMember.Create().
			SetSourceID(sourceID).
			SetExternalID(member.ExternalID).
			SetEmailNormalized(member.EmailNormalized).
			SetDisplayName(member.DisplayName).
			SetDepartmentExternalID(member.DepartmentExternalID).
			SetStatus(member.Status).
			SetMetadata(member.Metadata).
			SetLastSeenRunID(runID)
		if matched, err := tx.User.Query().Where(entuser.EmailEqualFold(member.EmailNormalized)).Only(ctx); err == nil {
			create.SetMatchedUserID(matched.ID)
		} else if !ent.IsNotFound(err) {
			return err
		}
		if _, err := create.Save(ctx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Service) GetRun(ctx context.Context, runID int) (*ent.DirectorySyncRun, error) {
	return s.client.DirectorySyncRun.Get(ctx, runID)
}

func (s *Service) ListRuns(ctx context.Context, sourceID int) ([]*ent.DirectorySyncRun, error) {
	return s.client.DirectorySyncRun.Query().
		Where(directorysyncrun.SourceIDEQ(sourceID)).
		Order(ent.Desc(directorysyncrun.FieldCreatedAt)).
		All(ctx)
}

func (s *Service) ListDepartments(ctx context.Context, sourceID int, q string) ([]*ent.DirectoryDepartment, error) {
	query := s.client.DirectoryDepartment.Query().Where(directorydepartment.SourceIDEQ(sourceID))
	if strings.TrimSpace(q) != "" {
		query = query.Where(directorydepartment.Or(
			directorydepartment.NameContainsFold(q),
			directorydepartment.ExternalIDContainsFold(q),
			directorydepartment.PathContainsFold(q),
		))
	}
	return query.Order(ent.Asc(directorydepartment.FieldName)).All(ctx)
}

func (s *Service) ListMembers(ctx context.Context, sourceID int, q string) ([]*ent.DirectoryMember, error) {
	query := s.client.DirectoryMember.Query().Where(directorymember.SourceIDEQ(sourceID))
	if strings.TrimSpace(q) != "" {
		query = query.Where(directorymember.Or(
			directorymember.EmailNormalizedContainsFold(q),
			directorymember.DisplayNameContainsFold(q),
			directorymember.DepartmentExternalIDContainsFold(q),
		))
	}
	return query.Order(ent.Asc(directorymember.FieldEmailNormalized)).All(ctx)
}

type OffboardingCandidate struct {
	UserID              int        `json:"user_id"`
	Username            string     `json:"username"`
	Email               string     `json:"email"`
	AuthSource          string     `json:"auth_source"`
	RelayUserID         int        `json:"relay_user_id"`
	Reason              string     `json:"reason"`
	DirectoryRunID      int        `json:"directory_run_id"`
	DirectoryRunAt      *time.Time `json:"directory_run_at,omitempty"`
	TokenValidAfter     *time.Time `json:"token_valid_after,omitempty"`
	OffboardingStatus   string     `json:"offboarding_status,omitempty"`
	OffboardingActionID *int       `json:"offboarding_action_id,omitempty"`
}

func (s *Service) ListOffboardingCandidates(ctx context.Context, sourceID int, q string) ([]OffboardingCandidate, error) {
	source, err := s.client.DirectorySource.Get(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	if source.LastSuccessfulRunID == nil {
		return []OffboardingCandidate{}, nil
	}
	run, err := s.client.DirectorySyncRun.Get(ctx, *source.LastSuccessfulRunID)
	if err != nil {
		return nil, err
	}

	userQuery := s.client.User.Query().Where(entuser.RelayUserIDNotNil())
	if strings.TrimSpace(q) != "" {
		userQuery = userQuery.Where(entuser.Or(
			entuser.UsernameContainsFold(q),
			entuser.EmailContainsFold(q),
		))
	}
	users, err := userQuery.Order(ent.Asc(entuser.FieldUsername)).All(ctx)
	if err != nil {
		return nil, err
	}

	candidates := make([]OffboardingCandidate, 0)
	for _, u := range users {
		email := normalizeEmail(u.Email)
		if email == "" {
			continue
		}
		count, err := s.client.DirectoryMember.Query().
			Where(directorymember.SourceIDEQ(sourceID), directorymember.EmailNormalizedEQ(email)).
			Count(ctx)
		if err != nil {
			return nil, err
		}
		if count > 0 {
			continue
		}
		action, err := s.latestOffboardingAction(ctx, sourceID, u.ID)
		if err != nil {
			return nil, err
		}
		if action != nil && action.Status == directoryoffboardingaction.StatusSucceeded {
			continue
		}
		relayUserID := 0
		if u.RelayUserID != nil {
			relayUserID = *u.RelayUserID
		}
		candidate := OffboardingCandidate{
			UserID:          u.ID,
			Username:        u.Username,
			Email:           u.Email,
			AuthSource:      string(u.AuthSource),
			RelayUserID:     relayUserID,
			Reason:          offboardingReasonMissingFromDirectory,
			DirectoryRunID:  run.ID,
			TokenValidAfter: u.TokenValidAfter,
		}
		if run.CompletedAt != nil {
			candidate.DirectoryRunAt = run.CompletedAt
		}
		if action != nil {
			candidate.OffboardingStatus = string(action.Status)
			id := action.ID
			candidate.OffboardingActionID = &id
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

type DisableCandidateRequest struct {
	SourceID          int
	UserID            int
	ConfirmEmail      string
	Reason            string
	PerformedByUserID int
}

func (s *Service) DisableRelayUserForCandidate(ctx context.Context, req DisableCandidateRequest) (*ent.DirectoryOffboardingAction, error) {
	req.ConfirmEmail = normalizeEmail(req.ConfirmEmail)
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Reason == "" {
		req.Reason = offboardingReasonMissingFromDirectory
	}
	if req.Reason != offboardingReasonMissingFromDirectory {
		return nil, &ValidationError{Message: "unsupported offboarding reason"}
	}
	u, err := s.client.User.Get(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	if normalizeEmail(u.Email) != req.ConfirmEmail {
		return nil, &ValidationError{Message: "confirm_email must match candidate email"}
	}
	if u.RelayUserID == nil {
		return nil, &ValidationError{Message: "user does not have relay_user_id"}
	}
	source, err := s.client.DirectorySource.Get(ctx, req.SourceID)
	if err != nil {
		return nil, err
	}
	if source.LastSuccessfulRunID == nil {
		return nil, &ConflictError{Message: "source has no successful full-company sync"}
	}
	memberCount, err := s.client.DirectoryMember.Query().
		Where(directorymember.SourceIDEQ(req.SourceID), directorymember.EmailNormalizedEQ(normalizeEmail(u.Email))).
		Count(ctx)
	if err != nil {
		return nil, err
	}
	if memberCount > 0 {
		return nil, &ConflictError{Message: "candidate is no longer missing from latest directory snapshot"}
	}
	if s.relayDisablers == nil {
		return nil, &ValidationError{Message: "relay disable capability is not configured"}
	}
	if s.tokenRevoker == nil {
		return nil, &ValidationError{Message: "token revocation capability is not configured"}
	}

	providerID, err := s.primaryRelayProviderID(ctx)
	if err != nil {
		return nil, err
	}
	disabler, err := s.relayDisablers.ResolveRelayDisabler(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if disabler == nil {
		return nil, &ValidationError{Message: "relay provider does not support user disable"}
	}

	if _, err := s.upsertOffboardingAction(ctx, req, *u.RelayUserID, *source.LastSuccessfulRunID, directoryoffboardingaction.StatusRunning, nil); err != nil {
		return nil, err
	}
	if err := disabler.DisableUser(ctx, int64(*u.RelayUserID)); err != nil {
		failed, saveErr := s.upsertOffboardingAction(ctx, req, *u.RelayUserID, *source.LastSuccessfulRunID, directoryoffboardingaction.StatusFailed, err)
		if saveErr != nil {
			return nil, saveErr
		}
		return failed, &UpstreamError{Message: "disable relay user", Err: err}
	}
	revokedAt := s.now()
	if err := s.tokenRevoker.RevokeUserTokens(ctx, u.ID, revokedAt); err != nil {
		partial, saveErr := s.upsertOffboardingAction(ctx, req, *u.RelayUserID, *source.LastSuccessfulRunID, directoryoffboardingaction.StatusPartialFailed, err)
		if saveErr != nil {
			return nil, saveErr
		}
		return partial, err
	}
	return s.upsertOffboardingAction(ctx, req, *u.RelayUserID, *source.LastSuccessfulRunID, directoryoffboardingaction.StatusSucceeded, nil)
}

func (s *Service) primaryRelayProviderID(ctx context.Context) (int, error) {
	if s.client == nil {
		return 0, nil
	}
	p, err := s.client.RelayProvider.Query().
		Where(relayprovider.EnabledEQ(true), relayprovider.IsPrimaryEQ(true)).
		Order(ent.Asc(relayprovider.FieldID)).
		First(ctx)
	if err == nil {
		return p.ID, nil
	}
	if !ent.IsNotFound(err) {
		return 0, err
	}
	p, err = s.client.RelayProvider.Query().
		Where(relayprovider.EnabledEQ(true)).
		Order(ent.Asc(relayprovider.FieldID)).
		First(ctx)
	if ent.IsNotFound(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return p.ID, nil
}

func (s *Service) upsertOffboardingAction(ctx context.Context, req DisableCandidateRequest, relayUserID, runID int, status directoryoffboardingaction.Status, cause error) (*ent.DirectoryOffboardingAction, error) {
	action, err := s.client.DirectoryOffboardingAction.Query().
		Where(
			directoryoffboardingaction.SourceIDEQ(req.SourceID),
			directoryoffboardingaction.UserIDEQ(req.UserID),
			directoryoffboardingaction.ActionEQ(directoryoffboardingaction.ActionDisableRelayUser),
		).
		Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return nil, err
	}
	errorMessage := ""
	if cause != nil {
		errorMessage = cause.Error()
	}
	if ent.IsNotFound(err) {
		create := s.client.DirectoryOffboardingAction.Create().
			SetSourceID(req.SourceID).
			SetUserID(req.UserID).
			SetRelayUserID(relayUserID).
			SetDirectoryRunID(runID).
			SetAction(directoryoffboardingaction.ActionDisableRelayUser).
			SetStatus(status).
			SetReason(req.Reason).
			SetPerformedByUserID(req.PerformedByUserID)
		if errorMessage != "" {
			create.SetErrorMessage(errorMessage)
		}
		return create.Save(ctx)
	}
	update := s.client.DirectoryOffboardingAction.UpdateOne(action).
		SetRelayUserID(relayUserID).
		SetDirectoryRunID(runID).
		SetStatus(status).
		SetReason(req.Reason).
		SetPerformedByUserID(req.PerformedByUserID)
	if errorMessage != "" {
		update.SetErrorMessage(errorMessage)
	} else {
		update.ClearErrorMessage()
	}
	return update.Save(ctx)
}

func (s *Service) latestOffboardingAction(ctx context.Context, sourceID, userID int) (*ent.DirectoryOffboardingAction, error) {
	action, err := s.client.DirectoryOffboardingAction.Query().
		Where(
			directoryoffboardingaction.SourceIDEQ(sourceID),
			directoryoffboardingaction.UserIDEQ(userID),
			directoryoffboardingaction.ActionEQ(directoryoffboardingaction.ActionDisableRelayUser),
		).
		Order(ent.Desc(directoryoffboardingaction.FieldUpdatedAt)).
		First(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	return action, err
}

func userSearchPredicate(q string) predicate.User {
	return entuser.Or(entuser.UsernameContainsFold(q), entuser.EmailContainsFold(q))
}

func (s *Service) StartScheduler(ctx context.Context, tickInterval time.Duration) {
	if tickInterval <= 0 {
		tickInterval = time.Minute
	}
	go func() {
		s.runScheduledSources(ctx)
		ticker := time.NewTicker(tickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runScheduledSources(ctx)
			}
		}
	}()
}

func (s *Service) runScheduledSources(ctx context.Context) {
	if s == nil || s.client == nil {
		return
	}
	sources, err := s.client.DirectorySource.Query().
		Where(
			directorysource.EnabledEQ(true),
			directorysource.DeletedEQ(false),
			directorysource.ScheduleEnabledEQ(true),
		).
		All(ctx)
	if err != nil {
		return
	}
	now := s.now()
	for _, source := range sources {
		if !scheduleDue(source, now) || !s.markSourceRunning(source.ID) {
			continue
		}
		_, _ = s.client.DirectorySource.UpdateOneID(source.ID).SetLastScheduledAt(now).Save(ctx)
		go func(sourceID int) {
			defer s.unmarkSourceRunning(sourceID)
			runCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			_, _ = s.RunSource(runCtx, sourceID, "apply", "schedule")
		}(source.ID)
	}
}

func scheduleDue(source *ent.DirectorySource, now time.Time) bool {
	if source == nil {
		return false
	}
	if source.LastScheduledAt == nil {
		return true
	}
	var interval time.Duration
	switch source.ScheduleInterval {
	case directorysource.ScheduleIntervalHourly:
		interval = time.Hour
	case directorysource.ScheduleIntervalWeekly:
		interval = 7 * 24 * time.Hour
	default:
		interval = 24 * time.Hour
	}
	return !source.LastScheduledAt.Add(interval).After(now)
}

func (s *Service) markSourceRunning(sourceID int) bool {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	if _, ok := s.runningSources[sourceID]; ok {
		return false
	}
	s.runningSources[sourceID] = struct{}{}
	return true
}

func (s *Service) unmarkSourceRunning(sourceID int) {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	delete(s.runningSources, sourceID)
}
