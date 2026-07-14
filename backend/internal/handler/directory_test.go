package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorysyncrun"
	"github.com/ai-efficiency/backend/internal/directorysync"
	"github.com/gin-gonic/gin"
)

type fakeDirectoryService struct {
	validateIssues    []directorysync.ValidationIssue
	disableReq        directorysync.DisableCandidateRequest
	offboardingParams directorysync.OffboardingCandidateListParams
	offboardingPage   *directorysync.OffboardingCandidatePage
	countCall         int
}

func (f *fakeDirectoryService) ListSources(context.Context) ([]*ent.DirectorySource, error) {
	return []*ent.DirectorySource{{ID: 1, Name: "Example Directory", Scope: "full_company"}}, nil
}

func (f *fakeDirectoryService) CreateSource(context.Context, directorysync.SourceInput) (*ent.DirectorySource, error) {
	return &ent.DirectorySource{ID: 2, Name: "Example Directory", Scope: "full_company"}, nil
}

func (f *fakeDirectoryService) UpdateSource(_ context.Context, id int, _ directorysync.SourceInput) (*ent.DirectorySource, error) {
	return &ent.DirectorySource{ID: id, Name: "Updated Directory", Scope: "full_company"}, nil
}

func (f *fakeDirectoryService) DeleteSource(context.Context, int) error { return nil }

func (f *fakeDirectoryService) ValidateSource(context.Context, int) ([]directorysync.ValidationIssue, error) {
	return f.validateIssues, nil
}

func (f *fakeDirectoryService) RunSource(_ context.Context, sourceID int, mode, trigger string) (*ent.DirectorySyncRun, error) {
	return &ent.DirectorySyncRun{ID: 3, SourceID: sourceID, Mode: directorysyncrun.Mode(mode), Trigger: directorysyncrun.Trigger(trigger), Status: "queued"}, nil
}

func (f *fakeDirectoryService) ExecuteRun(_ context.Context, id int) (*ent.DirectorySyncRun, error) {
	return &ent.DirectorySyncRun{ID: id, SourceID: 1, Mode: "apply", Status: "completed"}, nil
}

func (f *fakeDirectoryService) GetRun(_ context.Context, id int) (*ent.DirectorySyncRun, error) {
	return &ent.DirectorySyncRun{ID: id, SourceID: 1, Mode: "apply", Status: "completed"}, nil
}

func (f *fakeDirectoryService) ListRuns(context.Context, int) ([]*ent.DirectorySyncRun, error) {
	return []*ent.DirectorySyncRun{{ID: 3, SourceID: 1, Mode: "apply", Status: "completed"}}, nil
}

func (f *fakeDirectoryService) ListDepartments(context.Context, int, string) ([]directorysync.DepartmentOption, error) {
	return []directorysync.DepartmentOption{{ID: 4, SourceID: 1, ExternalID: "dept-alpha", Name: "Department Alpha", DisplayPath: "Department Alpha"}}, nil
}

func (f *fakeDirectoryService) ListMembers(context.Context, int, string) ([]*ent.DirectoryMember, error) {
	return []*ent.DirectoryMember{{ID: 5, SourceID: 1, EmailNormalized: "alice@example.com"}}, nil
}

func (f *fakeDirectoryService) ListOffboardingCandidates(_ context.Context, params directorysync.OffboardingCandidateListParams) (*directorysync.OffboardingCandidatePage, error) {
	f.offboardingParams = params
	if f.offboardingPage != nil {
		return f.offboardingPage, nil
	}
	return &directorysync.OffboardingCandidatePage{Items: []directorysync.OffboardingCandidate{{
		UserID:      7,
		Username:    "bob",
		Email:       "bob@example.org",
		RelayUserID: 99,
		Reason:      "missing_from_latest_full_company_directory",
	}}, Page: 1, PageSize: 20, Total: 1}, nil
}

func (f *fakeDirectoryService) CountOffboardingCandidates(context.Context, int) (int, error) {
	f.countCall++
	if f.offboardingPage != nil {
		return f.offboardingPage.Total, nil
	}
	return 1, nil
}

func (f *fakeDirectoryService) DisableRelayUserForCandidate(_ context.Context, req directorysync.DisableCandidateRequest) (*ent.DirectoryOffboardingAction, error) {
	f.disableReq = req
	return &ent.DirectoryOffboardingAction{ID: 8, SourceID: req.SourceID, UserID: req.UserID, Status: "succeeded"}, nil
}

func TestDirectoryHandlerValidateSource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &fakeDirectoryService{validateIssues: []directorysync.ValidationIssue{{Path: "steps[0].request.url", Message: "url must use https"}}}
	router := gin.New()
	h := NewDirectoryHandler(svc)
	router.POST("/api/v1/admin/directory/sources/:id/validate", h.ValidateSource)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/directory/sources/1/validate", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			Issues []directorysync.ValidationIssue `json:"issues"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data.Issues) != 1 || body.Data.Issues[0].Path != "steps[0].request.url" {
		t.Fatalf("issues = %+v", body.Data.Issues)
	}
}

func TestDirectoryHandlerPreviewReturnsQueuedRun(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewDirectoryHandler(&fakeDirectoryService{})
	router.POST("/api/v1/admin/directory/sources/:id/preview", h.PreviewSource)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/directory/sources/1/preview", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data ent.DirectorySyncRun `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.Status != directorysyncrun.StatusQueued || body.Data.Mode != directorysyncrun.ModePreview {
		t.Fatalf("run = %+v, want queued preview", body.Data)
	}
}

