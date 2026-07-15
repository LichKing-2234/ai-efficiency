package toolusage

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/prcommitusagesnapshot"
	"github.com/ai-efficiency/backend/ent/predicate"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/ent/toolusageevent"
	"github.com/ai-efficiency/backend/ent/user"
)

const (
	DefaultEventPageSize = 20
	MaxEventPageSize     = 100
)

type QueryService struct {
	entClient *ent.Client
}

type SummaryRequest struct {
	ActorUserID   int
	ActorRole     string
	From          time.Time
	To            time.Time
	Tool          string
	RepoID        int
	BindingStatus string
	UserID        int
	Q             string
}

type ListEventsRequest struct {
	ActorUserID   int
	ActorRole     string
	From          time.Time
	To            time.Time
	Tool          string
	RepoID        int
	BindingStatus string
	UserID        int
	Q             string
	Limit         int
	Offset        int
}

type GetEventDetailRequest struct {
	ActorUserID int
	ActorRole   string
	EventID     int
}

type EventUserSearchRequest struct {
	Q     string
	Limit int
}

type EventUserOption struct {
	ID            int       `json:"id"`
	Username      string    `json:"username"`
	Email         string    `json:"email"`
	Role          string    `json:"role"`
	EventCount    int       `json:"event_count"`
	LatestEventAt time.Time `json:"latest_event_at"`
}

type ToolCountDTO struct {
	Tool  string `json:"tool"`
	Count int    `json:"count"`
}

type SummaryResponse struct {
	TotalEvents   int            `json:"total_events"`
	BoundEvents   int            `json:"bound_events"`
	UnboundEvents int            `json:"unbound_events"`
	ToolCounts    []ToolCountDTO `json:"tool_counts"`
}

type EventListRow struct {
	ID                 int       `json:"id"`
	Tool               string    `json:"tool"`
	RepoID             int       `json:"repo_id"`
	RepoName           string    `json:"repo_name"`
	Username           string    `json:"username,omitempty"`
	ToolSessionID      string    `json:"tool_session_id"`
	ToolEventID        string    `json:"tool_event_id,omitempty"`
	DedupeKey          string    `json:"dedupe_key"`
	ObservedEndAt      time.Time `json:"observed_end_at"`
	RequestCount       int       `json:"request_count"`
	InputTokens        int64     `json:"input_tokens"`
	OutputTokens       int64     `json:"output_tokens"`
	CachedInputTokens  int64     `json:"cached_input_tokens"`
	ReasoningTokens    int64     `json:"reasoning_tokens"`
	CreditUsage        float64   `json:"credit_usage"`
	CommitCheckpointID *int      `json:"commit_checkpoint_id,omitempty"`
	CommitSHA          string    `json:"commit_sha,omitempty"`
	SourceBasename     string    `json:"source_basename"`
	BindingStatus      string    `json:"binding_status"`
}

