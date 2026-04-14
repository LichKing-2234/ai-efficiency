# SCM Credentials & Provider Binding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Jenkins-style credentials module, refactor SCM providers to reference credentials, keep repos bound to one provider, and make scan clone use provider-authenticated HTTPS or SSH access.

**Architecture:** Keep `repo_config -> scm_provider` as the only repo-side binding. Introduce a reusable `Credential` entity for secret storage, let `ScmProvider` reference API and clone credentials, and resolve clone auth at runtime through provider configuration rather than raw `clone_url` alone.

**Tech Stack:** Go, Gin, Ent, PostgreSQL, Vue 3, Vite, Pinia, Vitest, Docker Compose

---

## File Map

- `backend/ent/schema/credential.go`
  Defines the new generic credential entity and its persistent fields.
- `backend/ent/schema/scmprovider.go`
  Replaces inline provider secrets with credential references and clone protocol fields.
- `backend/internal/credential/types.go`
  Defines credential kinds, payload structs, validation, and masking helpers.
- `backend/internal/credential/types_test.go`
  Unit-tests credential payload validation and provider compatibility rules.
- `backend/internal/credential/backfill.go`
  Migrates legacy `scm_provider.credentials` rows into reusable credentials.
- `backend/internal/credential/backfill_test.go`
  Covers one-time migration behavior and idempotency.
- `backend/internal/handler/credential.go`
  Admin CRUD endpoints for credentials.
- `backend/internal/handler/credential_test.go`
  HTTP tests for credential CRUD and delete protection.
- `backend/internal/handler/scmprovider.go`
  Changes provider create/update/list APIs from raw secrets to credential references.
- `backend/internal/handler/handler_scm_analysis_test.go`
  Existing provider-handler test file to extend for new request shapes and validation.
- `backend/internal/repo/factory.go`
  Resolves provider API credentials into SCM provider clients.
- `backend/internal/repo/service.go`
  Loads provider references, validates credential compatibility, and preserves repo-to-provider binding.
- `backend/internal/repo/repo_test.go`
  Regression tests for repo creation, provider lookup, and provider-bound behavior.
- `backend/internal/scm/cloneauth.go`
  Builds provider-aware HTTPS and SSH clone auth configurations.
- `backend/internal/scm/cloneauth_test.go`
  Unit-tests HTTPS and SSH runtime clone auth materialization.
- `backend/internal/analysis/cloner.go`
  Executes authenticated clone/fetch commands instead of bare `git clone <url>`.
- `backend/internal/analysis/service.go`
  Resolves repo clone auth through bound provider before cloning.
- `backend/internal/analysis/analysis_service_test.go`
  Regression tests for scan clone with provider-bound credentials.
- `backend/internal/handler/interfaces.go`
  Adjusts interfaces if analysis/service constructors need clone auth resolution.
- `backend/cmd/server/main.go`
  Wires credential backfill and any new clone auth resolver dependencies.
- `frontend/src/types/index.ts`
  Adds `Credential` and expands `SCMProvider` UI models.
- `frontend/src/api/credential.ts`
  Frontend CRUD client for credentials.
- `frontend/src/api/scmProvider.ts`
  Updates provider request payloads to send credential IDs and clone protocol.
- `frontend/src/views/SettingsView.vue`
  Adds credentials management and refactors provider form to select credentials.
- `frontend/src/__tests__/settings-view.test.ts`
  Covers credential CRUD UI and provider form behavior.
- `docs/architecture.md`
  Updates module responsibilities to include the new credentials module.

## Preflight Notes

- The worktree baseline frontend test `pnpm test repo-detail-view.test.ts` passes.
- Backend package tests that touch the shared test database currently fail in this environment because PostgreSQL is not listening on `127.0.0.1:5432`.
- Use the local Docker Compose stack for backend verification steps that require the DB.

### Task 1: Add Credential Domain Types And Ent Schema

**Files:**
- Create: `backend/internal/credential/types.go`
- Create: `backend/internal/credential/types_test.go`
- Create: `backend/ent/schema/credential.go`
- Modify: `backend/ent/schema/scmprovider.go`
- Modify: `backend/ent/generate.go`

