package bitbucket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ai-efficiency/backend/internal/scm"
	"go.uber.org/zap"
)

// Provider implements scm.SCMProvider for Bitbucket Server.
type Provider struct {
	baseURL            string
	token              string
	webhookCallbackURL string
	client             *http.Client
	logger             *zap.Logger
}

// New creates a new Bitbucket Server SCM provider.
// webhookCallbackURL is the base URL for webhook callbacks (e.g., "https://ae.example.com/api/v1/webhooks/bitbucket").
func New(baseURL, token string, logger *zap.Logger, webhookCallbackURL ...string) (*Provider, error) {
	cbURL := ""
	if len(webhookCallbackURL) > 0 {
		cbURL = webhookCallbackURL[0]
	}
	return &Provider{
		baseURL:            strings.TrimRight(baseURL, "/"),
		token:              token,
		webhookCallbackURL: cbURL,
		client:             &http.Client{},
		logger:             logger,
	}, nil
}

func splitFullName(fullName string) (project, repo string, err error) {
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid repo full name: %s (expected project/repo)", fullName)
	}
	return parts[0], parts[1], nil
}

func (p *Provider) doRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(b)
	}

	url := p.baseURL + "/rest/api/1.0" + path
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("bitbucket API %s %s returned %d: %s", method, path, resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// doRequestRaw makes an HTTP request without the /rest/api/1.0 prefix.
func (p *Provider) doRequestRaw(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(b)
	}

	url := p.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("bitbucket API %s %s returned %d: %s", method, path, resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// GetRepo returns repo info.
func (p *Provider) GetRepo(ctx context.Context, fullName string) (*scm.Repo, error) {
	project, repo, err := splitFullName(fullName)
	if err != nil {
		return nil, err
	}

	data, err := p.doRequest(ctx, "GET", fmt.Sprintf("/projects/%s/repos/%s", project, repo), nil)
	if err != nil {
		return nil, err
	}

	var bbRepo struct {
		Slug    string `json:"slug"`
		Name    string `json:"name"`
		Project struct {
			Key string `json:"key"`
		} `json:"project"`
		Links struct {
			Clone []struct {
				Href string `json:"href"`
				Name string `json:"name"`
			} `json:"clone"`
		} `json:"links"`
	}
	if err := json.Unmarshal(data, &bbRepo); err != nil {
		return nil, err
	}

	cloneURL := ""
	for _, l := range bbRepo.Links.Clone {
		if l.Name == "http" || l.Name == "https" {
			cloneURL = l.Href
			break
		}
	}

	return &scm.Repo{
		FullName:      fullName,
		Name:          bbRepo.Name,
		CloneURL:      cloneURL,
		DefaultBranch: "main",
	}, nil
}

// ListRepos lists repos in the Bitbucket Server instance.
func (p *Provider) ListRepos(ctx context.Context, opts scm.ListOpts) ([]*scm.Repo, error) {
	path := fmt.Sprintf("/repos?limit=%d&start=%d", opts.PageSize, (opts.Page-1)*opts.PageSize)
	data, err := p.doRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Values []struct {
			Slug    string `json:"slug"`
			Name    string `json:"name"`
			Project struct {
				Key string `json:"key"`
			} `json:"project"`
		} `json:"values"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	var repos []*scm.Repo
	for _, r := range result.Values {
		repos = append(repos, &scm.Repo{
			FullName: r.Project.Key + "/" + r.Slug,
			Name:     r.Name,
		})
	}
	return repos, nil
}

// CreatePR creates a pull request.
func (p *Provider) CreatePR(ctx context.Context, req scm.CreatePRRequest) (*scm.PR, error) {
	project, repo, err := splitFullName(req.RepoFullName)
	if err != nil {
		return nil, err
	}

	body := map[string]interface{}{
		"title":       req.Title,
		"description": req.Body,
		"fromRef":     map[string]string{"id": "refs/heads/" + req.SourceBranch},
		"toRef":       map[string]string{"id": "refs/heads/" + req.TargetBranch},
	}

	data, err := p.doRequest(ctx, "POST", fmt.Sprintf("/projects/%s/repos/%s/pull-requests", project, repo), body)
	if err != nil {
		return nil, err
	}

	var bbPR struct {
		ID    int    `json:"id"`
		Title string `json:"title"`
		Links struct {
			Self []struct {
				Href string `json:"href"`
			} `json:"self"`
		} `json:"links"`
	}
	if err := json.Unmarshal(data, &bbPR); err != nil {
		return nil, err
	}

	url := ""
	if len(bbPR.Links.Self) > 0 {
		url = bbPR.Links.Self[0].Href
	}

	return &scm.PR{
		ID:    bbPR.ID,
		Title: bbPR.Title,
		URL:   url,
	}, nil
}

// GetPR returns a pull request.
func (p *Provider) GetPR(ctx context.Context, repoFullName string, prID int) (*scm.PR, error) {
	project, repo, err := splitFullName(repoFullName)
	if err != nil {
		return nil, err
	}

	data, err := p.doRequest(ctx, "GET", fmt.Sprintf("/projects/%s/repos/%s/pull-requests/%d", project, repo, prID), nil)
	if err != nil {
		return nil, err
	}

	var bbPR struct {
		ID     int    `json:"id"`
		Title  string `json:"title"`
		State  string `json:"state"`
		Author struct {
			User struct {
				Name string `json:"name"`
			} `json:"user"`
		} `json:"author"`
		FromRef struct {
			DisplayID string `json:"displayId"`
		} `json:"fromRef"`
		ToRef struct {
			DisplayID string `json:"displayId"`
		} `json:"toRef"`
	}
	if err := json.Unmarshal(data, &bbPR); err != nil {
		return nil, err
	}

	state := "open"
	switch bbPR.State {
	case "MERGED":
		state = "merged"
	case "DECLINED":
		state = "closed"
	}

	return &scm.PR{
		ID:           bbPR.ID,
		Title:        bbPR.Title,
		Author:       bbPR.Author.User.Name,
		SourceBranch: bbPR.FromRef.DisplayID,
		TargetBranch: bbPR.ToRef.DisplayID,
		State:        state,
	}, nil
}

// ListPRs lists pull requests.
func (p *Provider) ListPRs(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
	project, repo, err := splitFullName(repoFullName)
	if err != nil {
		return nil, err
	}

	state := "OPEN"
	if opts.State == "closed" {
		state = "DECLINED"
	} else if opts.State == "all" {
		state = "ALL"
	}

	data, err := p.doRequest(ctx, "GET", fmt.Sprintf("/projects/%s/repos/%s/pull-requests?state=%s&limit=%d", project, repo, state, opts.PageSize), nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Values []struct {
			ID     int    `json:"id"`
			Title  string `json:"title"`
			State  string `json:"state"`
			Author struct {
				User struct {
					Name string `json:"name"`
				} `json:"user"`
			} `json:"author"`
			FromRef struct {
				DisplayID string `json:"displayId"`
			} `json:"fromRef"`
			ToRef struct {
				DisplayID string `json:"displayId"`
			} `json:"toRef"`
			Links struct {
				Self []struct {
					Href string `json:"href"`
				} `json:"self"`
			} `json:"links"`
		} `json:"values"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	var prs []*scm.PR
	for _, v := range result.Values {
		url := ""
		if len(v.Links.Self) > 0 {
			url = v.Links.Self[0].Href
		}
		prs = append(prs, &scm.PR{
			ID:           v.ID,
			Title:        v.Title,
			Author:       v.Author.User.Name,
			SourceBranch: v.FromRef.DisplayID,
			TargetBranch: v.ToRef.DisplayID,
			State:        strings.ToLower(v.State),
			URL:          url,
		})
	}
	return prs, nil
}

// GetPRChangedFiles returns changed files in a PR.
func (p *Provider) GetPRChangedFiles(ctx context.Context, repoFullName string, prID int) ([]string, error) {
	project, repo, err := splitFullName(repoFullName)
	if err != nil {
		return nil, err
	}

	data, err := p.doRequest(ctx, "GET", fmt.Sprintf("/projects/%s/repos/%s/pull-requests/%d/changes?limit=1000", project, repo, prID), nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Values []struct {
			Path struct {
				ToString string `json:"toString"`
			} `json:"path"`
		} `json:"values"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	var files []string
	for _, v := range result.Values {
		files = append(files, v.Path.ToString)
	}
	return files, nil
}

// GetPRApprovals returns the number of approvals on a PR.
func (p *Provider) GetPRApprovals(ctx context.Context, repoFullName string, prID int) (int, error) {
	pr, err := p.GetPR(ctx, repoFullName, prID)
	if err != nil {
		return 0, err
	}
	_ = pr
	// Bitbucket Server doesn't return approval count in the PR response directly
	// Would need to check participants/reviewers — simplified for now
	return 0, nil
}

// AddLabels adds labels to a PR (Bitbucket Server doesn't natively support PR labels, use comments instead).
func (p *Provider) AddLabels(ctx context.Context, repoFullName string, prID int, labels []string) error {
	project, repo, err := splitFullName(repoFullName)
	if err != nil {
		return err
	}

	comment := "AI Efficiency Labels: " + strings.Join(labels, ", ")
	body := map[string]interface{}{
		"text": comment,
	}

	_, err = p.doRequest(ctx, "POST", fmt.Sprintf("/projects/%s/repos/%s/pull-requests/%d/comments", project, repo, prID), body)
	return err
}

// SetPRStatus sets a commit status.
func (p *Provider) SetPRStatus(ctx context.Context, req scm.SetStatusRequest) error {
	body := map[string]interface{}{
		"state":       strings.ToUpper(req.State),
		"key":         req.Context,
		"description": req.Description,
		"url":         req.TargetURL,
	}

	_, err := p.doRequestRaw(ctx, "POST", fmt.Sprintf("/rest/build-status/1.0/commits/%s", req.SHA), body)
	return err
}

// MergePR merges a pull request.
func (p *Provider) MergePR(ctx context.Context, repoFullName string, prID int, opts scm.MergeOpts) error {
	project, repo, err := splitFullName(repoFullName)
	if err != nil {
		return err
	}

	// Get current PR version for optimistic locking
	pr, err := p.doRequest(ctx, "GET", fmt.Sprintf("/projects/%s/repos/%s/pull-requests/%d", project, repo, prID), nil)
	if err != nil {
		return err
	}

	var prData struct {
		Version int `json:"version"`
	}
	json.Unmarshal(pr, &prData)

	_, err = p.doRequest(ctx, "POST", fmt.Sprintf("/projects/%s/repos/%s/pull-requests/%d/merge?version=%d", project, repo, prID, prData.Version), nil)
	return err
}

// RegisterWebhook registers a webhook.
func (p *Provider) RegisterWebhook(ctx context.Context, repoFullName string, events []string, secret string) (string, error) {
	project, repo, err := splitFullName(repoFullName)
	if err != nil {
		return "", err
	}

	bbEvents := []string{}
	for _, e := range events {
		switch e {
		case "pull_request":
			bbEvents = append(bbEvents, "pr:opened", "pr:modified", "pr:merged", "pr:declined")
		case "push":
			bbEvents = append(bbEvents, "repo:refs_changed")
		}
	}

	body := map[string]interface{}{
		"name":          "ai-efficiency",
		"events":        bbEvents,
		"configuration": map[string]string{"secret": secret},
		"url":           p.webhookCallbackURL,
		"active":        true,
	}

	data, err := p.doRequest(ctx, "POST", fmt.Sprintf("/projects/%s/repos/%s/webhooks", project, repo), body)
	if err != nil {
		return "", err
	}

	var result struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", err
	}

	return fmt.Sprintf("%d", result.ID), nil
}

// DeleteWebhook deletes a webhook.
func (p *Provider) DeleteWebhook(ctx context.Context, repoFullName string, webhookID string) error {
	project, repo, err := splitFullName(repoFullName)
	if err != nil {
		return err
	}

	_, err = p.doRequest(ctx, "DELETE", fmt.Sprintf("/projects/%s/repos/%s/webhooks/%s", project, repo, webhookID), nil)
	return err
}

// ParseWebhookPayload parses a Bitbucket Server webhook payload.
func (p *Provider) ParseWebhookPayload(r *http.Request, secret string) (*scm.WebhookEvent, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	eventKey := r.Header.Get("X-Event-Key")
	if eventKey == "" {
		return nil, fmt.Errorf("missing X-Event-Key header")
	}

	var payload struct {
		Actor struct {
			Name string `json:"name"`
		} `json:"actor"`
		PullRequest struct {
			ID    int    `json:"id"`
			Title string `json:"title"`
			FromRef struct {
				DisplayID  string `json:"displayId"`
				Repository struct {
					Slug    string `json:"slug"`
					Project struct {
						Key string `json:"key"`
					} `json:"project"`
				} `json:"repository"`
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
		Repository struct {
			Slug    string `json:"slug"`
			Project struct {
				Key string `json:"key"`
			} `json:"project"`
		} `json:"repository"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}

	repoFullName := ""
	if payload.Repository.Project.Key != "" {
		repoFullName = payload.Repository.Project.Key + "/" + payload.Repository.Slug
	}

	event := &scm.WebhookEvent{
		RepoFullName: repoFullName,
		Sender:       payload.Actor.Name,
		Raw:          body,
	}

	switch eventKey {
	case "pr:opened":
		event.Type = scm.EventPROpened
		event.PR = parseBBPRInfo(&payload)
	case "pr:modified":
		event.Type = scm.EventPRUpdated
		event.PR = parseBBPRInfo(&payload)
	case "pr:merged":
		event.Type = scm.EventPRMerged
		event.PR = parseBBPRInfo(&payload)
	case "repo:refs_changed":
		event.Type = scm.EventPush
	default:
		return nil, nil // unsupported event
	}

	return event, nil
}

func parseBBPRInfo(payload interface{}) *scm.PRInfo {
	// Type assert to access the parsed payload
	type bbPayload struct {
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
	}

	// Re-marshal and unmarshal to get the right type
	b, _ := json.Marshal(payload)
	var p bbPayload
	json.Unmarshal(b, &p)

	url := ""
	if len(p.PullRequest.Links.Self) > 0 {
		url = p.PullRequest.Links.Self[0].Href
	}

	return &scm.PRInfo{
		ID:           p.PullRequest.ID,
		Title:        p.PullRequest.Title,
		Author:       p.PullRequest.Author.User.Name,
		SourceBranch: p.PullRequest.FromRef.DisplayID,
		TargetBranch: p.PullRequest.ToRef.DisplayID,
		URL:          url,
	}
}

// GetFileContent returns file content.
func (p *Provider) GetFileContent(ctx context.Context, repoFullName, path, ref string) ([]byte, error) {
	project, repo, err := splitFullName(repoFullName)
	if err != nil {
		return nil, err
	}

	apiPath := fmt.Sprintf("/projects/%s/repos/%s/raw/%s", project, repo, path)
	if ref != "" {
		apiPath += "?at=" + ref
	}

	return p.doRequest(ctx, "GET", apiPath, nil)
}

// GetTree returns the file tree.
func (p *Provider) GetTree(ctx context.Context, repoFullName, ref string) ([]*scm.TreeEntry, error) {
	project, repo, err := splitFullName(repoFullName)
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("/projects/%s/repos/%s/files?limit=1000", project, repo)
	if ref != "" {
		path += "&at=" + ref
	}

	data, err := p.doRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Values []string `json:"values"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	var entries []*scm.TreeEntry
	for _, f := range result.Values {
		entries = append(entries, &scm.TreeEntry{Path: f, Type: "blob"})
	}
	return entries, nil
}

// GetBranchSHA returns the latest commit SHA for a branch.
func (p *Provider) GetBranchSHA(ctx context.Context, repoFullName, branch string) (string, error) {
	project, repo, err := splitFullName(repoFullName)
	if err != nil {
		return "", err
	}

	resp, err := p.doRequest(ctx, "GET", fmt.Sprintf("/projects/%s/repos/%s/branches?filterText=%s&limit=1", project, repo, branch), nil)
	if err != nil {
		return "", fmt.Errorf("bitbucket get branch: %w", err)
	}

	var result struct {
		Values []struct {
			LatestCommit string `json:"latestCommit"`
			DisplayID    string `json:"displayId"`
		} `json:"values"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("parse branch response: %w", err)
	}
	for _, b := range result.Values {
		if b.DisplayID == branch {
			return b.LatestCommit, nil
		}
	}
	return "", fmt.Errorf("branch %s not found", branch)
}

// CreateBranch creates a new branch.
func (p *Provider) CreateBranch(ctx context.Context, repoFullName, branchName, baseSHA string) error {
	project, repo, err := splitFullName(repoFullName)
	if err != nil {
		return err
	}

	body := map[string]interface{}{
		"name":       branchName,
		"startPoint": baseSHA,
	}

	_, err = p.doRequest(ctx, "POST", fmt.Sprintf("/projects/%s/repos/%s/branches", project, repo), body)
	return err
}

// CommitFiles commits files to a branch.
func (p *Provider) CommitFiles(ctx context.Context, req scm.CommitFilesRequest) (string, error) {
	project, repo, err := splitFullName(req.RepoFullName)
	if err != nil {
		return "", err
	}

	// Bitbucket Server doesn't have a direct multi-file commit API like GitHub
	// Use the browse endpoint to upload files one at a time
	for path, content := range req.Files {
		body := map[string]interface{}{
			"content": content,
			"message": req.Message,
			"branch":  req.Branch,
		}
		_, err := p.doRequest(ctx, "PUT", fmt.Sprintf("/projects/%s/repos/%s/browse/%s", project, repo, path), body)
		if err != nil {
			return "", fmt.Errorf("commit file %s: %w", path, err)
		}
	}

	return "", nil
}
