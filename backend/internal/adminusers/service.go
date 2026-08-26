package adminusers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/adminuseraccess"
	"github.com/ai-efficiency/backend/internal/directoryfacts"
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
	facts  directoryfacts.Reader
}

type resolvedSource struct {
	id    int
	runID int
	view  directoryfacts.View
	found bool
}

func NewService(client *ent.Client) *Service {
	return &Service{client: client, facts: directoryfacts.New(client)}
}

func (s *Service) List(ctx context.Context, request ListRequest) (*Page, error) {
	request = normalizeListRequest(request)
	filters := normalizeFilters(request.Filters)
	source, err := s.currentSource(ctx)
	if err != nil {
		return nil, err
	}
	var snapshot *directoryfacts.Snapshot
	if source.found {
		value := source.view.Snapshot()
		snapshot = &value
	}
	selection, err := s.facts.LocalUsers(ctx, snapshot, directoryfacts.LocalUserQuery{
		Search:       filters.Query,
		DepartmentID: filters.DepartmentID,
		AccessStatus: filters.AccessStatus,
		Page:         request.Page,
		Limit:        request.PageSize,
		IncludeTotal: true,
	})
	if err != nil {
		if errors.Is(err, directoryfacts.ErrInvalidLocalUserAccessStatus) {
			return nil, fmt.Errorf("%w: %v", ErrInvalidAccessStatus, err)
		}
		return nil, err
	}

	page := &Page{
		Users:               []*ent.User{},
		Page:                request.Page,
		PageSize:            request.PageSize,
		DepartmentsByUserID: map[int]*Department{},
		OffboardingByUserID: map[int]adminuseraccess.OffboardingFact{},
	}
	page.Total = selection.Total
	if len(selection.IDs) == 0 {
		return page, nil
	}
	users, err := s.client.User.Query().Where(entuser.IDIn(selection.IDs...)).
		Order(ent.Asc(entuser.FieldID)).
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
	filters = normalizeFilters(filters)
	var snapshot *directoryfacts.Snapshot
	if filters.DepartmentID != "" {
		source, err := s.currentSource(ctx)
		if err != nil {
			return nil, err
		}
		if source.found {
			value := source.view.Snapshot()
			snapshot = &value
		}
	}
	selection, err := s.facts.LocalUsers(ctx, snapshot, directoryfacts.LocalUserQuery{
		Search:       filters.Query,
		DepartmentID: filters.DepartmentID,
		AccessStatus: filters.AccessStatus,
		Limit:        limit,
	})
	if err != nil {
		if errors.Is(err, directoryfacts.ErrInvalidLocalUserAccessStatus) {
			return nil, fmt.Errorf("%w: %v", ErrInvalidAccessStatus, err)
		}
		return nil, err
	}
	if len(selection.IDs) == 0 {
		return []*ent.User{}, nil
	}
	users, err := s.client.User.Query().Where(entuser.IDIn(selection.IDs...)).
		Order(ent.Asc(entuser.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list admin user targets: %w", err)
	}
	return users, nil
}

func (s *Service) currentSource(ctx context.Context) (resolvedSource, error) {
	view, found, err := s.facts.Current(ctx)
	if err != nil {
		return resolvedSource{}, fmt.Errorf("resolve current directory source: %w", err)
	}
	if !found {
		return resolvedSource{}, nil
	}
	snapshot := view.Snapshot()
	return resolvedSource{id: snapshot.SourceID, runID: snapshot.RunID, view: view, found: true}, nil
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

func userIDs(users []*ent.User) []int {
	ids := make([]int, 0, len(users))
	for _, user := range users {
		if user != nil {
			ids = append(ids, user.ID)
		}
	}
	return ids
}