type MatchedPR struct {
	PRRecordID int    `json:"pr_record_id"`
	ScmPRID    int    `json:"scm_pr_id"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	SCMPRURL   string `json:"scm_pr_url"`
}

type EventDetail struct {
	ID                   int            `json:"id"`
	Tool                 string         `json:"tool"`
	RepoID               int            `json:"repo_id"`
	RepoName             string         `json:"repo_name"`
	UserID               int            `json:"user_id"`
	Username             string         `json:"username,omitempty"`
	WorkspaceID          string         `json:"workspace_id"`
	ToolSessionID        string         `json:"tool_session_id"`
	ToolEventID          string         `json:"tool_event_id,omitempty"`
	DedupeKey            string         `json:"dedupe_key"`
	ObservedStartAt      time.Time      `json:"observed_start_at"`
	ObservedEndAt        time.Time      `json:"observed_end_at"`
	RequestCount         int            `json:"request_count"`
	InputTokens          int64          `json:"input_tokens"`
	OutputTokens         int64          `json:"output_tokens"`
	CachedInputTokens    int64          `json:"cached_input_tokens"`
	ReasoningTokens      int64          `json:"reasoning_tokens"`
	CreditUsage          float64        `json:"credit_usage"`
	ContextUsagePct      float64        `json:"context_usage_pct"`
	CommitCheckpointID   *int           `json:"commit_checkpoint_id,omitempty"`
	CommitSHA            string         `json:"commit_sha,omitempty"`
	CheckpointCapturedAt *time.Time     `json:"checkpoint_captured_at,omitempty"`
	SourceBasename       string         `json:"source_basename"`
	RawSourcePath        string         `json:"raw_source_path,omitempty"`
	RawSourceLocator     string         `json:"raw_source_locator,omitempty"`
	RawPayload           map[string]any `json:"raw_payload,omitempty"`
	BindingStatus        string         `json:"binding_status"`
	MatchedPRs           []MatchedPR    `json:"matched_prs"`
}

func NewQueryService(entClient *ent.Client) *QueryService {
	return &QueryService{entClient: entClient}
}

func (s *QueryService) SearchEventUsers(ctx context.Context, req EventUserSearchRequest) ([]EventUserOption, error) {
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("search event users: ent client is required")
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	q := strings.TrimSpace(req.Q)
	query := s.entClient.ToolUsageEvent.Query()
	if q != "" {
		query.Where(toolusageevent.HasUserWith(
			user.Or(
				user.EmailContainsFold(q),
				user.UsernameContainsFold(q),
			),
		))
	}

	var aggregates []struct {
		UserID        int       `json:"user_id"`
		EventCount    int       `json:"event_count"`
		LatestEventAt time.Time `json:"latest_event_at"`
	}
	if err := query.
		GroupBy(toolusageevent.FieldUserID).
		Aggregate(
			ent.As(ent.Count(), "event_count"),
			ent.As(ent.Max(toolusageevent.FieldObservedEndAt), "latest_event_at"),
		).
		Scan(ctx, &aggregates); err != nil {
		return nil, fmt.Errorf("search event users: aggregate events: %w", err)
	}
	sort.SliceStable(aggregates, func(i, j int) bool {
		return aggregates[i].LatestEventAt.After(aggregates[j].LatestEventAt)
	})
	if len(aggregates) > limit {
		aggregates = aggregates[:limit]
	}
	if len(aggregates) == 0 {
		return []EventUserOption{}, nil
	}

	userIDs := make([]int, 0, len(aggregates))
	for _, aggregate := range aggregates {
		userIDs = append(userIDs, aggregate.UserID)
	}
	users, err := s.entClient.User.Query().
		Where(user.IDIn(userIDs...)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("search event users: load users: %w", err)
	}
	usersByID := make(map[int]*ent.User, len(users))
	for _, item := range users {
		usersByID[item.ID] = item
	}

	out := make([]EventUserOption, 0, len(aggregates))
	for _, aggregate := range aggregates {
		item := usersByID[aggregate.UserID]
		if item == nil {
			continue
		}
		out = append(out, EventUserOption{
			ID:            item.ID,
			Username:      item.Username,
			Email:         item.Email,
			Role:          string(item.Role),
			EventCount:    aggregate.EventCount,
			LatestEventAt: aggregate.LatestEventAt,
		})
	}
	return out, nil
}

func (s *QueryService) GetSummary(ctx context.Context, req SummaryRequest) (*SummaryResponse, error) {
	base, err := s.filteredEventsQuery(filterFromSummary(req))
	if err != nil {
		return nil, fmt.Errorf("get event summary: %w", err)
	}

	total, err := base.Clone().Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("get event summary: count total events: %w", err)
	}
	bound, err := base.Clone().
		Where(toolusageevent.CommitCheckpointIDNotNil()).
		Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("get event summary: count bound events: %w", err)
	}
	unbound, err := base.Clone().
		Where(toolusageevent.CommitCheckpointIDIsNil()).
		Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("get event summary: count unbound events: %w", err)
	}
	tools := make([]ToolCountDTO, 0)
	if err := base.Clone().
		Order(ent.Asc(toolusageevent.FieldTool)).
		GroupBy(toolusageevent.FieldTool).
		Aggregate(ent.As(ent.Count(), "count")).
		Scan(ctx, &tools); err != nil {
		return nil, fmt.Errorf("get event summary: count events by tool: %w", err)
	}

	return &SummaryResponse{
		TotalEvents:   total,
		BoundEvents:   bound,
		UnboundEvents: unbound,
		ToolCounts:    tools,
	}, nil
}

func (s *QueryService) ListEvents(ctx context.Context, req ListEventsRequest) ([]EventListRow, int, error) {
	base, err := s.filteredEventsQuery(filterFromList(req))
	if err != nil {
		return nil, 0, err
	}
	total, err := base.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list events: count filtered events: %w", err)
	}

	limit, offset := normalizeEventPage(req.Limit, req.Offset)
	page := base.
		Order(
			ent.Desc(toolusageevent.FieldObservedEndAt),
			ent.Desc(toolusageevent.FieldID),
		).
		Offset(offset).
		Limit(limit)
	page.WithRepoConfig(func(query *ent.RepoConfigQuery) {
		query.Select(
			repoconfig.FieldName,
			repoconfig.FieldFullName,
		)
	})
	if isAdminRole(req.ActorRole) {
		page.WithUser(func(query *ent.UserQuery) {
			query.Select(user.FieldUsername)
		})
	}
	page.WithCommitCheckpoint(func(query *ent.CommitCheckpointQuery) {
		query.Select(commitcheckpoint.FieldCommitSha)
	})
	events, err := page.Select(
		toolusageevent.FieldID,
		toolusageevent.FieldTool,
		toolusageevent.FieldRepoConfigID,
		toolusageevent.FieldUserID,
		toolusageevent.FieldToolSessionID,
		toolusageevent.FieldToolEventID,
		toolusageevent.FieldObservedEndAt,
		toolusageevent.FieldRequestCount,
		toolusageevent.FieldInputTokens,
		toolusageevent.FieldOutputTokens,
		toolusageevent.FieldCachedInputTokens,
		toolusageevent.FieldReasoningTokens,
		toolusageevent.FieldCreditUsage,
		toolusageevent.FieldCommitCheckpointID,
		toolusageevent.FieldDedupeKey,
		toolusageevent.FieldRawSourcePath,
		toolusageevent.FieldRawSourceLocator,
	).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list events: load page: %w", err)
	}

	rows := make([]EventListRow, 0, len(events))
	for _, item := range events {
		rows = append(rows, toEventListRow(item))
	}
	return rows, total, nil
}

func normalizeEventPage(limit, offset int) (normalizedLimit, normalizedOffset int) {
	if limit <= 0 {
		limit = DefaultEventPageSize
	} else if limit > MaxEventPageSize {
		limit = MaxEventPageSize
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func (s *QueryService) GetEventDetail(ctx context.Context, req GetEventDetailRequest) (*EventDetail, error) {
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("get event detail: ent client is required")
	}
	if req.ActorUserID <= 0 {
		return nil, fmt.Errorf("get event detail: actor user is required")
	}
	if req.EventID <= 0 {
		return nil, fmt.Errorf("get event detail: event id is required")
	}

	item, err := s.entClient.ToolUsageEvent.Query().
		Where(toolusageevent.IDEQ(req.EventID)).
		WithRepoConfig().
		WithCommitCheckpoint().
		WithUser().
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("get event detail: load event: %w", err)
	}
	if !isAdminRole(req.ActorRole) && item.UserID != req.ActorUserID {
		return nil, fmt.Errorf("get event detail: %w", ErrUsageEventForbidden)
	}

	detail := &EventDetail{
		ID:                item.ID,
		Tool:              item.Tool,
		RepoID:            item.RepoConfigID,
		RepoName:          repoDisplayName(item),
		UserID:            item.UserID,
		WorkspaceID:       item.WorkspaceID,
		ToolSessionID:     item.ToolSessionID,
		ToolEventID:       valueOrEmpty(item.ToolEventID),
		DedupeKey:         item.DedupeKey,
		ObservedStartAt:   item.ObservedStartAt,
		ObservedEndAt:     item.ObservedEndAt,
		RequestCount:      item.RequestCount,
		InputTokens:       item.InputTokens,
		OutputTokens:      item.OutputTokens,
		CachedInputTokens: item.CachedInputTokens,
		ReasoningTokens:   item.ReasoningTokens,
		CreditUsage:       item.CreditUsage,
		ContextUsagePct:   item.ContextUsagePct,
		SourceBasename:    sourceBasenamePtr(item.RawSourcePath, item.RawSourceLocator, item.ToolSessionID),
		BindingStatus:     bindingStatus(item.CommitCheckpointID),
		MatchedPRs:        []MatchedPR{},
	}
	if cp := item.Edges.CommitCheckpoint; cp != nil {
		detail.CommitCheckpointID = &cp.ID
		detail.CommitSHA = cp.CommitSha
		capturedAt := cp.CapturedAt
		detail.CheckpointCapturedAt = &capturedAt
	}
	if isAdminRole(req.ActorRole) {
		if userEdge := item.Edges.User; userEdge != nil {
			detail.Username = userEdge.Username
		}
		detail.RawSourcePath = valueOrEmpty(item.RawSourcePath)
		detail.RawSourceLocator = valueOrEmpty(item.RawSourceLocator)
		if len(item.RawPayload) > 0 {
			detail.RawPayload = item.RawPayload
		}
	}
	if strings.TrimSpace(detail.CommitSHA) != "" {
		matched, err := s.findMatchedPRs(ctx, detail.CommitSHA)
		if err != nil {
			return nil, err
		}
		detail.MatchedPRs = matched
	}
	return detail, nil
}

type queryFilter struct {
	ActorUserID   int
	ActorRole     string
	From          time.Time
	To            time.Time
	Tool          string
	RepoID        int
	BindingStatus string
	UserID        int
	Q             string
}

func filterFromSummary(req SummaryRequest) queryFilter {
	return queryFilter{
		ActorUserID:   req.ActorUserID,
		ActorRole:     req.ActorRole,
		From:          req.From,
		To:            req.To,
		Tool:          req.Tool,
		RepoID:        req.RepoID,
		BindingStatus: req.BindingStatus,
		UserID:        req.UserID,
		Q:             req.Q,
	}
}

func filterFromList(req ListEventsRequest) queryFilter {
	return queryFilter{
		ActorUserID:   req.ActorUserID,
		ActorRole:     req.ActorRole,
		From:          req.From,
		To:            req.To,
		Tool:          req.Tool,
		RepoID:        req.RepoID,
		BindingStatus: req.BindingStatus,
		UserID:        req.UserID,
		Q:             req.Q,
	}
}

func (s *QueryService) filteredEventsQuery(filter queryFilter) (*ent.ToolUsageEventQuery, error) {
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("query events: ent client is required")
	}
	if filter.ActorUserID <= 0 {
		return nil, fmt.Errorf("query events: actor user is required")
	}

	query := s.entClient.ToolUsageEvent.Query()

	if !isAdminRole(filter.ActorRole) {
		query.Where(toolusageevent.UserIDEQ(filter.ActorUserID))
	} else if filter.UserID > 0 {
		query.Where(toolusageevent.UserIDEQ(filter.UserID))
	}
	if !filter.From.IsZero() {
		query.Where(toolusageevent.ObservedEndAtGTE(filter.From.UTC()))
	}
	if !filter.To.IsZero() {
		query.Where(toolusageevent.ObservedEndAtLTE(filter.To.UTC()))
	}
	if tool := strings.TrimSpace(filter.Tool); tool != "" {
		query.Where(toolusageevent.ToolEQ(tool))
	}
	if filter.RepoID > 0 {
		query.Where(toolusageevent.RepoConfigIDEQ(filter.RepoID))
	}
	switch strings.TrimSpace(filter.BindingStatus) {
	case "bound":
		query.Where(toolusageevent.CommitCheckpointIDNotNil())
	case "unbound":
		query.Where(toolusageevent.CommitCheckpointIDIsNil())
	}
	if q := strings.TrimSpace(filter.Q); q != "" {
		query.Where(eventSearchPredicate(q))
	}
	return query, nil
}

func eventSearchPredicate(q string) predicate.ToolUsageEvent {
	q = strings.TrimSpace(q)
	sourceBasenameMatches := predicate.ToolUsageEvent(func(selector *entsql.Selector) {
		rawSourcePath := selector.C(toolusageevent.FieldRawSourcePath)
		rawSourceLocator := selector.C(toolusageevent.FieldRawSourceLocator)
		toolSessionID := selector.C(toolusageevent.FieldToolSessionID)
		expression := fmt.Sprintf(`CASE
			WHEN BTRIM(COALESCE(%s, '')) <> ''
				THEN REGEXP_REPLACE(RTRIM(BTRIM(%s), '/'), '^.*/', '')
			WHEN BTRIM(COALESCE(%s, '')) <> ''
				THEN BTRIM(%s)
			ELSE BTRIM(%s)
		END ILIKE `, rawSourcePath, rawSourcePath, rawSourceLocator, rawSourceLocator, toolSessionID)
		selector.Where(entsql.P(func(builder *entsql.Builder) {
			builder.WriteString(expression).Arg(eventSearchPattern(q))
		}))
	})

	return toolusageevent.Or(
		toolusageevent.ToolSessionIDContainsFold(q),
		toolusageevent.ToolEventIDContainsFold(q),
		toolusageevent.DedupeKeyContainsFold(q),
		toolusageevent.HasCommitCheckpointWith(commitcheckpoint.CommitShaContainsFold(q)),
		sourceBasenameMatches,
	)
}

func eventSearchPattern(q string) string {
	var pattern strings.Builder
	pattern.Grow(len(q) + 2)
	pattern.WriteByte('%')
	for _, r := range q {
		if r == '%' || r == '_' || r == '\\' {
			pattern.WriteByte('\\')
		}
		pattern.WriteRune(r)
	}
	pattern.WriteByte('%')
	return pattern.String()
}

func (s *QueryService) findMatchedPRs(ctx context.Context, commitSHA string) ([]MatchedPR, error) {
	rows, err := s.entClient.PRCommitUsageSnapshot.Query().
		Where(prcommitusagesnapshot.CommitShaEQ(strings.TrimSpace(commitSHA))).
		WithPrRecord().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("get event detail: query matched prs: %w", err)
	}

	out := make([]MatchedPR, 0, len(rows))
	seen := map[int]struct{}{}
	for _, row := range rows {
		pr := row.Edges.PrRecord
		if pr == nil {
			continue
		}
		if _, ok := seen[pr.ID]; ok {
			continue
		}
		seen[pr.ID] = struct{}{}
		out = append(out, MatchedPR{
			PRRecordID: pr.ID,
			ScmPRID:    pr.ScmPrID,
			Title:      pr.Title,
			Status:     string(pr.Status),
			SCMPRURL:   pr.ScmPrURL,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PRRecordID < out[j].PRRecordID })
	return out, nil
}

func toEventListRow(item *ent.ToolUsageEvent) EventListRow {
	row := EventListRow{
		ID:                item.ID,
		Tool:              item.Tool,
		RepoID:            item.RepoConfigID,
		RepoName:          repoDisplayName(item),
		ToolSessionID:     item.ToolSessionID,
		ToolEventID:       valueOrEmpty(item.ToolEventID),
		DedupeKey:         item.DedupeKey,
		ObservedEndAt:     item.ObservedEndAt,
		RequestCount:      item.RequestCount,
		InputTokens:       item.InputTokens,
		OutputTokens:      item.OutputTokens,
		CachedInputTokens: item.CachedInputTokens,
		ReasoningTokens:   item.ReasoningTokens,
		CreditUsage:       item.CreditUsage,
		SourceBasename:    sourceBasenamePtr(item.RawSourcePath, item.RawSourceLocator, item.ToolSessionID),
		BindingStatus:     bindingStatus(item.CommitCheckpointID),
	}
	if userEdge := item.Edges.User; userEdge != nil {
		row.Username = userEdge.Username
	}
	if cp := item.Edges.CommitCheckpoint; cp != nil {
		row.CommitCheckpointID = &cp.ID
		row.CommitSHA = cp.CommitSha
	}
	return row
}

func repoDisplayName(item *ent.ToolUsageEvent) string {
	if item != nil && item.Edges.RepoConfig != nil {
		if v := strings.TrimSpace(item.Edges.RepoConfig.FullName); v != "" {
			return v
		}
		if v := strings.TrimSpace(item.Edges.RepoConfig.Name); v != "" {
			return v
		}
	}
	return ""
}

func sourceBasename(rawSourcePath, rawSourceLocator, toolSessionID string) string {
	if v := strings.TrimSpace(rawSourcePath); v != "" {
		return filepath.Base(v)
	}
	if v := strings.TrimSpace(rawSourceLocator); v != "" {
		return v
	}
	return strings.TrimSpace(toolSessionID)
}

func sourceBasenamePtr(rawSourcePath, rawSourceLocator *string, toolSessionID string) string {
	return sourceBasename(valueOrEmpty(rawSourcePath), valueOrEmpty(rawSourceLocator), toolSessionID)
}

func bindingStatus(commitCheckpointID *int) string {
	if commitCheckpointID != nil {
		return "bound"
	}
	return "unbound"
}

func valueOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func isAdminRole(role string) bool {
	return strings.EqualFold(strings.TrimSpace(role), "admin")
}
