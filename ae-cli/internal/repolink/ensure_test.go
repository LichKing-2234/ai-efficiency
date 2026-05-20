package repolink

import (
	"context"
	"errors"
	"testing"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

type ensureStub struct {
	resp *client.RepoEnsureResponse
	err  error
}

func (s ensureStub) EnsureRepoFromRemote(ctx context.Context, remoteURL, branch string) (*client.RepoEnsureResponse, error) {
	return s.resp, s.err
}

func TestEnsureLinked(t *testing.T) {
	status, err := Ensure(context.Background(), ensureStub{
		resp: &client.RepoEnsureResponse{RepoKey: "github.com/acme/platform"},
	}, "https://github.com/acme/platform.git", "main")
	if err != nil {
		t.Fatalf("Ensure error: %v", err)
	}
	if status != "linked" {
		t.Fatalf("status = %q, want linked", status)
	}
}

func TestEnsureSkippedWithoutRemote(t *testing.T) {
	status, err := Ensure(context.Background(), ensureStub{}, "", "main")
	if err != nil {
		t.Fatalf("Ensure error: %v", err)
	}
	if status != "skipped" {
		t.Fatalf("status = %q, want skipped", status)
	}
}

func TestEnsureFailed(t *testing.T) {
	status, err := Ensure(context.Background(), ensureStub{err: errors.New("boom")}, "https://github.com/acme/platform.git", "main")
	if err == nil {
		t.Fatal("expected error")
	}
	if status != "failed" {
		t.Fatalf("status = %q, want failed", status)
	}
}
