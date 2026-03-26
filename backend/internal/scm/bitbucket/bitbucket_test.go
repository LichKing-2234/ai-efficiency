package bitbucket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/internal/scm"
	"go.uber.org/zap"
)

// helper: create a test server + provider pointing at it.
func setup(t *testing.T, handler http.HandlerFunc) (*Provider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	p, _ := New(srv.URL, "test-token", zap.NewNop())
	t.Cleanup(srv.Close)
	return p, srv
}

// --- New ---

func TestNew(t *testing.T) {
	p, err := New("https://bb.example.com/", "tok", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if p.baseURL != "https://bb.example.com" {
		t.Errorf("baseURL = %q, want trailing slash trimmed", p.baseURL)
	}
	if p.token != "tok" {
		t.Errorf("token = %q", p.token)
	}
}

// --- splitFullName ---

func TestSplitFullNameValid(t *testing.T) {
	proj, repo, err := splitFullName("PROJ/my-repo")
	if err != nil {
		t.Fatal(err)
	}
	if proj != "PROJ" || repo != "my-repo" {
		t.Errorf("got %s/%s", proj, repo)
	}
}

func TestSplitFullNameInvalid(t *testing.T) {
	_, _, err := splitFullName("noslash")
	if err == nil {
		t.Fatal("expected error for invalid full name")
	}
}

// --- doRequest ---

func TestDoRequestAuthHeader(t *testing.T) {
	var gotAuth string
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte("{}"))
	})
	p.doRequest(context.Background(), "GET", "/test", nil)
	if gotAuth != "Bearer test-token" {
		t.Errorf("auth = %q", gotAuth)
	}
}

func TestDoRequestWithBody(t *testing.T) {
	var gotBody map[string]interface{}
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte("{}"))
	})
	p.doRequest(context.Background(), "POST", "/test", map[string]string{"key": "val"})
	if gotBody["key"] != "val" {
		t.Errorf("body = %v", gotBody)
	}
}

func TestDoRequest4xxError(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte("not found"))
	})
	_, err := p.doRequest(context.Background(), "GET", "/missing", nil)
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

// --- GetRepo ---

func TestGetRepo(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"slug": "my-repo",
			"name": "My Repo",
			"project": map[string]string{"key": "PROJ"},
			"links": map[string]interface{}{
				"clone": []map[string]string{
					{"href": "ssh://git@bb/proj/my-repo.git", "name": "ssh"},
					{"href": "https://bb/proj/my-repo.git", "name": "https"},
				},
			},
		})
	})

	repo, err := p.GetRepo(context.Background(), "PROJ/my-repo")
	if err != nil {
		t.Fatal(err)
	}
	if repo.Name != "My Repo" {
		t.Errorf("name = %q", repo.Name)
	}
	if repo.CloneURL != "https://bb/proj/my-repo.git" {
		t.Errorf("clone_url = %q", repo.CloneURL)
	}
	if repo.DefaultBranch != "main" {
		t.Errorf("default_branch = %q", repo.DefaultBranch)
	}
}

func TestGetRepoInvalidName(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {})
	_, err := p.GetRepo(context.Background(), "noslash")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetRepoNoHTTPClone(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"slug": "r", "name": "R", "project": map[string]string{"key": "P"},
			"links": map[string]interface{}{
				"clone": []map[string]string{{"href": "ssh://x", "name": "ssh"}},
			},
		})
	})
	repo, err := p.GetRepo(context.Background(), "P/r")
	if err != nil {
		t.Fatal(err)
	}
	if repo.CloneURL != "" {
		t.Errorf("expected empty clone url, got %q", repo.CloneURL)
	}
}

// --- ListRepos ---

func TestListRepos(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"values": []map[string]interface{}{
				{"slug": "repo1", "name": "Repo 1", "project": map[string]string{"key": "P"}},
				{"slug": "repo2", "name": "Repo 2", "project": map[string]string{"key": "P"}},
			},
		})
	})

	repos, err := p.ListRepos(context.Background(), scm.ListOpts{Page: 1, PageSize: 25})
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 {
		t.Fatalf("len = %d", len(repos))
	}
	if repos[0].FullName != "P/repo1" {
		t.Errorf("full_name = %q", repos[0].FullName)
	}
}

// --- CreatePR ---

