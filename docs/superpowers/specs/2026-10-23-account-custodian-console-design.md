# Account Custodian Console ("我的账号") Design

Status: Implemented by PR #505, 2026-10-23. The implementation-facing contract
for the shipped routes, response shapes, failure identifiers and gates is issue
#502. Revision 3 simplifies revision 2 after product review: there is **no
local ownership registry**. An upstream account belongs to the local user whose
email matches the account's OpenAI email. The document records the reviewed
direction and rationale for a user-facing surface through which that user can
read account-scoped facts and use the two account-scoped mutations that
previously required Sub2API admin access. Where this design and the shipped code
differ, the implementation notes below and issue #502 prevail.

Upstream facts were verified against Sub2API `main` HEAD `dba770d` (2026-10-22,
version `0.2.17`); cited files are byte-identical to tag `v0.2.17` (`6d6c4e7`).
Sub2API remains an external HTTP provider; nothing inside it changes.

## Implementation notes (PR #505)

The shipped implementation intentionally differs from some early wording in this
document. These notes are authoritative for the delivered contract.

- Routes live under `/api/v1/user/owner-accounts/...`, not the earlier
  `/api/v1/user/accounts/...`; the read-only admin route is
  `/api/v1/admin/owner-operations`. Exact request and response shapes are in
  issue #502.
- The configuration switch is `relay.owner_account_mutations_enabled`, read from
  the prefixed environment variable `AE_RELAY_OWNER_ACCOUNT_MUTATIONS_ENABLED`
  (default false). The five `deploy/docker-compose*.yml` examples pass it
  through explicitly.
- Failure identifiers are kebab-case `details.error_code` values such as
  `account-not-found`, `reset-freshness-required`, `mutation-in-flight` and
  `refresh-cooldown`; §13's snake_case names are the early design spelling. The
  closed set and HTTP statuses live in
  `backend/internal/owneraccount/errors.go`.
- `POST /api/v1/user/owner-accounts/usage` accepts up to 100 account ids per
  request and chunks upstream reads at 20 ids. Partial per-account failures are
  returned in `errors`; one failed chunk does not fail the batch.
- The owner-safe directory projection has no referral program-name source, so
  `referral.program_name` is always null in v1.
- The projection does not populate `health.reauth_required`, `health.banned` or
  `health.forbidden`; they are always false in v1. Only `health.schedulable` and
  `health.rate_limit_reset_at` carry upstream values. This is a known v1
  limitation, not an upstream claim.
- The refresh cooldown is 30 seconds per account and is shared by usage, quota
  and referral refresh. A usage refresh checks the cooldown but does not start
  it, because no usage operation row is written; only quota and referral
  refreshes write terminal rows and restart the window.
- The earlier §11.2 in-process "20 seconds per account / 30 refresh units per
  5 minutes / at most 2 usage chunks in flight" limits are not implemented. The
  enforced controls are the shared 30-second cooldown, the 100-id request bound
  with 20-id upstream chunks, DB operation rows and the rolling daily caps;
  Sub2API remains the upstream backstop.

## 1. Problem

Colleagues register personal OpenAI/ChatGPT accounts into the company Sub2API
pool. The account then serves relay groups and appears in AI Efficiency only as
pool load. The account's owner cannot see account-level usage, reset credits,
Codex credits, referral eligibility, or health, and cannot consume a banked
reset credit or send a referral invitation without Sub2API admin access, which
operators must not distribute.

AI Efficiency has no account-owner mapping today. Revision 2 proposed a local
custody registry; revision 3 replaces it with email matching, which is enough
for the actual users: the Sub2API account name and OAuth credential email are
normally the colleague's company email.

## 2. Decision

### 2.1 Accepted shape

1. **Ownership is derived, not stored.** An OpenAI OAuth account belongs to the
   local user whose `users.email` equals the account's normalized
   `credentials.email` **or** its email-shaped `name`; either field matching is
   sufficient. No new ownership table, no admin registration, transfer, revoke,
   or restore.
2. New user tab 「我的账号」 at `/usage/accounts`. It lists the caller's accounts
   on the primary Relay provider, with account usage, banked reset credits,
   Codex credits, referral eligibility, and upstream health facts.
3. New read capabilities through `relay.Provider`: account directory (cached),
   usage windows, quota refresh, referral refresh.
4. New gated mutations: consume one banked reset credit (「使用重置次数」) and
   send a Codex referral invitation (「发送邀请」). Both are asynchronous,
   audited, non-retried, gated by a global operator switch plus a fresh
   observation precondition, and capped per day.
5. No new admin page. One read-only admin operations endpoint supports incident
   review of `unknown` outcomes.

### 2.2 Not in this change

- No ownership registry, admin ownership UI, transfer/revoke/restore, custody
  events, candidate picker, provider-instance pin, or suggested owner.
- No account creation, OAuth re-authentication, credential rotation, proxy
  change, or group membership change.
