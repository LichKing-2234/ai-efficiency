package scm

import (
	"context"
	"net/http"
	"time"
)

// EventType represents the type of SCM webhook event.
type EventType string

const (
	EventPROpened  EventType = "pr_opened"
	EventPRMerged  EventType = "pr_merged"
	EventPRUpdated EventType = "pr_updated"
	EventPush      EventType = "push"
)

// Repo represents a repository from an SCM provider.
type Repo struct {
	FullName      string `json:"full_name"`
	Name          string `json:"name"`
	CloneURL      string `json:"clone_url"`
	DefaultBranch string `json:"default_branch"`
	Description   string `json:"description"`
	Private       bool   `json:"private"`
}

// PR represents a pull request.
type PR struct {
	ID           int       `json:"id"`
	Title        string    `json:"title"`
	Author       string    `json:"author"`
	SourceBranch string    `json:"source_branch"`
	TargetBranch string    `json:"target_branch"`
	State        string    `json:"state"` // open / merged / closed
	URL          string    `json:"url"`
	LinesAdded   int       `json:"lines_added"`
	LinesDeleted int       `json:"lines_deleted"`
	Labels       []string  `json:"labels"`
	CreatedAt    time.Time `json:"created_at,omitempty"`
	MergedAt     time.Time `json:"merged_at,omitempty"`
}

// PRInfo is a lightweight PR reference in webhook events.
type PRInfo struct {
	ID           int    `json:"id"`
	Title        string `json:"title"`
	Author       string `json:"author"`
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
	URL          string `json:"url"`
}

// WebhookEvent represents a parsed webhook event.
type WebhookEvent struct {
	Type         EventType `json:"type"`
	RepoFullName string    `json:"repo_full_name"`
	PR           *PRInfo   `json:"pr,omitempty"`
	Sender       string    `json:"sender"`
	Raw          []byte    `json:"-"`
}

// CreatePRRequest is the request to create a pull request.
type CreatePRRequest struct {
	RepoFullName string `json:"repo_full_name"`
	Title        string `json:"title"`
	Body         string `json:"body"`
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
}

// SetStatusRequest is the request to set a commit status.
type SetStatusRequest struct {
	RepoFullName string `json:"repo_full_name"`
	SHA          string `json:"sha"`
	State        string `json:"state"` // success / failure / pending
	Context      string `json:"context"`
	Description  string `json:"description"`
	TargetURL    string `json:"target_url"`
}

// CommitFilesRequest is the request to commit files.
type CommitFilesRequest struct {
	RepoFullName string            `json:"repo_full_name"`
	Branch       string            `json:"branch"`
	Message      string            `json:"message"`
	Files        map[string]string `json:"files"` // path -> content
}

// ListOpts are options for listing repos.
type ListOpts struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

// PRListOpts are options for listing PRs.
type PRListOpts struct {
	State    string `json:"state"` // open / closed / all
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

// MergeOpts are options for merging a PR.
type MergeOpts struct {
	Method  string `json:"method"` // merge / squash / rebase
	Message string `json:"message"`
}

// TreeEntry represents a file in a repo tree.
type TreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"` // blob / tree
	Size int64  `json:"size"`
}

// SCMProvider defines the interface for SCM platform integrations.
type SCMProvider interface {
	// Basic operations
	GetRepo(ctx context.Context, fullName string) (*Repo, error)
	ListRepos(ctx context.Context, opts ListOpts) ([]*Repo, error)

	// PR operations
	CreatePR(ctx context.Context, req CreatePRRequest) (*PR, error)
	GetPR(ctx context.Context, repoFullName string, prID int) (*PR, error)
	ListPRs(ctx context.Context, repoFullName string, opts PRListOpts) ([]*PR, error)
	GetPRChangedFiles(ctx context.Context, repoFullName string, prID int) ([]string, error)
	ListPRCommits(ctx context.Context, repoFullName string, prID int) ([]string, error)
	GetPRApprovals(ctx context.Context, repoFullName string, prID int) (int, error)
	AddLabels(ctx context.Context, repoFullName string, prID int, labels []string) error
	SetPRStatus(ctx context.Context, req SetStatusRequest) error
	MergePR(ctx context.Context, repoFullName string, prID int, opts MergeOpts) error

	// Webhook management
	RegisterWebhook(ctx context.Context, repoFullName string, events []string, secret string) (webhookID string, err error)
	DeleteWebhook(ctx context.Context, repoFullName string, webhookID string) error
	ParseWebhookPayload(r *http.Request, secret string) (*WebhookEvent, error)

	// File operations
	GetFileContent(ctx context.Context, repoFullName, path, ref string) ([]byte, error)
	GetTree(ctx context.Context, repoFullName, ref string) ([]*TreeEntry, error)
	GetBranchSHA(ctx context.Context, repoFullName, branch string) (string, error)
	CreateBranch(ctx context.Context, repoFullName, branchName, baseSHA string) error
	CommitFiles(ctx context.Context, req CommitFilesRequest) (sha string, err error)
}
