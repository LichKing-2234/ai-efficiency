package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/internal/scm"
	gh "github.com/google/go-github/v60/github"
	"go.uber.org/zap"
)

// setupGitHub creates a mock HTTP server and a Provider pointing at it.
func setupGitHub(t *testing.T, mux *http.ServeMux) *Provider {
	t.Helper()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	ghClient := gh.NewClient(nil).WithAuthToken("test-token")
	u, _ := url.Parse(server.URL + "/")
	ghClient.BaseURL = u

	return &Provider{
		client:  ghClient,
		baseURL: server.URL,
		logger:  zap.NewNop(),
	}
}

// --- New ---

func TestNewWithToken(t *testing.T) {
	p, err := New("https://api.github.com", "my-token", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if p.client == nil {
		t.Fatal("client is nil")
	}
	if p.baseURL != "https://api.github.com" {
		t.Errorf("baseURL = %q", p.baseURL)
	}
}

func TestNewWithoutToken(t *testing.T) {
	p, err := New("https://api.github.com", "", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if p.client == nil {
		t.Fatal("client is nil")
	}
}

func TestNewEnterpriseURL(t *testing.T) {
	p, err := New("https://github.example.com/api/v3", "tok", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if p.client == nil {
		t.Fatal("client is nil")
	}
}

func TestNewEnterpriseInvalidURL(t *testing.T) {
	_, err := New("://bad-url", "tok", zap.NewNop())
	if err == nil {
		t.Fatal("expected error for invalid enterprise URL")
	}
}

func TestNewEmptyBaseURL(t *testing.T) {
	p, err := New("", "tok", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if p.client == nil {
		t.Fatal("client is nil")
	}
}

// --- GetRepo ---

func TestGetRepoSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(gh.Repository{
			FullName:      strPtr("owner/repo"),
			Name:          strPtr("repo"),
			CloneURL:      strPtr("https://github.com/owner/repo.git"),
			DefaultBranch: strPtr("main"),
			Description:   strPtr("A test repo"),
			Private:       boolPtr(false),
		})
	})
	p := setupGitHub(t, mux)

	repo, err := p.GetRepo(context.Background(), "owner/repo")
	if err != nil {
		t.Fatal(err)
	}
	if repo.FullName != "owner/repo" {
		t.Errorf("FullName = %q", repo.FullName)
	}
	if repo.Name != "repo" {
		t.Errorf("Name = %q", repo.Name)
	}
	if repo.CloneURL != "https://github.com/owner/repo.git" {
		t.Errorf("CloneURL = %q", repo.CloneURL)
	}
	if repo.DefaultBranch != "main" {
		t.Errorf("DefaultBranch = %q", repo.DefaultBranch)
	}
	if repo.Private != false {
		t.Errorf("Private = %v", repo.Private)
	}
}

func TestGetRepoInvalidName(t *testing.T) {
	mux := http.NewServeMux()
	p := setupGitHub(t, mux)
	_, err := p.GetRepo(context.Background(), "noslash")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetRepoAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		json.NewEncoder(w).Encode(map[string]string{"message": "Not Found"})
	})
	p := setupGitHub(t, mux)
	_, err := p.GetRepo(context.Background(), "owner/repo")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- ListRepos ---

func TestListReposSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user/repos", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]*gh.Repository{
			{FullName: strPtr("owner/repo1"), Name: strPtr("repo1"), DefaultBranch: strPtr("main")},
			{FullName: strPtr("owner/repo2"), Name: strPtr("repo2"), DefaultBranch: strPtr("develop")},
		})
	})
	p := setupGitHub(t, mux)

	repos, err := p.ListRepos(context.Background(), scm.ListOpts{Page: 1, PageSize: 25})
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 {
		t.Fatalf("len = %d", len(repos))
	}
	if repos[0].FullName != "owner/repo1" {
		t.Errorf("repos[0].FullName = %q", repos[0].FullName)
	}
}

func TestListReposDefaultOpts(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user/repos", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]*gh.Repository{})
	})
	p := setupGitHub(t, mux)

	_, err := p.ListRepos(context.Background(), scm.ListOpts{Page: 0, PageSize: 0})
	if err != nil {
		t.Fatal(err)
	}
}

func TestListReposAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user/repos", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	})
	p := setupGitHub(t, mux)
	_, err := p.ListRepos(context.Background(), scm.ListOpts{Page: 1, PageSize: 10})
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- CreatePR ---

func TestCreatePRSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s", r.Method)
		}
		json.NewEncoder(w).Encode(gh.PullRequest{
			Number:    intPtr(42),
			Title:     strPtr("My PR"),
			HTMLURL:   strPtr("https://github.com/owner/repo/pull/42"),
			Additions: intPtr(10),
			Deletions: intPtr(5),
			User:      &gh.User{Login: strPtr("alice")},
			Head:      &gh.PullRequestBranch{Ref: strPtr("feature")},
			Base:      &gh.PullRequestBranch{Ref: strPtr("main")},
			State:     strPtr("open"),
		})
	})
	p := setupGitHub(t, mux)

	pr, err := p.CreatePR(context.Background(), scm.CreatePRRequest{
		RepoFullName: "owner/repo",
		Title:        "My PR",
		Body:         "description",
		SourceBranch: "feature",
		TargetBranch: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if pr.ID != 42 {
		t.Errorf("ID = %d", pr.ID)
	}
	if pr.Title != "My PR" {
		t.Errorf("Title = %q", pr.Title)
	}
}

func TestCreatePRInvalidName(t *testing.T) {
	mux := http.NewServeMux()
	p := setupGitHub(t, mux)
	_, err := p.CreatePR(context.Background(), scm.CreatePRRequest{RepoFullName: "bad"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCreatePRAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		json.NewEncoder(w).Encode(map[string]string{"message": "Validation Failed"})
	})
	p := setupGitHub(t, mux)
	_, err := p.CreatePR(context.Background(), scm.CreatePRRequest{
		RepoFullName: "owner/repo", SourceBranch: "f", TargetBranch: "m",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func boolPtr(b bool) *bool { return &b }
func intPtr(i int) *int    { return &i }

// --- GetPR ---

func TestGetPRSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/10", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(gh.PullRequest{
			Number:  intPtr(10),
			Title:   strPtr("Fix bug"),
			HTMLURL: strPtr("https://github.com/owner/repo/pull/10"),
			User:    &gh.User{Login: strPtr("bob")},
			Head:    &gh.PullRequestBranch{Ref: strPtr("fix-branch")},
			Base:    &gh.PullRequestBranch{Ref: strPtr("main")},
			State:   strPtr("open"),
			Labels:  []*gh.Label{{Name: strPtr("bug")}},
		})
	})
	p := setupGitHub(t, mux)

	pr, err := p.GetPR(context.Background(), "owner/repo", 10)
	if err != nil {
		t.Fatal(err)
	}
	if pr.ID != 10 {
		t.Errorf("ID = %d", pr.ID)
	}
	if pr.Author != "bob" {
		t.Errorf("Author = %q", pr.Author)
	}
	if pr.State != "open" {
		t.Errorf("State = %q", pr.State)
	}
	if len(pr.Labels) != 1 || pr.Labels[0] != "bug" {
		t.Errorf("Labels = %v", pr.Labels)
	}
}

func TestGetPRMergedState(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/1", func(w http.ResponseWriter, r *http.Request) {
		merged := true
		json.NewEncoder(w).Encode(gh.PullRequest{
			Number: intPtr(1),
			Title:  strPtr("T"),
			User:   &gh.User{Login: strPtr("u")},
			Head:   &gh.PullRequestBranch{Ref: strPtr("f")},
			Base:   &gh.PullRequestBranch{Ref: strPtr("m")},
			State:  strPtr("closed"),
			Merged: &merged,
		})
	})
	p := setupGitHub(t, mux)
	pr, err := p.GetPR(context.Background(), "owner/repo", 1)
	if err != nil {
		t.Fatal(err)
	}
	if pr.State != "merged" {
		t.Errorf("State = %q, want merged", pr.State)
	}
}

func TestGetPRClosedState(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(gh.PullRequest{
			Number: intPtr(1),
			Title:  strPtr("T"),
			User:   &gh.User{Login: strPtr("u")},
			Head:   &gh.PullRequestBranch{Ref: strPtr("f")},
			Base:   &gh.PullRequestBranch{Ref: strPtr("m")},
			State:  strPtr("closed"),
		})
	})
	p := setupGitHub(t, mux)
	pr, err := p.GetPR(context.Background(), "owner/repo", 1)
	if err != nil {
		t.Fatal(err)
	}
	if pr.State != "closed" {
		t.Errorf("State = %q, want closed", pr.State)
	}
}

func TestGetPRInvalidName(t *testing.T) {
	mux := http.NewServeMux()
	p := setupGitHub(t, mux)
	_, err := p.GetPR(context.Background(), "bad", 1)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetPRAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	})
	p := setupGitHub(t, mux)
	_, err := p.GetPR(context.Background(), "owner/repo", 1)
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- ListPRs ---

func TestListPRsSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != "open" {
			t.Errorf("state = %q", r.URL.Query().Get("state"))
		}
		json.NewEncoder(w).Encode([]*gh.PullRequest{
			{
				Number: intPtr(1), Title: strPtr("PR1"),
				User: &gh.User{Login: strPtr("alice")},
				Head: &gh.PullRequestBranch{Ref: strPtr("feat-1")},
				Base: &gh.PullRequestBranch{Ref: strPtr("main")},
				State: strPtr("open"),
			},
			{
				Number: intPtr(2), Title: strPtr("PR2"),
				User: &gh.User{Login: strPtr("bob")},
				Head: &gh.PullRequestBranch{Ref: strPtr("feat-2")},
				Base: &gh.PullRequestBranch{Ref: strPtr("main")},
				State: strPtr("open"),
			},
		})
	})
	p := setupGitHub(t, mux)

	prs, err := p.ListPRs(context.Background(), "owner/repo", scm.PRListOpts{State: "open", PageSize: 25})
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 2 {
		t.Fatalf("len = %d", len(prs))
	}
	if prs[0].Title != "PR1" {
		t.Errorf("prs[0].Title = %q", prs[0].Title)
	}
}

func TestListPRsDefaultState(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != "open" {
			t.Errorf("default state should be open, got %q", r.URL.Query().Get("state"))
		}
		json.NewEncoder(w).Encode([]*gh.PullRequest{})
	})
	p := setupGitHub(t, mux)
	_, err := p.ListPRs(context.Background(), "owner/repo", scm.PRListOpts{})
	if err != nil {
		t.Fatal(err)
	}
}

func TestListPRsInvalidName(t *testing.T) {
	mux := http.NewServeMux()
	p := setupGitHub(t, mux)
	_, err := p.ListPRs(context.Background(), "bad", scm.PRListOpts{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestListPRsAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	})
	p := setupGitHub(t, mux)
	_, err := p.ListPRs(context.Background(), "owner/repo", scm.PRListOpts{PageSize: 10})
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- GetPRChangedFiles ---

func TestGetPRChangedFilesSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/5/files", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]*gh.CommitFile{
			{Filename: strPtr("src/main.go")},
			{Filename: strPtr("README.md")},
		})
	})
	p := setupGitHub(t, mux)

	files, err := p.GetPRChangedFiles(context.Background(), "owner/repo", 5)
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
	mux := http.NewServeMux()
	p := setupGitHub(t, mux)
	_, err := p.GetPRChangedFiles(context.Background(), "bad", 1)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetPRChangedFilesAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/1/files", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	})
	p := setupGitHub(t, mux)
	_, err := p.GetPRChangedFiles(context.Background(), "owner/repo", 1)
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- GetPRApprovals ---

func TestGetPRApprovalsSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/3/reviews", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]*gh.PullRequestReview{
			{State: strPtr("APPROVED")},
			{State: strPtr("CHANGES_REQUESTED")},
			{State: strPtr("APPROVED")},
		})
	})
	p := setupGitHub(t, mux)

	count, err := p.GetPRApprovals(context.Background(), "owner/repo", 3)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestGetPRApprovalsZero(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/1/reviews", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]*gh.PullRequestReview{
			{State: strPtr("COMMENTED")},
		})
	})
	p := setupGitHub(t, mux)

	count, err := p.GetPRApprovals(context.Background(), "owner/repo", 1)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestGetPRApprovalsInvalidName(t *testing.T) {
	mux := http.NewServeMux()
	p := setupGitHub(t, mux)
	_, err := p.GetPRApprovals(context.Background(), "bad", 1)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetPRApprovalsAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/1/reviews", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	})
	p := setupGitHub(t, mux)
	_, err := p.GetPRApprovals(context.Background(), "owner/repo", 1)
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- AddLabels ---

func TestAddLabelsSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues/7/labels", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s", r.Method)
		}
		json.NewEncoder(w).Encode([]*gh.Label{
			{Name: strPtr("bug")},
			{Name: strPtr("enhancement")},
		})
	})
	p := setupGitHub(t, mux)

	err := p.AddLabels(context.Background(), "owner/repo", 7, []string{"bug", "enhancement"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAddLabelsInvalidName(t *testing.T) {
	mux := http.NewServeMux()
	p := setupGitHub(t, mux)
	err := p.AddLabels(context.Background(), "bad", 1, []string{"l"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAddLabelsAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues/1/labels", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	})
	p := setupGitHub(t, mux)
	err := p.AddLabels(context.Background(), "owner/repo", 1, []string{"l"})
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- SetPRStatus ---

func TestSetPRStatusSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/statuses/abc123", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s", r.Method)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["state"] != "success" {
			t.Errorf("state = %q", body["state"])
		}
		json.NewEncoder(w).Encode(gh.RepoStatus{})
	})
	p := setupGitHub(t, mux)

	err := p.SetPRStatus(context.Background(), scm.SetStatusRequest{
		RepoFullName: "owner/repo",
		SHA:          "abc123",
		State:        "success",
		Context:      "ci/test",
		Description:  "All tests passed",
		TargetURL:    "https://ci.example.com/1",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSetPRStatusInvalidName(t *testing.T) {
	mux := http.NewServeMux()
	p := setupGitHub(t, mux)
	err := p.SetPRStatus(context.Background(), scm.SetStatusRequest{RepoFullName: "bad"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSetPRStatusAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/statuses/sha1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	})
	p := setupGitHub(t, mux)
	err := p.SetPRStatus(context.Background(), scm.SetStatusRequest{
		RepoFullName: "owner/repo", SHA: "sha1", State: "failure",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- MergePR ---

func TestMergePRSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/5/merge", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			t.Errorf("method = %s", r.Method)
		}
		json.NewEncoder(w).Encode(gh.PullRequestMergeResult{
			Merged: boolPtr(true),
		})
	})
	p := setupGitHub(t, mux)

	err := p.MergePR(context.Background(), "owner/repo", 5, scm.MergeOpts{
		Method:  "squash",
		Message: "Squash merge",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMergePRDefaultMethod(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/1/merge", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(gh.PullRequestMergeResult{Merged: boolPtr(true)})
	})
	p := setupGitHub(t, mux)

	err := p.MergePR(context.Background(), "owner/repo", 1, scm.MergeOpts{})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMergePRInvalidName(t *testing.T) {
	mux := http.NewServeMux()
	p := setupGitHub(t, mux)
	err := p.MergePR(context.Background(), "bad", 1, scm.MergeOpts{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMergePRAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/1/merge", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(405)
		json.NewEncoder(w).Encode(map[string]string{"message": "not mergeable"})
	})
	p := setupGitHub(t, mux)
	err := p.MergePR(context.Background(), "owner/repo", 1, scm.MergeOpts{})
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- RegisterWebhook ---

func TestRegisterWebhookSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/hooks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s", r.Method)
		}
		id := int64(99)
		json.NewEncoder(w).Encode(gh.Hook{ID: &id})
	})
	p := setupGitHub(t, mux)

	id, err := p.RegisterWebhook(context.Background(), "owner/repo", []string{"push", "pull_request"}, "secret123")
	if err != nil {
		t.Fatal(err)
	}
	if id != "99" {
		t.Errorf("id = %q", id)
	}
}

func TestRegisterWebhookDefaultEvents(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/hooks", func(w http.ResponseWriter, r *http.Request) {
		id := int64(1)
		json.NewEncoder(w).Encode(gh.Hook{ID: &id})
	})
	p := setupGitHub(t, mux)

	_, err := p.RegisterWebhook(context.Background(), "owner/repo", nil, "s")
	if err != nil {
		t.Fatal(err)
	}
}

func TestRegisterWebhookInvalidName(t *testing.T) {
	mux := http.NewServeMux()
	p := setupGitHub(t, mux)
	_, err := p.RegisterWebhook(context.Background(), "bad", []string{"push"}, "s")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRegisterWebhookAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/hooks", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	})
	p := setupGitHub(t, mux)
	_, err := p.RegisterWebhook(context.Background(), "owner/repo", []string{"push"}, "s")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- DeleteWebhook ---

func TestDeleteWebhookSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/hooks/42", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("method = %s", r.Method)
		}
		w.WriteHeader(204)
	})
	p := setupGitHub(t, mux)

	err := p.DeleteWebhook(context.Background(), "owner/repo", "42")
	if err != nil {
		t.Fatal(err)
	}
}

