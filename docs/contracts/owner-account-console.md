# Owner Account Console

The Owner Account Console is the user-facing 「我的账号」 surface at
`/usage/accounts`. It lets an **Account Custodian** read the facts of the OpenAI
OAuth accounts they own and use two account-scoped upstream mutations that
would otherwise require Sub2API admin access.

Read this contract before changing owner-account ownership matching, the
owner-account service, the operation worker, the `/user/owner-accounts` or
`/admin/owner-operations` routes, or the console view.

## Ownership

Ownership is **derived on every request; it is never stored**:

- Normalize with trim and lower-case.
- The caller's `users.email` must equal the account's `credentials.email`
  **or** the account's `name` when the name is email-shaped (exactly one `@`
  with non-empty local and domain parts). Either field matching is sufficient;
  there is no precedence between the fields.
- Exclude accounts that are not `platform=openai`, not `type=oauth`, are shadow
  accounts (they carry a parent-account relation), or belong to a non-primary
  Relay provider.
- A non-matching account returns `404 account-not-found`; a locally disabled or
  offboarded user returns `403 owner-inactive` and sees no accounts. The
  canonical disabled predicate covers token revocation, Relay disablement, and
  a succeeded offboarding action.
- Because the account name is admin-editable, renaming an account to another
  person's email grants that person access. Operators keep account names equal
  to the owner's email. If the two fields match different local users, both
  users see the account.

There is no alias table, no plus-address normalization, no co-custody, and no
self-service claim.

## Routes

All routes use the standard `/api/v1` envelope and session auth.

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/user/owner-accounts/scope` | `{account_count, mutations_enabled}` for tab visibility |
| GET | `/user/owner-accounts?page&page_size` | Paginated list, exact total, `mutations_enabled` |
| POST | `/user/owner-accounts/usage` | Batch usage for up to 100 account ids (chunks upstream at 20) |
| POST | `/user/owner-accounts/:id/usage/refresh` | Operator-triggered usage refresh |
| POST | `/user/owner-accounts/:id/quota/refresh` | Fresh quota observation |
| POST | `/user/owner-accounts/:id/referral/refresh` | Fresh referral observation |
| POST | `/user/owner-accounts/:id/reset-credit` | `202` async reset-credit operation |
| POST | `/user/owner-accounts/:id/invitations` | `202` async invitation operation |
| GET | `/user/owner-accounts/operations/:operation_id` | Poll one operation |
| GET | `/user/owner-accounts/:id/operations` | Paginated operation history |
| GET | `/admin/owner-operations` | Read-only admin operation list (status/action/account filters) |

Page size is 20, 50, or 100. Account rows expose identity, group names,
expiry-clamped reset credits, Codex credits, referral eligibility, scheduler and
rate-limit health, `blocked_actions`, and `refresh_cooldown_until`.

## Mutation gates

Both mutations are disabled by default by
`relay.owner_account_mutations_enabled` (`AE_RELAY_OWNER_ACCOUNT_MUTATIONS_ENABLED`).
The switch is read when the backend process starts; changing it requires a
restart. When enabled, the following are enforced server-side, in this order:

1. Actor is the matched owner and the confirm/consent acknowledgement is present.
2. Reset requires a **persisted successful quota observation** newer than the
   last non-rejected reset attempt, no older than 15 minutes, reporting at least
   one unexpired reset credit. Invitation requires the analogous referral
   observation, no older than 10 minutes, with remaining send capacity and the
   `program_id` taken from that observation.
3. Rolling 24-hour per-account caps: five reset attempts and ten invitation
   attempts. Rejections before an attempt do not count; attempts that reached
   the upstream (including failures and unknown outcomes) do.
4. At most one running mutation per account across both actions; a conflict
   returns `409 mutation-in-flight` with the existing operation id.
5. Gates read only persisted observation rows and operation rows. They never
   read the Relay directory cache or Redis, so a stale cache cannot admit or
   reject a spend.

The request inserts a `running` row and returns `202` with an operation id. A
detached worker (65-second bound, database-time heartbeat every 5 seconds, one
upstream call, no retry) writes the terminal state. A panic, an ambiguous
invite response, a timeout, or a heartbeat older than 30 seconds becomes
`unknown`. `unknown` blocks the same action until an observation newer than that
operation exists; there is no administrator override. Startup and the next
mutation for the account also sweep stale `running` rows to `unknown`.

`usage refresh` does not start the 30-second refresh cooldown; quota and
referral refresh rows do.

## Failure identifiers

Errors put a stable identifier in top-level `details.error_code` (kebab-case)
and never forward upstream error text:

`account-not-found`, `operation-not-found`, `owner-inactive`,
`actions-disabled`, `consent-required`, `invalid-email`,
`reset-freshness-required`, `invite-freshness-required`, `credits-expired`,
`mutation-in-flight`, `busy`, `program-changed`, `referral-unavailable`,
`refresh-cooldown`, `mutation-rate-limited`, `upstream-unavailable`,
`capability-unsupported`, `compliance-ack-required`, `usage-unavailable`,
`usage-batch-too-large`, `invalid-pagination`, `invalid-filter`,
`invalid-request`, `internal-error`.

`429 mutation-rate-limited` is used only for daily caps; refresh cooldown uses
`refresh-cooldown` plus `retry_after_seconds` / `cooldown_until`.

## Privacy and observability

The console may disclose a custodian's own account identity, account-level cost
figures (A $ = upstream `cost`, U $ = `user_cost`, `standard_cost` in a tooltip
only), and group names. It never discloses parent-account identity, notes,
proxies, credentials other than the matching email, fingerprint seed material,
or arbitrary `extra` keys; only five allow-listed `extra` snapshots are read.
Invite email is stored on the operation row for accountability and is never
logged.

Telemetry emits `ai_efficiency_owner_account_operations_total{action,outcome,release}`
and a duration histogram without per-user labels; `unknown` is a first-class
outcome and is alertable.

## Known v1 limits

- `program_name` is always null; the owner-safe directory projection has no
  program name.
- `health.reauth_required`, `banned`, and `forbidden` are not available from the
  directory projection; usage-derived health facts appear only on the usage
  surface.
- Client-side freshness checks are UX defense only; server gates are
  authoritative.
- A poll timeout or transport error is shown as "poll interrupted / still
  running"; only a server `unknown` state blocks the action.
