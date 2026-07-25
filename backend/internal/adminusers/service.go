package adminusers

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/predicate"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/adminuseraccess"
	"github.com/ai-efficiency/backend/internal/directorysync"
)

var ErrInvalidAccessStatus = errors.New("invalid admin user access status")

type Filters struct {
	Query        string
	DepartmentID string
	AccessStatus string
}

type ListRequest struct {
	Filters  Filters
	Page     int
	PageSize int
}

type Department struct {
	ExternalID  string
	Name        string
	Path        string
	DisplayPath string
}

type Page struct {
	Users               []*ent.User
	Total               int
	Page                int
	PageSize            int
	DepartmentsByUserID map[int]*Department
	OffboardingByUserID map[int]adminuseraccess.OffboardingFact
}

type Service struct {
	client *ent.Client
}

type resolvedSource struct {
	id    int
	found bool
}

func NewService(client *ent.Client) *Service {
	return &Service{client: client}
}

func (s *Service) List(ctx context.Context, request ListRequest) (*Page, error) {
	request = normalizeListRequest(request)
	filters := normalizeFilters(request.Filters)
	query, err := s.baseUsersQuery(filters)
	if err != nil {
		return nil, err
	}
	source, err := s.currentSource(ctx)
	if err != nil {
		return nil, err
	}
	query = applyDepartmentFilter(query, filters.DepartmentID, source)

	page := &Page{
		Users:               []*ent.User{},
		Page:                request.Page,
		PageSize:            request.PageSize,
		DepartmentsByUserID: map[int]*Department{},
		OffboardingByUserID: map[int]adminuseraccess.OffboardingFact{},
	}
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count admin users: %w", err)
	}
	page.Total = total
	pageCount := total / request.PageSize
	if total%request.PageSize != 0 {
		pageCount++
	}
	if total == 0 || request.Page-1 >= pageCount {
		return page, nil
	}

	offset := (request.Page - 1) * request.PageSize
	users, err := query.
		Order(ent.Asc(entuser.FieldID)).
		Limit(request.PageSize).
		Offset(offset).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list admin users: %w", err)
	}
	page.Users = users
	page.DepartmentsByUserID, err = s.departmentsForPage(ctx, source, users)
	if err != nil {
		return nil, err
	}
	page.OffboardingByUserID, err = adminuseraccess.OffboardingFactsForUsers(ctx, s.client, userIDs(users))
	if err != nil {
		return nil, err
	}
	return page, nil
}

func (s *Service) Targets(ctx context.Context, filters Filters, limit int) ([]*ent.User, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be positive")
	}
	query, err := s.filteredUsersQuery(ctx, normalizeFilters(filters))
	if err != nil {
		return nil, err
	}
	users, err := query.
		Order(ent.Asc(entuser.FieldID)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list admin user targets: %w", err)
	}
	return users, nil
}

func (s *Service) filteredUsersQuery(ctx context.Context, filters Filters) (*ent.UserQuery, error) {
	query, err := s.baseUsersQuery(filters)
	if err != nil {
		return nil, err
	}
	if filters.DepartmentID == "" {
		return query, nil
	}
	source, err := s.currentSource(ctx)
	if err != nil {
		return nil, err
	}
	return applyDepartmentFilter(query, filters.DepartmentID, source), nil
}

func (s *Service) baseUsersQuery(filters Filters) (*ent.UserQuery, error) {
	query := s.client.User.Query()
	if filters.AccessStatus != "" {
		var err error
		query, err = adminuseraccess.ApplyFilter(query, filters.AccessStatus)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidAccessStatus, err)
		}
	}
	if filters.Query != "" {
		query = query.Where(searchPredicate(filters.Query))
	}
	return query, nil
}

func applyDepartmentFilter(query *ent.UserQuery, departmentID string, source resolvedSource) *ent.UserQuery {
	if departmentID == "" {
		return query
	}
	if !source.found {
		return query.Where(entuser.IDEQ(0))
	}
	return query.Where(departmentUserPredicate(source.id, departmentID))
}

func (s *Service) currentSource(ctx context.Context) (resolvedSource, error) {
	sourceID, found, err := directorysync.CurrentSourceID(ctx, s.client)
	if err != nil {
		return resolvedSource{}, fmt.Errorf("resolve current directory source: %w", err)
	}
	return resolvedSource{id: sourceID, found: found}, nil
}

func normalizeFilters(filters Filters) Filters {
	return Filters{
		Query:        strings.TrimSpace(filters.Query),
		DepartmentID: strings.TrimSpace(filters.DepartmentID),
		AccessStatus: strings.TrimSpace(filters.AccessStatus),
	}
}

func normalizeListRequest(request ListRequest) ListRequest {
	if request.Page <= 0 {
		request.Page = 1
	}
	switch {
	case request.PageSize <= 0:
		request.PageSize = 20
	case request.PageSize > 100:
		request.PageSize = 100
	}
	return request
}

func searchPredicate(query string) predicate.User {
	predicates := []predicate.User{
		entuser.UsernameContainsFold(query),
		entuser.EmailContainsFold(query),
	}
	if value, err := strconv.Atoi(query); err == nil {
		predicates = append(predicates, entuser.IDEQ(value), entuser.RelayUserIDEQ(value))
	}
	return entuser.Or(predicates...)
}

func userIDs(users []*ent.User) []int {
	ids := make([]int, 0, len(users))
	for _, user := range users {
		if user != nil {
			ids = append(ids, user.ID)
		}
	}
	return ids
}