- [ ] **Step 1: Write the failing credential validation tests**

```go
package credential

import (
	"encoding/json"
	"testing"
)

func TestParsePayloadRejectsInvalidSSHKey(t *testing.T) {
	_, err := ParsePayload(KindSSHUsernameWithPrivateKey, json.RawMessage(`{"username":"git"}`))
	if err == nil {
		t.Fatal("expected missing private_key to fail")
	}
}

func TestParsePayloadAcceptsSecretText(t *testing.T) {
	got, err := ParsePayload(KindSecretText, json.RawMessage(`{"text":"ghp_test"}`))
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if got.Kind() != KindSecretText {
		t.Fatalf("kind = %s", got.Kind())
	}
}

func TestValidateProviderCredentialRefs(t *testing.T) {
	if err := ValidateProviderCredentialRefs(KindSecretText, "ssh", KindSecretText); err == nil {
		t.Fatal("expected ssh clone to reject secret_text clone credential")
	}
	if err := ValidateProviderCredentialRefs(KindSecretText, "ssh", KindSSHUsernameWithPrivateKey); err != nil {
		t.Fatalf("ValidateProviderCredentialRefs: %v", err)
	}
}
```

- [ ] **Step 2: Run the credential package tests to verify they fail**

Run: `cd backend && go test ./internal/credential -run 'TestParsePayload|TestValidateProviderCredentialRefs'`

Expected: FAIL with `undefined: ParsePayload`, `undefined: KindSSHUsernameWithPrivateKey`, or missing package/build errors because the credential module does not exist yet.

- [ ] **Step 3: Implement the credential kinds and validation helpers**

```go
package credential

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Kind string

const (
	KindSecretText                  Kind = "secret_text"
	KindUsernamePassword            Kind = "username_password"
	KindSSHUsernameWithPrivateKey   Kind = "ssh_username_with_private_key"
)

type Payload interface {
	Kind() Kind
	MaskedSummary() map[string]any
}

type SecretTextPayload struct {
	Text string `json:"text"`
}

func (p SecretTextPayload) Kind() Kind { return KindSecretText }
func (p SecretTextPayload) MaskedSummary() map[string]any {
	return map[string]any{"preview": MaskSecret(p.Text)}
}

type UsernamePasswordPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (p UsernamePasswordPayload) Kind() Kind { return KindUsernamePassword }
func (p UsernamePasswordPayload) MaskedSummary() map[string]any {
	return map[string]any{"username": p.Username, "password_preview": MaskSecret(p.Password)}
}

type SSHUsernameWithPrivateKeyPayload struct {
	Username   string `json:"username"`
	PrivateKey string `json:"private_key"`
	Passphrase string `json:"passphrase,omitempty"`
}

func (p SSHUsernameWithPrivateKeyPayload) Kind() Kind { return KindSSHUsernameWithPrivateKey }
func (p SSHUsernameWithPrivateKeyPayload) MaskedSummary() map[string]any {
	return map[string]any{"username": p.Username, "private_key_preview": "configured", "has_passphrase": strings.TrimSpace(p.Passphrase) != ""}
}

func ParsePayload(kind Kind, raw json.RawMessage) (Payload, error) {
	switch kind {
	case KindSecretText:
		var payload SecretTextPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("decode secret_text payload: %w", err)
		}
		if strings.TrimSpace(payload.Text) == "" {
			return nil, errors.New("secret_text.text is required")
		}
		return payload, nil
	case KindUsernamePassword:
		var payload UsernamePasswordPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("decode username_password payload: %w", err)
		}
		if strings.TrimSpace(payload.Username) == "" || strings.TrimSpace(payload.Password) == "" {
			return nil, errors.New("username_password.username and password are required")
		}
		return payload, nil
	case KindSSHUsernameWithPrivateKey:
		var payload SSHUsernameWithPrivateKeyPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("decode ssh payload: %w", err)
		}
		if strings.TrimSpace(payload.Username) == "" || strings.TrimSpace(payload.PrivateKey) == "" {
			return nil, errors.New("ssh_username_with_private_key.username and private_key are required")
		}
		return payload, nil
	default:
		return nil, fmt.Errorf("unsupported credential kind %q", kind)
	}
}

func ValidateProviderCredentialRefs(apiKind Kind, cloneProtocol string, cloneKind Kind) error {
	if apiKind == KindSSHUsernameWithPrivateKey {
		return errors.New("api credential cannot be ssh_username_with_private_key")
	}
	switch cloneProtocol {
	case "https":
		if cloneKind != "" && cloneKind != KindSecretText && cloneKind != KindUsernamePassword {
			return fmt.Errorf("https clone does not allow %s", cloneKind)
		}
	case "ssh":
		if cloneKind != KindSSHUsernameWithPrivateKey {
			return fmt.Errorf("ssh clone requires %s", KindSSHUsernameWithPrivateKey)
		}
	default:
		return fmt.Errorf("unsupported clone protocol %q", cloneProtocol)
	}
	return nil
}
```

