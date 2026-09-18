# Domain Docs

This repository uses a single-context domain documentation layout.

These rules supplement the source-of-truth order in `AGENTS.md`; they do not
replace `docs/architecture.md` or the current domain-specific contracts.

## Before Exploring

Read these when they exist and are relevant:

- Root `CONTEXT.md`
- Relevant ADRs under `docs/adr/`

If they do not exist, proceed silently. Domain-modeling skills create them
lazily when terminology or architectural decisions are resolved.

## Layout

```text
/
|- CONTEXT.md
|- docs/adr/
|- backend/
|- frontend/
`- ae-cli/
```

## Vocabulary

Use terms defined in `CONTEXT.md` consistently in issues, proposals, tests,
and implementation. A missing term may indicate either incorrect wording or
a domain-modeling gap.

## ADR Conflicts

Surface conflicts with existing ADRs explicitly rather than silently
overriding them.
