package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/ai-efficiency/backend/internal/workitems"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type fakeWorkItemsCounter struct {
	userID int
	admin  bool
}

func (f *fakeWorkItemsCounter) Counts(_ context.Context, userID int, admin bool) (*workitems.CountsResponse, error) {
	f.userID = userID
	f.admin = admin
	return &workitems.CountsResponse{
		QuotaResetApprovalCount: 2,
		QuotaResetAdminCount:    5,
		OffboardingCount:        3,
		TotalCount:              8,
	}, nil
}

func TestWorkItemsCountsPassesActorAndAdminFlag(t *testing.T) {
	client := testdb.Open(t)
	authSvc := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, zap.NewNop())
	admin := client.User.Create().
		SetUsername("admin").
		SetEmail("admin@example.com").
		SetAuthSource("ldap").
		SetRole(entuser.RoleAdmin).
		SaveX(context.Background())
	pair, err := authSvc.GenerateTokenPairForUser(&auth.UserInfo{ID: admin.ID, Username: admin.Username, Role: string(admin.Role)})
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	counter := &fakeWorkItemsCounter{}
	router := gin.New()
	group := router.Group("/api/v1")
	group.Use(auth.RequireAuth(authSvc))
	RegisterWorkItemsRoutes(group, NewWorkItemsHandler(counter))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/work-items/counts", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if counter.userID != admin.ID || !counter.admin {
		t.Fatalf("counter actor = %d admin=%v, want %d true", counter.userID, counter.admin, admin.ID)
	}
	if !strings.Contains(rec.Body.String(), `"total_count":8`) {
		t.Fatalf("body = %s, want total_count 8", rec.Body.String())
	}
}