- [ ] **Step 4: Add the Ent schema and regenerate models**

```go
// backend/ent/schema/credential.go
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

type Credential struct {
	ent.Schema
}

func (Credential) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty(),
		field.String("description").Default(""),
		field.Enum("kind").Values("secret_text", "username_password", "ssh_username_with_private_key"),
		field.String("payload").Sensitive(),
		field.Time("created_at").Immutable().Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (Credential) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("api_scm_providers", ScmProvider.Type),
		edge.To("clone_scm_providers", ScmProvider.Type),
	}
}
```

```go
// backend/ent/schema/scmprovider.go (replace inline credentials usage)
field.Int("api_credential_id"),
field.Int("clone_credential_id").Optional().Nillable(),
field.Enum("clone_protocol").Values("https", "ssh").Default("https"),
field.String("credentials").Sensitive().Optional(),
```

Run: `cd backend && go generate ./ent`

Expected: generated Ent files update successfully with a new `Credential` entity and new `ScmProvider` fields.

- [ ] **Step 5: Re-run credential tests and commit**

Run: `cd backend && go test ./internal/credential -run 'TestParsePayload|TestValidateProviderCredentialRefs'`

Expected: PASS

```bash
cd /Users/admin/ai-efficiency/.worktrees/scm-credentials-provider-binding
git add backend/internal/credential/types.go backend/internal/credential/types_test.go backend/ent/schema/credential.go backend/ent/schema/scmprovider.go backend/ent
git commit -m "feat(backend): add credential domain and schema"
```

### Task 2: Add Admin Credential CRUD Endpoints

**Files:**
- Create: `backend/internal/handler/credential.go`
- Create: `backend/internal/handler/credential_test.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/internal/pkg/response.go`

- [ ] **Step 1: Write the failing credential handler tests**

```go
func TestCredentialCRUD(t *testing.T) {
	env := setupTestEnv(t)

	createBody := `{"name":"GitHub PAT","description":"org rw","kind":"secret_text","payload":{"text":"ghp_test"}}`
	createResp := doRequest(env, "POST", "/api/v1/admin/credentials", bytes.NewBufferString(createBody))
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
	cred := env.client.Credential.Create().
		SetName("GitHub PAT").
		SetKind(credential.KindSecretText.String()).
		SetPayload("encrypted").
		SaveX(context.Background())
	env.client.ScmProvider.Create().
		SetName("GitHub").
		SetType(scmprovider.TypeGithub).
		SetBaseURL("https://api.github.com").
		SetApiCredentialID(cred.ID).
		SaveX(context.Background())

	resp := doRequest(env, "DELETE", fmt.Sprintf("/api/v1/admin/credentials/%d", cred.ID), nil)
	if resp.Code != http.StatusConflict {
		t.Fatalf("delete status = %d body=%s", resp.Code, resp.Body.String())
	}
}
```

- [ ] **Step 2: Run the credential handler tests to verify they fail**

