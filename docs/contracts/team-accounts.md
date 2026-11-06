# Department Team Account Contract

This contract describes department team accounts: Relay users owned by a
directory department instead of a natural person. Read it before changing team
account provisioning, ownership transfer, key issuance, credential reveal, or
the administrator console.

Directory department facts come from
[`directory-sync.md`](./directory-sync.md). Representative identity comes from
`representativescope`; Relay access rules come from
[`relay-user-access.md`](./relay-user-access.md).

## Ownership Model

A team account is owned by a department, identified by the directory stable
department id. People never own the account: they are recorded in
`team_account_managers` with `role=owner|manager`.

- Exactly one row per account is `owner`.
- The owner may add and remove managers, transfer ownership, and manage keys.
- Managers may manage keys and reveal credentials, but may not change owners or
  the manager list.
- Administrators may perform every operation, including taking over a handover
  the previous owner never completed.
- Handover is manual. Directory sync never mutates ownership; a representative
  who left without transferring leaves the account usable and claimable by an
  administrator.

Organization accounts are stored in `team_accounts` as the canonical record. An
SSO sign-in as the account creates a hidden local user (`auth_source=relay_sso`)
that only binds browser sessions; it is excluded from the administrator user
list, people pickers, and directory offboarding candidates, so it is never
treated as a person or a leaver.

`Transfer` replaces the owner atomically and removes the previous owner's
manager row in the same transaction, so a completed handover immediately
revokes the old owner's access.

## Provisioning

Creating an account for a department:

1. Requires an administrator, or a representative of that department. A
   representative may not create an account for a department they do not
   represent, and the department is never accepted from an untrusted source.
2. Derives the Relay identity deterministically from the department id:
   username `team-<normalized-department-id>`, mailbox
   `<username>@invalid.account`. The domain is the organization-account domain
   so every synthetic identity stays out of the corporate namespace.
4. Generates the Relay password server-side and stores only its ciphertext in
   `team_accounts.relay_auth_password`. The plaintext exists only inside the
   provisioning call and the explicit credential-reveal response.
5. Creates the Relay user exactly like a personal account: no allow-list is
   pinned and the Relay assigns its default subscriptions. Optional additional
   groups are granted on top; a non-administrative caller may only grant groups
   they are subscribed to themselves.
6. Records the account, the owner row, and an `account_created` event in one
   local transaction. If any step fails, the Relay user is disabled so no orphan
   identity can authenticate.

Creating a second account for the same department is rejected regardless of the
existing account's status. A username or mailbox collision fails the create; an
existing identity is never adopted. There is no delete operation: a suspended
account keeps its department and Relay identity reserved until an administrator
removes the data out of band.

## Keys

Relay offers no administrator endpoint for minting another user's API key, so
the platform authenticates as the team account with its stored password and
calls the user-scoped key endpoint. A key is always bound to a group the account
holds an active subscription for, and the plaintext secret is returned exactly
once.

Revocation calls the Relay revoke endpoint and marks the local row. Only a
`secret_preview` is persisted; the console never receives a stored full secret.

## Credentials

Owner, manager, and administrator may retrieve the Relay username and decrypted
password. Every reveal appends `credentials_revealed` to the audit trail. The
password is decrypted in place and never cached.

## Usage

Usage is read from Relay and aggregated for the account itself, by day and by
key. It is attributed to the team account's Relay user, so it never appears in
any person's personal usage.

Inside the AI usage center the organization account appears in the department
member details as a `team_account` subject: it carries the account name, the
department path, and the Relay usage, participates in the usage trend totals,
and can be opened like a member dashboard. It is not a directory member and is
never included in member counts of the directory itself.

## Audit

`team_account_events` is append-only. Payloads never contain plaintext
credentials or full key secrets. Owners and managers read events for their own
accounts; administrators read every account.

## Administration

`/api/v1/admin/team-accounts` exposes listing, creation, status changes, the
credential and key operations, and transfer. The representative opens accounts inside the AI usage center (there is no
separate organization-account navigation); the account card shows ownership,
credentials, and managers only, while keys, access groups, usage, and the audit
record stay in the administrator console. The admin console page follows the
directory offboarding interaction: search, card list, typed confirmation for
transfer and suspend, and an audit drawer.

## Boundaries

- One team account per department.
- No automatic ownership transfer from directory sync.
- No parent-department read-only view; access is granted by adding a manager.
- No per-department spending cap; Relay group limits still apply per
  subscription.
