# Authentication and OAuth Contract

This contract describes the current authentication, local identity, JWT, and
OAuth behavior. Read it before changing login source selection, LDAP-to-Relay
identity repair, browser session expiry, CLI login, token issuance, or either
OAuth grant. Current code remains the observed fact; a behavior change updates
this contract in the same delivery.

Relay provider selection, Access Groups, personal API keys, Connection Tests,
and administrator subscription jobs are defined in
[`relay-user-access.md`](./relay-user-access.md).

## Credential Boundaries

| Credential | Owner | Persistence and use |
| --- | --- | --- |
| LDAP login password | The authenticating user | Used only for the LDAP user bind. It is not stored locally and is not sent to Relay. |
| LDAP service bind password | Deployment configuration or administrator-managed database settings | Encrypted at rest when stored by the admin API, returned masked, preserved on masked updates, and hot-reloaded with the rest of the LDAP configuration. |
| OIDC client secret | Deployment configuration or administrator-managed database settings | Encrypted at rest when stored by the admin API, returned masked, preserved on masked updates, and rebuilt into the runtime OIDC client on save. |
| Relay SSO password | The authenticating Relay user | Stored locally only after a successful upstream Relay login, encrypted with the platform encryption key, for later user-JWT Relay writes. |
| Generated Relay password | AI Efficiency | Generated for Relay identities provisioned or repaired by AI Efficiency, stored encrypted, and never derived from the LDAP password. |
| Access and refresh JWTs | AI Efficiency | Issued by the same token service for browser login, PKCE, and device authorization. |
| CLI token file | `ae-cli` | Stored atomically with mode `0600`; contains the token pair, expiry, login server, and stable auth subject. |

Admin Relay credentials are not user credentials. They may resolve and manage
Relay identities, but a user-scoped operation that requires an upstream user
JWT uses a stored or generated Relay user password.

## Admin-Managed Login Configuration

Administrators can edit browser OIDC and LDAP settings at runtime through the
administrator-only `GET/PUT/DELETE /api/v1/admin/settings/oidc` and
`GET/PUT /api/v1/admin/settings/ldap` endpoints. Values are stored in
`SystemSetting` rows (`auth.oidc` and `auth.ldap`) with secrets encrypted by the
platform encryption key. A stored row overrides the deployment configuration
(`auth.oidc.*` / `auth.ldap.*` environment or file values); deleting the OIDC
row restores the deployment values.

Saving OIDC settings validates the input, performs provider discovery, stores
the encrypted secret, and replaces the runtime OIDC client without a restart.
Other replicas reload the stored settings within about ten seconds (the
refreshed read is throttled and its failures keep the running client).
The secret is always returned masked; an empty secret or the mask keeps the
current value. The auth options response exposes the configured OIDC button
label as `oidc_label`. A failed discovery rejects the save and leaves the
running configuration unchanged.

## Login Sources

### Browser OIDC

When `auth.oidc.enabled` is true, the browser uses the standard OIDC
authorization-code flow with PKCE. The application discovers the configured
issuer, validates the ID Token signature, issuer, audience, expiry and nonce,
then confirms that UserInfo has the same `sub`. The callback is a dedicated
browser route; API, CLI, Connector, WebSocket and webhook routes keep their
existing authentication.

The application stores the external binding as `(issuer, subject)`. An
unbound identity may be linked to an existing local email only when the
deployment explicitly enables `allow_email_link` and the provider marks the
email verified. Otherwise the user must be pre-provisioned by subject. A
successful new identity receives the default local `user` role; the provider
cannot grant local administrator access. The OIDC client secret and issuer
configuration remain server side: they come from deployment configuration or
the administrator-managed database settings described above.

The two pieces of transient state that join the browser redirect back to the
application are stored in the shared auth state store, not in process memory.
`/auth/oidc/start` stores the pending state (`state`, nonce, PKCE verifier,
requested redirect and callback) under a five-minute TTL; the callback consumes
it atomically, so exactly one process completes a given authorization. The
one-use completion handoff then holds only the resolved local identity and the
requested redirect for two minutes; the token pair is issued when the browser
consumes the handoff, so no credential is written to the state store. Any
replica may serve the start, callback, or completion request. A state-store
failure makes the start and callback paths fail closed with an unavailable
response; ordinary Bearer-authenticated API requests do not depend on it.

A browser callback that cannot complete redirects to `/login` with an
`oidc=failed`, `oidc=conflict`, or `oidc=unavailable` reason so the login page
can ask the user to sign in again instead of leaving a JSON error response on
screen.

