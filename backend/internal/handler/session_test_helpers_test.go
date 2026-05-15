package handler

import (
	"context"
	"testing"

	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/google/uuid"
)

func createOwnedSessionForUser(t *testing.T, env *fullTestEnv, userID int) uuid.UUID {
	t.Helper()
	repoID := createFullTestRepo(t, env.client)
	sessionID := uuid.New()
	env.client.Session.Create().
		SetID(sessionID).
		SetRepoConfigID(repoID).
		SetBranch("main").
		SetUserID(userID).
		SaveX(context.Background())
	return sessionID
}

func fullAdminUserID(t *testing.T, env *fullTestEnv) int {
	t.Helper()
	u := env.client.User.Query().Where(entuser.UsernameEQ("fulladmin")).OnlyX(context.Background())
	return u.ID
}