Run: `cd backend && go test ./internal/handler -run 'TestCredentialCRUD|TestCredentialDeleteRejectsProviderInUse'`

Expected: FAIL with missing routes, missing `Credential` entity wiring, or missing handler/build errors.

- [ ] **Step 3: Implement the credential handler**

```go
type createCredentialRequest struct {
	Name        string          `json:"name" binding:"required"`
	Description string          `json:"description"`
	Kind        credential.Kind `json:"kind" binding:"required"`
	Payload     json.RawMessage `json:"payload" binding:"required"`
}

type credentialResponse struct {
	ID          int            `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Kind        string         `json:"kind"`
	UsageCount  int            `json:"usage_count"`
	Summary     map[string]any `json:"summary"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

func (h *CredentialHandler) Create(c *gin.Context) {
	var req createCredentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	payload, err := credential.ParsePayload(req.Kind, req.Payload)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	encrypted, err := pkg.Encrypt(string(req.Payload), h.encryptionKey)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to encrypt credential payload")
		return
	}
	row, err := h.entClient.Credential.Create().
		SetName(req.Name).
		SetDescription(req.Description).
		SetKind(string(req.Kind)).
		SetPayload(encrypted).
		Save(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to create credential")
		return
	}
	pkg.Created(c, toCredentialResponse(row, payload.MaskedSummary(), 0))
}
```

- [ ] **Step 4: Wire the routes into the admin section**

```go
credentialHandler := NewCredentialHandler(entClient, encryptionKey)

adminCredentialGroup := protected.Group("/admin/credentials")
adminCredentialGroup.Use(auth.RequireAdmin())
{
	adminCredentialGroup.GET("", credentialHandler.List)
	adminCredentialGroup.POST("", credentialHandler.Create)
	adminCredentialGroup.GET("/:id", credentialHandler.Get)
	adminCredentialGroup.PUT("/:id", credentialHandler.Update)
	adminCredentialGroup.DELETE("/:id", credentialHandler.Delete)
}
```

- [ ] **Step 5: Re-run the handler tests and commit**

Run: `cd backend && go test ./internal/handler -run 'TestCredentialCRUD|TestCredentialDeleteRejectsProviderInUse'`

Expected: PASS when the test database is available

```bash
cd /Users/admin/ai-efficiency/.worktrees/scm-credentials-provider-binding
git add backend/internal/handler/credential.go backend/internal/handler/credential_test.go backend/internal/handler/router.go
git commit -m "feat(backend): add credential admin api"
```

### Task 3: Refactor SCM Providers To Reference Credentials And Backfill Legacy Data

