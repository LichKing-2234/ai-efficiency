# Relay User Access Contract

This contract describes how current users receive Relay provider, Access
Group, credential, model, protocol, Connection Test, and administrator
subscription capabilities. Read it before changing Relay provider management,
user onboarding, personal API-key writes, group capability metadata, protocol
probes, or administrator subscription jobs.

Authentication, local identity reconciliation, JWTs, and OAuth grants are
defined in [`auth-and-oauth.md`](./auth-and-oauth.md).

## Provider Authority and Runtime

DB-backed Relay Provider rows are the normal source for user-facing provider
selection and administrator multi-provider management. Each row carries its
display identity, base URL, encrypted admin key, default model, enabled/primary
flags, and a monotonically increasing configuration version.

Runtime configuration may seed the primary provider at startup. The legacy LLM
settings endpoints remain an admin-only compatibility surface for the
bootstrapped primary runtime: Relay URL is read-only there, while admin key and
model can be persisted and hot-reloaded. The main Settings provider surface
manages DB-backed Relay Provider rows.

The shared Relay runtime:

- Resolves process clients by provider ID and configuration version.
- Rejects a provider row older than the latest observed version.
- Bounds a process client to at most five minutes and evicts it after a provider
  mutation.
- Publishes only provider ID and configuration version for cross-replica
  invalidation; secrets and display fields are excluded.
- Uses shared metadata only for reconstructible group/model display facts.
  Current membership, personal keys, subscriptions, quota, and mutations remain
  fresh Relay reads.
- Creates an uncached user-scoped provider for model listing and Connection
  Tests so the selected personal key is never replaced with the admin key.

All Relay integration stays behind the Relay Provider HTTP adapter. AI
Efficiency does not read the Relay database directly and does not require a
source-code change in the Relay service.

## Access Group Projection

The user setup surface lists enabled providers with primary first. A local user
without a Relay binding receives an empty provider list and an explanatory
message rather than invented access.

For each enabled provider, AI Efficiency reads the current Relay user's API
keys and allowed Access Groups. A group is exposed only when it has a stable ID
and platform. Each group summary contains:

- Stable ID, display name, and platform.
- Current supported protocols and one recommended protocol.
- A credential state derived from an active group-scoped managed key.

An allowed group without a managed key remains visible with credential state
`missing`. Existing personal keys are matched by group, active status, and the
managed username/email-derived key name. The response may expose a key value
when Relay returns it, but callers must treat key material as a secret and must
not log it.

The legacy provider-delivery endpoint remains a compatibility path and may
ensure one key for the first group. The `/user` workflow uses explicit
group-scoped list, create, and regenerate operations instead.

## Relay Identity for User Writes

Personal key creation and regeneration require Relay user credentials because
the upstream key-write endpoint does not accept a provider admin key as a user
JWT substitute.

Before a write, AI Efficiency verifies the stored Relay binding. A missing or
upstream-missing binding is resolved by exact email, canonical username, then
legacy username; if none exists, AI Efficiency provisions a Relay user with a
generated password and stores both the binding and encrypted password.

For a valid binding, AI Efficiency uses an available encrypted Relay password.
If none is usable, or if a credentialed write fails, it rotates the Relay user
password through the admin API, stores the generated replacement encrypted,
and retries the user-scoped write once. The LDAP password is never used for
this repair.

These repairs are mutation prerequisites, not login behavior: Relay SSO itself
still authenticates existing Relay users only.

## Personal Credential Lifecycle

Create is idempotent within one backend process for a local user, provider, and
Access Group:

1. Acquire the keyed process-local create lock.
2. Ensure the current Relay binding and write credential.
3. Reread current personal keys while holding the lock.
4. Return the existing active managed key for the group when present.
5. Otherwise create one group-scoped key with the current Relay user JWT.

Regenerate verifies the same binding and credentials, marks matching managed
keys inactive, then creates a new group-scoped key. Create and regenerate may
rotate the generated Relay password and retry once when the first user-scoped
write fails.

The frontend suppresses duplicate in-flight clicks, but the backend lock owns
same-process idempotency. There is no claim of cross-process global
serialization.

## Onboarding Workflow

The browser treats an Access Group as the first-class selection. Provider and
group changes reset every visible state that depends on that selection before
new requests start.

The workflow separates business facts from the visible step:

- Opening the page or changing provider/group shows the Access Group step even
  when a key already exists.
- A missing key offers an explicit create-and-continue action.
- A key makes model loading, Connection Test, and configuration methods
  reachable.
- Connection Test is the recommended next action, not a gate for manual,
  automatic, or CC Switch configuration.
- Success stays on the test step until the user explicitly advances. Failure
  keeps the selected group/key and offers retry.
- Key regeneration invalidates the prior Connection Test result.

Provider, credential, model, and Connection Test requests use independent
monotonic generations. A response from an older provider/group/key/model/
protocol selection cannot update loading, results, or navigation for the
current selection.

The workflow owner holds selection, credential, Current Step, model/protocol,
Connection Test, configuration-method, and stale-response state. Transport,
deterministic configuration generation, and view rendering remain separate
owners. The page has one shared path for users; it does not branch its primary
state model by developer/non-developer identity.

