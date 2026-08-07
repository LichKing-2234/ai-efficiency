package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/activity"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/gin-gonic/gin"
)

type fakeActivityService struct {
	memberWindow activity.Window
	memberErr    error
}

func (f *fakeActivityService) Scope(context.Context, int) (*activity.ScopeResponse, error) {
	return &activity.ScopeResponse{Teams: []activity.Team{}}, nil
}

func (f *fakeActivityService) Members(context.Context, int, activity.Window, activity.PageOptions) (*activity.MembersActivity, error) {
	return &activity.MembersActivity{Members: activity.Page[activity.MemberRow]{Items: []activity.MemberRow{}}}, nil
}

func (f *fakeActivityService) Member(_ context.Context, _, _ int, window activity.Window, _ activity.DetailPageOptions) (*activity.MemberActivity, error) {
	f.memberWindow = window
	if f.memberErr != nil {
		return nil, f.memberErr
	}
	return &activity.MemberActivity{
		Window: window, PRs: activity.Page[activity.PullRequest]{Items: []activity.PullRequest{}},
		Commits: activity.Page[activity.Commit]{Items: []activity.Commit{}}, Buckets: activity.Page[activity.BucketSummary]{Items: []activity.BucketSummary{}},
	}, nil
}

func (f *fakeActivityService) Team(context.Context, int, string, activity.Window, activity.PageOptions) (*activity.TeamActivity, error) {
	return &activity.TeamActivity{Members: activity.Page[activity.MemberRow]{Items: []activity.MemberRow{}}}, nil
}

func (f *fakeActivityService) Repository(context.Context, int, int, activity.Window, activity.RepositoryPageOptions) (*activity.RepositoryActivity, error) {
	return &activity.RepositoryActivity{
		Members: activity.Page[activity.MemberRow]{Items: []activity.MemberRow{}}, PRs: activity.Page[activity.PullRequest]{Items: []activity.PullRequest{}}, Commits: activity.Page[activity.Commit]{Items: []activity.Commit{}},
	}, nil
}

func (f *fakeActivityService) Bucket(context.Context, int, string) (*activity.BucketDetail, error) {
	return &activity.BucketDetail{RequestIDs: activity.RequestIDDetail{State: "unlinked", Evidence: []activity.RequestIDEvidence{}}}, nil
}

func TestActivitySummaryDefaultsToThirtyDaysAndReturnsEmptyArrays(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &fakeActivityService{}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(auth.ContextKeyUser, &auth.UserContext{UserID: 42, Username: "alice", Role: "user"})
	})
	group := router.Group("/api/v1/activity")
	RegisterActivityRoutes(group, NewActivityHandler(service))

	before := time.Now().UTC()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/activity/summary", nil))
	after := time.Now().UTC()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if service.memberWindow.To.Before(before) || service.memberWindow.To.After(after) || service.memberWindow.To.Sub(service.memberWindow.From) != 30*24*time.Hour {
		t.Fatalf("default activity window = %+v", service.memberWindow)
	}
	var response struct {
		Data struct {
			PRs     activity.Page[activity.PullRequest]   `json:"prs"`
			Commits activity.Page[activity.Commit]        `json:"commits"`
			Buckets activity.Page[activity.BucketSummary] `json:"buckets"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.PRs.Items == nil || response.Data.Commits.Items == nil || response.Data.Buckets.Items == nil {
		t.Fatalf("empty collections were not arrays: %s", recorder.Body.String())
	}
}

func TestActivityHandlerReturnsSnapshotExpiredConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &fakeActivityService{memberErr: activity.ErrSnapshotExpired}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(auth.ContextKeyUser, &auth.UserContext{UserID: 42})
	})
	RegisterActivityRoutes(router.Group("/api/v1/activity"), NewActivityHandler(service))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/activity/members/7?pr_cursor=stale", nil))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(recorder.Body.Bytes(), &response)
	if response.Message != "snapshot_expired" {
		t.Fatalf("message = %q", response.Message)
	}
}