**Files:**
- Create: `backend/internal/credential/backfill.go`
- Create: `backend/internal/credential/backfill_test.go`
- Modify: `backend/internal/handler/scmprovider.go`
- Modify: `backend/internal/repo/factory.go`
- Modify: `backend/internal/repo/service.go`
- Modify: `backend/internal/handler/handler_scm_analysis_test.go`
- Modify: `backend/internal/repo/repo_test.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Write failing tests for provider request validation and legacy backfill**

```go
func TestCreateSCMProviderRequiresApiCredential(t *testing.T) {
	env := setupTestEnv(t)
	body := `{"name":"GitHub","type":"github","base_url":"https://api.github.com","clone_protocol":"https"}`
	resp := doRequest(env, "POST", "/api/v1/scm-providers", bytes.NewBufferString(body))
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestBackfillLegacySCMCredentials(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	legacy := client.ScmProvider.Create().
		SetName("legacy-gh").
		SetType(scmprovider.TypeGithub).
		SetBaseURL("https://api.github.com").
		SetCredentials("encrypted-json-token").
		SaveX(ctx)

	if err := BackfillLegacySCMCredentials(ctx, client, "test-key"); err != nil {
		t.Fatalf("BackfillLegacySCMCredentials: %v", err)
	}
	updated := client.ScmProvider.GetX(ctx, legacy.ID)
	if updated.ApiCredentialID == 0 {
		t.Fatal("expected api_credential_id to be populated")
	}
}
```

- [ ] **Step 2: Run the provider/backfill tests to verify they fail**

Run: `cd backend && go test ./internal/handler -run 'TestCreateSCMProviderRequiresApiCredential' && go test ./internal/credential -run 'TestBackfillLegacySCMCredentials'`

Expected: FAIL with missing fields, missing backfill function, or outdated handler request schema.

- [ ] **Step 3: Refactor provider requests and provider factory**

```go
type createSCMProviderRequest struct {
	Name              string `json:"name" binding:"required"`
	Type              string `json:"type" binding:"required,oneof=github bitbucket_server"`
	BaseURL           string `json:"base_url" binding:"required"`
	APICredentialID   int    `json:"api_credential_id" binding:"required"`
	CloneProtocol     string `json:"clone_protocol" binding:"required,oneof=https ssh"`
	CloneCredentialID *int   `json:"clone_credential_id"`
}

func newGitHubProvider(baseURL string, apiCredential credential.Payload, logger *zap.Logger) (scm.SCMProvider, error) {
	username, password, err := credential.ResolveAPIAuth(apiCredential, scmprovider.TypeGithub)
	if err != nil {
		return nil, err
	}
	return github.New(baseURL, password, logger), nil
}
```

- [ ] **Step 4: Add startup backfill after Ent migration**

```go
if err := credential.BackfillLegacySCMCredentials(ctx, entClient, cfg.Encryption.Key); err != nil {
	logger.Fatal("backfill legacy scm credentials", zap.Error(err))
}
```

- [ ] **Step 5: Re-run the provider/backfill tests and commit**

Run: `cd backend && go test ./internal/credential -run 'TestBackfillLegacySCMCredentials' && go test ./internal/handler -run 'TestCreateSCMProviderRequiresApiCredential'`

Expected: PASS when the test database is available

```bash
cd /Users/admin/ai-efficiency/.worktrees/scm-credentials-provider-binding
git add backend/internal/credential/backfill.go backend/internal/credential/backfill_test.go backend/internal/handler/scmprovider.go backend/internal/repo/factory.go backend/internal/repo/service.go backend/internal/handler/handler_scm_analysis_test.go backend/internal/repo/repo_test.go backend/cmd/server/main.go
git commit -m "refactor(backend): bind scm providers to credentials"
```

### Task 4: Make Scan Clone Use Provider Credentials

**Files:**
- Create: `backend/internal/scm/cloneauth.go`
- Create: `backend/internal/scm/cloneauth_test.go`
- Modify: `backend/internal/analysis/cloner.go`
- Modify: `backend/internal/analysis/service.go`
- Modify: `backend/internal/analysis/analysis_service_test.go`

- [ ] **Step 1: Write the failing clone auth tests**

```go
func TestBuildHTTPSCloneConfigFromSecretText(t *testing.T) {
	cfg, err := BuildCloneAuthConfig(scmprovider.TypeGithub, "https", credential.SecretTextPayload{Text: "ghp_test"}, nil)
	if err != nil {
		t.Fatalf("BuildCloneAuthConfig: %v", err)
	}
	if cfg.HTTPSUsername != "x-access-token" {
		t.Fatalf("username = %q", cfg.HTTPSUsername)
	}
}

func TestBuildSSHCloneConfigRequiresPrivateKey(t *testing.T) {
	_, err := BuildCloneAuthConfig(scmprovider.TypeBitbucketServer, "ssh", nil, nil)
	if err == nil {
		t.Fatal("expected missing ssh clone credential to fail")
	}
}
```

- [ ] **Step 2: Run the clone auth tests to verify they fail**

Run: `cd backend && go test ./internal/scm -run 'TestBuildHTTPSCloneConfigFromSecretText|TestBuildSSHCloneConfigRequiresPrivateKey'`

Expected: FAIL with missing `BuildCloneAuthConfig` or incorrect clone auth behavior.

- [ ] **Step 3: Implement clone auth resolution**

```go
type CloneAuthConfig struct {
	CloneURL       string
	Protocol       string
	HTTPSUsername  string
	HTTPSPassword  string
	SSHUsername    string
	SSHPrivateKey  string
	SSHPassphrase  string
	KnownHostsPath string
}

func BuildCloneAuthConfig(providerType scmprovider.Type, cloneProtocol string, apiPayload credential.Payload, clonePayload credential.Payload) (*CloneAuthConfig, error) {
	switch cloneProtocol {
	case "https":
		payload := clonePayload
		if payload == nil {
			payload = apiPayload
		}
		username, password, err := credential.ResolveHTTPSCloneAuth(providerType, payload)
		if err != nil {
			return nil, err
		}
		return &CloneAuthConfig{Protocol: "https", HTTPSUsername: username, HTTPSPassword: password}, nil
	case "ssh":
		sshPayload, ok := clonePayload.(credential.SSHUsernameWithPrivateKeyPayload)
		if !ok {
			return nil, fmt.Errorf("ssh clone requires ssh_username_with_private_key credential")
		}
		return &CloneAuthConfig{
			Protocol:      "ssh",
			SSHUsername:   sshPayload.Username,
			SSHPrivateKey: sshPayload.PrivateKey,
			SSHPassphrase: sshPayload.Passphrase,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported clone protocol %q", cloneProtocol)
	}
}
```

- [ ] **Step 4: Refactor the cloner to execute authenticated Git commands**

```go
func (c *Cloner) CloneOrUpdate(req CloneRequest) (string, error) {
	if req.Auth.Protocol == "https" {
		return c.cloneHTTPS(req)
	}
	return c.cloneSSH(req)
}

func (c *Cloner) cloneHTTPS(req CloneRequest) (string, error) {
	askpassPath, err := writeAskPassScript(req.Auth.HTTPSUsername, req.Auth.HTTPSPassword)
	if err != nil {
		return "", err
	}
	defer os.Remove(askpassPath)

	cmd := exec.Command("git", "clone", "--depth", "1", req.CloneURL, req.RepoDir)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS="+askpassPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git clone: %s: %w", string(out), err)
	}
	return req.RepoDir, nil
}
```

- [ ] **Step 5: Run clone-related tests and commit**

Run: `cd backend && go test ./internal/scm -run 'TestBuildHTTPSCloneConfigFromSecretText|TestBuildSSHCloneConfigRequiresPrivateKey' && go test ./internal/analysis -run 'TestRunScanCloneError|TestRunScanStaticOnly'`

Expected: PASS when the test database is available

```bash
cd /Users/admin/ai-efficiency/.worktrees/scm-credentials-provider-binding
git add backend/internal/scm/cloneauth.go backend/internal/scm/cloneauth_test.go backend/internal/analysis/cloner.go backend/internal/analysis/service.go backend/internal/analysis/analysis_service_test.go
git commit -m "fix(backend): clone repos with provider credentials"
```

### Task 5: Add Credentials Management To Settings

**Files:**
- Create: `frontend/src/api/credential.ts`
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/views/SettingsView.vue`
- Modify: `frontend/src/__tests__/settings-view.test.ts`