- No subscription quota reset. Existing subscription-window reset/approval is a
  different upstream object; the console action is named 「使用重置次数」.
- No shadow (Spark) accounts in v1. They cannot reset/invite and their DTO
  exposes parent-account identity, so they are excluded from matching.
- No self-service claim and no multi-custodian/co-custody.
- No platform-computed cost estimate (「预计总费用」 is excluded; §9.6).
- No exposure of credentials, tokens, `notes`, proxies, raw `extra`, or parent
  account identity.

## 3. Relationship to existing contracts

| Contract | Relationship |
| --- | --- |
| `2026-08-19-personal-usage-reset-and-oauth-pool-design.md` | Explicit amendment recorded here. That spec freezes account identifiers out of usage payloads except the pool tooltip. This surface adds a narrower exception for the caller's own email-matched accounts, and additionally discloses account-level cost (`cost`/`user_cost`) and the account's group names. Those two disclosures are deliberate product decisions (§10, open question). |
| `2026-08-19-relay-group-mapping-contract.md` | Orthogonal. Group↔account relationships stay mapping-owned and read-only here; email matching grants no group edit or account move. |
| `2026-10-14-relay-ops-monitoring-design.md` | Orthogonal. Ops Monitoring stays admin-only and aggregate; the console is owner-scoped and per-account. Its "upstream facts, no platform-derived scores" rule applies to health facts, and its no-recompute rule excludes 「预计总费用」. |
| Existing subscription quota-reset approval workflow | Not reused. The console mutation consumes a banked ChatGPT/Codex reset credit on the account, not a subscription window, and never enters the approval queue. |
| `2026-08-25-list-and-pagination-consistency-design.md` | Applies. The owner list is a full-page indexed collection with URL-owned `page/page_size`, 20/50/100, exact total. |
| `2026-07-14-end-to-end-page-loading-performance-design.md` | Applies. All upstream work is explicit, cached, bounded, and independently budgeted; the shared page-load budget is not extended. |
| `docs/architecture.md` | This is a design, so current architecture text is unchanged now. Implementation updates it for the new module and for the `relayruntime.Manager` account-directory cache and long-running client (§7, §18). |
| Relay Provider Runtime ownership rule | Applies. Handlers and services must not own a second provider client or metadata cache. The account-directory cache and the long-running client are added to `relayruntime.Manager`. |

## 4. Vocabulary

- **Owner Account**: an upstream OpenAI OAuth account whose normalized
  `credentials.email` **or** email-shaped `name` equals the current user's
  email, not shadow, on the primary provider. Ownership is derived on every
  request; it is not stored.
- **Account Usage Window**: one upstream 5h or 7d account window with
  utilization, reset time, requests, tokens, `cost` (A $), `standard_cost`, and
  `user_cost` (U $).
- **Reset Credit (次数)**: a banked ChatGPT/Codex rate-limit reset grant consumed
  by 「使用重置次数」. Distinct from subscription resets and from Codex Credits.
- **Codex Credits (点数)**: the spendable Codex balance reported by the same
  quota read. Rendered as the upstream decimal string.
- **Referral Eligibility (可邀请)**: upstream Codex referral program state.
- **Account Health Facts (账号健康度)**: flat upstream runtime/probe facts; never
  a platform score.
- **Owner Operation**: one asynchronous, audited, non-retried owner mutation
  (`reset_credit`, `send_invitation`), or one audited refresh
  (`refresh_quota`, `refresh_referral`). It is the only local state this feature
  adds.

## 5. Ownership mapping (email only)

### 5.1 Rule

1. The caller is authenticated by the existing session; the server never accepts
   a `user_id` from the client.
2. Resolve the primary Relay provider, load its OpenAI account directory through
   `relayruntime.Manager` (§7), and select accounts whose **normalized email**
   equals the caller's normalized email.
3. Normalization: trim, lower-case. Compare the whole string; no aliases, plus
   addressing, display names, or fuzzy matching.
4. Both email fields are checked; a match on **either** field is sufficient:
   - `credentials.email` (the OpenAI OAuth account email, exposed by the admin
     account DTO after credential redaction);
   - `name`, when it is email-shaped (contains exactly one `@` with non-empty
     local and domain parts).

   There is no precedence: if both fields are present and disagree, both are
   still compared against the caller's email. An account with neither field
   matchable is invisible to this feature.
5. Exclusions: `platform != "openai"`, `type != "oauth"`, shadow rows
   (`parent_account_id` present), and accounts on non-primary providers.
6. The caller's local user must exist and have `relay_disabled_at IS NULL`;
   otherwise the tab is hidden and all endpoints return `owner_inactive`.
7. A user may match multiple accounts. Since `users.email` is unique, one field
   matches at most one user, but the either-field rule can match two users when
   `credentials.email` and `name` disagree (§5.3).

### 5.2 Why email is acceptable

