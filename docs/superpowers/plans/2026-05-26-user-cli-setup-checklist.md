# User CLI Setup Checklist Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `/user`'s paste-based Verify checklist with a two-scope CLI setup guide for machine-level setup and per-repo readiness.

**Architecture:** This is a frontend-led UI and documentation change. Command string construction stays in `frontend/src/utils/userSetupReview.ts`; `UserView.vue` renders the two setup scopes; `docs/architecture.md` records the current project-level `/user` behavior. No backend or CLI command behavior changes.

**Tech Stack:** Vue 3 `<script setup lang="ts">`, Pinia, Vitest, Vue Test Utils, TailwindCSS, Markdown docs.

---

## File Structure

- Modify `frontend/src/utils/userSetupReview.ts`: keep command builders, remove paste-output review parser, add builders for hooks, repo init, doctor, sync, and upload status.
- Modify `frontend/src/types/index.ts`: remove `VerifyReviewItem` and `VerifyReviewSummary`.
- Modify `frontend/src/views/UserView.vue`: remove verify state and review UI, render `Machine Setup`, `Per-Repo Setup`, and `Manual backfill / recovery`.
- Modify `frontend/src/__tests__/user-setup-review.test.ts`: replace parser tests with command-builder tests.
- Modify `frontend/src/__tests__/user-view.test.ts`: assert the two-scope guide and absence of paste-based Verify UI.
- Modify `docs/architecture.md`: update `/user` surface and CLI runtime flow descriptions.

---

### Task 1: Command Builder Contract

**Files:**
- Modify: `frontend/src/utils/userSetupReview.ts`
- Modify: `frontend/src/types/index.ts`
- Test: `frontend/src/__tests__/user-setup-review.test.ts`

- [x] **Step 1: Write failing command-builder tests**

Replace `frontend/src/__tests__/user-setup-review.test.ts` with tests that import only command builders:

```ts
import { describe, expect, it } from 'vitest'
import {
  buildDeviceLoginCommand,
  buildDiscoverCommand,
  buildDoctorCommand,
  buildHooksGlobalCommand,
  buildHooksStatusUploadsCommand,
  buildInstallCommand,
  buildLoginCommand,
  buildRepoInitCommand,
  buildSyncCommand,
  buildWindowsInstallCommand,
} from '@/utils/userSetupReview'

describe('userSetupReview command builders', () => {
  it('buildDiscoverCommand uses the selected provider', () => {
    expect(buildDiscoverCommand('https://ae.example.com', 'sub2api-prod')).toBe(
      'ae-cli discover --provider sub2api-prod'
    )
  })

  it('buildInstallCommand passes AE_CLI_INSTALL_SERVER_URL to bash', () => {
    expect(buildInstallCommand('https://ae.example.com')).toBe(
      'curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | AE_CLI_INSTALL_SERVER_URL=https://ae.example.com bash'
    )
  })

  it('buildWindowsInstallCommand passes AE_CLI_INSTALL_SERVER_URL to PowerShell', () => {
    expect(buildWindowsInstallCommand('https://ae.example.com')).toBe(
      '$env:AE_CLI_INSTALL_SERVER_URL = "https://ae.example.com"; iwr -UseB https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.ps1 | iex'
    )
  })

  it('buildLoginCommand and buildDeviceLoginCommand use the installed server config', () => {
    expect(buildLoginCommand('https://ae.example.com')).toBe('ae-cli login')
    expect(buildDeviceLoginCommand('https://ae.example.com')).toBe('ae-cli login --device')
  })

  it('builds machine and repo setup commands', () => {
    expect(buildHooksGlobalCommand()).toBe('ae-cli hooks enable --global')
    expect(buildRepoInitCommand()).toBe('ae-cli init')
    expect(buildDoctorCommand()).toBe('ae-cli doctor')
    expect(buildSyncCommand()).toBe('ae-cli sync')
    expect(buildHooksStatusUploadsCommand()).toBe('ae-cli hooks status --uploads')
  })
})
```

- [x] **Step 2: Run command-builder test and confirm failure**

