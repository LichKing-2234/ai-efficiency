package directorysync

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorydepartment"
	"github.com/ai-efficiency/backend/ent/directorymember"
	"github.com/ai-efficiency/backend/ent/directorymemberdepartment"
	"github.com/ai-efficiency/backend/ent/directoryoffboardingaction"
	"github.com/ai-efficiency/backend/ent/directorysource"
	"github.com/ai-efficiency/backend/ent/directorysyncrun"
	"github.com/ai-efficiency/backend/ent/predicate"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/directorytree"
	"github.com/ai-efficiency/backend/internal/relay"
)

const offboardingReasonMissingFromDirectory = "missing_from_latest_full_company_directory"

const (
	defaultOffboardingPageSize = 20
	maxOffboardingPageSize     = 100
)

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
	RevokeUserTokensTx(ctx context.Context, tx *ent.Tx, userID int, revokedAt time.Time) error
}

type workItemCountsInvalidator interface {
	InvalidateWorkItemCountsTx(ctx context.Context, tx *ent.Tx) error
}

type RelayDisablerResolver interface {
	ResolveRelayDisabler(ctx context.Context, providerID int) (relay.UserDisabler, error)
}

type ServiceOptions struct {
	Executor                  *Executor
	Credentials               CredentialResolver
	RelayDisablers            RelayDisablerResolver
	TokenRevoker              TokenRevoker
	WorkItemCountsInvalidator workItemCountsInvalidator
	Now                       func() time.Time
}

type Service struct {
	client         *ent.Client
	executor       *Executor
	credentials    CredentialResolver
	relayDisablers RelayDisablerResolver
	tokenRevoker   TokenRevoker
	invalidator    workItemCountsInvalidator
	now            func() time.Time
	runningMu      sync.Mutex
	runningSources map[int]struct{}
}