## Models and Protocol Capabilities

Models are listed only after the backend revalidates current group membership
and selects an active personal key for that provider/group/platform. Missing
binding, membership, key, or provider capability yields an empty model list
with an explanatory message rather than an admin-key request.

Stable Connection Test capabilities are centralized in the backend:

| Group platform | Recommended | Supported protocols |
| --- | --- | --- |
| OpenAI | Responses | Responses, Chat Completions, and Messages only when message dispatch is enabled |
| Anthropic or Claude | Messages | Messages, Responses, Chat Completions |
| Gemini | GenerateContent | GenerateContent, Chat Completions |
| Antigravity | Messages | Messages, Antigravity GenerateContent |
| Grok | Responses | Responses, Chat Completions, Messages |
| Composite | Chat Completions | Chat Completions, Responses, Messages, GenerateContent |

Capability metadata flows from Relay group facts through the shared metadata
envelope to user setup. A schema version that predates a required capability
field is a cache miss, not an implicit false value. The frontend renders the
backend matrix and does not duplicate it.

## Connection Test Proof

A Connection Test result is bound to exactly this identity:

```text
provider + Access Group + current personal key + model + protocol
```

The backend revalidates current group membership, protocol support, and the
active group-scoped personal key for every request. Omitting protocol selects
the current recommendation; an explicitly unsupported protocol is rejected.
The test sends one small non-streaming `Reply with OK` request through the
selected protocol with no retry and no fallback.

Success requires all of these facts:

| Protocol | Required terminal evidence |
| --- | --- |
| Responses | Completed status and non-empty output text |
| Chat Completions | Non-empty first-choice finish reason and assistant text |
| Messages | Non-empty stop reason and text content |
| GenerateContent | Candidate finish reason and candidate text |
| Antigravity GenerateContent | Candidate finish reason and candidate text on the dedicated route |

HTTP success without valid terminal evidence or assistant text is failure.
The Relay adapter owns protocol endpoint selection, payload/headers, terminal
parsing, and a bounded upstream error body. AI Efficiency performs no protocol
retry or automatic fallback.

Claude/Anthropic Messages and OpenAI Responses probes may carry the exact
official-client-compatible identity profile currently required by Relay
policy. Those profiles are confined to their corresponding Connection Test
request. They do not alter other protocols, ordinary Relay requests, generated
client configuration, or local client state.

The result exists only in the current page instance. Changing provider, group,
key, model, or protocol clears it and invalidates in-flight work. A successful
result proves that AI Efficiency reached Relay with the selected personal
credential/model/protocol and received a valid completion. It does not prove
that a local Codex, Claude, Gemini, or other client is installed or configured.

The displayed error may include up to the bounded raw body AI Efficiency
received from Relay/upstream. AI Efficiency adds no second redaction layer, so
this contract depends on the upstream error response excluding secrets.

## Administrator Subscription Jobs

The current administrator UI uses persisted subscription jobs rather than one
long browser request. A job snapshots its local target set and each target's
current Relay binding before execution. Later list filters, pagination, or
binding changes do not alter that job.

Supported scopes are:

- Selected local users, deduplicated in first-seen order. Missing positive IDs
  remain explicit failed results.
- Every user matching the current Admin Users filter across pages.
- All local users with a positive Relay binding.

Supported operations are add, extend, remove, and reset quota. Add requires a
positive validity period; extend requires positive days; reset quota clears
the selected subscription's daily, weekly, and monthly upstream windows.

The provider must be enabled and expose the selected assignable subscription
group. Invalid provider/group/scope/operation input rejects job creation. More
than 500 targets is rejected before mutation.

Unmapped targets become skipped results except in all-mapped scope, which
excludes them before execution. Each mapped target has its own bounded Relay
deadline; a target failure becomes a failed row and does not stop later
targets. The whole-job deadline scales with target count. A queued or running
job with no progress for more than one hour is marked abandoned during latest
job recovery.

Only unambiguous already-existing subscription responses, or a follow-up read
proving the exact active group, are idempotent add success. Semantic conflicts
remain visible failures. Subscription jobs mutate Relay subscription state;
they do not edit local user identity.

The synchronous batch and single-user add routes remain compatibility
surfaces. The current frontend starts jobs and polls their progress/results.

## Security and Failure Boundaries

- Provider admin keys remain backend-only and are returned masked on admin
  reads. Personal key/model/probe paths use the current personal key.
- Relay credentials and API keys are excluded from shared metadata and
  invalidation payloads.
- Provider mutation advances configuration version and invalidates process and
  cross-replica runtime state without publishing secrets.
- Provider, membership, credential, model, and protocol failures remain
  distinct user-visible outcomes; absence is not silently converted to another
  group, key, model, or protocol.
- Browser Connection Test and configuration state is ephemeral. The backend
  does not persist a successful test as account health.
- AI Efficiency does not modify Relay source code or couple to its database.

These boundaries describe current behavior. New provider protocols, key
lifecycle rules, subscription operations, or onboarding state require a
current GitHub spec/ticket before implementation.