func TestCreatePR(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": 42, "title": "My PR",
			"links": map[string]interface{}{
				"self": []map[string]string{{"href": "https://bb/pr/42"}},
			},
		})
	})

	pr, err := p.CreatePR(context.Background(), scm.CreatePRRequest{
		RepoFullName: "P/r", Title: "My PR", Body: "desc",
		SourceBranch: "feat", TargetBranch: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if pr.ID != 42 {
		t.Errorf("id = %d", pr.ID)
	}
	if pr.URL != "https://bb/pr/42" {
		t.Errorf("url = %q", pr.URL)
	}
}

func TestCreatePRInvalidName(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {})
	_, err := p.CreatePR(context.Background(), scm.CreatePRRequest{RepoFullName: "bad"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCreatePRNoSelfLink(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": 1, "title": "T", "links": map[string]interface{}{"self": []interface{}{}},
		})
	})
	pr, err := p.CreatePR(context.Background(), scm.CreatePRRequest{
		RepoFullName: "P/r", SourceBranch: "f", TargetBranch: "m",
	})
	if err != nil {
		t.Fatal(err)
	}
	if pr.URL != "" {
		t.Errorf("url = %q, want empty", pr.URL)
	}
}

// --- GetPR ---

func TestGetPR(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": 5, "title": "Fix", "state": "OPEN",
			"author":  map[string]interface{}{"user": map[string]string{"name": "alice"}},
			"fromRef": map[string]string{"displayId": "feat"},
			"toRef":   map[string]string{"displayId": "main"},
		})
	})

	pr, err := p.GetPR(context.Background(), "P/r", 5)
	if err != nil {
		t.Fatal(err)
	}
	if pr.State != "open" {
		t.Errorf("state = %q", pr.State)
	}
	if pr.Author != "alice" {
		t.Errorf("author = %q", pr.Author)
	}
}

func TestGetPRMergedState(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": 1, "title": "T", "state": "MERGED",
			"author": map[string]interface{}{"user": map[string]string{"name": "u"}},
			"fromRef": map[string]string{"displayId": "f"}, "toRef": map[string]string{"displayId": "m"},
		})
	})
	pr, _ := p.GetPR(context.Background(), "P/r", 1)
	if pr.State != "merged" {
		t.Errorf("state = %q", pr.State)
	}
}

func TestGetPRDeclinedState(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": 1, "title": "T", "state": "DECLINED",
			"author": map[string]interface{}{"user": map[string]string{"name": "u"}},
			"fromRef": map[string]string{"displayId": "f"}, "toRef": map[string]string{"displayId": "m"},
		})
	})
	pr, _ := p.GetPR(context.Background(), "P/r", 1)
	if pr.State != "closed" {
		t.Errorf("state = %q", pr.State)
	}
}

func TestGetPRInvalidName(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {})
	_, err := p.GetPR(context.Background(), "bad", 1)
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- ListPRs ---

func TestListPRs(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"values": []map[string]interface{}{
				{
					"id": 1, "title": "PR1", "state": "OPEN",
					"author":  map[string]interface{}{"user": map[string]string{"name": "alice"}},
					"fromRef": map[string]string{"displayId": "feat-1"},
					"toRef":   map[string]string{"displayId": "main"},
					"links":   map[string]interface{}{"self": []map[string]string{{"href": "https://bb/pr/1"}}},
				},
			},
		})
	})

	prs, err := p.ListPRs(context.Background(), "P/r", scm.PRListOpts{State: "open", PageSize: 25})
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 1 {
		t.Fatalf("len = %d", len(prs))
	}
	if prs[0].State != "open" {
		t.Errorf("state = %q", prs[0].State)
	}
	if prs[0].URL != "https://bb/pr/1" {
		t.Errorf("url = %q", prs[0].URL)
	}
}

func TestListPRsClosedState(t *testing.T) {
	var gotPath string
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		json.NewEncoder(w).Encode(map[string]interface{}{"values": []interface{}{}})
	})
	p.ListPRs(context.Background(), "P/r", scm.PRListOpts{State: "closed", PageSize: 10})
	if !strings.Contains(gotPath, "state=DECLINED") {
		t.Errorf("path = %q, want state=DECLINED", gotPath)
	}
}

func TestListPRsAllState(t *testing.T) {
	var gotPath string
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		json.NewEncoder(w).Encode(map[string]interface{}{"values": []interface{}{}})
	})
	p.ListPRs(context.Background(), "P/r", scm.PRListOpts{State: "all", PageSize: 10})
	if !strings.Contains(gotPath, "state=ALL") {
		t.Errorf("path = %q, want state=ALL", gotPath)
	}
}

