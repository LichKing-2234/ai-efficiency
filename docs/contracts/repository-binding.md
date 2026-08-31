# Repository Binding Contract

This contract describes repository identity, deterministic Code Platform
binding, read-only hook eligibility, and post-bind repair. Read it before
changing repo discovery, SCM provider matching, hook eligibility, webhook
registration, or SCM-dependent operation gates.

## Repository Identity and Binding

A repository can exist before it is bound to a Code Platform. Its durable
identity is derived from the remote URL into a stable repo key, full name,
clone URL, and default branch. Identity creation is not proof that an SCM
provider is configured.

Binding points to one configured SCM provider, never directly to one
credential. SCM API access remains behind the unified SCM Provider interface.
An explicit administrator-supplied provider wins and is never replaced by
automatic matching.

Active and webhook-failed repos are hook-eligible whether bound or unbound.
SCM-dependent operations still require a binding and return the stable
`repo_unbound` error when it is missing.

## Mutation and Read-Only Entry Points

| Entry point | May create identity | May auto-bind | Purpose |
| --- | --- | --- | --- |
| Normal authenticated remote ensure | Yes | Newly created repos only | Establish durable repo identity for normal CLI use. |
| Reporter-authenticated remote ensure | Yes | No | Establish minimum identity for attribution reporting. |
| Remote eligibility resolve | No | No | Read current eligibility and binding state. |
| Batch hook eligibility | No | No | Classify only the requested repos for hook installation/refresh. |
| Direct admin create without provider | Yes | Yes | Create a visible unbound repo, then attempt deterministic binding. |
| Admin auto-bind repair | No | Yes | Repair existing unbound active or webhook-failed repos. |

Finding an existing repo during normal ensure may refresh identity metadata but
does not overwrite its binding or auto-bind an existing unbound row. Existing
unbound rows are repaired only by the explicit administrator batch action or
manual binding.

## Deterministic Provider Matching

The repo host comes from the parsed clone URL when possible, then the repo-key
host. Matching lowercases hosts, removes a trailing dot, and removes default
ports when represented explicitly.

Each active Code Platform contributes its API host and any explicit SSH host
alias. GitHub SaaS maps `api.github.com` to the clone host `github.com`.
Enterprise and Bitbucket deployments use their configured API host plus an
explicit SSH alias when API and clone hosts differ.

The result is deterministic:

- Exactly one active host match binds the repo.
- No match or an invalid repo host leaves it unbound.
- More than one match is ambiguous and leaves it unbound.
- Inactive providers do not participate.
- Repo names alone never select a provider.

The API and structured log expose the result reason (`matched`,
`already_bound`, `no_match`, `ambiguous`, `invalid_repo_host`, or
`provider_error`) without logging credentials or webhook secrets.

## Binding Transaction and Post-Bind Work

Once exactly one provider matches, AI Efficiency persists the provider binding
as an inventory mutation before calling the SCM API. Post-bind work then:

1. Verifies the repository through the selected SCM provider.
2. Refreshes canonical repository metadata when verification succeeds.
3. Registers pull-request and push webhooks with a generated secret.

Provider verification, credential resolution, metadata persistence, or webhook
registration can fail after the deterministic binding. That failure does not
guess another provider or remove the binding. The result becomes
`provider_error`; webhook registration failure also records
`webhook_failed`. A later explicit webhook-repair workflow owns an already-bound
webhook failure.

## Administrator Repair

The admin repair action scans only unbound repos whose status is active or
webhook-failed. It applies the same host matcher and post-bind behavior to each
repo, returns aggregate counts plus per-repo reasons, and continues after a
per-repo failure when the batch itself remains usable.

No-match and ambiguous repos remain visible and manually bindable. There is no
scheduled background binder and no ordinary-user binding mutation.

## Safety Boundaries

- Remote resolve and hook eligibility remain read-only and never call SCM APIs.
- Attribution-reporter ensure never binds or registers webhooks.
- Existing bindings are not overwritten by discovery or repair.
- Binding is preserved across post-bind operational failure.
- Logs and responses explain matching without secret-bearing URLs, tokens,
  credentials, or webhook secrets.
- A new match source, background binder, or ordinary-user binding path requires
  a current GitHub spec/ticket before implementation.
