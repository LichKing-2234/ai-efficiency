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
| LDAP service bind password | Deployment configuration | Returned masked by the admin API, preserved on masked updates, and hot-reloaded with the rest of the LDAP configuration. |
| Relay SSO password | The authenticating Relay user | Stored locally only after a successful upstream Relay login, encrypted with the platform encryption key, for later user-JWT Relay writes. |
| Generated Relay password | AI Efficiency | Generated for Relay identities provisioned or repaired by AI Efficiency, stored encrypted, and never derived from the LDAP password. |
| Access and refresh JWTs | AI Efficiency | Issued by the same token service for browser login, PKCE, and device authorization. |
| CLI token file | `ae-cli` | Stored atomically with mode `0600`; contains the token pair, expiry, login server, and stable auth subject. |

Admin Relay credentials are not user credentials. They may resolve and manage
Relay identities, but a user-scoped operation that requires an upstream user
JWT uses a stored or generated Relay user password.

## Login Sources

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

## Local User Reconciliation

After one auth provider succeeds, AI Efficiency reconciles the upstream
identity into one local user before issuing tokens.

1. Reuse an existing local username match.
2. Otherwise reuse a non-empty exact email match.
3. Create a local user only when neither identity exists.
4. On every successful login, synchronize the current auth source and a valid
   upstream `admin` or `user` role.

LDAP login always re-resolves the Relay identity so historical local bindings
can be repaired. Resolution prefers an exact Relay email match, then a
canonical username, then a legacy full username. A missing Relay identity is
provisioned with a high-entropy generated password and the configured default
concurrency. An existing AI Efficiency LDAP-provisioned Relay user with no
group facts may receive idempotent default-subscription repair.

When LDAP reuses a local user that previously authenticated through Relay SSO,
the local auth source becomes `ldap` while any previously stored Relay password
is preserved. A Relay binding move may rotate a generated password only for an
AI Efficiency LDAP-provisioned Relay user. The LDAP login password is cleared
before any local persistence or Relay identity operation.

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
bound to the client, callback URI, user, and PKCE challenge. It is process
memory state, expires after five minutes, and is removed on the first exchange
attempt. A mismatched client, callback, verifier, expired code, or reused code
returns `invalid_grant`.

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

Device state is process memory with this lifecycle:

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

- Authorization codes and device entries are process-local and are lost on
  restart. A client that reaches another process or loses the issuing process
  must restart authorization.
- OAuth has one public client and no scope model. Both grants receive the same
  current CLI access.
- OAuth does not provide a CLI username/password grant or a device
  `verification_uri_complete` shortcut.
- Relay extra-verification challenges are not proxied through SSO.

These limitations describe current behavior. They are not open work unless a
current GitHub spec or ticket owns a replacement.