func TestListPRsNoSelfLink(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"values": []map[string]interface{}{
				{
					"id": 1, "title": "T", "state": "OPEN",
					"author":  map[string]interface{}{"user": map[string]string{"name": "u"}},
					"fromRef": map[string]string{"displayId": "f"},
					"toRef":   map[string]string{"displayId": "m"},
					"links":   map[string]interface{}{"self": []interface{}{}},
				},
			},
		})
	})
	prs, _ := p.ListPRs(context.Background(), "P/r", scm.PRListOpts{PageSize: 10})
	if prs[0].URL != "" {
		t.Errorf("url = %q, want empty", prs[0].URL)
	}
}

func TestListPRsInvalidName(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {})
	_, err := p.ListPRs(context.Background(), "bad", scm.PRListOpts{})
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- GetPRChangedFiles ---

func TestGetPRChangedFiles(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"values": []map[string]interface{}{
				{"path": map[string]string{"toString": "src/main.go"}},
				{"path": map[string]string{"toString": "README.md"}},
			},
		})
	})

	files, err := p.GetPRChangedFiles(context.Background(), "P/r", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("len = %d", len(files))
	}
	if files[0] != "src/main.go" {
		t.Errorf("files[0] = %q", files[0])
	}
}

func TestGetPRChangedFilesInvalidName(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {})
	_, err := p.GetPRChangedFiles(context.Background(), "bad", 1)
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- GetPRApprovals ---

func TestGetPRApprovals(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": 1, "title": "T", "state": "OPEN",
			"author":  map[string]interface{}{"user": map[string]string{"name": "u"}},
			"fromRef": map[string]string{"displayId": "f"},
			"toRef":   map[string]string{"displayId": "m"},
		})
	})

	count, err := p.GetPRApprovals(context.Background(), "P/r", 1)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("count = %d", count)
	}
}

// --- AddLabels ---

func TestAddLabels(t *testing.T) {
	var gotBody map[string]interface{}
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte("{}"))
	})

	err := p.AddLabels(context.Background(), "P/r", 1, []string{"ai-assisted", "feature"})
	if err != nil {
		t.Fatal(err)
	}
	text, _ := gotBody["text"].(string)
	if !strings.Contains(text, "ai-assisted") || !strings.Contains(text, "feature") {
		t.Errorf("text = %q", text)
	}
}

func TestAddLabelsInvalidName(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {})
	err := p.AddLabels(context.Background(), "bad", 1, []string{"l"})
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- SetPRStatus ---

func TestSetPRStatus(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte("{}"))
	})

	err := p.SetPRStatus(context.Background(), scm.SetStatusRequest{
		SHA: "abc123", State: "success", Context: "ci", Description: "passed", TargetURL: "https://ci/1",
	})
	if err != nil {
		t.Fatal(err)
	}
	// SetPRStatus uses /rest/build-status/1.0/commits/{sha} which gets prepended with /rest/api/1.0
	if !strings.Contains(gotPath, "abc123") {
		t.Errorf("path = %q, want to contain sha", gotPath)
	}
	if gotBody["state"] != "SUCCESS" {
		t.Errorf("state = %v", gotBody["state"])
	}
}

// --- MergePR ---

func TestMergePR(t *testing.T) {
	callCount := 0
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// GET PR for version
			json.NewEncoder(w).Encode(map[string]interface{}{"version": 3})
		} else {
			// POST merge
			if r.Method != "POST" {
				t.Errorf("merge method = %s", r.Method)
			}
			if !strings.Contains(r.URL.RawQuery, "version=3") {
				t.Errorf("query = %q, want version=3", r.URL.RawQuery)
			}
			w.Write([]byte("{}"))
		}
	})

	err := p.MergePR(context.Background(), "P/r", 1, scm.MergeOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if callCount != 2 {
		t.Errorf("callCount = %d, want 2", callCount)
	}
}

func TestMergePRInvalidName(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {})
	err := p.MergePR(context.Background(), "bad", 1, scm.MergeOpts{})
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- RegisterWebhook ---