Run:

```bash
cd frontend && pnpm test src/__tests__/user-setup-review.test.ts
```

Expected result: the test fails because `buildDoctorCommand`, `buildHooksGlobalCommand`, `buildHooksStatusUploadsCommand`, `buildRepoInitCommand`, and `buildSyncCommand` are not exported yet.

- [x] **Step 3: Implement command builders and remove verify parser**

Update `frontend/src/utils/userSetupReview.ts` to export these functions:

```ts
export function buildInstallCommand(origin: string) {
  return `curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | AE_CLI_INSTALL_SERVER_URL=${origin} bash`
}

export function buildWindowsInstallCommand(origin: string) {
  return `$env:AE_CLI_INSTALL_SERVER_URL = "${origin}"; iwr -UseB https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.ps1 | iex`
}

export function buildLoginCommand(origin: string) {
  return 'ae-cli login'
}

export function buildDeviceLoginCommand(origin: string) {
  return 'ae-cli login --device'
}

export function buildDiscoverCommand(origin: string, providerName: string) {
  return `ae-cli discover --provider ${providerName}`
}

export function buildHooksGlobalCommand() {
  return 'ae-cli hooks enable --global'
}

export function buildRepoInitCommand() {
  return 'ae-cli init'
}

export function buildDoctorCommand() {
  return 'ae-cli doctor'
}

export function buildSyncCommand() {
  return 'ae-cli sync'
}

export function buildHooksStatusUploadsCommand() {
  return 'ae-cli hooks status --uploads'
}
```

Remove `ReviewVerifyOutputInput`, `reviewVerifyOutput`, and its private helper functions from the same file.

Remove these interfaces from `frontend/src/types/index.ts`:

```ts
export interface VerifyReviewItem {
  status: 'looks_good' | 'needs_attention' | 'cannot_determine'
  message: string
}

export interface VerifyReviewSummary {
  version: VerifyReviewItem
  discover: VerifyReviewItem
  doctor: VerifyReviewItem
}
```

- [x] **Step 4: Run command-builder test and confirm pass**

Run:

```bash
cd frontend && pnpm test src/__tests__/user-setup-review.test.ts
```

Expected result: command-builder tests pass.

- [x] **Step 5: Commit command-builder contract**

Run:

```bash
git add frontend/src/utils/userSetupReview.ts frontend/src/types/index.ts frontend/src/__tests__/user-setup-review.test.ts
git commit -m "refactor(frontend): simplify user CLI setup commands"
```

---

### Task 2: User View Checklist UI

**Files:**
- Modify: `frontend/src/views/UserView.vue`
- Test: `frontend/src/__tests__/user-view.test.ts`

- [x] **Step 1: Write failing UserView tests**

Update `frontend/src/__tests__/user-view.test.ts` to add assertions to the first render test:

```ts
expect(wrapper.text()).toContain('Machine Setup')
expect(wrapper.text()).toContain('Per-Repo Setup')
expect(wrapper.text()).toContain('Manual backfill / recovery')
expect(wrapper.text()).toContain('ae-cli hooks enable --global')
expect(wrapper.text()).toContain('cd <repo>')
expect(wrapper.text()).toContain('ae-cli init')
expect(wrapper.text()).toContain('ae-cli doctor')
expect(wrapper.text()).toContain('ae-cli sync')
expect(wrapper.text()).toContain('ae-cli hooks status --uploads')
expect(wrapper.text()).not.toContain('Paste ae-cli version output')
expect(wrapper.text()).not.toContain('Paste ae-cli discover --dry-run output')
expect(wrapper.text()).not.toContain('Paste ae-cli doctor output')
expect(wrapper.text()).not.toContain('Review')
```

Keep the existing provider-switch test that checks `discover --provider staging`.

- [x] **Step 2: Run UserView test and confirm failure**

Run:

```bash
cd frontend && pnpm test src/__tests__/user-view.test.ts
```

Expected result: the test fails because the current UI still renders `CLI Setup Checklist` with `Verify` textareas and does not render the new setup sections.

