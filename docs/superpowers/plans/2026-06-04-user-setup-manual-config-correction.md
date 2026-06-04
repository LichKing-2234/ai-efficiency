# User Setup Manual Config Correction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修正 `/user` 接入进度中的非研发手动配置路径，使其对标当前 `ae-cli discover` 配置合同，并把代理提示限定在 Web 安装前检查与 installer 兜底。

**Architecture:** 前端新增纯函数生成 platform-specific manual config snippets，`UserView.vue` 只负责根据当前 provider/group/key 状态渲染和复制。`ae-cli/internal/update` 保持原有错误包装；GitHub/proxy guidance 只留在 `/user` install step 与 `ae-cli/install.sh` / `ae-cli/install.ps1`。

**Tech Stack:** Vue 3 `<script setup>`, TypeScript, Vitest, Go, shell installer tests.

**Status:** Complete. 主体实现和 code review follow-up 均已完成；Gemini reload snippet 已改为 guarded shell-specific rc reload，targeted/full frontend verification、diff whitespace check、commit 和 PR branch update 均已完成。

---

### Task 1: Add Failing Tests For Manual Config Snippets

**Files:**
- Modify: `frontend/src/__tests__/user-setup-review.test.ts`
- Modify: `frontend/src/__tests__/user-view.test.ts`

- [x] **Step 1: Add utility tests for discover-equivalent snippets**

Add tests importing the future helpers from `@/utils/userSetupReview`:

```ts
expect(buildCodexConfigSnippet('prod', 'https://prod.example.com')).toContain('model_provider = "prod"')
expect(buildCodexAuthSnippet('sk-openai')).toBe('{"OPENAI_API_KEY":"sk-openai"}')
expect(buildClaudeSettingsSnippet('https://prod.example.com', 'sk-claude')).toContain('"ANTHROPIC_AUTH_TOKEN": "sk-claude"')
expect(buildGeminiEnvSnippet('https://prod.example.com', 'sk-gemini')).toContain('export GEMINI_API_KEY="sk-gemini"')
```

- [x] **Step 2: Add `/user` rendering tests for non-developer path**

Extend the provider fixture with a Gemini group and assert that selecting the non-developer path renders Claude, Codex, and Gemini manual snippets while keeping `ae-cli` commands out of `data-testid="setup-progress"`.

- [x] **Step 3: Run targeted frontend tests and confirm RED**

Run:

```bash
cd frontend && pnpm test -- user-setup-review user-view
```

Expected: failing tests because snippet builders and manual snippet UI do not exist yet.

### Task 2: Implement Manual Config Builders And UI

**Files:**
- Modify: `frontend/src/utils/userSetupReview.ts`
- Modify: `frontend/src/views/UserView.vue`
- Modify: `frontend/src/i18n.ts`

- [x] **Step 1: Implement snippet builders**

Add pure helpers for:

```ts
buildCodexConfigSnippet(providerName, baseUrl)
buildCodexAuthSnippet(apiKey)
buildClaudeSettingsSnippet(baseUrl, apiKey)
buildGeminiEnvSnippet(baseUrl, apiKey)
buildGeminiReloadSnippet()
buildGeminiModelSnippet()
buildManualConfigSnippets(provider, group, apiKey)
```

The snippets must match the config fields specified in `docs/superpowers/specs/2026-05-26-user-cli-setup-checklist-design.md`.

- [x] **Step 2: Render manual snippet cards in `UserView.vue`**

Replace the current non-developer generic value list with snippet cards. Each card gets its own copy button. Snippets containing API keys use masked display by default and copy the full key-containing text only after the existing sensitive-action confirmation panel.

- [x] **Step 3: Add i18n labels**

Add English and Chinese labels for manual config files, copy snippet, missing key placeholder, Gemini reload/model guidance, and unsupported platform fallback.

- [x] **Step 4: Run targeted frontend tests and confirm GREEN**

Run:

```bash
cd frontend && pnpm test -- user-setup-review user-view
```

Expected: targeted tests pass.

### Task 3: Remove Proxy Guidance From `ae-cli/internal/update`

**Files:**
- Modify: `ae-cli/internal/update/update.go`
- Modify: `ae-cli/internal/update/update_test.go`

- [x] **Step 1: Add or adjust test expectation**

Remove the proxy-guidance test and ensure update errors remain simple wrappers such as:

```text
fetch latest release: dial tcp timeout
download install script: dial tcp timeout
```

- [x] **Step 2: Restore update error wrapping**

Delete `githubReleaseProxyGuidance` from the update package and remove appended GitHub/proxy text from `fetchLatestRelease` and `downloadInstallScript`.

- [x] **Step 3: Run update package tests**

Run:

```bash
cd ae-cli && go test ./internal/update
```

Expected: update package tests pass without new proxy guidance.

### Task 4: Sync Documentation State

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-05-26-user-cli-setup-checklist-design.md`
- Modify: `docs/superpowers/plans/2026-06-04-user-setup-manual-config-correction.md`

- [x] **Step 1: Fix architecture wording**

Change `/user` architecture text from `install/update` proxy guidance to Web install connectivity/proxy guidance plus installer fallback only.

- [x] **Step 2: Update spec status**

Once implementation is in place, change the spec status from pending PR implementation to implemented in the current PR branch.

- [x] **Step 3: Keep this plan status accurate**

Check off only completed steps and leave verification steps unchecked until their commands have run.

### Task 5: Full Verification And Commit

**Files:**
- Verify repository-wide changed behavior.

- [x] **Step 1: Run frontend unit tests**

Run:

```bash
cd frontend && pnpm test
```

- [x] **Step 2: Run frontend build**

Run:

```bash
cd frontend && pnpm build
```

- [x] **Step 3: Run ae-cli tests**

Run:

```bash
cd ae-cli && go test ./...
```

- [x] **Step 4: Run installer smoke tests**

Run:

```bash
bash ae-cli/test/install-test.sh
```

- [x] **Step 5: Run diff whitespace check**

Run:

```bash
git diff --check
```

- [x] **Step 6: Commit and update PR branch**

Commit with:

```bash
git commit -m "fix(frontend): align manual setup with discover config"
```

Then update PR #73 with the final branch head and refreshed verification notes.

### Task 6: Code Review Follow-up For Gemini Reload Snippet

**Files:**
- Modify: `frontend/src/utils/userSetupReview.ts`
- Modify: `frontend/src/__tests__/user-setup-review.test.ts`
- Modify: `frontend/src/__tests__/user-view.test.ts`
- Modify: `docs/superpowers/plans/2026-06-04-user-setup-manual-config-correction.md`

- [x] **Step 1: Write failing tests for guarded shell-specific reload guidance**

Update frontend tests so Gemini reload guidance requires a shell-selecting snippet with `[ -f "$rc_file" ] && source "$rc_file"` and rejects three unconditional source commands.

- [x] **Step 2: Run targeted frontend tests and confirm RED**

Run:

```bash
cd frontend && pnpm test -- user-setup-review user-view
```

- [x] **Step 3: Implement guarded reload snippet**

Update `buildGeminiReloadSnippet()` to choose one rc file based on `${SHELL##*/}` and source it only when the file exists.

- [x] **Step 4: Run targeted frontend tests and confirm GREEN**

Run:

```bash
cd frontend && pnpm test -- user-setup-review user-view
```

- [x] **Step 5: Run required verification**

Run:

```bash
cd frontend && pnpm test
cd frontend && pnpm build
git diff --check
```

- [x] **Step 6: Commit and update PR branch**

Commit and push the review follow-up to PR #73.
