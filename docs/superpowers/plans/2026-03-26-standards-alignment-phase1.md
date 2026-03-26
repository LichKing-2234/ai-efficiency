# Project Standards Alignment Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Align the repository's active documentation with the current relay-based implementation and restore the default frontend verification path to green.

**Architecture:** This plan treats documentation truth and default verification as one bounded cleanup slice. First, update repository guidance and active specs so contributors stop implementing against stale `sub2api`-era contracts. Then, repair the frontend unit tests and exposed package scripts so the documented verification entrypoint matches the current admin-gated settings UI and relay-backed configuration model.

**Tech Stack:** Markdown, Vue 3, Pinia, Vitest, pnpm, zsh

**Status:** 待实施

---

## File Structure

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `AGENTS.md` | Correct spec location and testing guidance |
| Modify | `CLAUDE.md` | Keep quick reference aligned with actual doc paths and verification entrypoints |
| Modify | `docs/superpowers/specs/2026-03-17-ai-efficiency-platform-design.md` | Make platform spec match the current relay-based implementation |
| Modify | `docs/superpowers/specs/2026-03-24-oauth-cli-login-design.md` | Add explicit verification matrix and keep OAuth spec aligned with current contracts |
| Modify | `docs/superpowers/specs/2026-03-24-ae-cli-smart-tool-discovery-design.md` | Clarify multi-round protocol, `api_key_id` flow, and verification expectations |
| Modify | `frontend/src/__tests__/app-sidebar.test.ts` | Match sidebar expectations to admin-gated Settings visibility |
| Modify | `frontend/src/__tests__/settings-view.test.ts` | Match LLM settings tests to the `relay_*` contract and current UI behavior |
| Modify | `frontend/package.json` | Expose the role-based E2E script as an official script |

---

### Task 1: Normalize Repository Guidance Docs

**Files:**
- Modify: `AGENTS.md`
- Modify: `CLAUDE.md`

- [ ] **Step 1: Verify the current guidance drift**

Run: `rg -n "docs/specs|前端测试：|集成测试需要 PostgreSQL \\+ Redis|sub2api 数据库" AGENTS.md CLAUDE.md`
Expected: matches the outdated spec path and incomplete testing guidance.

- [ ] **Step 2: Update `AGENTS.md` project-structure and testing sections**

Replace the stale path and test bullets in `AGENTS.md` with:

```md
## Project Structure

```
ai-efficiency/
├── backend/
├── frontend/
├── ae-cli/
├── deploy/
└── docs/superpowers/specs/   # 设计文档
```

## Testing

- 后端单元测试：`cd backend && go test ./...`
- ae-cli 默认测试：`cd ae-cli && go test ./...`
- 前端单元测试：`cd frontend && pnpm test`
- 前端角色回归脚本：`cd frontend && pnpm run test:e2e:role`
- 需要 TTY、tmux 或额外本地端口监听的测试属于环境敏感测试；失败时应与默认单元测试结果分开说明
```

- [ ] **Step 3: Update `CLAUDE.md` quick reference**

Replace the quick-reference doc paths and add the missing frontend E2E script:

```md
## Quick Reference

- Tech stack: Go 1.26+ (Gin + Ent) backend, Vue 3 (Vite + TailwindCSS + Pinia) frontend
- Design specs: `docs/superpowers/specs/`
- Implementation plans: `docs/superpowers/plans/`
- Default verification:
  - `cd backend && go test ./...`
  - `cd ae-cli && go test ./...`
  - `cd frontend && pnpm test`
  - `cd frontend && pnpm run test:e2e:role`
- Remote: `ssh://git@git.agoralab.co/ai/ai-efficiency.git`
```

- [ ] **Step 4: Re-run the consistency check**

Run: `rg -n "docs/superpowers/specs|ae-cli 默认测试|前端角色回归脚本|pnpm run test:e2e:role" AGENTS.md CLAUDE.md`
Expected: all new guidance is present.

- [ ] **Step 5: Commit**

```bash
git add AGENTS.md CLAUDE.md
git commit -m "docs(docs): align repository guidance with active spec paths and test entrypoints"
```

---

### Task 2: Align Active Specs To The Current Relay-Based Contracts

**Files:**
- Modify: `docs/superpowers/specs/2026-03-17-ai-efficiency-platform-design.md`
- Modify: `docs/superpowers/specs/2026-03-24-oauth-cli-login-design.md`
- Modify: `docs/superpowers/specs/2026-03-24-ae-cli-smart-tool-discovery-design.md`

- [ ] **Step 1: Verify the stale contract markers**

Run: `rg -n "sub2api_api_key|sub2api_api_key_id|只读访问 sub2api 数据库|POST /api/v1/tools/discover|api_key_id|Verification|验收" docs/superpowers/specs/*.md`
Expected: the platform spec still contains `sub2api_*` session payloads, and the tool-discovery spec shows both one-shot and multi-round protocol language without a single authoritative contract.

- [ ] **Step 2: Update the platform design spec to the relay-based source of truth**

In `docs/superpowers/specs/2026-03-17-ai-efficiency-platform-design.md`, replace the old sub2api DB-centric integration text with:

```md
### Relay 集成策略

效能平台不再通过只读数据库直接访问 sub2api。所有 relay server 交互统一通过 `backend/internal/relay.Provider` 接口和 HTTP API 完成。

- 用户认证通过 relay.Provider 的认证能力完成
- LLM 调用通过 relay.Provider 路由
- Session 与用量关联通过 `provider_name` + `relay_api_key_id` 完成
- 历史 `sub2api_*` 字段仅作为迁移背景，不再作为当前实现规范
```

Also replace the old session example with:

```json
POST /api/v1/sessions
{
  "id": "uuid-generated-by-cli",
  "repo_full_name": "org/repo-name",
  "branch": "feature-x",
  "tool_configs": [
    {
      "tool_name": "claude",
      "provider_name": "sub2api-claude",
      "relay_api_key_id": 123
    }
  ]
}
```

- [ ] **Step 3: Add explicit verification sections to the OAuth and tool-discovery specs**

Append the following section to `docs/superpowers/specs/2026-03-24-oauth-cli-login-design.md`:

```md
## Verification

- Backend: `cd backend && go test ./...`
- ae-cli: `cd ae-cli && go test ./...`
- Frontend: `cd frontend && pnpm test`
- Manual acceptance:
  - OAuth authorize page loads for authenticated users
  - PKCE callback rejects invalid verifier/state pairs
  - `GET /api/v1/providers` returns relay-backed provider payloads
  - Session creation accepts `tool_configs[].provider_name` and `relay_api_key_id`
```

Append the following section to `docs/superpowers/specs/2026-03-24-ae-cli-smart-tool-discovery-design.md` after the protocol description:

```md
## Authoritative Protocol

`POST /api/v1/tools/discover` starts a multi-round conversation. The authoritative response shape is the discriminated `status` protocol (`tool_call_required`, `complete`, `error`). Earlier one-shot examples are illustrative only and must not be implemented as a separate contract.

Final `complete` responses must include enough data for `~/.ae-cli/discovered_tools.json` to persist `provider_name` and `relay_api_key_id` for each discovered tool.

## Verification

- Backend: `cd backend && go test ./internal/handler/... ./internal/relay/...`
- ae-cli: `cd ae-cli && go test ./internal/client/... ./internal/session/...`
- Manual acceptance:
  - initial discover request returns `tool_call_required`
  - continue request can reach `complete`
  - final payload contains `provider_name` and `relay_api_key_id`
  - no API key secret is written to the cache file
```

- [ ] **Step 4: Verify the old contract markers are gone from the active sections**

Run: `rg -n "sub2api_api_key\"|sub2api_api_key_id|只读访问 sub2api 数据库" docs/superpowers/specs/2026-03-17-ai-efficiency-platform-design.md`
Expected: no active session example or current-design section still uses the old fields.

Run: `rg -n "Authoritative Protocol|relay_api_key_id|## Verification" docs/superpowers/specs/2026-03-24-oauth-cli-login-design.md docs/superpowers/specs/2026-03-24-ae-cli-smart-tool-discovery-design.md`
Expected: both new specs expose explicit verification and the tool-discovery spec names a single authoritative protocol.

- [ ] **Step 5: Commit**

```bash
git add docs/superpowers/specs/2026-03-17-ai-efficiency-platform-design.md docs/superpowers/specs/2026-03-24-oauth-cli-login-design.md docs/superpowers/specs/2026-03-24-ae-cli-smart-tool-discovery-design.md
git commit -m "docs(specs): align active specs with relay-based contracts and verification"
```

---

### Task 3: Repair Sidebar Tests For Admin-Gated Settings Visibility

**Files:**
- Modify: `frontend/src/__tests__/app-sidebar.test.ts`

- [ ] **Step 1: Run the failing sidebar tests**

Run: `cd frontend && pnpm test -- src/__tests__/app-sidebar.test.ts`
Expected: FAIL because the tests still expect `Settings` to render for a non-admin store state.

- [ ] **Step 2: Refactor the test helper to control the auth role explicitly**

Update `frontend/src/__tests__/app-sidebar.test.ts` with a role-aware mount helper and split expectations by role:

```ts
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from '@/stores/auth'

async function mountSidebar(path = '/', role: 'admin' | 'user' = 'user') {
  const pinia = createPinia()
  setActivePinia(pinia)

  const router = createTestRouter()
  await router.push(path)
  await router.isReady()

  const auth = useAuthStore()
  auth.user = {
    user_id: 1,
    username: 'tester',
    role,
  } as any

  return mount(AppSidebar, {
    global: { plugins: [pinia, router] },
  })
}

it('renders Dashboard, Repos, and Sessions for a regular user', async () => {
  const wrapper = await mountSidebar('/', 'user')
  const linkTexts = wrapper.findAll('a').map((l) => l.text())

  expect(linkTexts).toContain('Dashboard')
  expect(linkTexts).toContain('Repos')
  expect(linkTexts).toContain('Sessions')
  expect(linkTexts).not.toContain('Settings')
})

it('renders Settings for an admin user', async () => {
  const wrapper = await mountSidebar('/', 'admin')
  const linkTexts = wrapper.findAll('a').map((l) => l.text())

  expect(linkTexts).toContain('Settings')
})

it('applies active class to Settings only for an admin on /settings', async () => {
  const wrapper = await mountSidebar('/settings', 'admin')
  const settingsLink = wrapper.findAll('a').find((a) => a.text() === 'Settings')

  expect(settingsLink).toBeTruthy()
  expect(settingsLink!.classes()).toContain('bg-gray-800')
})
```

- [ ] **Step 3: Re-run the sidebar tests**

Run: `cd frontend && pnpm test -- src/__tests__/app-sidebar.test.ts`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add frontend/src/__tests__/app-sidebar.test.ts
git commit -m "test(frontend): align sidebar tests with admin-gated settings navigation"
```

---

### Task 4: Repair Settings Tests For The Relay Configuration Contract

**Files:**
- Modify: `frontend/src/__tests__/settings-view.test.ts`

- [ ] **Step 1: Run the failing settings tests**

Run: `cd frontend && pnpm test -- src/__tests__/settings-view.test.ts`
Expected: FAIL because the mocks and assertions still use `sub2api_url` and `sub2api_api_key`.

- [ ] **Step 2: Update the mocked config payload and route set**

In `frontend/src/__tests__/settings-view.test.ts`, replace the LLM settings mock and router definitions with:

```ts
vi.mock('@/api/settings', () => ({
  getLLMConfig: vi.fn().mockResolvedValue({
    data: {
      data: {
        relay_url: 'http://localhost:3000',
        relay_api_key: 'sk-test',
        model: 'gpt-4',
        enabled: false,
      },
    },
  }),
  updateLLMConfig: vi.fn(),
  testLLMConnection: vi.fn(),
}))

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div>Home</div>' } },
      { path: '/settings', component: SettingsView },
      { path: '/login', component: { template: '<div>Login</div>' } },
      { path: '/repos', component: { template: '<div>Repos</div>' } },
      { path: '/sessions', component: { template: '<div>Sessions</div>' } },
    ],
  })
}
```

- [ ] **Step 3: Replace stale assertions with the current UI contract**

Rewrite the affected tests to assert the relay-backed read-only fields and editable model/prompt fields:

```ts
it('renders relay-backed LLM fields', async () => {
  const wrapper = await mountSettings()

  expect(wrapper.text()).toContain('Relay URL')
  expect(wrapper.text()).toContain('Relay API Key')
  expect(wrapper.text()).toContain('Model')
  expect(wrapper.text()).toContain('System Prompt')
  expect(wrapper.text()).toContain('User Prompt Template')
})

it('loads relay config on mount', async () => {
  const wrapper = await mountSettings({
    llmConfig: {
      relay_url: 'http://localhost:3000',
      relay_api_key: 'sk-test',
      model: 'gpt-4o',
      max_tokens_per_scan: 50000,
      system_prompt: 'You are helpful',
      user_prompt_template: 'Analyze {repo_context}',
      enabled: true,
    },
  })

  const inputs = wrapper.findAll('input[type=\"text\"]')
  expect(inputs[0]!.element.value).toBe('http://localhost:3000')
  expect(inputs[1]!.element.value).toBe('sk-test')
  expect(wrapper.text()).toContain('Enabled')
})

it('saves the editable LLM fields successfully', async () => {
  const { updateLLMConfig } = await import('@/api/settings')
  ;(updateLLMConfig as any).mockResolvedValue({ data: { data: {} } })

  const wrapper = await mountSettings()
  const modelInput = wrapper.findAll('input[type=\"text\"]')[2]
  await modelInput.setValue('gpt-4.1')

  const saveBtn = wrapper.findAll('button').find((b) => b.text() === 'Save')
  await saveBtn!.trigger('click')
  await flushPromises()

  expect(updateLLMConfig).toHaveBeenCalledWith(
    expect.objectContaining({ model: 'gpt-4.1' }),
  )
  expect(wrapper.text()).toContain('LLM configuration saved')
})
```

Remove the stale client-side validation test that expects `Sub2api URL is required`; the current component does not expose an editable URL field or local validation for it.

- [ ] **Step 4: Re-run the focused settings tests**

Run: `cd frontend && pnpm test -- src/__tests__/settings-view.test.ts`
Expected: PASS

- [ ] **Step 5: Run the full frontend suite**

Run: `cd frontend && pnpm test`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add frontend/src/__tests__/settings-view.test.ts
git commit -m "test(frontend): align settings tests with relay-backed configuration UI"
```

---

### Task 5: Expose The Existing Role-Based E2E Check As An Official Script

**Files:**
- Modify: `frontend/package.json`

- [ ] **Step 1: Add an explicit script for the existing role-based E2E check**

Update the scripts block in `frontend/package.json` to:

```json
"scripts": {
  "dev": "vite",
  "build": "vue-tsc -b && vite build",
  "preview": "vite preview",
  "test": "vitest --run",
  "test:e2e:role": "python e2e_role_test.py"
}
```

- [ ] **Step 2: Verify the script is discoverable**

Run: `cd frontend && node -e "const p=require('./package.json'); console.log(p.scripts['test:e2e:role'])"`
Expected: prints `python e2e_role_test.py`

- [ ] **Step 3: Commit**

```bash
git add frontend/package.json
git commit -m "chore(frontend): expose role-based e2e verification script"
```

---

## Scope Boundary

This Phase 1 plan intentionally does not rewrite the historical `phase1`, `phase2`, `oauth`, or `smart-tool-discovery` implementation plans into `writing-plans`-compliant documents. That is a separate follow-up slice once the repository has a single documentation truth and a green default frontend verification path.