type DepartmentOption struct {
	ID               int            `json:"id"`
	SourceID         int            `json:"source_id"`
	ExternalID       string         `json:"external_id"`
	ParentExternalID *string        `json:"parent_external_id,omitempty"`
	Name             string         `json:"name"`
	Path             string         `json:"path"`
	DisplayPath      string         `json:"display_path"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	LastSeenRunID    int            `json:"last_seen_run_id"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
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
		invalidator:    options.WorkItemCountsInvalidator,
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
	cfg, err := ParseDSL(input.DSL)
	if err != nil {
		return nil, err
	}
	if issues := s.validateConfig(ctx, cfg); len(issues) > 0 {
		return nil, &ValidationError{Message: validationIssuesMessage(issues)}
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
	cfg, err := ParseDSL(input.DSL)
	if err != nil {
		return nil, err
	}
	if issues := s.validateConfig(ctx, cfg); len(issues) > 0 {
		return nil, &ValidationError{Message: validationIssuesMessage(issues)}
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin directory source update tx: %w", err)
	}
	defer tx.Rollback()
	updated, err := tx.DirectorySource.UpdateOneID(id).
		SetName(input.Name).
		SetDescription(input.Description).
		SetScope(directorysource.Scope(input.Scope)).
		SetEnabled(input.Enabled).
		SetDsl(input.DSL).
		SetScheduleEnabled(input.ScheduleEnabled).
		SetScheduleInterval(directorysource.ScheduleInterval(input.ScheduleInterval)).
		SetScheduleTimezone(input.ScheduleTimezone).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.invalidateWorkItemCountsTx(ctx, tx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit directory source update: %w", err)
	}
	updated.Unwrap()
	return updated, nil
}

func (s *Service) DeleteSource(ctx context.Context, id int) error {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin directory source delete tx: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.DirectorySource.UpdateOneID(id).
		SetDeleted(true).
		SetEnabled(false).
		SetScheduleEnabled(false).
		Save(ctx); err != nil {
		return err
	}
	if err := s.invalidateWorkItemCountsTx(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit directory source delete: %w", err)
	}
	return nil
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
	applyMode := directorysyncrun.Mode(mode) == directorysyncrun.ModeApply
	if applyMode && !s.markSourceRunning(sourceID) {
		return nil, &ConflictError{Message: "another full-company apply sync is already queued or running for this source"}
	}
	runCreated := false
	defer func() {
		if applyMode && !runCreated {
			s.unmarkSourceRunning(sourceID)
		}
	}()
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if applyMode {
		count, err := tx.DirectorySyncRun.Query().
			Where(
				directorysyncrun.SourceIDEQ(sourceID),
				directorysyncrun.ModeEQ(directorysyncrun.ModeApply),
				directorysyncrun.StatusIn(directorysyncrun.StatusQueued, directorysyncrun.StatusRunning),
			).
			Count(ctx)
		if err != nil {
			return nil, err
		}
		if count > 0 {
			return nil, &ConflictError{Message: "another full-company apply sync is already queued or running for this source"}
		}
	}
	run, err := tx.DirectorySyncRun.Create().
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
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	runCreated = true
	return run, nil
}

func (s *Service) ExecuteRun(ctx context.Context, runID int) (*ent.DirectorySyncRun, error) {
	run, err := s.client.DirectorySyncRun.Get(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.Mode == directorysyncrun.ModeApply {
		defer s.unmarkSourceRunning(run.SourceID)
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
		completed, err := s.completeApplyRun(ctx, run.ID, source.ID, result)
		if err != nil {
			return s.failRun(ctx, run.ID, "apply facts: "+err.Error())
		}
		return completed, nil
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

func (s *Service) completeApplyRun(ctx context.Context, runID, sourceID int, result *ExecutionResult) (*ent.DirectorySyncRun, error) {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if err := s.replaceFactsTx(ctx, tx, sourceID, runID, result); err != nil {
		return nil, err
	}
	completedAt := s.now()
	status := directorysyncrun.StatusCompleted
	if len(result.Warnings) > 0 {
		status = directorysyncrun.StatusCompletedWithWarnings
	}
	run, err := tx.DirectorySyncRun.UpdateOneID(runID).
		SetPhase(directorysyncrun.PhaseCompleted).
		SetCompletedAt(completedAt).
		SetStatus(status).
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
		}).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := tx.DirectorySource.UpdateOneID(sourceID).
		SetLastRunID(run.ID).
		SetLastSuccessfulRunID(run.ID).
		Save(ctx); err != nil {
		return nil, err
	}
	if err := s.invalidateWorkItemCountsTx(ctx, tx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	run.Unwrap()
	return run, nil
}

func (s *Service) invalidateWorkItemCountsTx(ctx context.Context, tx *ent.Tx) error {
	if s.invalidator == nil {
		return nil
	}
	if err := s.invalidator.InvalidateWorkItemCountsTx(ctx, tx); err != nil {
		return fmt.Errorf("invalidate work item counts: %w", err)
	}
	return nil
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

	if err := s.replaceFactsTx(ctx, tx, sourceID, runID, result); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) replaceFactsTx(ctx context.Context, tx *ent.Tx, sourceID, runID int, result *ExecutionResult) error {
	if _, err := tx.DirectoryDepartment.Delete().Where(directorydepartment.SourceIDEQ(sourceID)).Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.DirectoryMember.Delete().Where(directorymember.SourceIDEQ(sourceID)).Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.DirectoryMemberDepartment.Delete().Where(directorymemberdepartment.SourceIDEQ(sourceID)).Exec(ctx); err != nil {
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
		saved, err := create.Save(ctx)
		if err != nil {
			return err
		}
		departmentIDs := appendUniqueStrings(member.DepartmentExternalIDs)
		if len(departmentIDs) == 0 && strings.TrimSpace(member.DepartmentExternalID) != "" {
			departmentIDs = []string{strings.TrimSpace(member.DepartmentExternalID)}
		}
		for _, departmentID := range departmentIDs {
			if _, err := tx.DirectoryMemberDepartment.Create().
				SetSourceID(sourceID).
				SetDirectoryMemberID(saved.ID).
				SetMemberExternalID(saved.ExternalID).
				SetMemberEmailNormalized(saved.EmailNormalized).
				SetDepartmentExternalID(departmentID).
				SetLastSeenRunID(runID).
				Save(ctx); err != nil {
				return err
			}
		}
	}
	return nil
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

func (s *Service) ListDepartments(ctx context.Context, sourceID int, q string) ([]DepartmentOption, error) {
	departments, err := s.client.DirectoryDepartment.Query().
		Where(directorydepartment.SourceIDEQ(sourceID)).
		Order(ent.Asc(directorydepartment.FieldName), ent.Asc(directorydepartment.FieldExternalID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list directory departments: %w", err)
	}
	tree := directorytree.New(departments)
	needle := strings.ToLower(strings.TrimSpace(q))
	items := make([]DepartmentOption, 0, len(departments))
	for _, department := range tree.Ordered() {
		if department == nil {
			continue
		}
		displayPath := tree.DisplayPath(department.ExternalID)
		if needle != "" && !departmentOptionMatches(department, displayPath, needle) {
			continue
		}
		items = append(items, departmentOptionFromEnt(department, displayPath))
	}
	return items, nil
}

func departmentOptionMatches(department *ent.DirectoryDepartment, displayPath string, needle string) bool {
	for _, value := range []string{
		department.Name,
		department.ExternalID,
		department.Path,
		displayPath,
	} {
		if strings.Contains(strings.ToLower(strings.TrimSpace(value)), needle) {
			return true
		}
	}
	return false
}

func departmentOptionFromEnt(department *ent.DirectoryDepartment, displayPath string) DepartmentOption {
	if displayPath == "" {
		displayPath = strings.TrimSpace(department.Name)
	}
	if displayPath == "" {
		displayPath = strings.TrimSpace(department.ExternalID)
	}
	return DepartmentOption{
		ID:               department.ID,
		SourceID:         department.SourceID,
		ExternalID:       department.ExternalID,
		ParentExternalID: department.ParentExternalID,
		Name:             department.Name,
		Path:             department.Path,
		DisplayPath:      displayPath,
		Metadata:         department.Metadata,
		LastSeenRunID:    department.LastSeenRunID,
		CreatedAt:        department.CreatedAt,
		UpdatedAt:        department.UpdatedAt,
	}
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

type OffboardingCandidateListParams struct {
	SourceID int
	Query    string
	Page     int
	PageSize int
}

type OffboardingCandidatePage struct {
	Items    []OffboardingCandidate `json:"items"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"page_size"`
	Total    int                    `json:"total"`
}

type offboardingSnapshot struct {
	SourceID int
	RunID    int
	RunAt    *time.Time
}

func (s *Service) ListOffboardingCandidates(ctx context.Context, params OffboardingCandidateListParams) (*OffboardingCandidatePage, error) {
	params = normalizeOffboardingCandidateListParams(params)
	page := &OffboardingCandidatePage{
		Items:    []OffboardingCandidate{},
		Page:     params.Page,
		PageSize: params.PageSize,
	}
	snapshot, err := s.resolveOffboardingSnapshot(ctx, params.SourceID)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return page, nil
	}

	total, err := s.offboardingCandidateUsers(snapshot.SourceID, params.Query).Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count directory offboarding candidates: %w", err)
	}
	page.Total = total
	if total == 0 {
		return page, nil
	}

	offset := (params.Page - 1) * params.PageSize
	users, err := s.offboardingCandidateUsers(snapshot.SourceID, params.Query).
		Order(ent.Asc(entuser.FieldUsername), ent.Asc(entuser.FieldID)).
		Offset(offset).
		Limit(params.PageSize).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list directory offboarding candidate page: %w", err)
	}
	if len(users) == 0 {
		return page, nil
	}

	userIDs := make([]int, 0, len(users))
	for _, u := range users {
		userIDs = append(userIDs, u.ID)
	}
	actions, err := s.client.DirectoryOffboardingAction.Query().
		Where(
			directoryoffboardingaction.SourceIDEQ(snapshot.SourceID),
			directoryoffboardingaction.UserIDIn(userIDs...),
			directoryoffboardingaction.ActionEQ(directoryoffboardingaction.ActionDisableRelayUser),
		).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load directory offboarding actions for candidate page: %w", err)
	}
	actionByUserID := make(map[int]*ent.DirectoryOffboardingAction, len(actions))
	for _, action := range actions {
		actionByUserID[action.UserID] = action
	}

	page.Items = make([]OffboardingCandidate, 0, len(users))
	for _, u := range users {
		candidate := OffboardingCandidate{
			UserID:          u.ID,
			Username:        u.Username,
			Email:           u.Email,
			AuthSource:      string(u.AuthSource),
			RelayUserID:     *u.RelayUserID,
			Reason:          offboardingReasonMissingFromDirectory,
			DirectoryRunID:  snapshot.RunID,
			DirectoryRunAt:  snapshot.RunAt,
			TokenValidAfter: u.TokenValidAfter,
		}
		if action := actionByUserID[u.ID]; action != nil {
			candidate.OffboardingStatus = string(action.Status)
			actionID := action.ID
			candidate.OffboardingActionID = &actionID
		}
		page.Items = append(page.Items, candidate)
	}
	return page, nil
}

func (s *Service) CountOffboardingCandidates(ctx context.Context, sourceID int) (int, error) {
	snapshot, err := s.resolveOffboardingSnapshot(ctx, sourceID)
	if err != nil {
		return 0, err
	}
	if snapshot == nil {
		return 0, nil
	}
	count, err := s.offboardingCandidateUsers(snapshot.SourceID, "").Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count directory offboarding candidates: %w", err)
	}
	return count, nil
}

func normalizeOffboardingCandidateListParams(params OffboardingCandidateListParams) OffboardingCandidateListParams {
	params.Query = strings.TrimSpace(params.Query)
	if params.Page <= 0 {
		params.Page = 1
	}
	if params.PageSize <= 0 {
		params.PageSize = defaultOffboardingPageSize
	}
	if params.PageSize > maxOffboardingPageSize {
		params.PageSize = maxOffboardingPageSize
	}
	return params
}

func (s *Service) resolveOffboardingSnapshot(ctx context.Context, sourceID int) (*offboardingSnapshot, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("directory sync service is not configured")
	}
	runQuery := s.client.DirectorySyncRun.Query().
		Where(
			directorysyncrun.ModeEQ(directorysyncrun.ModeApply),
			directorysyncrun.StatusIn(directorysyncrun.StatusCompleted, directorysyncrun.StatusCompletedWithWarnings),
			directorysyncrun.CompletedAtNotNil(),
			offboardingSnapshotSourcePredicate(sourceID),
		)
	if sourceID > 0 {
		runQuery = runQuery.Where(directorysyncrun.SourceIDEQ(sourceID))
	}
	run, err := runQuery.
		Order(ent.Desc(directorysyncrun.FieldCompletedAt), ent.Desc(directorysyncrun.FieldID)).
		First(ctx)
	if ent.IsNotFound(err) {
		if sourceID > 0 {
			if _, sourceErr := s.client.DirectorySource.Get(ctx, sourceID); sourceErr != nil {
				return nil, sourceErr
			}
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("resolve directory offboarding snapshot: %w", err)
	}
	return &offboardingSnapshot{SourceID: run.SourceID, RunID: run.ID, RunAt: run.CompletedAt}, nil
}

func offboardingSnapshotSourcePredicate(sourceID int) predicate.DirectorySyncRun {
	return func(runs *entsql.Selector) {
		sources := entsql.Table(directorysource.Table)
		conditions := []*entsql.Predicate{
			entsql.ColumnsEQ(sources.C(directorysource.FieldID), runs.C(directorysyncrun.FieldSourceID)),
			entsql.ColumnsEQ(sources.C(directorysource.FieldLastSuccessfulRunID), runs.C(directorysyncrun.FieldID)),
			entsql.EQ(sources.C(directorysource.FieldScope), string(directorysource.ScopeFullCompany)),
			entsql.EQ(sources.C(directorysource.FieldDeleted), false),
		}
		if sourceID > 0 {
			conditions = append(conditions, entsql.EQ(sources.C(directorysource.FieldID), sourceID))
		}
		runs.Where(entsql.Exists(
			entsql.SelectExpr(entsql.Expr("1")).
				From(sources).
				Where(entsql.And(conditions...)),
		))
	}
}

func (s *Service) offboardingCandidateUsers(sourceID int, q string) *ent.UserQuery {
	query := s.client.User.Query().Where(
		entuser.RelayUserIDNotNil(),
		offboardingCandidateAntiJoin(sourceID),
	)
	if q != "" {
		query = query.Where(userSearchPredicate(q))
	}
	return query
}

func offboardingCandidateAntiJoin(sourceID int) predicate.User {
	return func(users *entsql.Selector) {
		members := entsql.Table(directorymember.Table)
		memberEmailMatchesUser := entsql.P(func(builder *entsql.Builder) {
			builder.Ident(members.C(directorymember.FieldEmailNormalized)).
				WriteOp(entsql.OpEQ).
				WriteString("LOWER(BTRIM(").
				Ident(users.C(entuser.FieldEmail)).
				WriteString("))")
		})
		memberExists := entsql.SelectExpr(entsql.Expr("1")).
			From(members).
			Where(entsql.And(
				entsql.EQ(members.C(directorymember.FieldSourceID), sourceID),
				memberEmailMatchesUser,
			))

		actions := entsql.Table(directoryoffboardingaction.Table)
		succeededActionExists := entsql.SelectExpr(entsql.Expr("1")).
			From(actions).
			Where(entsql.And(
				entsql.EQ(actions.C(directoryoffboardingaction.FieldSourceID), sourceID),
				entsql.ColumnsEQ(actions.C(directoryoffboardingaction.FieldUserID), users.C(entuser.FieldID)),
				entsql.EQ(actions.C(directoryoffboardingaction.FieldAction), string(directoryoffboardingaction.ActionDisableRelayUser)),
				entsql.EQ(actions.C(directoryoffboardingaction.FieldStatus), string(directoryoffboardingaction.StatusSucceeded)),
			))

		users.Where(entsql.And(
			entsql.P(func(builder *entsql.Builder) {
				builder.WriteString("BTRIM(").
					Ident(users.C(entuser.FieldEmail)).
					WriteString(") <> ''")
			}),
			entsql.NotExists(memberExists),
			entsql.NotExists(succeededActionExists),
		))
	}
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
	sourceID, ok, err := CurrentSourceID(ctx, s.client)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, &ConflictError{Message: "no successful full-company directory sync"}
	}
	req.SourceID = sourceID
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
	return s.finalizeOffboarding(ctx, req, u.ID, *u.RelayUserID, *source.LastSuccessfulRunID, s.now())
}

