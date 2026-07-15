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
	validateIssues      []directorysync.ValidationIssue
	disableReq          directorysync.DisableCandidateRequest
	offboardingSourceID int
	runListRequest      directorysync.RunListRequest
	runPage             *directorysync.RunPage
	runDetail           *ent.DirectorySyncRun
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
	if f.runDetail != nil {
		return f.runDetail, nil
	}
	return &ent.DirectorySyncRun{ID: id, SourceID: 1, Mode: "apply", Status: "completed"}, nil
}

func (f *fakeDirectoryService) ListRuns(_ context.Context, request directorysync.RunListRequest) (directorysync.RunPage, error) {
	f.runListRequest = request
	if f.runPage != nil {
		return *f.runPage, nil
	}
	page := 0
	if request.Limit > 0 {
		page = request.Offset / request.Limit
	}
	return directorysync.RunPage{
		Items: []directorysync.RunSummary{{
			ID:       3,
			SourceID: request.SourceID,
			Mode:     directorysyncrun.ModeApply,
			Trigger:  directorysyncrun.TriggerManual,
			Status:   directorysyncrun.StatusCompleted,
			Phase:    directorysyncrun.PhaseCompleted,
		}},
		Total:    125,
		Page:     page,
		PageSize: request.Limit,
	}, nil
}

func (f *fakeDirectoryService) ListDepartments(context.Context, int, string) ([]directorysync.DepartmentOption, error) {
	return []directorysync.DepartmentOption{{ID: 4, SourceID: 1, ExternalID: "dept-alpha", Name: "Department Alpha", DisplayPath: "Department Alpha"}}, nil
}

func (f *fakeDirectoryService) ListMembers(context.Context, int, string) ([]*ent.DirectoryMember, error) {
	return []*ent.DirectoryMember{{ID: 5, SourceID: 1, EmailNormalized: "alice@example.com"}}, nil
}