func TestDirectoryHandlerStartRunReturnsQueuedRun(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewDirectoryHandler(&fakeDirectoryService{})
	router.POST("/api/v1/admin/directory/sources/:id/runs", h.StartRun)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/directory/sources/1/runs", bytes.NewBufferString(`{"mode":"apply"}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data ent.DirectorySyncRun `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.Status != directorysyncrun.StatusQueued || body.Data.Mode != directorysyncrun.ModeApply {
		t.Fatalf("run = %+v, want queued apply", body.Data)
	}
}

func TestDirectoryHandlerDisableCandidateRequiresConfirmation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewDirectoryHandler(&fakeDirectoryService{})
	router.POST("/api/v1/admin/directory/offboarding-candidates/:user_id/disable-relay-user", h.DisableRelayUser)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/directory/offboarding-candidates/7/disable-relay-user", bytes.NewBufferString(`{"source_id":1}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
}

func TestDirectoryHandlerListOffboardingCandidatesUsesCurrentSourceByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &fakeDirectoryService{}
	router := gin.New()
	h := NewDirectoryHandler(svc)
	router.GET("/api/v1/admin/directory/offboarding-candidates", h.ListOffboardingCandidates)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/directory/offboarding-candidates", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
	if svc.offboardingParams.SourceID != 0 {
		t.Fatalf("source id = %d, want 0 for current source fallback", svc.offboardingParams.SourceID)
	}
}

func TestDirectoryHandlerListOffboardingCandidatesReturnsRequestedPage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &fakeDirectoryService{offboardingPage: &directorysync.OffboardingCandidatePage{
		Items: []directorysync.OffboardingCandidate{{UserID: 7, Username: "bob", Email: "bob@example.org"}},
		Page:  2, PageSize: 25, Total: 51,
	}}
	router := gin.New()
	router.GET("/api/v1/admin/directory/offboarding-candidates", NewDirectoryHandler(svc).ListOffboardingCandidates)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/directory/offboarding-candidates?page=2&page_size=25&q=bob", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
	if svc.offboardingParams.Page != 2 || svc.offboardingParams.PageSize != 25 || svc.offboardingParams.Query != "bob" {
		t.Fatalf("list params = %+v, want page=2 page_size=25 query=bob", svc.offboardingParams)
	}
	var body struct {
		Data directorysync.OffboardingCandidatePage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.Page != 2 || body.Data.PageSize != 25 || body.Data.Total != 51 || len(body.Data.Items) != 1 {
		t.Fatalf("response page = %+v, want items/page/page_size/total", body.Data)
	}
}

func TestDirectoryHandlerListOffboardingCandidatesRejectsNonPositivePagination(t *testing.T) {
	tests := []string{
		"page=0",
		"page=-1",
		"page_size=0",
		"page_size=-1",
	}
	for _, query := range tests {
		t.Run(query, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.GET("/api/v1/admin/directory/offboarding-candidates", NewDirectoryHandler(&fakeDirectoryService{}).ListOffboardingCandidates)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/directory/offboarding-candidates?"+query, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 for %s, body = %s", rec.Code, query, rec.Body.String())
			}
		})
	}
}

func TestDirectoryHandlerListOffboardingCandidatesClampsPageSize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &fakeDirectoryService{}
	router := gin.New()
	router.GET("/api/v1/admin/directory/offboarding-candidates", NewDirectoryHandler(svc).ListOffboardingCandidates)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/directory/offboarding-candidates?page_size=1000", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
	if svc.offboardingParams.PageSize != 100 {
		t.Fatalf("page size = %d, want clamp to 100", svc.offboardingParams.PageSize)
	}
}

func TestDirectoryHandlerDisableCandidate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &fakeDirectoryService{}
	router := gin.New()
	h := NewDirectoryHandler(svc)
	router.POST("/api/v1/admin/directory/offboarding-candidates/:user_id/disable-relay-user", h.DisableRelayUser)

	body := bytes.NewBufferString(`{"source_id":1,"confirm_email":"bob@example.org","reason":"missing_from_latest_full_company_directory"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/directory/offboarding-candidates/7/disable-relay-user", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if svc.disableReq.SourceID != 1 || svc.disableReq.UserID != 7 || svc.disableReq.ConfirmEmail != "bob@example.org" {
		t.Fatalf("disable request = %+v", svc.disableReq)
	}
}

func TestDirectoryHandlerDisableCandidateUsesCurrentSourceByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &fakeDirectoryService{}
	router := gin.New()
	h := NewDirectoryHandler(svc)
	router.POST("/api/v1/admin/directory/offboarding-candidates/:user_id/disable-relay-user", h.DisableRelayUser)

	body := bytes.NewBufferString(`{"confirm_email":"bob@example.org","reason":"missing_from_latest_full_company_directory"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/directory/offboarding-candidates/7/disable-relay-user", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if svc.disableReq.SourceID != 0 || svc.disableReq.UserID != 7 || svc.disableReq.ConfirmEmail != "bob@example.org" {
		t.Fatalf("disable request = %+v", svc.disableReq)
	}
}