When a verified email matches an existing local account whose stored
`(issuer, subject)` binding belongs to a different issuer, that binding is
stale environment state (for example a database restored from another
environment or a replaced OIDC client). The deployment verifies and trusts
only its currently configured issuer, so the sign-in replaces the stale
binding with the current issuer and subject and continues. A binding under the
current issuer with a different subject is a real identity conflict: it is
never rebound, and the browser shows the account-conflict guidance so the user
can retry another sign-in path or ask an administrator.

A successful OIDC sign-in also reconciles the local user's Relay identity. When
the local user has no Relay binding, AI Efficiency resolves an exact Relay email
match, then the canonical or legacy username, and otherwise provisions a Relay
user with a high-entropy generated password and the default concurrency, then
stores the binding and the encrypted password locally. Relay users created this
way carry the `provisioned_by_ai_efficiency_oidc` note. This preserves the
Relay provisioning that LDAP sign-in already provides, so an OIDC-only account
can create personal Relay API keys without an upstream password. OIDC
sign-in never replaces an existing Relay binding, never rotates credentials for
an already-bound user, and never copies a Relay role into the local account. A
failed Relay resolution fails the sign-in attempt; the next attempt retries.

The auth options response exposes whether LDAP and the debug-only Dev Login are
available. Relay SSO is the ordinary non-LDAP login source when its provider is
registered.

- An implicit login tries registered LDAP first and then the remaining
  providers, including Relay SSO.
- An explicit source tries only that provider and rejects an unknown source.
- Dev Login is outside the ordinary provider chain. It is available only when
  the server runs in debug mode and the explicit enablement environment flag is
  true.
- Browser login stores the returned access and refresh tokens as one browser
  session generation, then resolves the current user before treating identity
  or role as confirmed.

Invalid credentials, an upstream extra-verification requirement, or a Relay
authentication transport error does not provision a Relay user during SSO.
Relay SSO authenticates existing upstream users only.

### Back-Channel Logout

When OIDC is enabled the server exposes `POST /auth/oidc/backchannel-logout`,
which is the endpoint registered with the identity provider. The endpoint
accepts an `application/x-www-form-urlencoded` body with a single
`logout_token` field, verifies the JWT signature against the provider JWKS, and
checks `iss`, `aud`, the
`http://schemas.openid.net/event/backchannel-logout` event, a recent `iat`, a
non-empty `jti` with replay rejection, absence of `nonce`, and a non-empty
`sub`. The `jti` replay marker is a shared store entry with a ten-minute TTL, so
redelivery is rejected regardless of which replica receives it. A valid token
revokes the matching local account's access and refresh tokens through the same
`token_valid_after` boundary used by directory offboarding, so existing
sessions fail on their next request or refresh.

Unknown subjects still return `200` so provider delivery is idempotent.
Invalid tokens return `400`, a disabled OIDC client returns `404`, and a state
store failure returns `503` so provider delivery can retry a transient backend
problem. The endpoint never redirects and requires no browser session.

## Local User Reconciliation

After one auth provider succeeds, AI Efficiency reconciles the upstream
identity into one local user before issuing tokens.

1. Reuse an existing local username match.
2. Otherwise reuse a non-empty exact email match.
3. Create a local user only when neither identity exists.
4. On every successful login, synchronize the current auth source and a valid
   upstream `admin` or `user` role.

LDAP login always re-resolves the Relay identity so historical local bindings
can be repaired. OIDC sign-in resolves the Relay identity only when the local
user has no binding; an existing binding is never replaced, so later OIDC
sign-ins make no Relay identity call. Resolution prefers an exact Relay email
match, then a canonical username, then a legacy full username. A missing Relay
identity is provisioned with a high-entropy generated password and the
configured default concurrency. An existing AI Efficiency-provisioned Relay
user (created by LDAP or OIDC sign-in) with no group facts may receive
idempotent default-subscription repair.

When LDAP reuses a local user that previously authenticated through Relay SSO,
the local auth source becomes `ldap` while any previously stored Relay password
is preserved. A Relay binding move may rotate a generated password only for an
AI Efficiency-provisioned Relay user. The LDAP login password is cleared
before any local persistence or Relay identity operation.

An OIDC sign-in that provisions a Relay identity also stores the generated
password encrypted, so the same user can later create or regenerate personal
Relay API keys without an upstream password. When OIDC links an existing local
account, it does not change the local username, local role, or Relay binding.

If LDAP omits email, AI Efficiency derives a deterministic non-empty fallback
from the login identity. Local identity repair must not create a duplicate row
when a stable username changes but the email still identifies the same person.

