package handler

import (
	"context"
	"testing"

	entuser "github.com/ai-efficiency/backend/ent/user"
)

func fullAdminUserID(t *testing.T, env *fullTestEnv) int {
	t.Helper()
	u := env.client.User.Query().Where(entuser.UsernameEQ("fulladmin")).OnlyX(context.Background())
	return u.ID
}