- `users.email` is admin/SSO-owned and unique; users cannot set it themselves.
- `credentials.email` comes from the OAuth identity and is stable, while `name`
  is admin-editable display text. Because either field matches, renaming an
  account to another person's email can also grant that person access; admins
  should keep account names equal to the owner's email. This is accepted for the
  convenience of accounts whose name is the only email-shaped field.
- The feature exposes only data the account already belongs to; there is no
  cross-user read path.
- Limitations accepted in v1: no alias/multi-email support; a personal-email
  account (e.g. a Gmail address) does not match unless the local user email is
  that same address; email changes in the directory change ownership
  immediately; a departed user's accounts become invisible to end users and
  remain admin-managed in Sub2API.

### 5.3 Edge cases

- Neither `credentials.email` nor an email-shaped `name` exists: hidden, never
  guessed.
- `credentials.email` and `name` disagree and each matches a different local
  user: both users see the account, because either-match is the rule. This is an
  accepted consequence; admins should not rename an account to a different
  person's email.
- Two upstream accounts with the same email (e.g. re-registered): both listed;
  the owner sees both.
- Duplicate `@` or IDN domains: not normalized beyond trim/lower-case; no match.
- If the primary provider changes, the derived set changes on the next cache
  refresh; no migration is required.

## 6. Verified upstream contract (Sub2API v0.2.17)

### 6.1 Provenance

- Routes: `backend/internal/server/routes/admin.go:380-466`.
- Account DTO/mapper: `backend/internal/handler/dto/types.go:211-330`,
  `mappers.go:235-415`; credential redaction keeps `email` and removes sensitive
  keys (`dto/credentials_redact.go:11-29`; list test asserts
  `credentials["email"]`).
- Usage: `backend/internal/service/account_usage_service.go:130-253,348-830`.
- Quota/reset: `backend/internal/service/openai_quota_service.go:30,128,340-470`;
  auto reset `openai_quota_auto_reset.go:390-425`.
- Referral: `backend/internal/service/openai_referral_service.go:25-100,147-185`;
  handler `openai_referral_handler.go:45-101`.
- Compliance lock: `backend/internal/server/middleware/admin_compliance.go:10-42`.
- Envelope: `backend/internal/pkg/response/response.go:15-20` — responses are
  `{code,message,reason,data}`; the stable error code is in `reason`.
- Frontend parity: `frontend/src/components/account/AccountUsageCell.vue`,
  `OpenAIQuotaResetCell.vue:290-330` (expiry clamp), `OpenAIReferralCell.vue`.

Authentication is `x-api-key: <admin-api-key>` (already held by the provider) or
an admin JWT. Any admin route may return 423 `ADMIN_COMPLIANCE_ACK_REQUIRED`.

### 6.2 Endpoints used

| Purpose | Endpoint | Notes |
| --- | --- | --- |
| Account directory | `GET /api/v1/admin/accounts?platform=openai&page=&page_size=1000` | Existing adapter call; loops pages with `lite=false` (default). The list DTO contains runtime fields, `extra`, `credentials.email`, and embedded group objects. |
| Batch usage | `POST /api/v1/admin/accounts/usage/batch` | `{account_ids, force}` (non-empty required); response `{usage: {id: UsageInfo}, errors: {id: msg}}`; side-effecting read (§9.1). |
| Quota refresh | `POST /api/v1/admin/openai/accounts/:id/quota/refresh` | Persists `codex_credits_snapshot` / `codex_reset_credit_snapshot`. |
| Reset credit | `POST /api/v1/admin/openai/accounts/:id/reset-quota` | 20s upstream + 8s detached post-process; fresh `redeem_request_id` per call ⇒ not idempotent. |
| Referral refresh | `POST /api/v1/admin/openai/accounts/:id/referrals/refresh` | Persists `codex_referral_snapshot`. |
| Referral invite | `POST /api/v1/admin/openai/accounts/:id/referrals/invite` | `{email, program_id, confirmed}`; 20s send + detached refresh/persist; not idempotent. |

Not used: `GET /admin/openai/accounts/:id/quota` (notifies the auto-reset
watcher), `GET /admin/users/:id/usage` (zeros stub), local
`POST /admin/accounts/:id/reset-quota`, and subscription resets.

Latency facts: each upstream OpenAI call is bounded at 20s; post-reset
post-processing at 8s; post-invite refresh/persist ~8s + 3s. Worst case ≈28s
reset, ≈31s invite. The 90s value in Sub2API's browser client is a client
timeout, not an upstream bound. AI Efficiency's default request budget is 35s,
hence asynchronous mutations.

### 6.3 Field semantics

`UsageInfo`: `five_hour`/`seven_day` with `utilization`, `resets_at`,
`remaining_seconds`, `window_stats{requests,tokens,cost,standard_cost,user_cost}`;
health flags `is_forbidden`, `forbidden_reason`, `needs_reauth`, `needs_verify`,
`is_banned`, `error_code`, `error`. For OpenAI, `source` is omitted and
`updated_at` is the request time, so neither is used; freshness comes from
`extra.codex_usage_updated_at`.