## JWT and Browser Session

Access and refresh tokens are HMAC-signed JWTs with configured lifetimes. Both
carry the local user ID and issuance time; access tokens also carry username
and role. The refresh endpoint accepts the refresh token in JSON and returns a
new pair after rereading the local user.

Each protected request validates the access token type and expiry, rereads the
local user's token-valid-after boundary, and rejects tokens issued before that
boundary. Refresh performs the same revocation check. Directory offboarding or
another authorized revocation therefore invalidates both existing access and
refresh tokens without maintaining a token denylist.

The browser owns one generation-aware session:

- Login replaces the token pair and starts a new generation.
- A refresh may rotate tokens only if the captured generation is still current.
- Logout or terminal expiry clears both tokens, advances the generation, and
  resets session-owned application resources.
- Concurrent current-user loads collapse within one generation; an older
  response cannot restore identity after a newer login, logout, or expiry.
- A protected route may render only after the still-current identity/role
  result permits it. Administrator routes fail closed until the role is known.

## Browser PKCE Login

`ae-cli` is the only registered OAuth public client. It has no client secret.
Browser login uses Authorization Code Flow with PKCE S256 and a random state.

The callback URI is accepted only when URL parsing proves all of these facts:

- Scheme is `http`.
- Host is exactly `localhost` or `127.0.0.1`.
- Port is present and numeric.
- Path is exactly `/callback`.

The server validates the same callback and public-client identity before
authorization approval and token exchange. An approved authorization code is
bound to the client, callback URI, user, and PKCE challenge. It is short-lived
shared state with a five-minute TTL and is consumed atomically on the first
exchange attempt, so any backend process may redeem it and a replay cannot
observe it twice. A mismatched client, callback, verifier, expired code, or
reused code returns `invalid_grant`.

The CLI opens a random localhost listener, verifies the returned state, and
exchanges the code through the OAuth token endpoint. The ordinary login waits
for at most three minutes. It prints the authorization URL even when automatic
browser launch fails.

## Device Authorization

`ae-cli login --device` uses the same public client and token issuer without a
localhost callback. Ordinary `ae-cli login` remains PKCE by default. On Linux
with neither `DISPLAY` nor `WAYLAND_DISPLAY`, ordinary login fails immediately
with a direction to use device authorization.

The device-code endpoint returns a high-entropy device code, a human user code,
the browser verification URI, a 15-minute expiry, and a five-second polling
interval. The browser must have a valid AI Efficiency access token before it
can approve or deny the normalized user code. The browser never receives the
device code.

Device state lives in the same shared store. It has a fifteen-minute TTL; the
poll, approval, denial, and consumption transitions are atomic, so any backend
process can poll or approve a device authorization another process issued. Its
lifecycle is:

```text
pending -> approved -> consumed
       `-> denied
any unconsumed state -> expired
```

- `authorization_pending` keeps the CLI polling at the current interval.
- `slow_down` increases the CLI interval by five seconds.
- `access_denied` and `expired_token` stop the flow.
- A successful token exchange consumes the device code exactly once.
- Unknown, mismatched, reused, or otherwise invalid device codes return
  `invalid_grant`.

Successful PKCE and device grants both write the same CLI token shape. CLI
commands refresh an expiring access token through the JSON refresh endpoint and
atomically replace the token file. If refresh fails while the old access token
is still valid, that token may continue for the current command; otherwise the
user must log in again. Logout removes the token file.

## OAuth Browser Delivery

The browser entry routes for PKCE and device authorization reuse the standalone
AuthShell experience. In the embedded deployment, the backend serves the
bundled SPA representation directly for both GET and HEAD. A deployment
without the embedded frontend redirects to the configured frontend route.

This path-based decision prevents proxy host or scheme rewriting from creating
a same-route redirect loop. Approval and device verification remain protected
API operations even though the browser entry pages themselves are public.

## Current Limitations

- Interactive login completion requires the shared auth state store. Flows
  whose state entries expire return `invalid_grant` or an expired handoff, and
  a Redis outage fails start/completion with an unavailable response. Existing
  Bearer sessions, token validation, and refresh use only JWT and PostgreSQL
  facts and keep working.
- OAuth has one public client and no scope model. Both grants receive the same
  current CLI access.
- OAuth does not provide a CLI username/password grant or a device
  `verification_uri_complete` shortcut.
- Relay extra-verification challenges are not proxied through SSO.

These limitations describe current behavior. They are not open work unless a
current GitHub spec or ticket owns a replacement.
