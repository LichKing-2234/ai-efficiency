# Directory Sync Contract

This contract describes the current organization-directory source, safe HTTP
DSL, applied snapshot, shared Directory facts, and explicit offboarding flow.
Read it before changing Directory source configuration, apply semantics,
department/member interpretation, representative metadata, organization-backed
user filters, or offboarding token revocation.

Directory Sync is an organization-facts source. It is not an authentication
provider, user provisioner, or subscription manager. LDAP and Relay SSO retain
their authentication roles, and Relay subscription changes remain explicit
administrator or quota-reset workflows.

## Source and DSL

An administrator configures a `full_company` Directory source as JSON or YAML.
The persisted DSL contains credential references, never credential values. A
source may be validated, previewed, applied manually, or applied by its
schedule; deleting a source is soft deletion and disables future scheduling
without removing historical runs.

DSL version 1 is deliberately constrained:

- External steps use bounded `GET` requests.
- URLs are HTTPS in production; authentication injects one referenced secret
  through the declared header contract.
- Steps may use literal safe headers/query values, prior-step `foreach`, simple
  `item`/`source` templates, and a bounded JSONPath subset.
- Mapping produces canonical departments or members. Departments require a
  stable external ID and name; members require a valid normalized email.
- A member may map one or many department IDs. Duplicate email/department
  pairs coalesce into one canonical membership.
- Allowlisted metadata supports department representative member IDs, member
  leader-department IDs, and an explicit WeCom user ID used only for
  quota-reset notifications.
- Representative overrides are exact-target, bounded append/remove operations.
  Removal runs before append, preserves unrelated mapped values, and fails the
  whole preview/apply when a target is missing or ambiguous.

The DSL cannot execute JavaScript, jq, shell, browser automation, webhook code,
external writes, arbitrary secret interpolation, request bodies, filters, or
unbounded expressions. Literal secret-bearing headers/query values and common
secret-shaped values fail static validation.

Templates, fixtures, docs, and examples use synthetic organization data. Run
history excludes credentials, request headers, raw response bodies, and
unbounded employee payloads.

## Run Lifecycle

Static validation performs no external HTTP request. Preview executes the DSL
and stores redacted counts, warnings, and diff evidence, but never changes the
current Directory facts or offboarding candidate set.

Apply is the only facts writer:

1. Reject another queued/running apply for the same source.
2. Execute every required step under configured limits.
3. Resolve all overrides against the complete mapped result.
4. Normalize departments, members, memberships, warnings, and effective
   hierarchy.
5. In one transaction, replace that source's facts, complete the run, update
   the source's last-run and last-successful-run pointers, and advance the
   shared Work Items revision.

A failed apply, validate, or preview does not change current facts or the Work
Items revision. Source update and soft delete do advance that revision in the
same transaction as their source mutation because they can change which
snapshot is current.

The in-process scheduler starts due enabled sources and uses the same apply
guard. The current contract does not claim cross-replica scheduler leadership;
adding that requires a database lease or another explicit coordination design.

Run history is bounded and independently exposes the newest active run so UI
recovery does not depend on the selected history page. Full warnings and
diagnostics are fetched only for the selected run.

## Current Snapshot Authority

Directory Sync is the only writer. `directoryfacts` is the shared read owner.

The current company view is the newest completed `full_company` apply referenced
by a non-deleted source's last-successful pointer, ordered by completion time
and run ID. Editing a source does not make an older apply current. A request
first resolves one `{source_id, run_id}` snapshot and every subsequent fact read
is pinned to that pair.

Consumers do not independently select a source, reinterpret raw parents, or
rebuild membership/representative algorithms.

## Persisted Fact Semantics

Departments preserve the upstream parent fact and a separately persisted
effective parent. Apply derives one deterministic effective forest:

- Blank or missing parents become roots.
- Valid edges remain unchanged.
- Each closed cycle loses exactly one deterministic, locale-independent anchor
  edge so readers can traverse without recursion loops.

Readers use the effective parent for hierarchy, depth, display path, child and
subtree navigation. The raw source path remains technical evidence and is not
the normal user-facing organization path.

One normalized email produces one canonical member per source. Current
multi-department membership is owned by membership rows; the member's primary
department remains only a compatibility fallback for older snapshots.

Local-user matching prefers a positive persisted match and falls back to
trimmed, case-insensitive email. Directory Sync does not create or delete local
users.

Representative roots are the union of:

- Member external IDs declared by department representative metadata.
- Department external IDs declared by member leader metadata.

Removing only one duplicate declaration does not remove the effective
relationship. Shared readers return ordinary domain facts and keep Ent/query
details inside the Directory facts module.

## Administrator Organization Views

Admin Users and other organization consumers resolve the current snapshot
server-side. They do not expose a normal source selector.

Department filters include the selected effective subtree and match local users
through current memberships/email. Default user listings retain unmatched local
users; a department filter excludes them. Department labels use name-based
display paths, and direct counts remain distinct from subtree aggregates.

Current-filter subscription jobs and the visible Admin Users filter must resolve
the same directory/access-status target set. Paginated paths use bounded
Directory facts queries rather than loading and reinterpreting the entire graph.

## Offboarding

An offboarding candidate is a local Relay-bound user whose non-empty normalized
email is absent from the current successful full-company snapshot and who has
no succeeded Directory offboarding action. Candidate count and page use the
same database anti-join; failed, preview, or partial runs never create absence.

Disabling requires administrator authentication, exact email confirmation, and
the fixed missing-directory reason. Immediately before mutation, the service
re-resolves the current source/run and confirms that the user is still absent
and still has a Relay binding.

The service then:

1. Records/rereads the action and disables the upstream Relay user through the
   optional provider capability.
2. After upstream success, uses an independent bounded context and one local
   transaction to set the user's token-valid-after floor, mark the action
   succeeded, and advance the Work Items revision.
3. If local finalization fails, rolls it back and records `partial_failed`
   under a second independent bounded context.

The upstream call cannot be part of the local transaction. A partial failure
therefore means Relay may already be disabled while local tokens still require
repair. The action remains visible for retry/investigation.

Directory offboarding does not remove subscriptions. The general Admin Users
direct-disable action is a separate contract: it records the upstream disable
but intentionally does not revoke local tokens or change subscriptions.

## Safety Boundaries

- Directory APIs and mutations are administrator-only.
- External execution is bounded by time, response size, and item limits.
- Directory Sync never writes to the external organization system.
- Credentials remain referenced/encrypted and are absent from logs and run
  summaries.
- Preview and failed apply cannot change authorization, offboarding, or current
  organization facts.
- Organization-derived actions fail closed when no current successful snapshot
  exists.
- New executable DSL features, automatic subscription mutation, local-user
  provisioning, or webhook-driven sync require a current GitHub spec/ticket.