Cost semantics: `cost` = total × account rate multiplier (A $),
`standard_cost` = no multiplier, `user_cost` = actual under group multipliers
(U $). A $ == U $ implies a 1.0 account multiplier.

`OpenAIQuotaUsage`: `plan_type`, `fetched_at` (unix seconds),
`rate_limit.primary_window/secondary_window`, `rate_limit_reset_credits
{available_count, credits[{expires_at}]}` (expired grants must be filtered and
count clamped), `credits{has_credits, unlimited, balance *string}`. `email` is
returned but only used for matching; never projected beyond the owner's own
address.

Reset result: `code`, `credit`, `windows_reset` (**int count**, not a list),
post-reset `quota`, `warning_code`. "Non-refundable" is a product assumption:
upstream exposes a consumed credit and `redeemed_at`, no refund endpoint.

Invite result: `{email, sent, eligibility, cache_persisted, refresh_failed}`.
Any `sent != true` or HTTP 502 is an **unknown** outcome, not a definitive
failure.

Display snapshots read from account `extra` (adapter allowlist, exactly):
`codex_credits_snapshot`, `codex_reset_credit_snapshot`,
`codex_referral_snapshot`, `codex_usage_updated_at` (RFC3339),
`auto_reset_credit_enabled` (bool). Sub2API's `extra` is a denylist, not a
whitelist; it also carries `codex_fingerprint_seed` and arbitrary keys, and the
same DTO carries `notes`, proxy ids, and partially redacted credentials. The
adapter must project only these five keys and must never pass a decoded
whole-account object to user paths.

### 6.4 Restrictions

- Reset needs an unexpired banked credit; Sub2API's auto-reset may consume one
  between page load and click (`auto_reset_credit_enabled`), so a fresh quota
  read is required before accepting reset.
- Shadow accounts cannot reset and must invite from the parent; v1 excludes them.
- Invites need a valid email, the current `program_id`
  (`codex_referral_workspace` | `codex_referral_consumer`, derived from
  credentials), and consent; Sub2API re-checks and may return
  `OPENAI_REFERRAL_PROGRAM_CHANGED` / `OPENAI_REFERRAL_UNAVAILABLE`.
- Timeout/5xx on reset or invite ⇒ unknown outcome.
- Admin routes may return 423 after a compliance-version bump.
- 「预计总费用」 is a Sub2API frontend computation (`cost*100/utilization`),
  not an upstream field; excluded (§9.6).

## 7. Relay provider boundary

### 7.1 Capabilities

Extend the optional-capability pattern in `backend/internal/relay/provider.go`:

```go
// AccountDirectoryReader returns the provider's accounts with owner-safe fields
// and the five allowlisted extra snapshots. It is cached, not called per item.
type AccountDirectoryReader interface {
    ReadAccountDirectory(ctx context.Context, platform string) ([]relay.AccountDirectoryEntry, error)
}

// AccountUsageWindowReader reuses the existing batch-usage decoder.
type AccountUsageWindowReader interface {
    ReadAccountUsageWindows(ctx context.Context, accountIDs []int64, force bool) (map[int64]relay.AccountUsageWindows, map[int64]string, error)
}

type OpenAIAccountQuotaManager interface {
    RefreshOpenAIAccountQuota(ctx context.Context, accountID int64) (*relay.OpenAIAccountQuota, error)
    ResetOpenAIAccountQuota(ctx context.Context, accountID int64) (*relay.OpenAIAccountQuotaResetResult, error)
}

type OpenAIAccountReferralManager interface {
    RefreshOpenAIAccountReferral(ctx context.Context, accountID int64) (*relay.OpenAIAccountReferralEligibility, error)
    SendOpenAIAccountReferralInvite(ctx context.Context, accountID int64, req relay.OpenAIAccountReferralInviteRequest) (*relay.OpenAIAccountReferralInviteResult, error)
}
```

`AccountDirectoryEntry` is an owner-safe projection, not the raw DTO: `ID`,
`Name`, `Email`, `Platform`, `Type`, `PlanType`, `Status`, `Schedulable`,
`ErrorMessage`, `RateLimitedAt`, `RateLimitResetAt`, `OverloadUntil`,
`TempUnschedulableUntil/Reason`, `ExpiresAt`, `AutoPauseOnExpired`, `IsShadow`,
`GroupNames`, `DisplaySnapshots`, `ObservedAt`. It never contains
`parent_*`, `notes`, proxies, credentials beyond the matching email, or
non-allowlisted `extra`.

`GroupNames` comes from the DTO's embedded groups (`dto/types.go:329` and the
`AccountGroup.Group` relation at `:429-437`); the current adapter decodes only
group IDs (`sub2api.go:2372-2389`) and must be extended. Missing names are
omitted, never rendered as numeric IDs.

`relayruntime.Manager` owns:

- the **account-directory cache**, keyed by `(provider_id,
  configuration_version, platform)`, 5-minute TTL, single-flight, stale-serve on
  error, 10s load timeout. It is invalidated after any successful refresh or
  mutation;
- the **long-running client** (60s overall / 55s response header) and its
  provider factory, cached under the same provider key, used only by the
  operation worker.

Read-class calls use the existing shared client (30s/15s). No handler or service
constructs a second provider client.

### 7.2 Error mapping

Map transport/JSON failures into the provider's existing error categories, read
the stable code from the envelope's `reason`, keep upstream `reason` strings for
audit/logs, and map to the §13 reasons. Specifically: 423 →
`provider_compliance_ack_required`; 404/405 on a new endpoint →
`provider_capability_unsupported`; invite 502/`sent != true` → unknown; never
forward upstream bodies to the browser.

### 7.3 Timeouts

- Directory load 10s (shared client); per-account usage batch 20s; quota refresh
  25s; referral refresh 25s. All stay below the 35s request budget.
- Reset/invite: dedicated client 60s overall / 55s header; worker deadline 65s;
  heartbeat every 5s.

## 8. Platform API

Standard `{code,message,reason,data}` envelope; stable identifiers are `reason`
values (§13). User routes are authenticated; admin route requires admin.

### 8.1 Reads

- `GET /api/v1/user/accounts/scope` → `{ "total": n }` — count of the caller's
  matched accounts from the cached directory. Used only to decide tab
  visibility; no PII.
- `GET /api/v1/user/accounts?page=&page_size=` → `{items,page,page_size,total}`
  over the caller's matched accounts. Item:

```json
{
  "account_id": 123,
  "name": "alice@example.com",
  "platform": "openai",
  "plan_type": "pro",
  "group_names": ["Group Alpha", "Group Beta"],
  "status": "active",
  "schedulable": true,
  "health": {
    "error_message": "",
    "rate_limited_at": null,
    "rate_limit_reset_at": null,
    "overload_until": null,
    "temp_unschedulable_until": null,
    "temp_unschedulable_reason": "",
    "expires_at": null,
    "auto_reset_credit_enabled": false,
    "observed_at": "2026-10-23T10:00:00+08:00"
  },
  "quota": {
    "available_reset_credits": 2,
    "reset_credit_expirations": ["2026-10-30T08:00:00+08:00"],
    "credits": {"has_credits": true, "unlimited": false, "balance": "12.50"},
    "fetched_at": "2026-10-23T09:58:00+08:00"
  },
  "referral": {
    "should_show": true,
    "available_invites": 10,
    "program_id": "codex_referral_workspace",
    "title": "Invite a teammate",
    "requires_explicit_confirmation": true,
    "fetched_at": "2026-10-23T09:58:00+08:00"
  },
  "actions": {"refresh": true, "reset_credit": true, "invite": true, "disabled_reason": null},
  "active_operation": null
}
```

`quota`/`referral` are `null` when no display snapshot exists ("未查询"), never
fabricated zeros. `available_reset_credits` and expirations are expiry-clamped at
read time; if the raw count is positive but no grant is unexpired, the count is
`null` with `actions.disabled_reason = "reset_credits_expired"`. `usage` is
loaded separately. `account_id` is the upstream account id; it is not a secret
and the owner has already seen it, but every route re-verifies the email match
server-side and returns 404 otherwise. `active_operation` is the running
`reset_credit`/`send_invitation` for this account, if any.

- `POST /api/v1/user/accounts/usage` body `{ "account_ids": [123, 124] }` →
  `{"123": {"usage": {...}, "error": null}, ...}`; max 20 ids per request, max 2
  in flight; non-matched and nonexistent ids both yield `account_not_found`.
- `POST /api/v1/user/accounts/:account_id/refresh/usage|quota|referral` — one
  upstream call each. Quota/referral refreshes are audited; usage refresh is
  not. A successful quota/referral refresh is the freshness precondition for the
  corresponding mutation.

### 8.2 Mutations (async)

- `POST /api/v1/user/accounts/:account_id/reset-credit`
  `{ "confirm": true, "reason": "" }`
- `POST /api/v1/user/accounts/:account_id/invitations`
  `{ "email": "bob@example.org", "program_id": "codex_referral_workspace", "consent": true }`

Response `202`: `{ "operation_id": 71, "status": "running", "action":
"reset_credit", "account_id": 123 }`. At most one mutation in flight per account
across both actions; conflict returns `account_mutation_in_flight` with
`operation_id`.

- `GET /api/v1/user/accounts/operations/:operation_id` → operation detail,
  readable only by the account's matched user; otherwise 404.
- `GET /api/v1/user/accounts/:account_id/operations?page=&page_size=` — paginated
  local operation history for the matched user (20/50/100).