- [ ] **Step 1: Write the failing frontend tests for credential UI**

```ts
it('renders credentials section and add credential dialog', async () => {
  const wrapper = await mountSettings()
  expect(wrapper.text()).toContain('Credentials')
  expect(wrapper.text()).toContain('Add Credential')
})

it('creates a secret text credential', async () => {
  const { createCredential } = await import('@/api/credential')
  const wrapper = await mountSettings()

  await wrapper.findAll('button').find((b) => b.text().includes('Add Credential'))!.trigger('click')
  await flushPromises()

  await wrapper.find('input[name="credential-name"]').setValue('GitHub PAT')
  await wrapper.find('select[name="credential-kind"]').setValue('secret_text')
  await wrapper.find('textarea[name="credential-secret-text"]').setValue('ghp_test')
  await wrapper.findAll('button').find((b) => b.text().includes('Save Credential'))!.trigger('click')
  await flushPromises()

  expect(createCredential).toHaveBeenCalledWith({
    name: 'GitHub PAT',
    description: '',
    kind: 'secret_text',
    payload: { text: 'ghp_test' },
  })
})
```

- [ ] **Step 2: Run the settings tests to verify they fail**

Run: `cd frontend && pnpm test settings-view.test.ts`

Expected: FAIL because no credentials API or credentials section exists yet.

