package github

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ai-efficiency/backend/internal/scm"
	gh "github.com/google/go-github/v60/github"
	"go.uber.org/zap"
)

// Provider implements scm.SCMProvider for GitHub.
type Provider struct {
	client  *gh.Client
	baseURL string
	logger  *zap.Logger
}

// New creates a new GitHub SCM provider.
func New(baseURL, token string, logger *zap.Logger) (*Provider, error) {
	var client *gh.Client
	if token != "" {
		client = gh.NewClient(nil).WithAuthToken(token)
	} else {
		client = gh.NewClient(nil)
	}

	// If custom base URL (GitHub Enterprise), configure it
	if baseURL != "" && baseURL != "https://api.github.com" {
		var err error
		client, err = client.WithEnterpriseURLs(baseURL, baseURL)
		if err != nil {
			return nil, fmt.Errorf("github enterprise url: %w", err)
		}
	}

	return &Provider{
		client:  client,
		baseURL: baseURL,
		logger:  logger,
	}, nil
}

func splitFullName(fullName string) (owner, repo string, err error) {
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid repo full name: %s", fullName)
	}
	return parts[0], parts[1], nil
}

func (p *Provider) GetRepo(ctx context.Context, fullName string) (*scm.Repo, error) {
	owner, repo, err := splitFullName(fullName)
	if err != nil {
		return nil, err
	}
	r, _, err := p.client.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("github get repo: %w", err)
	}
	return &scm.Repo{
		FullName:      r.GetFullName(),
		Name:          r.GetName(),
		CloneURL:      r.GetCloneURL(),
		DefaultBranch: r.GetDefaultBranch(),
		Description:   r.GetDescription(),
		Private:       r.GetPrivate(),
	}, nil
}

func (p *Provider) ListRepos(ctx context.Context, opts scm.ListOpts) ([]*scm.Repo, error) {
	if opts.PageSize <= 0 {
		opts.PageSize = 30
	}
	if opts.Page <= 0 {
		opts.Page = 1
	}
	ghRepos, _, err := p.client.Repositories.List(ctx, "", &gh.RepositoryListOptions{
		ListOptions: gh.ListOptions{
			Page:    opts.Page,
			PerPage: opts.PageSize,
		},
		Sort: "updated",
	})
	if err != nil {
		return nil, fmt.Errorf("github list repos: %w", err)
	}
	repos := make([]*scm.Repo, 0, len(ghRepos))
	for _, r := range ghRepos {
		repos = append(repos, &scm.Repo{
			FullName:      r.GetFullName(),
			Name:          r.GetName(),
			CloneURL:      r.GetCloneURL(),
			DefaultBranch: r.GetDefaultBranch(),
			Description:   r.GetDescription(),
			Private:       r.GetPrivate(),
		})
	}
	return repos, nil
}

func (p *Provider) CreatePR(ctx context.Context, req scm.CreatePRRequest) (*scm.PR, error) {
	owner, repo, err := splitFullName(req.RepoFullName)
	if err != nil {
		return nil, err
	}
	pr, _, err := p.client.PullRequests.Create(ctx, owner, repo, &gh.NewPullRequest{
		Title: &req.Title,
		Body:  &req.Body,
		Head:  &req.SourceBranch,
		Base:  &req.TargetBranch,
	})
	if err != nil {
		return nil, fmt.Errorf("github create pr: %w", err)
	}
	return ghPRToSCM(pr), nil
}

func (p *Provider) GetPR(ctx context.Context, repoFullName string, prID int) (*scm.PR, error) {
	owner, repo, err := splitFullName(repoFullName)
	if err != nil {
		return nil, err
	}
	pr, _, err := p.client.PullRequests.Get(ctx, owner, repo, prID)
	if err != nil {
		return nil, fmt.Errorf("github get pr: %w", err)
	}
	return ghPRToSCM(pr), nil
}