Statuses: `running`, `succeeded`, `failed` (definitive upstream rejection, safe
to retry manually), `unknown` (timeout/5xx after possible execution, or invite
502/`sent != true`; never auto-retried), and local-gate errors are returned
directly (no audit row). A `running` row whose heartbeat is older than 30s is
marked `unknown` in the transaction that next starts a mutation for that account
and lazily on read.

### 8.3 Admin

- `GET /api/v1/admin/owner-operations?provider_id=&account_id=&action=&status=&page=&page_size=`
  — read-only operation list for support/incident review. No admin mutations and
  no `unknown` override.

## 9. Capabilities

### 9.1 Usage windows (查询)

- Initial load uses `force=false`; explicit 「查询」 uses `force=true`.
- Batch usage is a **side-effecting read**: Sub2API probes and persists
  `extra.codex_*` when a window is missing, the account is rate-limited, or its
  snapshot is stale past ~10 minutes (`account_usage_service.go:719-827`). The
  console accepts this, never forces it on page load, and does not treat it as
  freshness authority.
- Render 5h/7d utilization, reset time + countdown, requests, tokens, `A $`
  (`cost`), `U $` (`user_cost`); `standard_cost` as tooltip only.
- Freshness: `extra.codex_usage_updated_at` ("上游快照写入时间"), else "时间未知".
  Do not use `UsageInfo.updated_at`.
- Missing window → "未提供", never 0%; utilization >100% is shown as reported;
  helper text states the windows are account-level, not user/subscription-level.

### 9.2 Reset credits (次数) and Codex credits (点数)

- 次数 = `available_count` after dropping expired grants and clamping; raw >0
  with no unexpired grant → "未知（额度已过期）", not 0 or positive.
