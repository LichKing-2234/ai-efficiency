package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	entcredential "github.com/ai-efficiency/backend/ent/credential"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/repo"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type failingRepoInventoryInvalidator struct {
	err error
}

func (f failingRepoInventoryInvalidator) InvalidateTx(context.Context, *ent.Tx) error {
	return f.err
}

func TestSCMProviderUpdateAndDeleteAdvanceRepoInventoryRevision(t *testing.T) {
	const encryptionKey = "0000000000000000000000000000000000000000000000000000000000000000"
	client := testdb.Open(t)
	ctx := context.Background()
	revisions := repo.NewInventoryRevisionStore(client)
	if err := revisions.Ensure(ctx); err != nil {
		t.Fatalf("ensure repository inventory revision: %v", err)
	}
	repoService := repo.NewService(client, encryptionKey, zap.NewNop(), repo.ServiceOptions{
		InventoryRevisionStore: revisions,
	})
	providerID := createInventoryTestProviderAndRepo(t, client, encryptionKey)
	handler := NewSCMProviderHandler(client, encryptionKey, repoService)

	before, err := revisions.Current(ctx)
	if err != nil {
		t.Fatalf("current inventory revision: %v", err)
	}
	update := performSCMProviderMutation(handler.Update, http.MethodPut, fmt.Sprintf("/%d", providerID), `{"name":"GitHub Renamed"}`, providerID)
	if update.Code != http.StatusOK {
		t.Fatalf("update status = %d, body: %s", update.Code, update.Body.String())
	}
	afterUpdate, err := revisions.Current(ctx)
	if err != nil {
		t.Fatalf("current revision after update: %v", err)
	}
	if afterUpdate == before {
		t.Fatalf("inventory revision after provider update = %q, want change", afterUpdate)
	}

	deleted := performSCMProviderMutation(handler.Delete, http.MethodDelete, fmt.Sprintf("/%d", providerID), "", providerID)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body: %s", deleted.Code, deleted.Body.String())
	}
	afterDelete, err := revisions.Current(ctx)
	if err != nil {
		t.Fatalf("current revision after delete: %v", err)
	}
	if afterDelete == afterUpdate {
		t.Fatalf("inventory revision after provider delete = %q, want change", afterDelete)
	}
	repository := client.RepoConfig.Query().WithScmProvider().OnlyX(ctx)
	if repository.Edges.ScmProvider != nil {
		t.Fatalf("repository provider after delete = %#v, want unbound", repository.Edges.ScmProvider)
	}
}

func TestSCMProviderDeleteRollsBackWhenRepoInventoryRevisionFails(t *testing.T) {
	const encryptionKey = "0000000000000000000000000000000000000000000000000000000000000000"
	client := testdb.Open(t)
	ctx := context.Background()
	providerID := createInventoryTestProviderAndRepo(t, client, encryptionKey)
	failure := errors.New("test repository inventory revision failure")
	repoService := repo.NewService(client, encryptionKey, zap.NewNop(), repo.ServiceOptions{
		InventoryRevisionStore: failingRepoInventoryInvalidator{err: failure},
	})
	handler := NewSCMProviderHandler(client, encryptionKey, repoService)

	deleted := performSCMProviderMutation(handler.Delete, http.MethodDelete, fmt.Sprintf("/%d", providerID), "", providerID)
	if deleted.Code != http.StatusInternalServerError {
		t.Fatalf("delete status = %d, want 500; body: %s", deleted.Code, deleted.Body.String())
	}
	if _, err := client.ScmProvider.Get(ctx, providerID); err != nil {
		t.Fatalf("provider lookup after rolled-back delete: %v", err)
	}
	repository := client.RepoConfig.Query().WithScmProvider().OnlyX(ctx)
	if repository.Edges.ScmProvider == nil || repository.Edges.ScmProvider.ID != providerID {
		t.Fatalf("repository provider after rolled-back delete = %#v, want provider %d", repository.Edges.ScmProvider, providerID)
	}
}

func createInventoryTestProviderAndRepo(t *testing.T, client *ent.Client, encryptionKey string) int {
	t.Helper()
	ctx := context.Background()
	encrypted, err := pkg.Encrypt(`{"text":"test-token"}`, encryptionKey)
	if err != nil {
		t.Fatalf("encrypt provider credentials: %v", err)
	}
	credential := client.Credential.Create().
		SetName("GitHub Test API credential").
		SetKind(entcredential.KindSecretText).
		SetPayload(encrypted).
		SaveX(ctx)
	provider := client.ScmProvider.Create().
		SetName("GitHub Test").
		SetType("github").
		SetBaseURL("https://github.example.com").
		SetAPICredentialID(credential.ID).
		SaveX(ctx)
	client.RepoConfig.Create().
		SetRepoKey("github.example.com/acme/platform").
		SetScmProviderID(provider.ID).
		SetName("platform").
		SetFullName("acme/platform").
		SetCloneURL("https://github.example.com/acme/platform.git").
		SetDefaultBranch("main").
		SetStatus(repoconfig.StatusActive).
		SaveX(ctx)
	return provider.ID
}

func performSCMProviderMutation(handler gin.HandlerFunc, method, path, body string, providerID int) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(providerID)}}
	c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	handler(c)
	return w
}