- [x] **Step 3: Update UserView script state**

In `frontend/src/views/UserView.vue`:

Remove these imports and state values:

```ts
VerifyReviewSummary,
reviewVerifyOutput,
const verifyDrafts = reactive<Record<number, { version: string; discover: string; doctor: string }>>({})
const reviewResults = reactive<Record<number, VerifyReviewSummary | null>>({})
const currentReview = computed(() => (selectedProvider.value ? reviewResults[selectedProvider.value.id] || null : null))
```

Remove `ensureVerifyDraft`, `handleReviewVerify`, and `reviewClass`.

Add imports and computed values:

```ts
import {
  buildDeviceLoginCommand,
  buildDiscoverCommand,
  buildDoctorCommand,
  buildHooksGlobalCommand,
  buildHooksStatusUploadsCommand,
  buildInstallCommand,
  buildLoginCommand,
  buildRepoInitCommand,
  buildSyncCommand,
  buildWindowsInstallCommand,
} from '@/utils/userSetupReview'

const hooksGlobalCommand = computed(() => buildHooksGlobalCommand())
const repoInitCommand = computed(() => buildRepoInitCommand())
const doctorCommand = computed(() => buildDoctorCommand())
const syncCommand = computed(() => buildSyncCommand())
const hooksStatusUploadsCommand = computed(() => buildHooksStatusUploadsCommand())
```

Remove calls to `ensureVerifyDraft(provider.id)` in provider selection paths.

- [x] **Step 4: Replace checklist template**

Replace the existing `CLI Setup Checklist` section body with these blocks:

```vue
<section class="rounded-lg bg-white p-5 shadow">
  <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">CLI Setup Checklist</h2>

  <div class="mt-4 space-y-5 text-sm">
    <div class="rounded-md border border-gray-200 p-4">
      <div class="font-medium text-gray-900">Machine Setup</div>
      <div class="mt-4 space-y-4">
        <div>
          <div class="text-sm font-medium text-gray-900">1. Install CLI</div>
          <div class="mt-3 text-xs font-medium uppercase tracking-wide text-gray-500">macOS / Linux</div>
          <pre class="mt-2 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 text-xs text-green-300">{{ installCommand }}</pre>
          <div class="mt-3 text-xs font-medium uppercase tracking-wide text-gray-500">Windows PowerShell</div>
          <pre class="mt-2 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 text-xs text-green-300">{{ windowsInstallCommand }}</pre>
        </div>
        <div>
          <div class="text-sm font-medium text-gray-900">2. Login</div>
          <pre class="mt-2 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 text-xs text-green-300">{{ loginCommand }}</pre>
          <pre class="mt-2 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 text-xs text-green-300">{{ deviceLoginCommand }}</pre>
        </div>
        <div>
          <div class="text-sm font-medium text-gray-900">3. Configure local AI tools</div>
          <pre class="mt-2 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 text-xs text-green-300">{{ discoverCommand || 'Select a provider to build the discover command.' }}</pre>
        </div>
        <div>
          <div class="text-sm font-medium text-gray-900">4. Enable automatic Git hooks</div>
          <pre class="mt-2 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 text-xs text-green-300">{{ hooksGlobalCommand }}</pre>
          <p class="mt-2 text-xs text-gray-500">Machine-level hook setup. It only reports backend-known eligible repositories.</p>
        </div>
      </div>
    </div>

    <div class="rounded-md border border-gray-200 p-4">
      <div class="font-medium text-gray-900">Per-Repo Setup</div>
      <div class="mt-4 space-y-4">
        <div>
          <div class="text-sm font-medium text-gray-900">1. Go to the repo you want to report</div>
          <pre class="mt-2 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 text-xs text-green-300">cd &lt;repo&gt;</pre>
        </div>
        <div>
          <div class="text-sm font-medium text-gray-900">2. Initialize repo attribution</div>
          <pre class="mt-2 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 text-xs text-green-300">{{ repoInitCommand }}</pre>
        </div>
        <div>
          <div class="text-sm font-medium text-gray-900">3. Diagnose setup</div>
          <pre class="mt-2 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 text-xs text-green-300">{{ doctorCommand }}</pre>
        </div>
      </div>
    </div>

    <details class="rounded-md border border-gray-200 p-4">
      <summary class="cursor-pointer font-medium text-gray-900">Manual backfill / recovery</summary>
      <div class="mt-4 space-y-4">
        <div>
          <div class="text-sm font-medium text-gray-900">Run a manual attribution sync</div>
          <pre class="mt-2 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 text-xs text-green-300">{{ syncCommand }}</pre>
        </div>
        <div>
          <div class="text-sm font-medium text-gray-900">Inspect hook upload status</div>
          <pre class="mt-2 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 text-xs text-green-300">{{ hooksStatusUploadsCommand }}</pre>
        </div>
      </div>
    </details>
  </div>
</section>
```

