# Current Contracts

Contracts describe stable current behavior below the authority of current code.
They contain no execution checklists or status narration. Unimplemented target
state, dependencies, and work status stay in GitHub Issues.

Read only the contracts directly relevant to a change:

| Contract | Read before changing |
| --- | --- |
| [Authentication and OAuth](./auth-and-oauth.md) | Authentication sources, OAuth login, device authorization, token issuance, or revocation |
| [Relay User Access](./relay-user-access.md) | Relay access, onboarding, credentials, protocol selection, subscriptions, or user disablement |
| [CLI Tool Configuration](./cli-tool-configuration.md) | `ae-cli discover`, managed tool configuration, or doctor checks |
| [Repository Binding](./repository-binding.md) | Remote resolution, repository auto-binding, SCM credentials, or hook eligibility |
| [Release Units](./release-units.md) | Platform or CLI release tags, artifacts, update discovery, or rollout boundaries |
| [Directory Sync](./directory-sync.md) | Directory source configuration, hierarchy, membership, representatives, or offboarding |
| [Usage and Quota](./usage-and-quota.md) | Personal or team Usage, range preferences, quotas, multipliers, or OAuth pools |
| [Quota Reset](./quota-reset.md) | Reset requests, approvals, queues, notifications, or Relay execution |
| [Platform Loading](./platform-loading.md) | Frontend loading, Redis read models, static serving, readiness, timeouts, or performance telemetry |
| [Team Usage Prewarm](./team-usage-prewarm.md) | Prewarm worker generation, manifests, Redis publication, backend reads, or fallback |
| [Attribution V2](./attribution-v2.md) | Checkpoints, hooks, collectors, claims, usage pools, Activity, or attribution readiness |
| [Relay Group Mapping](./relay-group-mapping.md) | Relay Planning, department-to-Group mapping, Replan, Accounts, member moves, or retries |
| [Collection Navigation](./collection-navigation.md) | Pagination, cursors, incremental branches, responsive collections, or navigation boundaries |
