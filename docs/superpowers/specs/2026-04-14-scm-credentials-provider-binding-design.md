# SCM Credentials & Provider Binding Design

**Status:** Proposed contract for SCM credential modularization and repo-to-provider binding

**Implementation note:** Current code stores an encrypted `credentials` blob directly on `scm_providers`, and scan clone paths do not yet reuse provider credentials. This spec defines the target contract to replace that model with a Jenkins-style credentials module while preserving `repo -> provider` as the runtime boundary.

## Overview

This spec introduces a dedicated `credentials` module for SCM authentication materials, keeps `scm_provider` as the reusable platform integration unit, and keeps each repo bound to exactly one active SCM provider.

The main goal is to solve the current mismatch between:

- provider-scoped API access, which is needed for repo metadata lookup, webhook registration, PR sync, changed-file lookup, and PR creation
- clone authentication, which may need either HTTPS token auth or SSH private key auth
- permission boundaries, where no single provider credential set can realistically cover every repo in the system

This design explicitly does **not** let repos bind directly to credentials. Repos continue to bind to a single SCM provider, and providers depend on reusable credentials.

## Relationship To Existing Specs

- This spec extends the platform-level module boundaries in [`docs/architecture.md`](/docs/architecture.md) with a new credentials module but does not change the modular-monolith direction.
- This spec supersedes the ad hoc SCM credential storage implied by the historical baseline in [`2026-03-17-ai-efficiency-platform-design.md`](/docs/superpowers/specs/2026-03-17-ai-efficiency-platform-design.md).
- This spec does not change relay/provider delivery from [`2026-03-24-oauth-cli-login-design.md`](/docs/superpowers/specs/2026-03-24-oauth-cli-login-design.md); relay providers and SCM providers remain separate concepts.

## Problems In Current Code

Current implementation issues:

1. `scm_provider.credentials` is a single encrypted blob, so credentials cannot be reused across providers.
2. `repo_config` already binds to one SCM provider, but scan clone currently ignores provider authentication and runs `git clone <clone_url>` directly.
3. SSH clone fails when runtime SSH trust or keys are missing.
4. HTTPS clone for private repos fails because Git is not given provider credentials at clone time.
5. The current frontend treats provider configuration as "platform instance + raw secret field", which does not scale to mixed API and clone credentials.

## Goals

1. Add a reusable credentials module modeled after Jenkins-style credential types.
2. Keep `scm_provider` as the reusable runtime integration object.
3. Keep each repo bound to exactly one SCM provider.
4. Allow multiple providers to reuse the same credential.
5. Support private repo clone over either HTTPS or SSH.
6. Preserve SCM API operations that require platform tokens, even when clone uses SSH.
7. Keep repo-level behavior simple: repo selects provider, not secrets.

## Non-Goals

1. Do not remove the SCM provider abstraction.
2. Do not allow repos to override provider-level clone protocol or credential selection in v1.
3. Do not add GitHub App, secret file, or certificate credential types in v1.
4. Do not generalize this module to relay credentials in the same change.
5. Do not rewrite historical specs to pretend this already exists in code.

## Core Decisions

| Topic | Decision | Reason |
| --- | --- | --- |
| Repo binding | Each repo binds to exactly one active SCM provider | Keeps runtime resolution simple and explicit |
| Provider role | SCM provider remains the runtime integration unit | Provider still owns platform type, base URL, webhook behavior, clone strategy, and API adapter |
| Credentials role | Credentials become reusable secret assets | Enables reuse, rotation, and independent lifecycle management |
| Credential scope | Credentials are generic, not SCM-specific | Matches Jenkins-style modeling and leaves room for future reuse |
| v1 credential kinds | `secret_text`, `username_password`, `ssh_username_with_private_key` | Covers current GitHub/Bitbucket API and clone needs |
| API auth | Provider must reference an API credential | SSH alone cannot access SCM platform APIs |
| Clone auth | Provider may reference a separate clone credential | HTTPS and SSH clone may need different auth material |
| Clone override | Repo does not override provider clone protocol in v1 | Avoids hidden repo-level drift |
| SSH host verification | Use provider-scoped trust-on-first-use known_hosts cache | Resolves current host key failures without adding repo-level host config |

## Data Model

### 1. Credentials

Introduce a new `Credential` entity.

