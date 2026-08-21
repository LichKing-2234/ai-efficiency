package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ai-efficiency/backend/internal/activity"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/gin-gonic/gin"
)

type fakeActivityV2Service struct {
	query             activity.V2Query
	availabilityQuery activity.V2TeamMemberAvailabilityQuery
}

func (f *fakeActivityV2Service) V2Overview(_ context.Context, _ int, query activity.V2Query) (*activity.V2Overview, error) {
	f.query = query
	return &activity.V2Overview{ContractVersion: activity.V2MetricContractVersion, Trend: []activity.V2TrendPoint{}}, nil
}

func (f *fakeActivityV2Service) V2Repositories(context.Context, int, activity.V2PageQuery) (*activity.V2Page[activity.V2RepositoryRow], error) {
	return &activity.V2Page[activity.V2RepositoryRow]{Items: []activity.V2RepositoryRow{}}, nil
}

func (f *fakeActivityV2Service) V2PullRequests(context.Context, int, activity.V2PageQuery) (*activity.V2Page[activity.V2PullRequestRow], error) {
	return &activity.V2Page[activity.V2PullRequestRow]{Items: []activity.V2PullRequestRow{}}, nil
}

func (f *fakeActivityV2Service) V2TeamMemberAvailability(_ context.Context, _ int, query activity.V2TeamMemberAvailabilityQuery) (*activity.V2TeamMemberAvailability, error) {
	f.availabilityQuery = query
	return &activity.V2TeamMemberAvailability{ContractVersion: activity.V2MetricContractVersion, AvailableUserIDs: []int{}}, nil
}

func TestActivityV2HandlerRequiresExactLocalDateQuery(t *testing.T) {
	service, router := newActivityHandlerTestRouter()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/activity/v2/overview?scope=member&subject_user_id=7&from=2026-08-01&to=2026-08-07&timezone=Asia%2FShanghai", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if service.query.Scope != activity.V2ScopeMember || service.query.SubjectID != 7 || service.query.Timezone != "Asia/Shanghai" {
		t.Fatalf("query=%+v", service.query)
	}
	invalid := httptest.NewRecorder()
	router.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/api/v1/activity/v2/overview?scope=personal", nil))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status=%d body=%s", invalid.Code, invalid.Body.String())
	}
}

func TestActivityV2TeamAvailabilityParsesBoundedUserIDs(t *testing.T) {
	service, router := newActivityHandlerTestRouter()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/activity/v2/teams/team-a/member-availability?from=2026-08-01&to=2026-08-07&timezone=UTC&user_ids=7,8,7", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if query := service.availabilityQuery; query.TeamID != "team-a" || len(query.UserIDs) != 2 || query.UserIDs[0] != 7 || query.UserIDs[1] != 8 {
		t.Fatalf("query=%+v", service.availabilityQuery)
	}
	invalid := httptest.NewRecorder()
	router.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/api/v1/activity/v2/teams/team-a/member-availability?user_ids=bad", nil))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status=%d body=%s", invalid.Code, invalid.Body.String())
	}
}

func newActivityHandlerTestRouter() (*fakeActivityV2Service, *gin.Engine) {
	gin.SetMode(gin.TestMode)
	service := &fakeActivityV2Service{}
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(auth.ContextKeyUser, &auth.UserContext{UserID: 42}) })
	RegisterActivityRoutes(router.Group("/api/v1/activity"), NewActivityHandler(service))
	return service, router
}