func (s *Service) finalizeOffboarding(ctx context.Context, req DisableCandidateRequest, userID, relayUserID, runID int, revokedAt time.Time) (*ent.DirectoryOffboardingAction, error) {
	finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	recordPartialFailure := func(cause error) (*ent.DirectoryOffboardingAction, error) {
		failureCtx, failureCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer failureCancel()
		return s.recordPartialOffboardingFailure(failureCtx, req, relayUserID, runID, cause)
	}

	tx, err := s.client.Tx(finalizeCtx)
	if err != nil {
		return recordPartialFailure(fmt.Errorf("begin offboarding finalization tx: %w", err))
	}
	fail := func(cause error) (*ent.DirectoryOffboardingAction, error) {
		_ = tx.Rollback()
		return recordPartialFailure(cause)
	}
	if err := s.tokenRevoker.RevokeUserTokensTx(finalizeCtx, tx, userID, revokedAt); err != nil {
		return fail(err)
	}
	action, err := s.upsertOffboardingActionTx(finalizeCtx, tx, req, relayUserID, runID, directoryoffboardingaction.StatusSucceeded, nil)
	if err != nil {
		return fail(err)
	}
	if err := s.invalidateWorkItemCountsTx(finalizeCtx, tx); err != nil {
		return fail(err)
	}
	if err := tx.Commit(); err != nil {
		return fail(fmt.Errorf("commit offboarding finalization: %w", err))
	}
	action.Unwrap()
	return action, nil
}