func TestRegisterWebhook(t *testing.T) {
	var gotBody map[string]interface{}
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]interface{}{"id": 99})
	})

	id, err := p.RegisterWebhook(context.Background(), "P/r", []string{"pull_request", "push"}, "secret123")
	if err != nil {
		t.Fatal(err)
	}
	if id != "99" {
		t.Errorf("id = %q", id)
	}
	events, _ := gotBody["events"].([]interface{})
	if len(events) != 5 {
		t.Errorf("events = %v, want 5 (4 PR + 1 push)", events)
	}
}

func TestRegisterWebhookInvalidName(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {})
	_, err := p.RegisterWebhook(context.Background(), "bad", []string{"push"}, "s")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRegisterWebhookAPIError(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("error"))
	})
	_, err := p.RegisterWebhook(context.Background(), "P/r", []string{"push"}, "s")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- DeleteWebhook ---

func TestDeleteWebhook(t *testing.T) {
	var gotMethod, gotPath string
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Write([]byte("{}"))
	})

	err := p.DeleteWebhook(context.Background(), "P/r", "42")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != "DELETE" {
		t.Errorf("method = %s", gotMethod)
	}
	if !strings.Contains(gotPath, "webhooks/42") {
		t.Errorf("path = %q", gotPath)
	}
}

func TestDeleteWebhookInvalidName(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {})
	err := p.DeleteWebhook(context.Background(), "bad", "1")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- ParseWebhookPayload ---