```go
type Credential struct {
    ID          int
    Name        string
    Description string
    Kind        string // secret_text | username_password | ssh_username_with_private_key
    Payload     string // encrypted serialized payload
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

`Payload` remains encrypted at rest using the existing backend encryption mechanism. List APIs only expose metadata and masked summaries, never raw secret material.

### 2. Credential Payload Shapes

`secret_text`

```json
{
  "text": "ghp_xxx"
}
```

`username_password`

```json
{
  "username": "alice",
  "password": "secret"
}
```

`ssh_username_with_private_key`

```json
{
  "username": "git",
  "private_key": "-----BEGIN OPENSSH PRIVATE KEY----- ...",
  "passphrase": "optional"
}
```

### 3. SCM Provider

Replace provider-owned raw credentials with references.

Current conceptual model:

```text
scm_provider {
  type
  base_url
  credentials
}
```

Target model:

```text
scm_provider {
  name
  type
  base_url
  status
  api_credential_id        // required
  clone_credential_id      // optional
  clone_protocol           // https | ssh
  created_at
  updated_at
}
```

Rules:

1. `api_credential_id` is required for all SCM providers in v1.
2. `clone_credential_id` is optional.
3. `api_credential_id` must reference `secret_text` or `username_password`.
4. `clone_protocol=https` means clone uses API credential unless a dedicated clone credential is configured.
5. `clone_protocol=ssh` requires `clone_credential_id` whose kind is `ssh_username_with_private_key`.

### 4. Repo Config

`repo_config -> scm_provider` remains the only repo-side SCM binding.

No new repo fields are introduced for:

- clone protocol override
- direct credential binding
- fallback provider list

That is an explicit contract choice, not an implementation shortcut.

## Runtime Model

### Provider Resolution

All repo-scoped SCM work resolves through the repo's bound provider:

1. Load repo
2. Load repo's SCM provider
3. Resolve provider's API credential
4. Resolve provider's clone configuration
5. Execute API or clone operations through that provider context

### API Operations

The following continue to require provider API credentials:

- repo existence lookup
- webhook registration and deletion
- PR sync
- changed-file lookup
- branch SHA lookup
- branch creation
- commit file operations
- PR creation / merge / labels / approvals

SSH credentials do not satisfy these operations.

### Clone Operations

Clone strategy is selected from provider configuration only.

#### HTTPS clone

Use the provider's effective clone credential:

- if `clone_credential_id` is set, it must be `secret_text` or `username_password`
- else fall back to `api_credential_id`

When `clone_protocol=https`, runtime should prefer the SCM provider's current HTTPS clone URL for the repo, resolved from the provider API by `repo.full_name`, rather than blindly trusting a stale stored `repo.clone_url`. If that lookup is unavailable, runtime may fall back to the stored clone URL as a compatibility path.

Credential application rules:

- `secret_text`
  - GitHub/Bitbucket token-style auth over HTTPS
  - runtime injects credential into Git invocation without persisting plaintext in repo config
- `username_password`
  - runtime injects explicit username/password for HTTPS clone/fetch

#### SSH clone

Use `clone_credential_id` of kind `ssh_username_with_private_key`.

Runtime behavior:

1. materialize a temporary private key file with `0600` permissions
2. maintain a provider-scoped `known_hosts` cache under backend runtime state
3. on first connection, use `StrictHostKeyChecking=accept-new` against that cache
4. on later connections, reuse the cached known_hosts file for strict verification
5. run Git with `GIT_SSH_COMMAND` pointing at that key and host verification config
6. remove temporary private key files after clone/fetch completes

The provider's API credential is still used for SCM platform API calls before and after clone.

## Credential Compatibility Rules

| Provider clone protocol | Allowed clone credential kinds |
| --- | --- |
| `https` | `secret_text`, `username_password`, or unset |
| `ssh` | `ssh_username_with_private_key` |

Additional rules:

1. `api_credential_id` cannot reference `ssh_username_with_private_key`.
2. `clone_protocol=ssh` with no `clone_credential_id` is invalid.
3. `clone_protocol=https` with no `clone_credential_id` inherits `api_credential_id`.
4. Cross-platform credential reuse is allowed structurally but must be validated semantically by provider type.
   Example: a GitHub PAT may be reused across multiple GitHub providers, but not accepted by a Bitbucket Server provider.

## Backend API Changes

### Credentials API

Add admin-only endpoints:

- `GET /api/v1/admin/credentials`
- `POST /api/v1/admin/credentials`
- `GET /api/v1/admin/credentials/:id`
- `PUT /api/v1/admin/credentials/:id`
- `DELETE /api/v1/admin/credentials/:id`

Request model examples:

```json
{
  "name": "GitHub org PAT",
  "description": "read/write for AgoraIO-Extensions repos",
  "kind": "secret_text",
  "payload": {
    "text": "ghp_xxx"
  }
}
```

```json
{
  "name": "Bitbucket SSH deploy key",
  "kind": "ssh_username_with_private_key",
  "payload": {
    "username": "git",
    "private_key": "-----BEGIN OPENSSH PRIVATE KEY----- ...",
    "passphrase": ""
  }
}
```

Response model hides plaintext payload and instead returns:

- `id`
- `name`
- `description`
- `kind`
- `usage_count`
- masked summary fields such as username or host-independent labels

### SCM Provider API

Provider create/update payload changes from raw credentials to credential references.

Target request fields:

```json
{
  "name": "GitHub Extensions RW",
  "type": "github",
  "base_url": "https://api.github.com",
  "api_credential_id": 12,
  "clone_protocol": "https",
  "clone_credential_id": null,
  "status": "active"
}
```

or

```json
{
  "name": "Bitbucket AI repos",
  "type": "bitbucket_server",
  "base_url": "https://bitbucket-api.agoralab.co",
  "api_credential_id": 21,
  "clone_protocol": "ssh",
  "clone_credential_id": 22,
  "status": "active"
}
```

Validation behavior:

- reject invalid credential kind/provider combinations
- reject deleting a credential while still referenced by a provider unless caller explicitly rebinds providers first

### Repo API

Repo create/update keeps selecting a provider only:

```json
{
  "scm_provider_id": 7,
  "full_name": "org/repo"
}
```

No credential fields are accepted on repo endpoints.

## Frontend UX

### Settings

Add a new Credentials section in Settings:

- list credentials
- add credential
- edit credential metadata and secret payload
- delete unused credential

Credential creation flow follows Jenkins-style type selection with these v1 options:

- Secret text
- Username with password
- SSH Username with private key

### Provider Form

Replace raw token input with:

- provider name
- provider type
- base URL
- API credential selector
- clone protocol selector
- clone credential selector, conditionally shown when needed

The UI should explain:

- API credential is required for provider operations
- SSH clone does not replace API credential

### Repo Form

Repo UI remains simple:

- select provider
- select repo URL / full name
- no direct credential selection

## Migration Plan

### Phase 1: Schema

1. Add `credentials` table
2. Add `api_credential_id`, `clone_credential_id`, `clone_protocol` to `scm_providers`
3. Keep old `scm_providers.credentials` temporarily during migration

### Phase 2: Data Backfill

For each existing SCM provider:

1. decrypt current `scm_providers.credentials`
2. create a new `Credential` of kind `secret_text` using the existing token
3. set provider `api_credential_id` to the new credential
4. set provider `clone_protocol=https`
5. set `clone_credential_id=NULL`

This preserves current behavior while enabling reuse going forward.

If a legacy `scm_providers.credentials` value cannot be decrypted with the active encryption key, the backfill must skip that provider, leave its new credential references unset, and allow the service to start. That provider remains admin-repairable rather than startup-blocking.

### Phase 3: Runtime Switch

1. switch provider factory and scan clone logic to resolve credentials from the new module
2. stop reading `scm_providers.credentials`
3. remove legacy field in a follow-up migration once all environments are upgraded

## Testing Strategy

### Backend

1. Credential CRUD validation for all three kinds
2. Provider validation by credential kind compatibility
3. Repo create/update still binds to exactly one provider
4. HTTPS clone with inherited API secret-text credential
5. HTTPS clone with explicit username/password credential
6. SSH clone with private key credential
7. Credential reuse across multiple providers
8. Deletion protection for credentials still in use

### Frontend

1. Settings credential list and type-specific forms
2. Provider form requires API credential
3. Provider form shows correct clone credential options for selected protocol
4. Repo form continues selecting only provider
5. Scan failure surfaces backend error text

### Integration

1. private GitHub repo scan via HTTPS token
2. private Bitbucket repo scan via SSH key
3. webhook registration and PR sync still work through provider API token

## Rollout Risks

1. Existing providers may be left without migrated API credentials.
2. SSH clone introduces temporary file handling and host verification concerns.
3. UI complexity increases if credential and provider editing are mixed in one dialog.
4. Legacy tests expecting inline provider credentials will need coordinated updates.

## Recommended Implementation Order

1. Ent schema and migration for credentials and provider references
2. backend credentials module and admin APIs
3. provider create/update/list API refactor
4. clone runtime refactor for HTTPS and SSH credential resolution
5. frontend credential management
6. frontend provider form refactor
7. repo add/edit flow verification
8. migration cleanup for legacy provider `credentials` field

## Acceptance Criteria

This design is considered implemented when all are true:

1. A repo still binds to exactly one SCM provider.
2. A provider can reuse an existing credential instead of storing raw secret material.
3. Scan clone for private HTTPS repos succeeds when provider API or clone credentials permit it.
4. Scan clone for SSH repos succeeds when provider clone credential is a valid SSH private key.
5. SCM API operations still succeed for SSH-cloned repos because provider API credentials remain available.
6. Repos never accept direct credential bindings in their API or UI.
