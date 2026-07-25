# Issue Tracker: GitHub

Issues and PRDs live in GitHub Issues for `LichKing-2234/ai-efficiency`.
Use the `gh` CLI from this repository clone.

## Conventions

- Create: `gh issue create --title "..." --body-file <file>`
- Read: `gh issue view <number> --comments`
- List: `gh issue list --state open --json number,title,body,labels,comments`
- Comment: `gh issue comment <number> --body "..."`
- Label: `gh issue edit <number> --add-label "..."` or `--remove-label "..."`
- Close: `gh issue close <number> --comment "..."`

Infer the repository from `git remote -v`.

## Pull Requests As A Triage Surface

**PRs as a request surface: no.**

GitHub shares one number space across issues and pull requests. Resolve an
ambiguous `#42` with `gh pr view 42`, falling back to `gh issue view 42`.

## Skill Operations

- When a skill says "publish to the issue tracker", create a GitHub issue.
- When a skill says "fetch the relevant ticket", run
  `gh issue view <number> --comments`.

## Wayfinding

A wayfinding map is one issue labelled `wayfinder:map`.

- Child tickets use GitHub sub-issues when available.
- Otherwise, link children through a task list and `Part of #<map>`.
- Child labels use `wayfinder:<type>`.
- Use native GitHub issue dependencies for blockers when available.
- Claim work with `gh issue edit <number> --add-assignee @me`.
- Resolve by commenting with the answer, closing the child, and updating the
  map's Decisions-so-far section.