func TestParseWebhookPROpened(t *testing.T) {
	payload := map[string]interface{}{
		"actor": map[string]string{"name": "alice"},
		"pullRequest": map[string]interface{}{
			"id": 10, "title": "New Feature",
			"fromRef": map[string]interface{}{
				"displayId": "feat-1",
				"repository": map[string]interface{}{
					"slug":    "repo",
					"project": map[string]string{"key": "PROJ"},
				},
			},
			"toRef": map[string]interface{}{"displayId": "main"},
			"author": map[string]interface{}{
				"user": map[string]string{"name": "bob"},
			},
			"links": map[string]interface{}{
				"self": []map[string]string{{"href": "https://bb/pr/10"}},
			},
		},
		"repository": map[string]interface{}{
			"slug":    "repo",
			"project": map[string]string{"key": "PROJ"},
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Event-Key", "pr:opened")

	p, _ := New("https://bb", "tok", zap.NewNop())
	event, err := p.ParseWebhookPayload(req, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != scm.EventPROpened {
		t.Errorf("type = %q", event.Type)
	}
	if event.RepoFullName != "PROJ/repo" {
		t.Errorf("repo = %q", event.RepoFullName)
	}
	if event.Sender != "alice" {
		t.Errorf("sender = %q", event.Sender)
	}
	if event.PR == nil {
		t.Fatal("PR is nil")
	}
	if event.PR.ID != 10 {
		t.Errorf("pr.id = %d", event.PR.ID)
	}
}

func TestParseWebhookPRModified(t *testing.T) {
	payload := map[string]interface{}{
		"actor":       map[string]string{"name": "u"},
		"pullRequest": map[string]interface{}{"id": 1, "title": "T", "fromRef": map[string]interface{}{"displayId": "f"}, "toRef": map[string]interface{}{"displayId": "m"}, "author": map[string]interface{}{"user": map[string]string{"name": "a"}}, "links": map[string]interface{}{"self": []interface{}{}}},
		"repository":  map[string]interface{}{"slug": "r", "project": map[string]string{"key": "P"}},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("X-Event-Key", "pr:modified")

	p, _ := New("https://bb", "tok", zap.NewNop())
	event, err := p.ParseWebhookPayload(req, "")
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != scm.EventPRUpdated {
		t.Errorf("type = %q", event.Type)
	}
}

func TestParseWebhookPRMerged(t *testing.T) {
	payload := map[string]interface{}{
		"actor":       map[string]string{"name": "u"},
		"pullRequest": map[string]interface{}{"id": 1, "title": "T", "fromRef": map[string]interface{}{"displayId": "f"}, "toRef": map[string]interface{}{"displayId": "m"}, "author": map[string]interface{}{"user": map[string]string{"name": "a"}}, "links": map[string]interface{}{"self": []interface{}{}}},
		"repository":  map[string]interface{}{"slug": "r", "project": map[string]string{"key": "P"}},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("X-Event-Key", "pr:merged")

	p, _ := New("https://bb", "tok", zap.NewNop())
	event, err := p.ParseWebhookPayload(req, "")
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != scm.EventPRMerged {
		t.Errorf("type = %q", event.Type)
	}
}

func TestParseWebhookPush(t *testing.T) {
	payload := map[string]interface{}{
		"actor":      map[string]string{"name": "u"},
		"repository": map[string]interface{}{"slug": "r", "project": map[string]string{"key": "P"}},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("X-Event-Key", "repo:refs_changed")

	p, _ := New("https://bb", "tok", zap.NewNop())
	event, err := p.ParseWebhookPayload(req, "")
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != scm.EventPush {
		t.Errorf("type = %q", event.Type)
	}
	if event.PR != nil {
		t.Error("push event should not have PR")
	}
}

func TestParseWebhookUnsupportedEvent(t *testing.T) {
	payload := map[string]interface{}{
		"actor":      map[string]string{"name": "u"},
		"repository": map[string]interface{}{"slug": "r", "project": map[string]string{"key": "P"}},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("X-Event-Key", "repo:comment:added")

	p, _ := New("https://bb", "tok", zap.NewNop())
	event, err := p.ParseWebhookPayload(req, "")
	if err != nil {
		t.Fatal(err)
	}
	if event != nil {
		t.Error("unsupported event should return nil")
	}
}

func TestParseWebhookMissingEventKey(t *testing.T) {
	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{}")))
	// No X-Event-Key header

	p, _ := New("https://bb", "tok", zap.NewNop())
	_, err := p.ParseWebhookPayload(req, "")
	if err == nil {
		t.Fatal("expected error for missing event key")
	}
}

func TestParseWebhookInvalidJSON(t *testing.T) {
	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("not json")))
	req.Header.Set("X-Event-Key", "pr:opened")

	p, _ := New("https://bb", "tok", zap.NewNop())
	_, err := p.ParseWebhookPayload(req, "")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseWebhookNoRepoProject(t *testing.T) {
	payload := map[string]interface{}{
		"actor":       map[string]string{"name": "u"},
		"pullRequest": map[string]interface{}{"id": 1, "title": "T", "fromRef": map[string]interface{}{"displayId": "f"}, "toRef": map[string]interface{}{"displayId": "m"}, "author": map[string]interface{}{"user": map[string]string{"name": "a"}}, "links": map[string]interface{}{"self": []interface{}{}}},
		"repository":  map[string]interface{}{"slug": "r", "project": map[string]string{}},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("X-Event-Key", "pr:opened")

	p, _ := New("https://bb", "tok", zap.NewNop())
	event, err := p.ParseWebhookPayload(req, "")
	if err != nil {
		t.Fatal(err)
	}
	if event.RepoFullName != "" {
		t.Errorf("repo = %q, want empty when project key missing", event.RepoFullName)
	}
}

// --- GetFileContent ---

func TestGetFileContent(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/raw/src/main.go") {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("at") != "main" {
			t.Errorf("at = %q", r.URL.Query().Get("at"))
		}
		w.Write([]byte("package main"))
	})

	content, err := p.GetFileContent(context.Background(), "P/r", "src/main.go", "main")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "package main" {
		t.Errorf("content = %q", string(content))
	}
}

func TestGetFileContentNoRef(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q, want empty", r.URL.RawQuery)
		}
		w.Write([]byte("data"))
	})

	_, err := p.GetFileContent(context.Background(), "P/r", "file.txt", "")
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetFileContentInvalidName(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {})
	_, err := p.GetFileContent(context.Background(), "bad", "f", "")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- GetTree ---

func TestGetTree(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"values": []string{"src/main.go", "README.md"},
		})
	})

	entries, err := p.GetTree(context.Background(), "P/r", "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("len = %d", len(entries))
	}
	if entries[0].Path != "src/main.go" {
		t.Errorf("path = %q", entries[0].Path)
	}
	if entries[0].Type != "blob" {
		t.Errorf("type = %q", entries[0].Type)
	}
}

func TestGetTreeNoRef(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RawQuery, "at=") {
			t.Errorf("query = %q, should not have at param", r.URL.RawQuery)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"values": []string{}})
	})

	_, err := p.GetTree(context.Background(), "P/r", "")
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetTreeInvalidName(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {})
	_, err := p.GetTree(context.Background(), "bad", "")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- GetBranchSHA ---

