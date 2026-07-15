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
	if filters.DepartmentID == "" {
		return query, nil
	}
	source, err := s.currentSource(ctx)
	if err != nil {
		return nil, err
	}
	if !source.found {
		return query.Where(entuser.IDEQ(0)), nil
	}
	return query.Where(departmentUserPredicate(source.id, filters.DepartmentID)), nil
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
