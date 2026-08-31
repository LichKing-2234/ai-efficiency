# Platform Origin

> Historical record. This file explains the platform's original boundary and
> vocabulary; it is not a current architecture or product contract.

The original merged design is preserved at
[`3cd9b1cf`](https://github.com/LichKing-2234/ai-efficiency/blob/3cd9b1cfe45e4d0dbe0a7bc929bcb1ab390eb08a/docs/superpowers/specs/2026-03-17-ai-efficiency-platform-design.md).
That source contains the complete early schema, endpoint, roadmap, and delivery
status. This record retains only context that materially explains later design.

## Original Problem

AI Efficiency began as a system beside `sub2api`, intended to measure and
improve AI-assisted software delivery without making the model gateway own SCM,
repository analysis, PR policy, or engineering-effectiveness workflows.

The initial product joined four kinds of evidence:

- SCM repositories, pull requests, webhooks, labels, and review state;
- AI usage and routing facts supplied through Relay/sub2api;
- local developer/CLI context intended to connect AI work to code changes;
- platform-owned analysis, gating, aggregation, and presentation.

The early vocabulary included Repository, PR, Session, Scan, Gating, Usage, and
Efficiency Metric. Some names survived; others were replaced as their authority
and precision became clearer.

## Lasting Boundary Decisions

Several origin choices became durable architecture principles:

- **Standalone product:** AI Efficiency owns its lifecycle, database, domain
  rules, and user experience rather than becoming a `sub2api` feature module.
- **Modular monolith:** one backend process contains explicit domain modules;
  module interfaces carry boundaries without premature service splitting.
- **Provider seams:** Relay and SCM integrations sit behind provider interfaces,
  allowing multiple upstream/SCM implementations without spreading their HTTP
  details through business modules.
- **API integration:** upstream usage and identity are consumed through provider
  APIs. Direct coupling to the `sub2api` database was considered early but did
  not become the product boundary.
- **SCM-centered outcomes:** repositories, commits, PRs, webhooks, and policy are
  platform facts. Gateway usage alone cannot answer where committed work went.
- **Separate local evidence:** developer-machine evidence may explain code/usage
  relationships, but it is collected and validated by AI Efficiency rather than
  changing `sub2api` request semantics.

These choices explain why current code still concentrates integration under
`relay.Provider` and `scm.SCMProvider`, keeps a modular backend, and treats
frontend/backend as one platform release unit.

## Early Hypotheses That Changed

| Early hypothesis | Later outcome |
| --- | --- |
| An explicit `ae-cli` Session could be the primary user workflow and precise PR-Token isolation boundary. | Session lifecycle became too coupled to developer behavior and only provided interval attribution. Current committed-code accounting uses deterministic commit proof and formal pools. |
| One session-scoped Relay API key per start could make usage exact. | Key churn, bootstrap failure modes, and commit-causality limits outweighed the isolation benefit. Provider identity still matters, but a platform Session is no longer the accounting subject. |
| Repository analysis, automated optimization, gating, rankings, and broad efficiency metrics could advance as one phase roadmap. | Current scope evolved through independently tracked product contracts. An unchecked historical phase is not open authority. |
| Frontend delivery might remain a separate implementation choice. | The frontend is now compiled and embedded into the backend platform release. |
| Direct `sub2api` database reads could simplify usage aggregation. | The product standardized on Relay/provider HTTP boundaries to preserve ownership and independent deployment. |

The important lesson is not that the initial design was wrong. It established
the independent-system, modularity, SCM, and provider vocabulary needed to test
the more specific hypotheses. Later contracts narrowed those hypotheses when
real identity, attribution, loading, and operational evidence became available.

## Current Successors

Current behavior is owned by neutral contracts and code, including:

- [authentication and OAuth](../contracts/auth-and-oauth.md)
- [Relay user access](../contracts/relay-user-access.md)
- [repository binding](../contracts/repository-binding.md)
- [attribution v2](../contracts/attribution-v2.md)
- [platform loading and serving](../contracts/platform-loading.md)
- [release units](../contracts/release-units.md)

This history does not revive the original Session model, direct database
integration, phase roadmap, proposed endpoints, or unimplemented product
surfaces.
