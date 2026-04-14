package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	entcredential "github.com/ai-efficiency/backend/ent/credential"
	"github.com/ai-efficiency/backend/ent/scmprovider"
)

func TestCredentialCRUD(t *testing.T) {
	env := setupTestEnv(t)

	createBody := map[string]any{
		"name":        "GitHub PAT",
		"description": "org rw",
		"kind":        "secret_text",
		"payload": map[string]any{
			"text": "ghp_test",
		},
	}
	createResp := doRequest(env, "POST", "/api/v1/admin/credentials", createBody)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", createResp.Code, createResp.Body.String())
	}

	listResp := doRequest(env, "GET", "/api/v1/admin/credentials", nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listResp.Code, listResp.Body.String())
	}
	if strings.Contains(listResp.Body.String(), "ghp_test") {
		t.Fatal("credential list leaked plaintext secret")
	}
}

func TestCredentialDeleteRejectsProviderInUse(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	cred := env.client.Credential.Create().
		SetName("GitHub PAT").
		SetDescription("org rw").
		SetKind(entcredential.KindSecretText).
		SetPayload("encrypted").
		SaveX(ctx)

	env.client.ScmProvider.Create().
		SetName("GitHub").
		SetType(scmprovider.TypeGithub).
		SetBaseURL("https://api.github.com").
		SetAPICredentialID(cred.ID).
		SaveX(ctx)

	resp := doRequest(env, "DELETE", fmt.Sprintf("/api/v1/admin/credentials/%d", cred.ID), nil)
	if resp.Code != http.StatusConflict {
		t.Fatalf("delete status = %d body=%s", resp.Code, resp.Body.String())
	}
}