func (s *Service) recordPartialOffboardingFailure(ctx context.Context, req DisableCandidateRequest, relayUserID, runID int, cause error) (*ent.DirectoryOffboardingAction, error) {
	partial, saveErr := s.upsertOffboardingAction(ctx, req, relayUserID, runID, directoryoffboardingaction.StatusPartialFailed, cause)
	if saveErr != nil {
		return nil, fmt.Errorf("record partial offboarding failure after %v: %w", cause, saveErr)
	}
	return partial, cause
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
	return upsertOffboardingAction(ctx, s.client.DirectoryOffboardingAction, req, relayUserID, runID, status, cause)
}

func (s *Service) upsertOffboardingActionTx(ctx context.Context, tx *ent.Tx, req DisableCandidateRequest, relayUserID, runID int, status directoryoffboardingaction.Status, cause error) (*ent.DirectoryOffboardingAction, error) {
	return upsertOffboardingAction(ctx, tx.DirectoryOffboardingAction, req, relayUserID, runID, status, cause)
}

func upsertOffboardingAction(ctx context.Context, actions *ent.DirectoryOffboardingActionClient, req DisableCandidateRequest, relayUserID, runID int, status directoryoffboardingaction.Status, cause error) (*ent.DirectoryOffboardingAction, error) {
	action, err := actions.Query().
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
		create := actions.Create().
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
	update := actions.UpdateOne(action).
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
		if !scheduleDue(source, now) {
			continue
		}
		_, _ = s.client.DirectorySource.UpdateOneID(source.ID).SetLastScheduledAt(now).Save(ctx)
		go func(sourceID int) {
			runCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			run, err := s.RunSource(runCtx, sourceID, "apply", "schedule")
			if err != nil {
				return
			}
			_, _ = s.ExecuteRun(runCtx, run.ID)
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
