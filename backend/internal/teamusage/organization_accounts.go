package teamusage

import (
	"context"
	"fmt"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/teamaccount"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/representativescope"
)

const teamAccountSubjectType = "team_account"

// appendOrganizationAccountSubjects merges organization accounts into the
// representative scope as member-like subjects. They live in team_accounts, not
// in the directory, so without this merge they never appear in member details
// or usage trends.
func (s *Service) appendOrganizationAccountSubjects(ctx context.Context, scope *representativescope.Scope) (*representativescope.Scope, error) {
	if scope == nil || !scope.IsRepresentative {
		return scope, nil
	}
	departments := representedDepartmentSet(scope)
	if len(departments) == 0 {
		return scope, nil
	}
	accounts, err := s.client.TeamAccount.Query().
		Where(teamaccount.DepartmentExternalIDIn(departments...)).
		Order(ent.Asc(teamaccount.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load organization account subjects: %w", err)
	}
	if len(accounts) == 0 {
		return scope, nil
	}

	usernames := make([]string, 0, len(accounts))
	emails := make([]string, 0, len(accounts))
	for _, account := range accounts {
		if username := strings.TrimSpace(account.RelayUsername); username != "" {
			usernames = append(usernames, username)
		}
		if email := strings.ToLower(strings.TrimSpace(account.RelayEmail)); email != "" {
			emails = append(emails, email)
		}
	}
	users, err := s.client.User.Query().
		Where(entuser.Or(entuser.UsernameIn(usernames...), entuser.EmailIn(emails...))).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load organization account identities: %w", err)
	}
	userByUsername := make(map[string]*ent.User, len(users))
	userByEmail := make(map[string]*ent.User, len(users))
	for _, user := range users {
		userByUsername[strings.ToLower(strings.TrimSpace(user.Username))] = user
		userByEmail[strings.ToLower(strings.TrimSpace(user.Email))] = user
	}

	existingRelayIDs := make(map[int]struct{}, len(scope.Subjects))
	existingEmails := make(map[string]struct{}, len(scope.Subjects))
	for _, subject := range scope.Subjects {
		if subject.RelayUserID != nil && *subject.RelayUserID > 0 {
			existingRelayIDs[*subject.RelayUserID] = struct{}{}
		}
		if email := strings.ToLower(strings.TrimSpace(subject.Email)); email != "" {
			existingEmails[email] = struct{}{}
		}
	}

	extra := make([]representativescope.Subject, 0, len(accounts))
	for _, account := range accounts {
		relayUserID := int(account.RelayUserID)
		if relayUserID <= 0 {
			continue
		}
		if _, exists := existingRelayIDs[relayUserID]; exists {
			continue
		}
		email := strings.ToLower(strings.TrimSpace(account.RelayEmail))
		if _, exists := existingEmails[email]; exists {
			continue
		}
		shadow := userByEmail[email]
		if shadow == nil {
			shadow = userByUsername[strings.ToLower(strings.TrimSpace(account.RelayUsername))]
		}
		subject := representativescope.Subject{
			SubjectType:           teamAccountSubjectType,
			DisplayName:           account.Name,
			Email:                 account.RelayEmail,
			DepartmentExternalID:  account.DepartmentExternalID,
			DepartmentExternalIDs: []string{account.DepartmentExternalID},
			DepartmentDisplayPath: account.DepartmentDisplayPath,
			RelayUserID:           &relayUserID,
			Selectable:            shadow != nil,
		}
		if shadow != nil {
			subject.UserID = shadow.ID
		}
		extra = append(extra, subject)
		existingRelayIDs[relayUserID] = struct{}{}
		if email != "" {
			existingEmails[email] = struct{}{}
		}
	}
	if len(extra) == 0 {
		return scope, nil
	}
	enriched := *scope
	enriched.Subjects = append(append([]representativescope.Subject(nil), scope.Subjects...), extra...)
	enriched.OverviewSubjects = append(append([]representativescope.Subject(nil), scope.OverviewSubjects...), extra...)
	return &enriched, nil
}

func representedDepartmentSet(scope *representativescope.Scope) []string {
	seen := map[string]struct{}{}
	for _, subtree := range scope.RepresentedSubtreeIDs {
		for departmentID := range subtree {
			if departmentID = strings.TrimSpace(departmentID); departmentID != "" {
				seen[departmentID] = struct{}{}
			}
		}
	}
	for _, departmentID := range scope.RepresentedDepartmentIDs {
		if departmentID = strings.TrimSpace(departmentID); departmentID != "" {
			seen[departmentID] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for departmentID := range seen {
		out = append(out, departmentID)
	}
	return out
}