func TestGetBranchSHA(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"values": []map[string]string{
				{"latestCommit": "abc123def", "displayId": "main"},
			},
		})
	})

	sha, err := p.GetBranchSHA(context.Background(), "P/r", "main")
	if err != nil {
		t.Fatal(err)
	}
	if sha != "abc123def" {
		t.Errorf("sha = %q", sha)
	}
}

func TestGetBranchSHANotFound(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"values": []map[string]string{
				{"latestCommit": "xyz", "displayId": "develop"},
			},
		})
	})

	_, err := p.GetBranchSHA(context.Background(), "P/r", "main")
	if err == nil {
		t.Fatal("expected error for branch not found")
	}
}

func TestGetBranchSHAInvalidName(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {})
	_, err := p.GetBranchSHA(context.Background(), "bad", "main")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- CreateBranch ---

func TestCreateBranch(t *testing.T) {
	var gotBody map[string]interface{}
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte("{}"))
	})

	err := p.CreateBranch(context.Background(), "P/r", "feature/new", "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if gotBody["name"] != "feature/new" {
		t.Errorf("name = %v", gotBody["name"])
	}
	if gotBody["startPoint"] != "abc123" {
		t.Errorf("startPoint = %v", gotBody["startPoint"])
	}
}

func TestCreateBranchInvalidName(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {})
	err := p.CreateBranch(context.Background(), "bad", "b", "sha")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- CommitFiles ---

func TestCommitFiles(t *testing.T) {
	var paths []string
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Write([]byte("{}"))
	})

	_, err := p.CommitFiles(context.Background(), scm.CommitFilesRequest{
		RepoFullName: "P/r",
		Branch:       "feat",
		Message:      "add files",
		Files:        map[string]string{"AGENTS.md": "# Agents", ".editorconfig": "root = true"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Errorf("paths = %d, want 2", len(paths))
	}
}

func TestCommitFilesInvalidName(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {})
	_, err := p.CommitFiles(context.Background(), scm.CommitFilesRequest{RepoFullName: "bad", Files: map[string]string{"f": "c"}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCommitFilesAPIError(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("internal error"))
	})

	_, err := p.CommitFiles(context.Background(), scm.CommitFilesRequest{
		RepoFullName: "P/r", Branch: "b", Message: "m",
		Files: map[string]string{"f.txt": "content"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- parseBBPRInfo ---

func TestParseBBPRInfoWithLink(t *testing.T) {
	payload := struct {
		PullRequest struct {
			ID    int    `json:"id"`
			Title string `json:"title"`
			FromRef struct {
				DisplayID string `json:"displayId"`
			} `json:"fromRef"`
			ToRef struct {
				DisplayID string `json:"displayId"`
			} `json:"toRef"`
			Author struct {
				User struct {
					Name string `json:"name"`
				} `json:"user"`
			} `json:"author"`
			Links struct {
				Self []struct {
					Href string `json:"href"`
				} `json:"self"`
			} `json:"links"`
		} `json:"pullRequest"`
	}{}
	payload.PullRequest.ID = 42
	payload.PullRequest.Title = "My PR"
	payload.PullRequest.FromRef.DisplayID = "feat"
	payload.PullRequest.ToRef.DisplayID = "main"
	payload.PullRequest.Author.User.Name = "alice"
	payload.PullRequest.Links.Self = []struct {
		Href string `json:"href"`
	}{{"https://bb/pr/42"}}

	info := parseBBPRInfo(&payload)
	if info.ID != 42 {
		t.Errorf("id = %d", info.ID)
	}
	if info.URL != "https://bb/pr/42" {
		t.Errorf("url = %q", info.URL)
	}
	if info.Author != "alice" {
		t.Errorf("author = %q", info.Author)
	}
}

func TestParseBBPRInfoNoLink(t *testing.T) {
	payload := map[string]interface{}{
		"pullRequest": map[string]interface{}{
			"id": 1, "title": "T",
			"fromRef": map[string]string{"displayId": "f"},
			"toRef":   map[string]string{"displayId": "m"},
			"author":  map[string]interface{}{"user": map[string]string{"name": "u"}},
			"links":   map[string]interface{}{"self": []interface{}{}},
		},
	}
	info := parseBBPRInfo(&payload)
	if info.URL != "" {
		t.Errorf("url = %q, want empty", info.URL)
	}
}

// --- doRequest edge cases ---

func TestDoRequestURLConstruction(t *testing.T) {
	var gotURL string
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.Path
		w.Write([]byte("{}"))
	})
	p.doRequest(context.Background(), "GET", "/projects/P/repos/r", nil)
	if gotURL != "/rest/api/1.0/projects/P/repos/r" {
		t.Errorf("url = %q", gotURL)
	}
}

func TestDoRequest5xxError(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("server error"))
	})
	_, err := p.doRequest(context.Background(), "GET", "/fail", nil)
	if err == nil {
		t.Fatal("expected error for 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q, want to contain 500", err.Error())
	}
}