- [x] **Step 5: Run UserView test and confirm pass**

Run:

```bash
cd frontend && pnpm test src/__tests__/user-view.test.ts
```

Expected result: UserView tests pass.

- [x] **Step 6: Commit UserView UI**

Run:

```bash
git add frontend/src/views/UserView.vue frontend/src/__tests__/user-view.test.ts
git commit -m "feat(frontend): split CLI setup checklist by scope"
```

---

### Task 3: Architecture Documentation

**Files:**
- Modify: `docs/architecture.md`

- [x] **Step 1: Update `/user` architecture description**

In `docs/architecture.md`, update the `/user` bullet to state:

```markdown
The embedded SPA exposes a regular-user `/user` surface for profile summary, provider-aware CLI install/login/discover guidance, machine-level global hook setup, per-repo `init` / `doctor` readiness guidance, and provider-first, group-second credential self-serve driven by the current relay user's allowed groups.
```

Preserve the existing credential and provider-test details after that sentence.

- [x] **Step 2: Update CLI runtime flow wording**

In `docs/architecture.md`, update the current runtime flow notes so they describe:

```markdown
`ae-cli hooks enable --global` is the recommended one-time machine-level hook setup in the `/user` guide, while `ae-cli init` remains the per-repo registration/cache bootstrap command. `ae-cli sync` remains a manual backfill/recovery command; hooks normally trigger checkpoint and managed tool-usage upload after eligible commits.
```

- [x] **Step 3: Review architecture diff**

Run:

```bash
git diff -- docs/architecture.md
```

Expected result: the diff only changes current `/user` and CLI flow descriptions, not historical spec text.

- [x] **Step 4: Commit docs update**

Run:

```bash
git add docs/architecture.md
git commit -m "docs(architecture): update user CLI setup flow"
```

---

### Task 4: Final Verification And PR

**Files:**
- Verify: `frontend/src/`
- Verify: `docs/`

- [x] **Step 1: Run frontend unit tests**

Run:

```bash
cd frontend && pnpm test
```

Expected result: all frontend tests pass.

- [x] **Step 2: Run frontend build**

Run:

```bash
cd frontend && pnpm build
```

Expected result: Vite build completes successfully.

- [x] **Step 3: Inspect final git diff**

Run:

```bash
git status --short
git diff --stat origin/main...HEAD
git log --oneline origin/main..HEAD
```

Expected result: branch contains the spec commit, implementation commits, architecture docs, and no untracked `.superpowers` files.

- [ ] **Step 4: Push branch**

Run:

```bash
git push -u origin feat/user-cli-setup-checklist
```

Expected result: branch is pushed to origin.

- [ ] **Step 5: Open PR**

Run:

```bash
gh pr create --title "feat(frontend): improve user CLI setup checklist" --body "$(cat <<'EOF'
## Summary
- split `/user` CLI setup into Machine Setup and Per-Repo Setup
- replace paste-based Verify with `ae-cli doctor` guidance
- move `ae-cli sync` into manual backfill/recovery guidance
- update architecture docs and add the design spec

## Tests
- `cd frontend && pnpm test`
- `cd frontend && pnpm build`
EOF
)"
```

Expected result: GitHub returns a PR URL.