- [ ] **Step 3: Add frontend types and API client**

```ts
export interface Credential {
  id: number
  name: string
  description: string
  kind: 'secret_text' | 'username_password' | 'ssh_username_with_private_key'
  usage_count: number
  summary: Record<string, unknown>
  created_at: string
  updated_at: string
}
```

```ts
import client from './client'
import type { ApiResponse, Credential } from '@/types'

export function listCredentials() {
  return client.get<ApiResponse<Credential[]>>('/admin/credentials')
}

export function createCredential(data: Record<string, unknown>) {
  return client.post<ApiResponse<Credential>>('/admin/credentials', data)
}
```

- [ ] **Step 4: Implement the credentials section in Settings**

```ts
const credentials = ref<Credential[]>([])
const showCredentialDialog = ref(false)
const credentialForm = ref({
  name: '',
  description: '',
  kind: 'secret_text',
  text: '',
  username: '',
  password: '',
  private_key: '',
  passphrase: '',
})

function credentialPayload() {
  switch (credentialForm.value.kind) {
    case 'secret_text':
      return { text: credentialForm.value.text }
    case 'username_password':
      return { username: credentialForm.value.username, password: credentialForm.value.password }
    default:
      return {
        username: credentialForm.value.username,
        private_key: credentialForm.value.private_key,
        passphrase: credentialForm.value.passphrase,
      }
  }
}
```

- [ ] **Step 5: Re-run settings tests and commit**

Run: `cd frontend && pnpm test settings-view.test.ts`

Expected: PASS

```bash
cd /Users/admin/ai-efficiency/.worktrees/scm-credentials-provider-binding
git add frontend/src/api/credential.ts frontend/src/types/index.ts frontend/src/views/SettingsView.vue frontend/src/__tests__/settings-view.test.ts
git commit -m "feat(frontend): add credentials management"
```

### Task 6: Refactor Provider Form To Select Credentials

**Files:**
- Modify: `frontend/src/api/scmProvider.ts`
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/views/SettingsView.vue`
- Modify: `frontend/src/__tests__/settings-view.test.ts`

- [ ] **Step 1: Write the failing provider form tests**

```ts
it('sends credential ids when creating a provider', async () => {
  const { createProvider } = await import('@/api/scmProvider')
  const wrapper = await mountSettings()

  await wrapper.findAll('button').find((b) => b.text().includes('Add Provider'))!.trigger('click')
  await flushPromises()

  await wrapper.find('input[name="provider-name"]').setValue('GitHub Extensions')
  await wrapper.find('select[name="provider-api-credential"]').setValue('12')
  await wrapper.find('select[name="provider-clone-protocol"]').setValue('ssh')
  await wrapper.find('select[name="provider-clone-credential"]').setValue('13')
  await wrapper.findAll('button').find((b) => b.text().includes('Save Provider'))!.trigger('click')
  await flushPromises()

  expect(createProvider).toHaveBeenCalledWith({
    name: 'GitHub Extensions',
    type: 'github',
    base_url: 'https://api.github.com',
    api_credential_id: 12,
    clone_protocol: 'ssh',
    clone_credential_id: 13,
  })
})
```

- [ ] **Step 2: Run the provider form tests to verify they fail**

Run: `cd frontend && pnpm test settings-view.test.ts`

Expected: FAIL because the provider form still submits raw `credentials`.

- [ ] **Step 3: Update the provider request and UI model**

```ts
export interface SCMProvider {
  id: number
  name: string
  type: string
  base_url: string
  status: string
  api_credential_id: number
  clone_protocol: 'https' | 'ssh'
  clone_credential_id: number | null
  created_at: string
}
```

```ts
const form = ref({
  name: '',
  type: 'github',
  base_url: 'https://api.github.com',
  api_credential_id: 0,
  clone_protocol: 'https',
  clone_credential_id: null as number | null,
})
```

- [ ] **Step 4: Replace raw token inputs with credential selectors**

```vue
<label class="block text-sm font-medium text-gray-700">API Credential</label>
<select v-model.number="form.api_credential_id" name="provider-api-credential" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm">
  <option :value="0" disabled>Select API credential</option>
  <option v-for="cred in credentials.filter(c => c.kind !== 'ssh_username_with_private_key')" :key="cred.id" :value="cred.id">
    {{ cred.name }} ({{ cred.kind }})
  </option>
