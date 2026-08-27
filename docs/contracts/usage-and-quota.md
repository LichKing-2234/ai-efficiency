# Usage and Quota Contract

This contract describes personal AI Usage, the shared usage-window preference,
representative scope, Team Usage/member views, quota presentation, OAuth pool
projection, and delegated rate-multiplier control. Read it before changing
usage range semantics, representative authorization, quota display, or member
multiplier writes.

Performance caches and prewarm generation are separate contracts. They may
accelerate these responses but cannot change authorization or business
semantics.

## Shared AI Usage Window

Personal, Team, and selected-member AI Usage share one browser-profile
preference with exactly three values: `today`, `7d`, and `30d`. Missing,
invalid, or unreadable storage falls back to `30d`.

The preference is restored synchronously before the first request is built.
Initialization and fallback do not write storage; only an explicit user
selection writes. A storage write failure does not block the visible selection
or request, and a request failure does not roll back expressed preference.

The preference belongs to browser origin/profile, not an authenticated user. It
survives logout, does not synchronize across tabs/devices, and contains no
identity or usage data.

Activity ranges remain URL-owned with their separate presets/custom dates.
Activity never reads or writes the AI Usage preference.

## Personal Usage

Personal AI Usage resolves the current local user and enabled primary Relay
provider. A missing encrypted Relay credential produces an unconfigured view;
a valid binding/credential reads the current user's usage through the Relay
user-origin capability.

Usage, group quota, and OAuth pool are independent sections:

- Usage returns range stats, trend, models, and explicit freshness.
- Group quota is a fresh request branch with `ok`, `empty`, or `unavailable`
  state. Quota failure does not erase usage.
- OAuth pool usage is optional and privacy-safe. Failure or missing snapshots
  hides/degrades only that pool section.

The combined dashboard remains a compatibility response; dedicated quota and
pool endpoints own those sections for independent browser loading.

## Personal Quota Presentation

Each visible quota row is based on an active personal key and its Access Group.
An explicit API-key quota wins. Otherwise a subscription group uses the
selected subscription window; a non-subscription group may expose its group
limit without inventing used values.

The dashboard range maps to subscription enforcement windows:

| AI Usage selection | Subscription window |
| --- | --- |
| `today` or hourly range | Daily |
| `7d` | Weekly |
| `30d` | Monthly |

A subscription reset timestamp appears only when the matching window returns a
valid reset. API-key quota rows and subscriptions without that timestamp retain
their Used/Quota presentation but do not claim a reset.

Quota and used values stay in Relay/subscription enforcement units. Missing
limits are shown as unlimited or unconfigured according to the source facts;
the frontend does not manufacture a limit.

## OAuth Account-Pool Projection

The pool endpoint uses only the current user's effective Access Groups and
active OAuth accounts, then selects valid seven-day snapshots. It returns an
average utilization, valid/active coverage, latest snapshot time, and nearest
future reset without exposing account identity or credentials.

Pool utilization is not the current user's Used/Quota and is not defined as
`sum(used) / sum(quota)`. Accounts without valid snapshots are omitted. Member
usage never calls this personal pool endpoint.

## Representative Scope

Representative authorization is derived from the current Directory facts
snapshot, not Relay group ownership and not administrator role.

The actor's representative roots are the union of department and member
representative metadata. Scope includes the largest represented roots, their
effective subtrees, current multi-department memberships, matched local users,
and directory-only members. Every authoritative scope is bound to actor ID and
role plus the current Directory source/run; a concurrent role or run change is
rechecked before scope is returned.

No current snapshot or no representative roots yields no delegated scope.
Redis or another optimization failure falls back to PostgreSQL; an
authoritative error fails closed.

Directory-only rows remain visible in Team Overview with stable directory
identity and `selectable=false`. They may resolve exact-email Relay usage for
read-only overview aggregation, but member detail and mutation require a
positive scoped local user and Relay binding.

## Team and Member Views

Personal `/usage` is current-user-only. Representatives use Team Overview for
their subtree and a dedicated selected-member route for one authorized local
member.

Team Overview:

- Uses the shared Today/7 Days/30 Days range for summary, ranking, trend,
  members, and organization branches.
- Ranks by selected-window tokens; billed usage is auxiliary and must not be
  labeled as direct financial cost.
- Includes the complete authorized Directory roster, including unmatched or
  no-usage members as non-selectable rows.
- Deduplicates canonical members for team/subtree totals while allowing one
  multi-membership member to appear in each direct department branch.
- Shows team/member/department usage and availability states, but never quota
  cards, subscription rows, or multiplier controls.

Selected-member detail revalidates the target against current scope and reads
usage through Relay admin aggregate capabilities rather than impersonating the
member. It exposes active subscription rows and delegated multiplier controls
only when the target is manageable.

Administrator role alone does not widen normal Team Usage routes. Admin-wide
audit remains a separate administrator surface.

## Delegated Rate Multiplier

A user-specific group rate multiplier changes how quickly future requests
consume that member's existing subscription limits. It does not edit the group
limit, assign/remove a subscription, modify an API key, or reprice historical
used values.

Displayed daily/weekly/monthly Used/Quota remains the raw Relay enforcement
basis. The effective multiplier is selected in this order: user-specific,
group default, system default. Reset clears the user-specific value and returns
to inheritance; it does not write the inherited value explicitly.

A set value must be finite, positive, at most two decimal places, and no greater
than the server maximum (default `10`). Draft edits remain browser-only and do
not change Used/Quota or create an audit row before confirmation.

Authorization rules fail closed:

- Self edits are forbidden.
- The target must be a current scoped local member with Relay mapping and an
  active subscription for the selected group.
- A target who is also a representative is manageable only when every target
  representative root is a strict descendant of one actor root. Peers cannot
  manage each other.

Every attempted write creates local audit evidence. Inside a provider/group
lock, AI Efficiency rereads the subscription and complete group rate entries,
preserves every non-target rate/RPM value, applies only the target rate change,
replaces the full upstream list, and reads it back. A no-op writes no Relay
mutation and records `changed=false`. A mismatched readback is partial failure,
not success.

## Failure Boundaries

- Usage authorization, current Directory scope, and current group membership
  are never inferred from cached presentation data.
- One section's transient failure does not silently replace another section's
  current facts.
- A stale request generation cannot overwrite a newer browser range or
  selection.
- Team Overview returns explicit unavailable reasons rather than ranking a
  truncated scope as complete.
- Provider capability absence disables the affected read/write without
  broadening scope or falling back to another user/provider.
- New windows, representative rules, quota bases, or multiplier policy require
  a current GitHub spec/ticket.