func (p *Provider) ListPRs(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
	owner, repo, err := splitFullName(repoFullName)
	if err != nil {
		return nil, err
	}
	state := opts.State
	if state == "" {
		state = "open"
	}
	prs, _, err := p.client.PullRequests.List(ctx, owner, repo, &gh.PullRequestListOptions{
		State:       state,
		ListOptions: gh.ListOptions{Page: opts.Page, PerPage: opts.PageSize},
	})
	if err != nil {
		return nil, fmt.Errorf("github list prs: %w", err)
	}
	result := make([]*scm.PR, 0, len(prs))
	for _, pr := range prs {
		result = append(result, ghPRToSCM(pr))
	}
	return result, nil
}

func (p *Provider) GetPRChangedFiles(ctx context.Context, repoFullName string, prID int) ([]string, error) {
	owner, repo, err := splitFullName(repoFullName)
	if err != nil {
		return nil, err
	}
	files, _, err := p.client.PullRequests.ListFiles(ctx, owner, repo, prID, nil)
	if err != nil {
		return nil, fmt.Errorf("github list pr files: %w", err)
	}
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.GetFilename())
	}
	return paths, nil
}

func (p *Provider) GetPRApprovals(ctx context.Context, repoFullName string, prID int) (int, error) {
	owner, repo, err := splitFullName(repoFullName)
	if err != nil {
		return 0, err
	}
	reviews, _, err := p.client.PullRequests.ListReviews(ctx, owner, repo, prID, nil)
	if err != nil {
		return 0, fmt.Errorf("github list reviews: %w", err)
	}
	count := 0
	for _, r := range reviews {
		if r.GetState() == "APPROVED" {
			count++
		}
	}
	return count, nil
}

func (p *Provider) AddLabels(ctx context.Context, repoFullName string, prID int, labels []string) error {
	owner, repo, err := splitFullName(repoFullName)
	if err != nil {
		return err
	}
	_, _, err = p.client.Issues.AddLabelsToIssue(ctx, owner, repo, prID, labels)
	if err != nil {
		return fmt.Errorf("github add labels: %w", err)
	}
	return nil
}

func (p *Provider) SetPRStatus(ctx context.Context, req scm.SetStatusRequest) error {
	owner, repo, err := splitFullName(req.RepoFullName)
	if err != nil {
		return err
	}
	_, _, err = p.client.Repositories.CreateStatus(ctx, owner, repo, req.SHA, &gh.RepoStatus{
		State:       &req.State,
		Context:     &req.Context,
		Description: &req.Description,
		TargetURL:   &req.TargetURL,
	})
	if err != nil {
		return fmt.Errorf("github set status: %w", err)
	}
	return nil
}

func (p *Provider) MergePR(ctx context.Context, repoFullName string, prID int, opts scm.MergeOpts) error {
	owner, repo, err := splitFullName(repoFullName)
	if err != nil {
		return err
	}
	method := opts.Method
	if method == "" {
		method = "merge"
	}
	_, _, err = p.client.PullRequests.Merge(ctx, owner, repo, prID, opts.Message, &gh.PullRequestOptions{
		MergeMethod: method,
	})
	if err != nil {
		return fmt.Errorf("github merge pr: %w", err)
	}
	return nil
}

func (p *Provider) RegisterWebhook(ctx context.Context, repoFullName string, events []string, secret string) (string, error) {
	owner, repo, err := splitFullName(repoFullName)
	if err != nil {
		return "", err
	}
	if len(events) == 0 {
		events = []string{"pull_request", "push"}
	}
	active := true
	hook, _, err := p.client.Repositories.CreateHook(ctx, owner, repo, &gh.Hook{
		Config: &gh.HookConfig{
			ContentType: strPtr("json"),
			Secret:      &secret,
		},
		Events: events,
		Active: &active,
	})
	if err != nil {
		return "", fmt.Errorf("github create webhook: %w", err)
	}
	return strconv.FormatInt(hook.GetID(), 10), nil
}