</select>

<label class="block text-sm font-medium text-gray-700">Clone Protocol</label>
<select v-model="form.clone_protocol" name="provider-clone-protocol" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm">
  <option value="https">https</option>
  <option value="ssh">ssh</option>
</select>

<select v-if="form.clone_protocol === 'ssh'" v-model.number="form.clone_credential_id" name="provider-clone-credential" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm">
  <option :value="null">Select SSH credential</option>
  <option v-for="cred in credentials.filter(c => c.kind === 'ssh_username_with_private_key')" :key="cred.id" :value="cred.id">
    {{ cred.name }}
  </option>
</select>
```

- [ ] **Step 5: Re-run settings tests and commit**

Run: `cd frontend && pnpm test settings-view.test.ts`

Expected: PASS

```bash
cd /Users/admin/ai-efficiency/.worktrees/scm-credentials-provider-binding
git add frontend/src/api/scmProvider.ts frontend/src/types/index.ts frontend/src/views/SettingsView.vue frontend/src/__tests__/settings-view.test.ts
git commit -m "refactor(frontend): bind scm providers to credentials"
```

### Task 7: Update Architecture Docs And Run End-To-End Verification

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-04-14-scm-credentials-provider-binding-design.md` (only if implementation changed the contract)

- [ ] **Step 1: Add the credentials module to architecture**

```md
| Credentials | `backend/internal/credential` | Generic secret assets, payload validation, masking, migration, and provider credential resolution |
```

```md
- SCM providers now reference reusable credentials instead of storing raw secret blobs inline.
- Repos still bind to exactly one SCM provider; clone protocol and clone credentials remain provider-owned.
```

- [ ] **Step 2: Run focused backend verification in the dev stack**

Run:

```bash
cd /Users/admin/ai-efficiency/.worktrees/scm-credentials-provider-binding
docker-compose -f deploy/docker-compose.dev.yml up -d --build backend postgres redis
docker exec ai-efficiency-backend-1 sh -lc 'which git && which ssh'
```

Expected:

- backend container starts
- `git` and `ssh` exist in the runtime image

- [ ] **Step 3: Run backend and frontend test commands**

Run:

```bash
cd backend && go test ./internal/credential ./internal/scm ./internal/analysis -run 'Test|^$'
cd frontend && pnpm test settings-view.test.ts
```

Expected:

- backend targeted packages pass when the test DB is available
- frontend settings tests pass

- [ ] **Step 4: Verify real scan behavior for both HTTPS and SSH providers**

Run:

```bash
curl -sS -X POST http://localhost:18081/api/v1/repos/<https-repo-id>/scan -H "Authorization: Bearer <token>"
curl -sS -X POST http://localhost:18081/api/v1/repos/<ssh-repo-id>/scan -H "Authorization: Bearer <token>"
```

Expected:

- HTTPS private repo no longer fails with `could not read Username`
- SSH repo no longer fails with `Host key verification failed`

- [ ] **Step 5: Commit the docs and verification updates**

```bash
cd /Users/admin/ai-efficiency/.worktrees/scm-credentials-provider-binding
git add docs/architecture.md docs/superpowers/specs/2026-04-14-scm-credentials-provider-binding-design.md
git commit -m "docs(architecture): reflect scm credentials module"
```