func TestDeleteWebhookInvalidName(t *testing.T) {
	mux := http.NewServeMux()
	p := setupGitHub(t, mux)
	err := p.DeleteWebhook(context.Background(), "bad", "1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDeleteWebhookInvalidID(t *testing.T) {
	mux := http.NewServeMux()
	p := setupGitHub(t, mux)
	err := p.DeleteWebhook(context.Background(), "owner/repo", "not-a-number")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invalid webhook id") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestDeleteWebhookAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/hooks/1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	})
	p := setupGitHub(t, mux)
	err := p.DeleteWebhook(context.Background(), "owner/repo", "1")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- ParseWebhookPayload ---

func signPayload(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + fmt.Sprintf("%x", mac.Sum(nil))
}

func TestParseWebhookPROpened(t *testing.T) {
	payload := map[string]interface{}{
		"action": "opened",
		"number": 10,
		"pull_request": map[string]interface{}{
			"number":   10,
			"title":    "New Feature",
			"html_url": "https://github.com/owner/repo/pull/10",
			"merged":   false,
			"user":     map[string]string{"login": "bob"},
			"head":     map[string]string{"ref": "feat-1"},
			"base":     map[string]string{"ref": "main"},
		},
		"repository": map[string]interface{}{
			"full_name": "owner/repo",
		},
		"sender": map[string]string{"login": "alice"},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", signPayload(body, "secret"))

	p := &Provider{logger: zap.NewNop()}
	event, err := p.ParseWebhookPayload(req, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != scm.EventPROpened {
		t.Errorf("type = %q", event.Type)
	}
	if event.RepoFullName != "owner/repo" {
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

func TestParseWebhookPRReopened(t *testing.T) {
	payload := map[string]interface{}{
		"action": "reopened",
		"number": 5,
		"pull_request": map[string]interface{}{
			"number": 5, "title": "T", "merged": false,
			"user": map[string]string{"login": "u"},
			"head": map[string]string{"ref": "f"},
			"base": map[string]string{"ref": "m"},
		},
		"repository": map[string]interface{}{"full_name": "o/r"},
		"sender":     map[string]string{"login": "u"},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", signPayload(body, "s"))

	p := &Provider{logger: zap.NewNop()}
	event, err := p.ParseWebhookPayload(req, "s")
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != scm.EventPROpened {
		t.Errorf("type = %q", event.Type)
	}
}

func TestParseWebhookPRMerged(t *testing.T) {
	payload := map[string]interface{}{
		"action": "closed",
		"number": 3,
		"pull_request": map[string]interface{}{
			"number": 3, "title": "T", "merged": true,
			"user": map[string]string{"login": "u"},
			"head": map[string]string{"ref": "f"},
			"base": map[string]string{"ref": "m"},
		},
		"repository": map[string]interface{}{"full_name": "o/r"},
		"sender":     map[string]string{"login": "u"},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", signPayload(body, "s"))

	p := &Provider{logger: zap.NewNop()}
	event, err := p.ParseWebhookPayload(req, "s")
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != scm.EventPRMerged {
		t.Errorf("type = %q", event.Type)
	}
}

func TestParseWebhookPRClosedNotMerged(t *testing.T) {
	payload := map[string]interface{}{
		"action": "closed",
		"number": 3,
		"pull_request": map[string]interface{}{
			"number": 3, "title": "T", "merged": false,
			"user": map[string]string{"login": "u"},
			"head": map[string]string{"ref": "f"},
			"base": map[string]string{"ref": "m"},
		},
		"repository": map[string]interface{}{"full_name": "o/r"},
		"sender":     map[string]string{"login": "u"},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", signPayload(body, "s"))

	p := &Provider{logger: zap.NewNop()}
	event, err := p.ParseWebhookPayload(req, "s")
	if err != nil {
		t.Fatal(err)
	}
	if event != nil {
		t.Errorf("closed-not-merged should return nil, got %+v", event)
	}
}

func TestParseWebhookPRSynchronize(t *testing.T) {
	payload := map[string]interface{}{
		"action": "synchronize",
		"number": 1,
		"pull_request": map[string]interface{}{
			"number": 1, "title": "T", "merged": false,
			"user": map[string]string{"login": "u"},
			"head": map[string]string{"ref": "f"},
			"base": map[string]string{"ref": "m"},
		},
		"repository": map[string]interface{}{"full_name": "o/r"},
		"sender":     map[string]string{"login": "u"},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", signPayload(body, "s"))

	p := &Provider{logger: zap.NewNop()}
	event, err := p.ParseWebhookPayload(req, "s")
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != scm.EventPRUpdated {
		t.Errorf("type = %q", event.Type)
	}
}

func TestParseWebhookPREdited(t *testing.T) {
	payload := map[string]interface{}{
		"action": "edited",
		"number": 1,
		"pull_request": map[string]interface{}{
			"number": 1, "title": "T", "merged": false,
			"user": map[string]string{"login": "u"},
			"head": map[string]string{"ref": "f"},
			"base": map[string]string{"ref": "m"},
		},
		"repository": map[string]interface{}{"full_name": "o/r"},
		"sender":     map[string]string{"login": "u"},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", signPayload(body, "s"))

	p := &Provider{logger: zap.NewNop()}
	event, err := p.ParseWebhookPayload(req, "s")
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != scm.EventPRUpdated {
		t.Errorf("type = %q", event.Type)
	}
}

func TestParseWebhookPRUnknownAction(t *testing.T) {
	payload := map[string]interface{}{
		"action": "labeled",
		"number": 1,
		"pull_request": map[string]interface{}{
			"number": 1, "title": "T", "merged": false,
			"user": map[string]string{"login": "u"},
			"head": map[string]string{"ref": "f"},
			"base": map[string]string{"ref": "m"},
		},
		"repository": map[string]interface{}{"full_name": "o/r"},
		"sender":     map[string]string{"login": "u"},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", signPayload(body, "s"))

	p := &Provider{logger: zap.NewNop()}
	event, err := p.ParseWebhookPayload(req, "s")
	if err != nil {
		t.Fatal(err)
	}
	if event != nil {
		t.Errorf("unknown action should return nil, got %+v", event)
	}
}

func TestParseWebhookPush(t *testing.T) {
	payload := map[string]interface{}{
		"ref": "refs/heads/main",
		"repository": map[string]interface{}{
			"full_name": "owner/repo",
		},
		"sender": map[string]string{"login": "alice"},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", signPayload(body, "s"))

	p := &Provider{logger: zap.NewNop()}
	event, err := p.ParseWebhookPayload(req, "s")
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != scm.EventPush {
		t.Errorf("type = %q", event.Type)
	}
	if event.RepoFullName != "owner/repo" {
		t.Errorf("repo = %q", event.RepoFullName)
	}
	if event.Sender != "alice" {
		t.Errorf("sender = %q", event.Sender)
	}
}

func TestParseWebhookUnsupportedEvent(t *testing.T) {
	payload := map[string]interface{}{
		"action": "created",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-Hub-Signature-256", signPayload(body, "s"))

	p := &Provider{logger: zap.NewNop()}
	_, err := p.ParseWebhookPayload(req, "s")
	if err == nil {
		t.Fatal("expected error for unsupported event")
	}
	if !strings.Contains(err.Error(), "unsupported event type") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestParseWebhookInvalidSignature(t *testing.T) {
	body := []byte(`{"action":"opened"}`)
	req := httptest.NewRequest("POST", "/", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", "sha256=invalid")

	p := &Provider{logger: zap.NewNop()}
	_, err := p.ParseWebhookPayload(req, "secret")
	if err == nil {
		t.Fatal("expected error for invalid signature")
	}
}

func TestParseWebhookNoSecret(t *testing.T) {
	payload := map[string]interface{}{
		"ref":        "refs/heads/main",
		"repository": map[string]interface{}{"full_name": "o/r"},
		"sender":     map[string]string{"login": "u"},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "push")

	p := &Provider{logger: zap.NewNop()}
	event, err := p.ParseWebhookPayload(req, "")
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != scm.EventPush {
		t.Errorf("type = %q", event.Type)
	}
}

// --- GetFileContent ---

func TestGetFileContentSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/contents/src/main.go", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ref") != "main" {
			t.Errorf("ref = %q", r.URL.Query().Get("ref"))
		}
		content := base64.StdEncoding.EncodeToString([]byte("package main"))
		json.NewEncoder(w).Encode(gh.RepositoryContent{
			Content:  strPtr(content),
			Encoding: strPtr("base64"),
		})
	})
	p := setupGitHub(t, mux)

	data, err := p.GetFileContent(context.Background(), "owner/repo", "src/main.go", "main")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "package main" {
		t.Errorf("content = %q", string(data))
	}
}

func TestGetFileContentInvalidName(t *testing.T) {
	mux := http.NewServeMux()
	p := setupGitHub(t, mux)
	_, err := p.GetFileContent(context.Background(), "bad", "f", "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetFileContentAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/contents/missing.go", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		json.NewEncoder(w).Encode(map[string]string{"message": "Not Found"})
	})
	p := setupGitHub(t, mux)
	_, err := p.GetFileContent(context.Background(), "owner/repo", "missing.go", "main")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- GetTree ---

func TestGetTreeSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/git/trees/main", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("recursive") != "1" {
			t.Errorf("recursive = %q", r.URL.Query().Get("recursive"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"sha": "treeSHA",
			"tree": [
				{"path": "src/main.go", "type": "blob", "size": 100},
				{"path": "README.md", "type": "blob", "size": 50},
				{"path": "src", "type": "tree", "size": 0}
			]
		}`))
	})
	p := setupGitHub(t, mux)

	entries, err := p.GetTree(context.Background(), "owner/repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("len = %d", len(entries))
	}
	if entries[0].Path != "src/main.go" {
		t.Errorf("path = %q", entries[0].Path)
	}
	if entries[0].Type != "blob" {
		t.Errorf("type = %q", entries[0].Type)
	}
	if entries[0].Size != 100 {
		t.Errorf("size = %d", entries[0].Size)
	}
}

func TestGetTreeInvalidName(t *testing.T) {
	mux := http.NewServeMux()
	p := setupGitHub(t, mux)
	_, err := p.GetTree(context.Background(), "bad", "main")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetTreeAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/git/trees/main", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	})
	p := setupGitHub(t, mux)
	_, err := p.GetTree(context.Background(), "owner/repo", "main")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- GetBranchSHA ---

func TestGetBranchSHASuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/git/ref/heads/main", func(w http.ResponseWriter, r *http.Request) {
		sha := "abc123def456"
		json.NewEncoder(w).Encode(gh.Reference{
			Object: &gh.GitObject{SHA: &sha},
		})
	})
	p := setupGitHub(t, mux)

	sha, err := p.GetBranchSHA(context.Background(), "owner/repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	if sha != "abc123def456" {
		t.Errorf("sha = %q", sha)
	}
}

func TestGetBranchSHAInvalidName(t *testing.T) {
	mux := http.NewServeMux()
	p := setupGitHub(t, mux)
	_, err := p.GetBranchSHA(context.Background(), "bad", "main")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetBranchSHAAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/git/ref/heads/main", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	})
	p := setupGitHub(t, mux)
	_, err := p.GetBranchSHA(context.Background(), "owner/repo", "main")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- CreateBranch ---

func TestCreateBranchSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/git/refs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s", r.Method)
		}
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if body["ref"] != "refs/heads/feature/new" {
			t.Errorf("ref = %v", body["ref"])
		}
		json.NewEncoder(w).Encode(gh.Reference{})
	})
	p := setupGitHub(t, mux)

	err := p.CreateBranch(context.Background(), "owner/repo", "feature/new", "abc123")
	if err != nil {
		t.Fatal(err)
	}
}

func TestCreateBranchInvalidName(t *testing.T) {
	mux := http.NewServeMux()
	p := setupGitHub(t, mux)
	err := p.CreateBranch(context.Background(), "bad", "b", "sha")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCreateBranchAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/git/refs", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		json.NewEncoder(w).Encode(map[string]string{"message": "Reference already exists"})
	})
	p := setupGitHub(t, mux)
	err := p.CreateBranch(context.Background(), "owner/repo", "existing", "sha")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- CommitFiles ---

func TestCommitFilesSuccess(t *testing.T) {
	baseSHA := "base-sha-123"
	treeSHA := "tree-sha-456"
	newTreeSHA := "new-tree-sha-789"
	commitSHA := "commit-sha-abc"

	mux := http.NewServeMux()

	refStr := "refs/heads/feat"

	// GetRef
	mux.HandleFunc("/repos/owner/repo/git/ref/heads/feat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(gh.Reference{
				Ref:    &refStr,
				Object: &gh.GitObject{SHA: &baseSHA},
			})
		}
	})

	// GetTree (base tree)
	mux.HandleFunc("/repos/owner/repo/git/trees/base-sha-123", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(gh.Tree{SHA: &treeSHA})
	})

	// CreateTree
	mux.HandleFunc("/repos/owner/repo/git/trees", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			json.NewEncoder(w).Encode(gh.Tree{SHA: &newTreeSHA})
		}
	})

	// CreateCommit
	mux.HandleFunc("/repos/owner/repo/git/commits", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			json.NewEncoder(w).Encode(gh.Commit{SHA: &commitSHA})
		}
	})

	// UpdateRef
	mux.HandleFunc("/repos/owner/repo/git/refs/heads/feat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			json.NewEncoder(w).Encode(gh.Reference{Ref: &refStr, Object: &gh.GitObject{SHA: &commitSHA}})
		}
	})

	p := setupGitHub(t, mux)

	sha, err := p.CommitFiles(context.Background(), scm.CommitFilesRequest{
		RepoFullName: "owner/repo",
		Branch:       "feat",
		Message:      "add files",
		Files:        map[string]string{"file.txt": "content"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sha != commitSHA {
		t.Errorf("sha = %q", sha)
	}
}

func TestCommitFilesInvalidName(t *testing.T) {
	mux := http.NewServeMux()
	p := setupGitHub(t, mux)
	_, err := p.CommitFiles(context.Background(), scm.CommitFilesRequest{
		RepoFullName: "bad", Files: map[string]string{"f": "c"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCommitFilesGetRefError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/git/ref/heads/feat", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	})
	p := setupGitHub(t, mux)
	_, err := p.CommitFiles(context.Background(), scm.CommitFilesRequest{
		RepoFullName: "owner/repo", Branch: "feat", Message: "m",
		Files: map[string]string{"f": "c"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCommitFilesGetBaseTreeError(t *testing.T) {
	baseSHA := "base-sha"
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/git/ref/heads/feat", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(gh.Reference{Object: &gh.GitObject{SHA: &baseSHA}})
	})
	mux.HandleFunc("/repos/owner/repo/git/trees/base-sha", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	})
	p := setupGitHub(t, mux)
	_, err := p.CommitFiles(context.Background(), scm.CommitFilesRequest{
		RepoFullName: "owner/repo", Branch: "feat", Message: "m",
		Files: map[string]string{"f": "c"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCommitFilesCreateTreeError(t *testing.T) {
	baseSHA := "base-sha"
	treeSHA := "tree-sha"
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/git/ref/heads/feat", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(gh.Reference{Object: &gh.GitObject{SHA: &baseSHA}})
	})
	mux.HandleFunc("/repos/owner/repo/git/trees/base-sha", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(gh.Tree{SHA: &treeSHA})
	})
	mux.HandleFunc("/repos/owner/repo/git/trees", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			w.WriteHeader(500)
		}
	})
	p := setupGitHub(t, mux)
	_, err := p.CommitFiles(context.Background(), scm.CommitFilesRequest{
		RepoFullName: "owner/repo", Branch: "feat", Message: "m",
		Files: map[string]string{"f": "c"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCommitFilesCreateCommitError(t *testing.T) {
	baseSHA := "base-sha"
	treeSHA := "tree-sha"
	newTreeSHA := "new-tree-sha"
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/git/ref/heads/feat", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(gh.Reference{Object: &gh.GitObject{SHA: &baseSHA}})
	})
	mux.HandleFunc("/repos/owner/repo/git/trees/base-sha", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(gh.Tree{SHA: &treeSHA})
	})
	mux.HandleFunc("/repos/owner/repo/git/trees", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			json.NewEncoder(w).Encode(gh.Tree{SHA: &newTreeSHA})
		}
	})
	mux.HandleFunc("/repos/owner/repo/git/commits", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	})
	p := setupGitHub(t, mux)
	_, err := p.CommitFiles(context.Background(), scm.CommitFilesRequest{
		RepoFullName: "owner/repo", Branch: "feat", Message: "m",
		Files: map[string]string{"f": "c"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCommitFilesUpdateRefError(t *testing.T) {
	baseSHA := "base-sha"
	treeSHA := "tree-sha"
	newTreeSHA := "new-tree-sha"
	commitSHA := "commit-sha"
	refStr := "refs/heads/feat"
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/git/ref/heads/feat", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(gh.Reference{Ref: &refStr, Object: &gh.GitObject{SHA: &baseSHA}})
	})
	mux.HandleFunc("/repos/owner/repo/git/trees/base-sha", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(gh.Tree{SHA: &treeSHA})
	})
	mux.HandleFunc("/repos/owner/repo/git/trees", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			json.NewEncoder(w).Encode(gh.Tree{SHA: &newTreeSHA})
		}
	})
	mux.HandleFunc("/repos/owner/repo/git/commits", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			json.NewEncoder(w).Encode(gh.Commit{SHA: &commitSHA})
		}
	})
	mux.HandleFunc("/repos/owner/repo/git/refs/heads/feat", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	})
	p := setupGitHub(t, mux)
	_, err := p.CommitFiles(context.Background(), scm.CommitFilesRequest{
		RepoFullName: "owner/repo", Branch: "feat", Message: "m",
		Files: map[string]string{"f": "c"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- ghPRToSCM edge cases ---

func TestGhPRToSCMWithMergedAt(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/1", func(w http.ResponseWriter, r *http.Request) {
		// Return a PR with MergedAt set (this triggers the merged state via MergedAt != nil)
		w.Write([]byte(`{
			"number": 1,
			"title": "T",
			"state": "closed",
			"merged": false,
			"merged_at": "2024-01-01T00:00:00Z",
			"created_at": "2023-12-01T00:00:00Z",
			"user": {"login": "u"},
			"head": {"ref": "f"},
			"base": {"ref": "m"},
			"additions": 10,
			"deletions": 5,
			"labels": [{"name": "bug"}, {"name": "fix"}],
			"html_url": "https://github.com/owner/repo/pull/1"
		}`))
	})
	p := setupGitHub(t, mux)
	pr, err := p.GetPR(context.Background(), "owner/repo", 1)
	if err != nil {
		t.Fatal(err)
	}
	if pr.State != "merged" {
		t.Errorf("State = %q, want merged", pr.State)
	}
	if pr.LinesAdded != 10 {
		t.Errorf("LinesAdded = %d", pr.LinesAdded)
	}
	if pr.LinesDeleted != 5 {
		t.Errorf("LinesDeleted = %d", pr.LinesDeleted)
	}
	if len(pr.Labels) != 2 {
		t.Errorf("Labels = %v", pr.Labels)
	}
	if pr.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if pr.MergedAt.IsZero() {
		t.Error("MergedAt should not be zero")
	}
}

// unused import guards
var _ = base64.StdEncoding
var _ = fmt.Sprintf
var _ = strings.Contains