func (p *Provider) DeleteWebhook(ctx context.Context, repoFullName string, webhookID string) error {
	owner, repo, err := splitFullName(repoFullName)
	if err != nil {
		return err
	}
	id, err := strconv.ParseInt(webhookID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid webhook id: %w", err)
	}
	_, err = p.client.Repositories.DeleteHook(ctx, owner, repo, id)
	if err != nil {
		return fmt.Errorf("github delete webhook: %w", err)
	}
	return nil
}

func (p *Provider) ParseWebhookPayload(r *http.Request, secret string) (*scm.WebhookEvent, error) {
	payload, err := gh.ValidatePayload(r, []byte(secret))
	if err != nil {
		return nil, fmt.Errorf("validate payload: %w", err)
	}

	event, err := gh.ParseWebHook(gh.WebHookType(r), payload)
	if err != nil {
		return nil, fmt.Errorf("parse webhook: %w", err)
	}

	switch e := event.(type) {
	case *gh.PullRequestEvent:
		return parsePREvent(e, payload), nil
	case *gh.PushEvent:
		return &scm.WebhookEvent{
			Type:         scm.EventPush,
			RepoFullName: e.GetRepo().GetFullName(),
			Sender:       e.GetSender().GetLogin(),
			Raw:          payload,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported event type: %s", gh.WebHookType(r))
	}
}

func (p *Provider) GetFileContent(ctx context.Context, repoFullName, path, ref string) ([]byte, error) {
	owner, repo, err := splitFullName(repoFullName)
	if err != nil {
		return nil, err
	}
	fc, _, _, err := p.client.Repositories.GetContents(ctx, owner, repo, path, &gh.RepositoryContentGetOptions{Ref: ref})
	if err != nil {
		return nil, fmt.Errorf("github get file: %w", err)
	}
	content, err := fc.GetContent()
	if err != nil {
		return nil, fmt.Errorf("decode content: %w", err)
	}
	return []byte(content), nil
}

func (p *Provider) GetTree(ctx context.Context, repoFullName, ref string) ([]*scm.TreeEntry, error) {
	owner, repo, err := splitFullName(repoFullName)
	if err != nil {
		return nil, err
	}
	tree, _, err := p.client.Git.GetTree(ctx, owner, repo, ref, true)
	if err != nil {
		return nil, fmt.Errorf("github get tree: %w", err)
	}
	entries := make([]*scm.TreeEntry, 0, len(tree.Entries))
	for _, e := range tree.Entries {
		entries = append(entries, &scm.TreeEntry{
			Path: e.GetPath(),
			Type: e.GetType(),
			Size: int64(e.GetSize()),
		})
	}
	return entries, nil
}

func (p *Provider) GetBranchSHA(ctx context.Context, repoFullName, branch string) (string, error) {
	owner, repo, err := splitFullName(repoFullName)
	if err != nil {
		return "", err
	}
	ref, _, err := p.client.Git.GetRef(ctx, owner, repo, "refs/heads/"+branch)
	if err != nil {
		return "", fmt.Errorf("github get branch sha: %w", err)
	}
	return ref.GetObject().GetSHA(), nil
}

func (p *Provider) CreateBranch(ctx context.Context, repoFullName, branchName, baseSHA string) error {
	owner, repo, err := splitFullName(repoFullName)
	if err != nil {
		return err
	}
	ref := "refs/heads/" + branchName
	_, _, err = p.client.Git.CreateRef(ctx, owner, repo, &gh.Reference{
		Ref:    &ref,
		Object: &gh.GitObject{SHA: &baseSHA},
	})
	if err != nil {
		return fmt.Errorf("github create branch: %w", err)
	}
	return nil
}

func (p *Provider) CommitFiles(ctx context.Context, req scm.CommitFilesRequest) (string, error) {
	owner, repo, err := splitFullName(req.RepoFullName)
	if err != nil {
		return "", err
	}

	// Get the reference for the branch
	ref, _, err := p.client.Git.GetRef(ctx, owner, repo, "refs/heads/"+req.Branch)
	if err != nil {
		return "", fmt.Errorf("get ref: %w", err)
	}
	baseSHA := ref.GetObject().GetSHA()

	// Get the base tree
	baseTree, _, err := p.client.Git.GetTree(ctx, owner, repo, baseSHA, false)
	if err != nil {
		return "", fmt.Errorf("get base tree: %w", err)
	}

	// Create tree entries for new files
	var treeEntries []*gh.TreeEntry
	for path, content := range req.Files {
		blob := "blob"
		mode := "100644"
		c := content
		treeEntries = append(treeEntries, &gh.TreeEntry{
			Path:    &path,
			Mode:    &mode,
			Type:    &blob,
			Content: &c,
		})
	}

	newTree, _, err := p.client.Git.CreateTree(ctx, owner, repo, baseTree.GetSHA(), treeEntries)
	if err != nil {
		return "", fmt.Errorf("create tree: %w", err)
	}

	// Create commit
	commit, _, err := p.client.Git.CreateCommit(ctx, owner, repo, &gh.Commit{
		Message: &req.Message,
		Tree:    newTree,
		Parents: []*gh.Commit{{SHA: &baseSHA}},
	}, nil)
	if err != nil {
		return "", fmt.Errorf("create commit: %w", err)
	}

	// Update reference
	newSHA := commit.GetSHA()
	ref.Object.SHA = &newSHA
	_, _, err = p.client.Git.UpdateRef(ctx, owner, repo, ref, false)
	if err != nil {
		return "", fmt.Errorf("update ref: %w", err)
	}

	return commit.GetSHA(), nil
}

func strPtr(s string) *string { return &s }

func parsePREvent(e *gh.PullRequestEvent, raw []byte) *scm.WebhookEvent {
	var eventType scm.EventType
	switch e.GetAction() {
	case "opened", "reopened":
		eventType = scm.EventPROpened
	case "closed":
		if e.GetPullRequest().GetMerged() {
			eventType = scm.EventPRMerged
		} else {
			return nil
		}
	case "synchronize", "edited":
		eventType = scm.EventPRUpdated
	default:
		return nil
	}

	pr := e.GetPullRequest()
	return &scm.WebhookEvent{
		Type:         eventType,
		RepoFullName: e.GetRepo().GetFullName(),
		PR: &scm.PRInfo{
			ID:           pr.GetNumber(),
			Title:        pr.GetTitle(),
			Author:       pr.GetUser().GetLogin(),
			SourceBranch: pr.GetHead().GetRef(),
			TargetBranch: pr.GetBase().GetRef(),
			URL:          pr.GetHTMLURL(),
		},
		Sender: e.GetSender().GetLogin(),
		Raw:    raw,
	}
}

func ghPRToSCM(pr *gh.PullRequest) *scm.PR {
	labels := make([]string, 0, len(pr.Labels))
	for _, l := range pr.Labels {
		labels = append(labels, l.GetName())
	}
	state := "open"
	if pr.GetMerged() || pr.MergedAt != nil {
		state = "merged"
	} else if pr.GetState() == "closed" {
		state = "closed"
	}
	result := &scm.PR{
		ID:           pr.GetNumber(),
		Title:        pr.GetTitle(),
		Author:       pr.GetUser().GetLogin(),
		SourceBranch: pr.GetHead().GetRef(),
		TargetBranch: pr.GetBase().GetRef(),
		State:        state,
		URL:          pr.GetHTMLURL(),
		LinesAdded:   pr.GetAdditions(),
		LinesDeleted: pr.GetDeletions(),
		Labels:       labels,
	}
	if pr.CreatedAt != nil {
		result.CreatedAt = pr.CreatedAt.Time
	}
	if pr.MergedAt != nil {
		result.MergedAt = pr.MergedAt.Time
	}
	return result
}
