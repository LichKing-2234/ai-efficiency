# Superpowers Reference and Cutover Audit

**Date:** 2026-08-27

**Research ticket:** [research(docs): audit inbound references and cutover constraints](https://github.com/LichKing-2234/ai-efficiency/issues/409)

**Parent map:** [wayfinder(docs): migrate repository knowledge beyond Superpowers](https://github.com/LichKing-2234/ai-efficiency/issues/407)

**Hosted baseline:** [`022670ac`](https://github.com/LichKing-2234/ai-efficiency/commit/022670ac02624e814c434b3657c244150a6eecec)

## Scope

This audit answers which current repository and tracker surfaces depend on
`docs/superpowers`, and which compatibility constraints a later migration must
satisfy. It does not classify the 163 documents, choose their destinations,
perform the migration, or edit current governance. Corpus disposition belongs
to [the parallel corpus audit](https://github.com/LichKing-2234/ai-efficiency/issues/408).

All tracked-file measurements use a clean worktree created from freshly fetched
hosted `origin/main` at `022670ac`. Tracker evidence was read live on 2026-08-27.

## Executive findings

1. The old tree is an active governance dependency, not an isolated archive.
   Six tracked files outside it contain 77 literal `docs/superpowers`
   references. `AGENTS.md`, `CLAUDE.md`, both READMEs, and
   `docs/architecture.md` tell people and agents to treat selected files there
   as current contracts or execution ledgers.
2. Moving retained documents changes more than top-level navigation. The old
   tree contains 387 literal self-path references across 90 files and 245
   Markdown links to `.md` files across 62 files. Four relative links are
   already broken, while 28 Markdown links use checkout-specific `/Users/...`
   paths.
3. Hosted work can recreate the removed path after cutover. Open PRs
   [#47](https://github.com/LichKing-2234/ai-efficiency/pull/47) and
   [#86](https://github.com/LichKing-2234/ai-efficiency/pull/86) add five files
   under `docs/superpowers`. Open architecture tickets also carry semantic
   obligations to update the "current" contract/spec even when they do not
   spell out its path.
4. GitHub commit-SHA blob links remain valid at their original path; branch
   links do not provide a durable path alias. Removing the old tree therefore
   cannot transparently redirect unknown external `/blob/main/docs/superpowers`
   links. A compatibility stub would preserve a branch URL only by keeping an
   old-path file in HEAD, which conflicts with a destination that requires the
   directory to be absent.
5. The detached primary checkout is both 15 commits behind `origin/main` and
   dirty in the exact governance/spec surfaces affected by migration. A later
   implementation must not bulk-move or delete from that checkout.

## Tracked repository inventory

### Corpus size and internal references

At the hosted baseline, `git ls-files 'docs/superpowers/**'` returns 163 files:
101 plans and 62 specs. The directory views are available at the commit-pinned
[plans tree](https://github.com/LichKing-2234/ai-efficiency/tree/022670ac02624e814c434b3657c244150a6eecec/docs/superpowers/plans)
and [specs tree](https://github.com/LichKing-2234/ai-efficiency/tree/022670ac02624e814c434b3657c244150a6eecec/docs/superpowers/specs).

Focused scans found:

| Reference class | Count | Consequence |
| --- | ---: | --- |
| Literal `docs/superpowers` occurrences inside the tree | 387 occurrences in 90 files | Commands, status notes, source pointers, and prose cannot all be treated as equivalent links. |
| Markdown `.md` links inside the tree | 245 occurrences in 62 files | Retained documents need link-resolution validation after their final destination is known. |
| Relative links whose target stays inside the old tree | 171 | Directory-depth and spec/plan separation matter during a move. |
| Relative links whose target is outside the old tree | 40 | Moving the source file changes the required relative path even when the target does not move. |
| Checkout-specific `/Users/...` Markdown links | 28 | These are not portable GitHub links and should not be propagated into retained current documents. |
| Repository-root-style `/docs/...` Markdown links | 6 | These are not relative to the repository directory when rendered as ordinary web links. |

The relative-link check found four already-broken links at the baseline:

- `docs/superpowers/plans/2026-04-13-ae-cli-user-install.md` ->
  `../ae-cli/README.md`
- `docs/superpowers/plans/2026-06-10-independent-cli-release.md` ->
  `../ae-cli/README.md`
- `docs/superpowers/plans/2026-06-16-ai-usage-center-group-quota.md` ->
  `./2026-06-16-ai-usage-center-group-quota-design.md`
- `docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md` ->
  `./2026-06-22-configurable-directory-sync-design.md`

These pre-existing failures mean a post-move zero-broken-link check must use a
declared baseline or repair the retained documents; it cannot honestly claim
that every current link was green before migration.

### Inbound references from outside the tree

`git grep -n -I -F 'docs/superpowers' -- ':!docs/superpowers/**'` found six
files and 77 occurrences:

| File | Occurrences | Current role |
| --- | ---: | --- |
| [`AGENTS.md`](https://github.com/LichKing-2234/ai-efficiency/blob/022670ac02624e814c434b3657c244150a6eecec/AGENTS.md#L11-L57) | 37 | Source-of-truth order, required pre-reading, documentation update rules, plan-ledger rules, and important-file navigation. |
| [`CLAUDE.md`](https://github.com/LichKing-2234/ai-efficiency/blob/022670ac02624e814c434b3657c244150a6eecec/CLAUDE.md#L3-L15) | 9 | Lightweight agent navigation naming specs, plans, current contracts, historical context, and a draft. |
| [`README.md`](https://github.com/LichKing-2234/ai-efficiency/blob/022670ac02624e814c434b3657c244150a6eecec/README.md#L39-L55) | 2 | Public key-document navigation and source ordering. |
| [`README.zh-CN.md`](https://github.com/LichKing-2234/ai-efficiency/blob/022670ac02624e814c434b3657c244150a6eecec/README.zh-CN.md#L39-L55) | 2 | Chinese public navigation and source ordering. |
| [`docs/architecture.md`](https://github.com/LichKing-2234/ai-efficiency/blob/022670ac02624e814c434b3657c244150a6eecec/docs/architecture.md#L1-L40) | 26 | Project-level precedence, current-contract index, one rendered relative contract link, and contract-level update instructions. |
| [`docs/ui-review/company-wide-usability-hardening-review.html`](https://github.com/LichKing-2234/ai-efficiency/blob/022670ac02624e814c434b3657c244150a6eecec/docs/ui-review/company-wide-usability-hardening-review.html#L639) | 1 | Historical review annotation naming its corresponding spec. |

Only one of those 77 occurrences is a Markdown link into the old tree: the
[active Codex v2 contract link](https://github.com/LichKing-2234/ai-efficiency/blob/022670ac02624e814c434b3657c244150a6eecec/docs/architecture.md#L806-L807).
The remaining occurrences are still operationally important because agents
consume inline-code paths as navigation and rules.

Concrete implication: current navigation and precedence cannot be migrated file
by file. `AGENTS.md`, `CLAUDE.md`, both READMEs, and `docs/architecture.md` must
switch to the approved neutral source model in the same logical cutover that
makes old current-contract paths unavailable. The historical HTML annotation
may preserve the old name if its disposition is historical, but it must not be
mistaken for current navigation.

## Hosted tracker dependencies

### Open Issues

A live GitHub Issue search for exact `docs/superpowers` text in open bodies and
comments returned four Issues, all in this migration map:

- [migrate repository knowledge beyond Superpowers](https://github.com/LichKing-2234/ai-efficiency/issues/407)
- [classify the Superpowers document corpus against live state](https://github.com/LichKing-2234/ai-efficiency/issues/408)
- [audit inbound references and cutover constraints](https://github.com/LichKing-2234/ai-efficiency/issues/409)
- [lock the cutover, compatibility, and verification contract](https://github.com/LichKing-2234/ai-efficiency/issues/412)

The same exact-text search limited to open comments returned zero results. These
four bodies describe the migration subject and remain valid planning history;
rewriting them is not required for repository path correctness and would
contradict the parent map's explicit rule against rewriting historical Issue/PR
content.

Three other open architecture tickets create semantic, rather than literal,
dependencies:

- [concentrate Access Group onboarding workflow](https://github.com/LichKing-2234/ai-efficiency/issues/386)
  requires `docs/architecture.md` and the "current onboarding contract" to be
  updated.
- [group Provider capabilities by workflow](https://github.com/LichKing-2234/ai-efficiency/issues/389)
  requires `docs/architecture.md` and relevant current Relay specs to be
  updated.
- [finish claim-to-pool conservation ownership](https://github.com/LichKing-2234/ai-efficiency/issues/390)
  requires `docs/architecture.md` and the active attribution v2 spec to be
  updated.

Before any of these tickets is implemented after cutover, its handoff must point
to the neutral current-contract owner. Updating or commenting on the still-open
ticket is sufficient; closed historical Issues and PRs should keep their
point-in-time paths.

### Open pull requests

The repository has two open PRs, and both add documents under the old path:

| PR | Old-tree additions |
| --- | --- |
| [#47: persist auth sessions in Redis](https://github.com/LichKing-2234/ai-efficiency/pull/47) | `docs/superpowers/plans/2026-05-25-redis-backed-auth-session.md`; `docs/superpowers/specs/2026-05-25-redis-backed-auth-session-design.md` |
| [#86: frontend next-gen](https://github.com/LichKing-2234/ai-efficiency/pull/86) | `docs/superpowers/plans/2026-06-09-frontend-ng-mainline-shadcn-i18n-alignment.md`; `docs/superpowers/plans/2026-06-09-frontend-ng-next-gen-design-system.md`; `docs/superpowers/specs/2026-06-05-frontend-ng-tanstack-start-migration-design.md` |

GitHub's PR file API reports all five as `added`, not modifications of the 163
tracked baseline files. Therefore the migration inventory is not complete if it
only maps hosted `main`: these PRs must be rebased and their documentation
disposition changed to the approved neutral workflow, or they must be closed,
before the old directory can be considered protected from reintroduction.

## GitHub blob URL behavior

GitHub's official
[permanent-link documentation](https://docs.github.com/en/repositories/working-with-files/using-files/getting-permanent-links-to-files)
distinguishes two URL contracts:

- `/blob/<branch>/<path>` shows the file at the current branch head and can
  change as commits are added.
- `/blob/<commit-sha>/<path>` permanently identifies that exact file version.

A repository-local deletion check confirms the practical boundary. The removed
`docs/ae-cli/session-pr-attribution.md` is still available at
[its pre-deletion commit and original path](https://github.com/LichKing-2234/ai-efficiency/blob/a7abbfefaf2de9529abd4630ce40274e803e888d/docs/ae-cli/session-pr-attribution.md),
while the corresponding
[main-branch path](https://github.com/LichKing-2234/ai-efficiency/blob/main/docs/ae-cli/session-pr-attribution.md)
is absent. The GitHub Contents API returned present for the former and absent
for the latter on 2026-08-27.

No commit-pinned `docs/superpowers` blob/raw URL was found in tracked files or
open Issue/PR bodies/comments. That does not prove external consumers do not
exist. The safe compatibility statement is:

- old SHA permalinks remain valid and should not be rewritten;
- old branch-path links may stop resolving after removal;
- Git history or a migration index can explain the new location but cannot
  redirect an HTTP request for the old branch path;
- keeping redirect/stub files under `docs/superpowers` is a policy choice that
  prevents the directory from being absent, not a transparent GitHub feature.

## Dirty-checkout overlap

The primary checkout was observed at detached `63af340`, 15 commits behind
fresh `origin/main`, with these relevant changes:

- modified: `AGENTS.md`, `CLAUDE.md`, and
  `docs/superpowers/specs/2026-08-19-relay-group-mapping-contract.md`;
- untracked: `CONTEXT.md`, `docs/adr/`, `docs/research/`, two
  `docs/superpowers/plans/` files, and three latest
  `docs/superpowers/specs/` files;
- all six checked governance/spec file contents differ from the corresponding
  `origin/main` blobs, including the three untracked specs that are now tracked
  on hosted main.

This is direct overlap, not unrelated dirt. The later migration must start from
a fresh hosted-main worktree and preserve the detached checkout untouched. It
must also reconcile ownership before merge: the untracked `CONTEXT.md` and ADR
are not yet hosted sources merely because the parent map names those document
roles, and the locally edited contract/navigation text must not be silently
discarded or overwritten by a bulk rename.

## Cutover constraints for the decision ticket

A later cutover contract should require all of the following:

1. Freeze a reviewed old-path-to-disposition manifest for the 163 hosted files,
   then separately account for the five old-path additions in open PRs and the
   dirty checkout's two untracked plans.
2. Move or create approved current contracts before changing precedence text,
   but merge the destination files and all current navigation rewrites as one
   logical cutover with no hosted-main interval containing dangling required
   pre-reading paths.
3. Update `AGENTS.md`, `CLAUDE.md`, both READMEs, and
   `docs/architecture.md` together. Replace the local-plan ledger rule rather
   than merely changing `docs/superpowers/plans` to another directory name.
4. Re-resolve Markdown links from each retained file's final directory. Check
   repository-relative links, cross-contract/history links, anchors, images,
   and machine-absolute paths; account explicitly for the four baseline broken
   links.
5. Coordinate or close PRs #47 and #86 before declaring the old tree removed,
   and update open tickets #386, #389, and #390 with neutral contract pointers
   before their implementation begins.
6. Preserve old commit-SHA links as historical evidence. Document that unknown
   `/blob/main/docs/superpowers/...` links cannot be guaranteed after removal;
   do not claim GitHub supplies redirects.
7. Perform implementation in a clean worktree based on freshly fetched hosted
   main. Do not use, reset, clean, move from, or overwrite the detached dirty
   checkout.
8. Gate deletion on focused evidence: zero required current references to old
   paths outside an explicitly approved historical/index allowlist; every
   manifest row resolved; retained relative links valid; open-PR collision
   check repeated live; `git diff --check` clean; and reviewer confirmation
   that source-of-truth ordering describes the neutral workflow rather than a
   renamed local planning system.

## Reproduction commands

```bash
git fetch origin main
git rev-parse origin/main
git ls-files 'docs/superpowers/**'
git grep -n -I -F 'docs/superpowers' -- ':!docs/superpowers/**'
git grep -n -I -F 'docs/superpowers' -- 'docs/superpowers/**'
rg -n -g '*.md' '\]\([^)]*\.md(?:#[^)]*)?\)' docs/superpowers
gh issue list --repo LichKing-2234/ai-efficiency --state open --limit 200
gh pr list --repo LichKing-2234/ai-efficiency --state open --limit 100
gh api repos/LichKing-2234/ai-efficiency/pulls/47/files --paginate
gh api repos/LichKing-2234/ai-efficiency/pulls/86/files --paginate
```

## Limitations

- This audit is a path/reference audit, not a lifecycle classification; it does
  not decide which individual document is a contract, history, active work, or
  removable duplication.
- GitHub search can prove matches it returns, not the absence of links on the
  wider public internet or in private downstream repositories.
- The dirty-checkout inventory is a point-in-time, read-only observation. Its
  owner may continue editing it, so a migration implementation must re-read it
  immediately before integration.
- No dependency installs or product test suites were run because the ticket is
  documentation-only. Validation is limited to Git cleanliness, focused
  reference/link scans, `git diff --check`, and live GitHub readback.