func (f *fakeDirectoryService) ListOffboardingCandidates(_ context.Context, sourceID int, _ string) ([]directorysync.OffboardingCandidate, error) {
	f.offboardingSourceID = sourceID
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

func TestDirectoryHandlerListRunsNormalizesPagination(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantLimit  int
		wantOffset int
		wantPage   int
	}{
		{name: "absent", wantLimit: 20},
		{name: "zero limit", query: "?limit=0", wantLimit: 20},
		{name: "invalid limit", query: "?limit=invalid", wantLimit: 20},
		{name: "overflow limit", query: "?limit=999999999999999999999999999999999999", wantLimit: 20},
		{name: "negative limit", query: "?limit=-1", wantLimit: 20},
		{name: "limit above max", query: "?limit=101", wantLimit: 100},
		{name: "large limit", query: "?limit=1000", wantLimit: 100},
		{name: "negative offset", query: "?limit=20&offset=-5", wantLimit: 20},
		{name: "invalid offset", query: "?limit=20&offset=invalid", wantLimit: 20},
		{name: "overflow offset", query: "?limit=20&offset=999999999999999999999999999999999999", wantLimit: 20},
		{name: "unaligned offset", query: "?limit=20&offset=21", wantLimit: 20, wantOffset: 21, wantPage: 1},
		{name: "third page", query: "?limit=20&offset=40", wantLimit: 20, wantOffset: 40, wantPage: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			svc := &fakeDirectoryService{}
			router := gin.New()
			h := NewDirectoryHandler(svc)
			router.GET("/api/v1/admin/directory/sources/:id/runs", h.ListRuns)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/directory/sources/7/runs"+tt.query, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if svc.runListRequest != (directorysync.RunListRequest{SourceID: 7, Limit: tt.wantLimit, Offset: tt.wantOffset}) {
				t.Fatalf("ListRuns request = %+v, want source=7 limit=%d offset=%d", svc.runListRequest, tt.wantLimit, tt.wantOffset)
			}
			var body struct {
				Data directorysync.RunPage `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.Data.PageSize != tt.wantLimit || body.Data.Page != tt.wantPage || body.Data.Total != 125 {
				t.Fatalf("page = %+v, want page_size=%d page=%d total=125", body.Data, tt.wantLimit, tt.wantPage)
			}
			if len(body.Data.Items) != 1 || body.Data.Items[0].ID != 3 {
				t.Fatalf("items = %+v, want run 3", body.Data.Items)
			}
			if body.Data.LatestActiveRun != nil {
				t.Fatalf("latest_active_run = %+v, want nil", body.Data.LatestActiveRun)
			}
			var raw struct {
				Data map[string]json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
				t.Fatalf("decode raw response: %v", err)
			}
			if value, ok := raw.Data["latest_active_run"]; !ok || string(value) != "null" {
				t.Fatalf("latest_active_run wire value = %s (present=%v), want null", value, ok)
			}
		})
	}
}

func TestDirectoryHandlerListRunsReturnsLatestActiveSummary(t *testing.T) {
	active := directorysync.RunSummary{
		ID:       9,
		SourceID: 7,
		Mode:     directorysyncrun.ModePreview,
		Trigger:  directorysyncrun.TriggerManual,
		Status:   directorysyncrun.StatusRunning,
		Phase:    directorysyncrun.PhaseExecuting,
	}
	svc := &fakeDirectoryService{runPage: &directorysync.RunPage{
		Items:           []directorysync.RunSummary{},
		Total:           125,
		Page:            5,
		PageSize:        20,
		LatestActiveRun: &active,
	}}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewDirectoryHandler(svc)
	router.GET("/api/v1/admin/directory/sources/:id/runs", h.ListRuns)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/directory/sources/7/runs?limit=20&offset=100", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data directorysync.RunPage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.LatestActiveRun == nil || body.Data.LatestActiveRun.ID != active.ID {
		t.Fatalf("latest_active_run = %+v, want id %d", body.Data.LatestActiveRun, active.ID)
	}
}

func TestDirectoryHandlerListRunsOmitsDiagnostics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewDirectoryHandler(&fakeDirectoryService{})
	router.GET("/api/v1/admin/directory/sources/:id/runs", h.ListRuns)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/directory/sources/1/runs", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			Items []map[string]json.RawMessage `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(body.Data.Items))
	}
	for _, key := range []string{"warnings", "summary", "preview_diff", "error_message"} {
		if _, ok := body.Data.Items[0][key]; ok {
			t.Fatalf("list item contains diagnostic key %q: %s", key, rec.Body.String())
		}
	}
}

func TestDirectoryHandlerGetRunKeepsCompleteDiagnostics(t *testing.T) {
	errorMessage := "handler-error-marker"
	svc := &fakeDirectoryService{runDetail: &ent.DirectorySyncRun{
		ID:           9,
		SourceID:     1,
		Mode:         directorysyncrun.ModeApply,
		Trigger:      directorysyncrun.TriggerManual,
		Status:       directorysyncrun.StatusFailed,
		Phase:        directorysyncrun.PhaseFailed,
		ErrorMessage: &errorMessage,
		Warnings:     []map[string]any{{"message": "handler-warning-marker"}},
		Summary:      map[string]any{"marker": "handler-summary-marker"},
		PreviewDiff:  map[string]any{"marker": "handler-diff-marker"},
	}}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewDirectoryHandler(svc)
	router.GET("/api/v1/admin/directory/runs/:id", h.GetRun)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/directory/runs/9", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, key := range []string{"warnings", "summary", "preview_diff", "error_message"} {
		if _, ok := body.Data[key]; !ok {
			t.Fatalf("detail missing diagnostic key %q: %s", key, rec.Body.String())
		}
	}
	for _, marker := range []string{"handler-warning-marker", "handler-summary-marker", "handler-diff-marker", "handler-error-marker"} {
		if !bytes.Contains(rec.Body.Bytes(), []byte(marker)) {
			t.Fatalf("detail missing marker %q: %s", marker, rec.Body.String())
		}
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
	if svc.offboardingSourceID != 0 {
		t.Fatalf("source id = %d, want 0 for current source fallback", svc.offboardingSourceID)
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