- 点数 = upstream `balance` string; `unlimited` → 「无限」; `has_credits=false`
  → 「无可用点数」 (deliberate deviation from Sub2API's "0"); absent → 「未查询」.
- Reset is enabled server-side only when: global switch on, owner active,
  account matchable/non-shadow, a quota observation (refresh row or completed
  reset result) newer than the last non-rejected reset attempt and no older than
  15 minutes, and that observation reports ≥1 unexpired credit.
- Failure explains the missing condition and offers the quota refresh.

### 9.3 Consuming a reset credit (使用重置次数)

- Irreversible and non-refundable in this design; human-triggered only.
- Confirm dialog: account name, current 5h/7d utilization, observation time,
  credits and expiries, and the auto-reset caveat; requires an explicit
  acknowledgement; server requires `confirm: true`.
- Gate order: owner active → email match → global switch → fresh quota with an
  unexpired credit, created after the last non-rejected reset attempt (including
  any `unknown` reset) → daily cap (5 non-rejected attempts per account per
  rolling 24h, DB-counted) → single-flight.
- Execution per §11.4; `202`; success shows `windows_reset` (int), warning code,
  and post-reset credits.
- On unknown: persistent banner 「操作结果未知，可能已消耗 1 次重置额度；请先
  查询，再决定是否重试」; server blocks until a fresh observation exists.

### 9.4 Referral eligibility and invites (可邀请/发送邀请)

- Page load reads `codex_referral_snapshot`; 「查询」 refreshes eligibility.
- Show available invites, remaining capacity, program
  (`codex_referral_workspace` = 同事邀请, `codex_referral_consumer` = 个人邀请),
  title/rules, snapshot time.
- Invite dialog: one valid email (client+server validated), `program_id` from a
  fresh eligibility, consent acknowledgement when
  `requires_explicit_confirmation` is true **or null**, and a note that the
  invite is sent under the account's identity. No bulk invite.
- Gate: owner active → email match → global switch → eligibility observation
  (refresh row or completed invite result) newer than the last non-rejected
  invite attempt and no older than 10 minutes → `should_show` and remaining send
  capacity > 0 → daily cap (10 non-rejected attempts per account per 24h) →
  single-flight. Sub2API re-checks eligibility/program/consent server-side.
- Any 502/`sent != true` or timeout → unknown; warn that the email may have been
  sent and block until a fresh eligibility observation.

### 9.5 Health facts (账号健康度)

Flat fact table: `status`, `schedulable`, `error_message` (truncated),
`rate_limited_at`, `rate_limit_reset_at`, `overload_until`,
`temp_unschedulable_until/reason`, `expires_at`, `auto_reset_credit_enabled`;
usage-probe `needs_reauth`, `needs_verify`, `is_banned`, `is_forbidden`,
`forbidden_reason`, `error_code`, `error`, plus snapshot time. Missing facts are
omitted, never synthesized. No score, ratio, average, or ranking.

### 9.6 Display rules

- Upstream values verbatim; compact numbers allowed (`57.8M`); no invented,
  extrapolated, or estimated values. 「预计总费用」 is excluded in v1 (a
  Sub2API frontend computation; the platform does not recompute metrics).
- No health score, risk badge, or cross-account ranking.
- `A $`/`U $` and group names are included deliberately (§3, §10).

## 10. Authorization and privacy

1. Every route resolves the account from the cached directory, verifies the
   normalized email match against the authenticated user, and returns
   `404 account_not_found` on any mismatch. There is no admin override on user
   routes.
2. Owner access also requires the local user to exist and have
   `relay_disabled_at IS NULL`; otherwise `403 owner_inactive` (direct) and the
   tab/list hide the accounts.
3. The owner sees only their own account(s): name, plan, group names,
   usage/cost, quota, referral, health, and operation history. Never credentials,
   tokens, `notes`, proxies, raw `extra`, `codex_fingerprint_seed`, parent
   identity, other owners' accounts, pool member emails, or group member
   identities.
4. Privacy amendment (see §3): the caller's own email-matched account may appear
   in this console; account-level cost and group names are disclosed to that
   user. This is an explicit reviewed decision, not an incidental widening.
5. Logs/metrics never contain tokens, balances, invite emails, or account names.
   Operation rows store the invite email for accountability only; it is never
   logged.
6. The admin key never reaches the browser; the user supplies only an upstream
   account id for their own account.
7. Read paths are bounded and rate-limited; write paths require the global
   switch, a fresh observation, single-flight, a daily cap, and an audit row.

## 11. Safety, audit, and idempotency

### 11.1 Local state

One new table, `owner_account_operations`:

| Field | Notes |
| --- | --- |
| `id`, `provider_id`, `account_id`, `actor_user_id` | account id is upstream; actor is the matched user |
| `action` | `refresh_quota`, `refresh_referral`, `reset_credit`, `send_invitation` |
| `status` | `running`, `succeeded`, `failed`, `unknown`; refreshes are terminal-only |
| `reason` | optional user note for mutations; never logged |
| `invite_email` | only for `send_invitation`; never logged |
| `upstream_code`, `warning_code`, `result` | sanitized terminal data; no credentials |
| `heartbeat_at`, `started_at`, `finished_at`, `created_at`, `updated_at` | DB time |

Indexes: `(provider_id, account_id, created_at)`, `(actor_user_id, created_at)`,
`(status, created_at)`, and a partial unique index on
`(provider_id, account_id)` where `status = 'running'` and action is a mutation.
All staleness/freshness/cap comparisons use DB time.

### 11.2 Gates and limits

- Mutation requests require `confirm: true` (reset) or `consent: true` (invite).
- DB single-flight for mutations; **no** in-memory mutation lock.
- Rolling 24h caps (5 reset, 10 invite) count non-rejected attempts; local gate
  rejections return errors without rows.
- Read-refresh limits are in-process and approximate under multiple replicas:
  20s per account, 30 refresh units per user per 5 minutes (one 「查询」 spends
  three), at most 2 usage chunks in flight, no "refresh all". Sub2API's own
  limits are the backstop; irreversible mutations never rely on these.
- A per-process semaphore of 2 long mutations is a local valve; DB single-flight
  is authoritative.

### 11.3 Unknown-outcome blocking

- Reset requires a quota observation created after the last `unknown` reset;
  otherwise `account_reset_refresh_required`.
- Invite requires a referral observation created after the last `unknown` invite;
  otherwise `account_invite_refresh_required`.
- No automatic observation and no admin override; the owner must refresh.

### 11.4 Execution contract (no retries)

- Handler inserts the operation row (`running`, DB-now heartbeat), commits,
  starts a goroutine, returns `202`. The goroutine uses a detached context
  (`context.WithoutCancel`/`Background` + 65s deadline), never `gin.Context`.
- Worker resolves the long-running provider via `relayruntime.Manager`, performs
  exactly one upstream call, renews heartbeat every 5s, and writes the terminal
  state under an independent 5s context. Terminal-write failures retry a bounded
  number of times; the upstream call is never repeated.
- Panic → `unknown`. Startup marks `running` rows with heartbeat older than 30s
  as `unknown`; a row that cannot be finalized becomes `unknown` after heartbeat
  expiry on the next read/mutation. A running row on another replica keeps a
  fresh heartbeat and is untouched.
- No retry middleware, background queue, or cron repair for mutations. Sub2API
  generates a fresh `redeem_request_id` per manual reset, so an HTTP retry would
  be a second spend.

## 12. Caching and performance

- One provider-directory load per 5 minutes per `(provider, version, platform)`,
  single-flight, stale-serve, 10s timeout, in `relayruntime.Manager`. The owner
  list, scope, and email matching are pure local filters over it; a page load
  issues no per-account upstream detail call.
- After a successful quota/referral refresh or mutation, the directory cache
  entry is invalidated so the next load shows fresh snapshots. Other replicas
  converge within the TTL; mutation gates read DB rows, not the cache.
- Usage windows load in chunks of ≤20 accounts, ≤2 in flight, 20s deadline,
  partial per-account results.
- Page load issues only the cached directory read + usage batch (chunks); it
  never refreshes quota/referral, never sets `force=true`, and never runs
  "refresh all".
- Redis is optional; no authorization or mutation gate depends on it.

## 13. Failure semantics

- `account_not_found` (404): no email match or unknown account id.
- `owner_inactive` (403): user deleted or `relay_disabled_at` set.
- `provider_capability_unsupported` (503): provider build/route mismatch.
- `provider_compliance_ack_required` (503): Sub2API 423; operator must
  acknowledge compliance.
- `upstream_unavailable` (502/503): read failure, user-retryable.
- `account_actions_disabled` (403): global switch off.
- `reset_requires_fresh_quota` (409).
- `reset_credits_expired` (409).
- `invite_requires_fresh_eligibility` (409).
- `account_reset_refresh_required` / `account_invite_refresh_required` (409).
- `account_mutation_in_flight` (409, with `operation_id`).
- `refresh_cooldown` (429, with `retry_after_seconds`).
- `referral_program_changed` (409).
- `referral_unavailable` (409).
- `invalid_email` (422); `consent_required` (422).
- `busy` (429); `mutation_rate_limited` (429).
- `unknown_upstream_outcome` (operation status `unknown`; see §11.3).

## 14. Observability

Structured logs: `action`, `provider_id`, `account_id`, `actor_user_id`,
`status`, `upstream_code`, `duration_ms`; never tokens, balances, invite emails,
or account names. Metrics: refresh latency/outcome by section, mutation outcomes
and durations, cache hit/miss, owner-console latency; no per-user labels.
Alert on any `unknown` mutation (potential real spend or duplicate email).

## 15. Testing

- Adapter: directory projection allowlist (assert `codex_fingerprint_seed`,
  credentials, notes, proxy, `parent_*` absent), email extraction, group-name
  decode, batch per-account errors, quota/referral projection, expiry clamp,
  unix-seconds→RFC3339, 423/404 mapping, invite-502→unknown.
- Service: email matching normalization and edge cases (§5.3) including
  either-field match and the disagreeing-fields case, 404 for non-matching, `owner_inactive`, shadow exclusion, global-switch gate, fresh
  precondition, unknown blocking, caps, single-flight, stale heartbeat recovery,
  startup sweep, panic→unknown, terminal-write retry without upstream re-call,
  admin operations read-only.
- Frontend: empty/loading/partial-failure/cooldown/unknown states, tab
  visibility, pagination URL state, reset confirm and precondition messaging,
  invite consent, operation history drawer, responsive checks, i18n union/count,
  build budget.
- Staging only: reset/invite on a disposable test account; 423 drill; read-only
  production smoke.

## 16. Rollout

- **Phase A — read-only.** Directory mapping, tab, usage/quota/referral reads,
  health, operation history. Mutations hidden and global switch off.
- **Phase B — gated mutations.** Global `relay.owner_account_mutations_enabled`
  (env `RELAY_OWNER_ACCOUNT_MUTATIONS_ENABLED`, default false); staging test on a
  disposable account; unknown alerts before first production action; runbook for
  423 and refresh-then-retry recovery.
- No migration: only `owner_account_operations` is new; rollback turns off the
  switch and hides the tab (no matching accounts → hidden). Upstream state is
  unaffected apart from deliberately performed mutations.

## 17. Open questions

1. **Email matching.** Is exact `credentials.email`/`name` matching enough, or is
   an alias table needed for personal-email accounts? Current answer: no alias
   table in v1.
2. **Cost and group names.** Keep the A $/U $ and group-name disclosure (current,
   for screenshot parity) or restrict to utilization/requests/tokens/reset facts?
3. **Async vs synchronous.** Reset/invite stay async because of the 20s+8s
   upstream path and irreversibility. Acceptable, or should mutations move to a
   separate later release?
4. **Per-account switch.** Revision 3 keeps only the global switch. Is a
   per-account/per-user allowlist needed before production?
5. **Admin visibility.** One read-only operations endpoint, no admin page. Enough
   for support?
6. **「预计总费用」.** Excluded in v1; add later only with a reviewed platform
   estimate.

## 18. Pending implementation

_Delivered by PR #505; retained as the original implementation checklist and
where the implementation notes above supersede it._

- One Ent schema/migration (`owner_account_operations`) with the partial unique
  index and heartbeat.
- `relay` capabilities, owner-safe directory projection with the five-key extra
  allowlist, email/group-name decode, `relayruntime.Manager` directory cache and
  long-running client/factory, and `docs/architecture.md` update.
- New service (email matching, reads, operation worker, gates, caps) and user
  handlers/routes; one read-only admin operations handler.
- Frontend tab/view, API module, dialogs, operation drawer, feature locale with
  the i18n test union/count update.
- Config plumbing for `relay.owner_account_mutations_enabled` and its
  `deploy/config.example.yaml` / `.env.example` entries, default off.
- Re-verify every §6 endpoint/field/status against the deployed Sub2API version
  at implementation; fail closed on drift; record any change here before coding.
- Tests from §15 and the staging runbook (disposable account, 423 drill).
