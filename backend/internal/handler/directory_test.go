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
	validateIssues []directorysync.ValidationIssue
	disableReq     directorysync.DisableCandidateRequest
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

func (f *fakeDirectoryService) ListDepartments(context.Context, int, string) ([]*ent.DirectoryDepartment, error) {
	return []*ent.DirectoryDepartment{{ID: 4, SourceID: 1, ExternalID: "dept-alpha", Name: "Department Alpha"}}, nil
}

func (f *fakeDirectoryService) ListMembers(context.Context, int, string) ([]*ent.DirectoryMember, error) {
	return []*ent.DirectoryMember{{ID: 5, SourceID: 1, EmailNormalized: "alice@example.com"}}, nil
}

func (f *fakeDirectoryService) ListOffboardingCandidates(context.Context, int, string) ([]directorysync.OffboardingCandidate, error) {
	return []directorysync.OffboardingCandidate{{
		UserID:      7,
		Username:    "bob",
		Email:       "bob@example.org",
		RelayUserID: 99,
		Reason:      "missing_from_latest_full_company_directory",
	}}, nil
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