func TestDoRequestCancelledContext(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("{}"))
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.doRequest(ctx, "GET", "/test", nil)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// --- JSON unmarshal error paths ---

func TestGetRepoInvalidJSON(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	})
	_, err := p.GetRepo(context.Background(), "P/r")
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestListReposInvalidJSON(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("bad"))
	})
	_, err := p.ListRepos(context.Background(), scm.ListOpts{Page: 1, PageSize: 10})
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestCreatePRInvalidJSON(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("bad"))
	})
	_, err := p.CreatePR(context.Background(), scm.CreatePRRequest{RepoFullName: "P/r", SourceBranch: "f", TargetBranch: "m"})
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestGetPRInvalidJSON(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("bad"))
	})
	_, err := p.GetPR(context.Background(), "P/r", 1)
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestListPRsInvalidJSON(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("bad"))
	})
	_, err := p.ListPRs(context.Background(), "P/r", scm.PRListOpts{PageSize: 10})
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestGetPRChangedFilesInvalidJSON(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("bad"))
	})
	_, err := p.GetPRChangedFiles(context.Background(), "P/r", 1)
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestGetPRApprovalsAPIError(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("error"))
	})
	_, err := p.GetPRApprovals(context.Background(), "P/r", 1)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMergePRGetVersionError(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("error"))
	})
	err := p.MergePR(context.Background(), "P/r", 1, scm.MergeOpts{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRegisterWebhookInvalidJSON(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("bad"))
	})
	_, err := p.RegisterWebhook(context.Background(), "P/r", []string{"push"}, "s")
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestGetTreeInvalidJSON(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("bad"))
	})
	_, err := p.GetTree(context.Background(), "P/r", "main")
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestGetBranchSHAInvalidJSON(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("bad"))
	})
	_, err := p.GetBranchSHA(context.Background(), "P/r", "main")
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestGetBranchSHAAPIError(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("error"))
	})
	_, err := p.GetBranchSHA(context.Background(), "P/r", "main")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetRepoAPIError(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("error"))
	})
	_, err := p.GetRepo(context.Background(), "P/r")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestListReposAPIError(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("error"))
	})
	_, err := p.ListRepos(context.Background(), scm.ListOpts{Page: 1, PageSize: 10})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCreatePRAPIError(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("error"))
	})
	_, err := p.CreatePR(context.Background(), scm.CreatePRRequest{RepoFullName: "P/r", SourceBranch: "f", TargetBranch: "m"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetPRAPIError(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("error"))
	})
	_, err := p.GetPR(context.Background(), "P/r", 1)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestListPRsAPIError(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("error"))
	})
	_, err := p.ListPRs(context.Background(), "P/r", scm.PRListOpts{PageSize: 10})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetPRChangedFilesAPIError(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("error"))
	})
	_, err := p.GetPRChangedFiles(context.Background(), "P/r", 1)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetTreeAPIError(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("error"))
	})
	_, err := p.GetTree(context.Background(), "P/r", "main")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseWebhookReadBodyError(t *testing.T) {
	// Create a request with a body that's already been read/closed
	req := httptest.NewRequest("POST", "/", &errorReader{})
	req.Header.Set("X-Event-Key", "pr:opened")

	p, _ := New("https://bb", "tok", zap.NewNop())
	_, err := p.ParseWebhookPayload(req, "")
	if err == nil {
		t.Fatal("expected error reading body")
	}
}

// errorReader always returns an error on Read.
type errorReader struct{}

func (e *errorReader) Read(p []byte) (int, error) {
	return 0, fmt.Errorf("read error")
}

// doRequest: test json.Marshal error for body
func TestDoRequestMarshalError(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("{}"))
	})
	// channels can't be marshaled to JSON
	_, err := p.doRequest(context.Background(), "POST", "/test", map[string]interface{}{"bad": make(chan int)})
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

// --- unused import guard ---
var _ = fmt.Sprintf
