package adminuseraccess

import (
	"context"
	"fmt"
	"strings"

	"entgo.io/ent/dialect/sql"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directoryoffboardingaction"
	"github.com/ai-efficiency/backend/ent/predicate"
	entuser "github.com/ai-efficiency/backend/ent/user"
)

const (
	StatusConfigured        = "configured"
	StatusDisabled          = "disabled"
	StatusMissingCredential = "missing_credential"
)

type OffboardingFact struct {
	LatestStatus string
	Succeeded    bool
}

func Derive(u *ent.User, relayPassword string, offboardingSucceeded bool) string {
	if u.TokenValidAfter != nil || offboardingSucceeded {
		return StatusDisabled
	}
	if strings.TrimSpace(relayPassword) != "" {
		return StatusConfigured
	}
	return StatusMissingCredential
}

func ApplyFilter(query *ent.UserQuery, status string) (*ent.UserQuery, error) {
	switch status {
	case StatusDisabled:
		return query.Where(entuser.Or(entuser.TokenValidAfterNotNil(), succeededOffboardingActionExists())), nil
	case StatusConfigured:
		return query.Where(
			entuser.TokenValidAfterIsNil(),
			relayPasswordHasNonBlankValue(),
			succeededOffboardingActionNotExists(),
		), nil
	case StatusMissingCredential:
		return query.Where(
			entuser.TokenValidAfterIsNil(),
			entuser.Or(entuser.RelayAuthPasswordIsNil(), relayPasswordIsBlank()),
			succeededOffboardingActionNotExists(),
		), nil
	default:
		return nil, fmt.Errorf("access_status must be configured, disabled, or missing_credential")
	}
}

func OffboardingFactsForUsers(ctx context.Context, client *ent.Client, userIDs []int) (map[int]OffboardingFact, error) {
	if len(userIDs) == 0 {
		return map[int]OffboardingFact{}, nil
	}
	actions, err := client.DirectoryOffboardingAction.Query().
		Where(
			directoryoffboardingaction.UserIDIn(userIDs...),
			directoryoffboardingaction.ActionEQ(directoryoffboardingaction.ActionDisableRelayUser),
		).
		Order(ent.Desc(directoryoffboardingaction.FieldUpdatedAt), ent.Desc(directoryoffboardingaction.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list offboarding actions for users: %w", err)
	}
	facts := make(map[int]OffboardingFact, len(actions))
	for _, action := range actions {
		fact := facts[action.UserID]
		if fact.LatestStatus == "" {
			fact.LatestStatus = string(action.Status)
		}
		if action.Status == directoryoffboardingaction.StatusSucceeded {
			fact.Succeeded = true
		}
		facts[action.UserID] = fact
	}
	return facts, nil
}

func relayPasswordHasNonBlankValue() predicate.User {
	return func(s *sql.Selector) {
		s.Where(sql.ExprP("TRIM(" + s.C(entuser.FieldRelayAuthPassword) + ") <> ''"))
	}
}

func relayPasswordIsBlank() predicate.User {
	return func(s *sql.Selector) {
		s.Where(sql.ExprP("TRIM(" + s.C(entuser.FieldRelayAuthPassword) + ") = ''"))
	}
}

func succeededOffboardingActionExists() predicate.User {
	return func(s *sql.Selector) {
		s.Where(sql.Exists(succeededOffboardingActionSubquery(s)))
	}
}

func succeededOffboardingActionNotExists() predicate.User {
	return func(s *sql.Selector) {
		s.Where(sql.NotExists(succeededOffboardingActionSubquery(s)))
	}
}

func succeededOffboardingActionSubquery(s *sql.Selector) *sql.Selector {
	actions := sql.Table(directoryoffboardingaction.Table)
	return sql.SelectExpr(sql.Expr("1")).
		From(actions).
		Where(sql.And(
			sql.ColumnsEQ(actions.C(directoryoffboardingaction.FieldUserID), s.C(entuser.FieldID)),
			sql.EQ(actions.C(directoryoffboardingaction.FieldAction), string(directoryoffboardingaction.ActionDisableRelayUser)),
			sql.EQ(actions.C(directoryoffboardingaction.FieldStatus), string(directoryoffboardingaction.StatusSucceeded)),
		))
}
