# User API-Key-First Onboarding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rework `/user` into an API-key-first onboarding flow that removes the developer/non-developer split, promotes group selection plus key creation and connection test as the primary path, and exposes manual, automatic, and CC Switch configuration methods only after a successful test.

**Architecture:** Keep the existing `/api/v1/user/providers` plus group-scoped key/model/test APIs and recompose the frontend around a small state machine keyed by the selected group. Reuse the existing manual-config helper and command builders, add a focused CC Switch deep-link helper, and update `/user` and its tests without changing CLI or backend contracts.

**Tech Stack:** Vue 3, TypeScript, TailwindCSS, Vitest, Vue Test Utils, Vue Router, existing `frontend/src/i18n.ts`, Markdown docs.

**Status:** Drafted on 2026-06-14 after spec approval. No implementation work from this plan has been executed yet.

---

## Scope Boundary

Included:

- `/user` primary flow rewrite from checklist to API-key-first onboarding.
- Removal of the developer/non-developer audience toggle from the UI and tests.
- New `/user` state gating for key creation, connection test success, and configuration method visibility.
- `CC Switch` app-specific provider import deep-link helper and matching UI.
- Updated bilingual `/user` copy and architecture docs.

Excluded:

- Backend API changes.
- `ae-cli` command behavior changes.
- New `/user` routes or wizard pages.
- Universal `CC Switch` provider import deep-link support.
- `/events`, `/repos`, `/settings`, or `/admin/users` implementation changes beyond doc wording.

## File Map

Implementation files:

- Modify: `frontend/src/utils/userSetupReview.ts`
  Adds `CC Switch` provider-import deep-link helpers and platform-to-app mapping.
- Modify: `frontend/src/views/UserView.vue`
  Replaces the current `setupProgress` checklist shell with a group-scoped primary flow and post-test configuration methods.
- Modify: `frontend/src/i18n.ts`
  Rewrites `/user` copy around the new flow and adds `CC Switch` labels/help.
- Modify: `docs/architecture.md`
  Replaces the current `/user` description that still mentions the developer/non-developer split and progress-style onboarding.

Tests:

- Modify: `frontend/src/__tests__/user-setup-review.test.ts`
  Adds direct unit coverage for `CC Switch` deep-link helpers.
- Modify: `frontend/src/__tests__/user-view.test.ts`
  Replaces audience-toggle and checklist expectations with API-key-first state and visibility expectations.

## Task 1: Add CC Switch Deep-Link Helpers

**Files:**
- Modify: `frontend/src/utils/userSetupReview.ts`
- Test: `frontend/src/__tests__/user-setup-review.test.ts`

- [x] **Step 1: Write failing helper tests for CC Switch provider import**

```ts
import {
  buildCCSwitchProviderImportLink,
  resolveCCSwitchAppForPlatform,
} from '@/utils/userSetupReview'

it('maps supported platforms to CC Switch apps', () => {
  expect(resolveCCSwitchAppForPlatform('openai')).toBe('codex')
  expect(resolveCCSwitchAppForPlatform('anthropic')).toBe('claude')
  expect(resolveCCSwitchAppForPlatform('gemini')).toBe('gemini')
  expect(resolveCCSwitchAppForPlatform('unknown')).toBeNull()
})

it('builds an app-specific CC Switch provider import link', () => {
  expect(buildCCSwitchProviderImportLink({
    app: 'claude',
    name: 'Production / Group Alpha',
    endpoint: 'https://prod.example.com',
    apiKey: 'sk-claude',
  })).toBe(
    'ccswitch://v1/import?resource=provider&app=claude&name=Production+%2F+Group+Alpha&endpoint=https%3A%2F%2Fprod.example.com&apiKey=sk-claude&enabled=true'
  )
})
```

- [x] **Step 2: Run the focused helper test to verify it fails**

Run: `cd frontend && pnpm test user-setup-review.test.ts`

Expected: FAIL with missing export errors for `buildCCSwitchProviderImportLink` and `resolveCCSwitchAppForPlatform`.

- [x] **Step 3: Add the minimal CC Switch helper implementation**

```ts
export type CCSwitchApp = 'codex' | 'claude' | 'gemini'

export type CCSwitchProviderImportInput = {
  app: CCSwitchApp
  name: string
  endpoint: string
  apiKey: string
  enabled?: boolean
}

export function resolveCCSwitchAppForPlatform(platform: string): CCSwitchApp | null {
  const normalized = platform.trim().toLowerCase()
  if (normalized === 'openai') return 'codex'
  if (normalized === 'anthropic') return 'claude'
  if (normalized === 'gemini') return 'gemini'
  return null
}

export function buildCCSwitchProviderImportLink(input: CCSwitchProviderImportInput) {
  const params = new URLSearchParams({
    resource: 'provider',
    app: input.app,
    name: input.name,
    endpoint: input.endpoint,
    apiKey: input.apiKey,
    enabled: String(input.enabled ?? true),
  })
  return `ccswitch://v1/import?${params.toString()}`
}
```

- [x] **Step 4: Run the focused helper test to verify it passes**

Run: `cd frontend && pnpm test user-setup-review.test.ts`

Expected: PASS, including the new `CC Switch` helper assertions and the existing manual/automatic setup helper assertions.

- [x] **Step 5: Commit the helper work**

```bash
git add frontend/src/utils/userSetupReview.ts frontend/src/__tests__/user-setup-review.test.ts
git commit -m "feat(frontend): add ccswitch import helpers"
```

## Task 2: Rewrite `/user` Regression Tests Around The New State Flow

**Files:**
- Modify: `frontend/src/__tests__/user-view.test.ts`

- [x] **Step 1: Replace checklist/audience-toggle assertions with API-key-first state tests**

```ts
it('shows create my api key as the primary action when the selected group has no key', async () => {
  const { wrapper } = await mountUserView()

  await wrapper.get('[data-testid="group-42"]').trigger('click')
  await flushPromises()

  expect(wrapper.get('[data-testid="primary-onboarding-action"]').text()).toBe('Create my API key')
  expect(wrapper.find('[data-testid="configuration-methods"]').exists()).toBe(false)
  expect(wrapper.text()).not.toContain("I'm a developer")
  expect(wrapper.text()).not.toContain("I'm not a developer")
})

it('reveals configuration methods only after a successful connection test', async () => {
  const { createGroupCredential, testUserProvider } = await import('@/api/user')
  ;(createGroupCredential as any).mockResolvedValue({
    data: { data: { api_key_id: 7, name: 'alice', status: 'active', secret: 'sk-openai' } },
  })
  ;(testUserProvider as any).mockResolvedValue({
    data: { data: { success: true, message: 'Connection successful', response: 'pong' } },
  })

  const { wrapper } = await mountUserView()
  await wrapper.get('[data-testid="group-42"]').trigger('click')
  await wrapper.get('[data-testid="create-key"]').trigger('click')
  await flushPromises()

  expect(wrapper.get('[data-testid="primary-onboarding-action"]').text()).toBe('Run connection test')
  expect(wrapper.find('[data-testid="configuration-methods"]').exists()).toBe(false)

  await wrapper.get('[data-testid="user-provider-test-model"]').setValue('gpt-5.4')
  await wrapper.get('[data-testid="user-provider-test-run"]').trigger('click')
  await flushPromises()

  expect(wrapper.get('[data-testid="configuration-methods"]').text()).toContain('Manual configuration')
  expect(wrapper.get('[data-testid="configuration-methods"]').text()).toContain('Automatic configuration')
  expect(wrapper.get('[data-testid="configuration-methods"]').text()).toContain('CC Switch configuration')
})

it('clears the successful test state when switching groups or regenerating the key', async () => {
  const { testUserProvider, regenerateGroupCredential } = await import('@/api/user')
  ;(testUserProvider as any).mockResolvedValue({
    data: { data: { success: true, message: 'Connection successful', response: 'pong' } },
  })
  ;(regenerateGroupCredential as any).mockResolvedValue({
    data: { data: { api_key_id: 99, name: 'alice', status: 'active', secret: 'sk-regenerated' } },
  })

  const { wrapper } = await mountUserView()
  await wrapper.get('[data-testid="user-provider-test-model"]').setValue('claude-sonnet-4-6')
  await wrapper.get('[data-testid="user-provider-test-run"]').trigger('click')
  await flushPromises()
  expect(wrapper.find('[data-testid="configuration-methods"]').exists()).toBe(true)

  await wrapper.get('[data-testid="group-44"]').trigger('click')
  await flushPromises()
  expect(wrapper.find('[data-testid="configuration-methods"]').exists()).toBe(false)

  await wrapper.get('[data-testid="group-43"]').trigger('click')
  await wrapper.get('[data-testid="regenerate-key"]').trigger('click')
  await wrapper.get('[data-testid="confirm-secret-action"]').trigger('click')
  await flushPromises()
  expect(wrapper.find('[data-testid="configuration-methods"]').exists()).toBe(false)
})

it('shows only the matching CC Switch import target for the selected group platform', async () => {
  const { testUserProvider } = await import('@/api/user')
  ;(testUserProvider as any).mockResolvedValue({
    data: { data: { success: true, message: 'Connection successful', response: 'pong' } },
  })

  const { wrapper } = await mountUserView()
  await wrapper.get('[data-testid="user-provider-test-model"]').setValue('claude-sonnet-4-6')
  await wrapper.get('[data-testid="user-provider-test-run"]').trigger('click')
  await flushPromises()

  const methods = wrapper.get('[data-testid="configuration-methods"]').text()
  expect(methods).toContain('Import to Claude')
  expect(methods).not.toContain('Import to Codex')
  expect(methods).not.toContain('Import to Gemini')
})
```

- [x] **Step 2: Run the focused `/user` test file to verify it fails**

Run: `cd frontend && pnpm test user-view.test.ts`

Expected: FAIL because the current view still renders `Setup progress`, audience toggles, and the old always-visible command/test layout.

- [ ] **Step 3: Commit the failing-test checkpoint**

```bash
git add frontend/src/__tests__/user-view.test.ts
git commit -m "test(frontend): cover api-key-first onboarding flow"
```

## Task 3: Implement The New `/user` State Model And Primary Layout

**Files:**
- Modify: `frontend/src/views/UserView.vue`

- [x] **Step 1: Add explicit onboarding state and reset helpers in the script block**

```ts
const selectedConfigMethod = ref<'manual' | 'automatic' | 'ccswitch' | null>(null)

const onboardingState = computed(() => {
  if (!selectedGroup.value) return 'no_group_selected' as const
  if (!selectedKeyValue.value) return 'group_selected_without_key' as const
  if (providerTestResult.value?.success) return 'test_success' as const
  if (providerTestResult.value && !providerTestResult.value.success) return 'test_failed' as const
  return 'key_ready_without_test' as const
})

const primaryOnboardingActionLabel = computed(() => {
  if (onboardingState.value === 'group_selected_without_key') return t('user.createMyApiKey')
  if (onboardingState.value === 'key_ready_without_test' || onboardingState.value === 'test_failed') {
    return t('user.runConnectionTest')
  }
  return ''
})

const showConfigurationMethods = computed(() => onboardingState.value === 'test_success')

function resetPostKeyFlow() {
  providerTestResult.value = null
  selectedConfigMethod.value = null
}
```

- [x] **Step 2: Clear success state whenever the group context changes**

```ts
function selectProvider(providerId: number) {
  secretConfirmAction.value = null
  manualConfigConfirmKey.value = ''
  selectedProviderId.value = providerId
  const provider = providers.value.find((item) => item.id === providerId) ?? null
  selectDefaultGroup(provider)
  resetPostKeyFlow()
}

function selectGroup(groupId: string) {
  secretConfirmAction.value = null
  manualConfigConfirmKey.value = ''
  selectedGroupId.value = groupId
  resetPostKeyFlow()
}

async function handleCreateKey() {
  // existing create logic...
  updateSelectedGroupCredential(data.api_key_id, data.name, data.status, data.secret)
  resetPostKeyFlow()
}

async function handleRegenerateKey() {
  // existing regenerate logic...
  updateSelectedGroupCredential(data.api_key_id, data.name, data.status, data.secret)
  resetPostKeyFlow()
}
```

- [x] **Step 3: Replace the `setup-progress` checklist section with the new primary-flow shell**

```vue
<section data-testid="primary-onboarding-flow" class="rounded-lg bg-white p-5 shadow">
  <div class="flex flex-col gap-4 border-b border-gray-100 pb-4">
    <div>
      <p class="text-xs font-semibold uppercase tracking-wide text-amber-700">{{ t('user.primaryGoalEyebrow') }}</p>
      <h2 class="mt-2 text-2xl font-bold text-gray-900">{{ t('user.createMyApiKey') }}</h2>
      <p class="mt-2 text-sm text-gray-600">{{ t('user.primaryFlowHelp') }}</p>
    </div>
  </div>

  <div class="mt-5 space-y-4">
    <section class="rounded-lg border border-gray-200 p-4">
      <div class="flex items-start justify-between gap-4">
        <div>
          <h3 class="text-base font-semibold text-gray-900">{{ t('user.accessTitle') }}</h3>
          <p class="mt-1 text-sm text-gray-600">{{ t('user.accessGroupHelp') }}</p>
        </div>
        <button
          v-if="onboardingState === 'group_selected_without_key'"
          data-testid="primary-onboarding-action"
          class="rounded-md bg-gray-900 px-3 py-2 text-sm font-medium text-white hover:bg-black disabled:opacity-50"
          :disabled="credentialMutationLoading"
          @click="handleCreateKey"
        >
          {{ credentialMutationLoading ? t('user.creatingKey') : primaryOnboardingActionLabel }}
        </button>
      </div>
      <!-- keep the existing group chip list and status summary here -->
    </section>

    <section class="rounded-lg border border-gray-200 p-4">
      <div class="flex items-start justify-between gap-4">
        <div>
          <h3 class="text-base font-semibold text-gray-900">{{ t('user.apiKeyTitle') }}</h3>
          <p class="mt-1 text-sm text-gray-600">{{ t('user.apiKeyStageHelp') }}</p>
        </div>
        <button
          v-if="onboardingState === 'key_ready_without_test' || onboardingState === 'test_failed'"
          data-testid="primary-onboarding-action"
          class="rounded-md bg-emerald-700 px-3 py-2 text-sm font-medium text-white hover:bg-emerald-800 disabled:opacity-50"
          :disabled="providerTestLoading || !canTestProvider"
          @click="handleTestProvider"
        >
          {{ providerTestLoading ? t('user.testing') : primaryOnboardingActionLabel }}
        </button>
      </div>
      <!-- keep reveal/copy/regenerate controls here -->
    </section>
  </div>
</section>
```

- [x] **Step 4: Run the focused `/user` tests to verify the new shell still fails only on missing method panels/copy**

Run: `cd frontend && pnpm test user-view.test.ts`

Expected: FAIL, but the failures should now be narrowed to configuration-method visibility/content rather than the removed audience toggle.

- [ ] **Step 5: Commit the primary-flow refactor checkpoint**

```bash
git add frontend/src/views/UserView.vue
git commit -m "feat(frontend): add api-key-first user onboarding flow"
```

## Task 4: Add Configuration Method Panels And New `/user` Copy

**Files:**
- Modify: `frontend/src/views/UserView.vue`
- Modify: `frontend/src/i18n.ts`

- [ ] **Step 1: Add the configuration-method selector and post-success panels**

```ts
const ccSwitchImports = computed(() => {
  if (!selectedProvider.value || !selectedGroup.value || !selectedKeyValue.value) return []
  const app = resolveCCSwitchAppForPlatform(selectedGroup.value.platform)
  if (!app) return []
  return [{
    key: app,
    label: app === 'codex' ? t('user.importToCodex') : app === 'claude' ? t('user.importToClaude') : t('user.importToGemini'),
    href: buildCCSwitchProviderImportLink({
      app,
      name: `${selectedProvider.value.display_name} / ${selectedGroup.value.group_name}`,
      endpoint: selectedProvider.value.base_url,
      apiKey: selectedKeyValue.value,
    }),
  }]
})
```

```vue
<section
  v-if="showConfigurationMethods"
  data-testid="configuration-methods"
  class="rounded-lg bg-white p-5 shadow"
>
  <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">{{ t('user.configurationMethodsTitle') }}</h2>
  <p class="mt-1 text-sm text-gray-600">{{ t('user.configurationMethodsHelp') }}</p>

  <div class="mt-4 grid gap-3 md:grid-cols-3">
    <button data-testid="config-method-manual" class="rounded-lg border px-4 py-3 text-left" @click="selectedConfigMethod = 'manual'">
      <div class="font-medium text-gray-900">{{ t('user.manualConfigMethodTitle') }}</div>
      <p class="mt-1 text-sm text-gray-600">{{ t('user.manualConfigMethodHelp') }}</p>
    </button>
    <button data-testid="config-method-automatic" class="rounded-lg border px-4 py-3 text-left" @click="selectedConfigMethod = 'automatic'">
      <div class="font-medium text-gray-900">{{ t('user.automaticConfigMethodTitle') }}</div>
      <p class="mt-1 text-sm text-gray-600">{{ t('user.automaticConfigMethodHelp') }}</p>
    </button>
    <button
      v-if="ccSwitchImports.length > 0"
      data-testid="config-method-ccswitch"
      class="rounded-lg border px-4 py-3 text-left"
      @click="selectedConfigMethod = 'ccswitch'"
    >
      <div class="font-medium text-gray-900">{{ t('user.ccSwitchConfigMethodTitle') }}</div>
      <p class="mt-1 text-sm text-gray-600">{{ t('user.ccSwitchConfigMethodHelp') }}</p>
    </button>
  </div>
  <!-- render the existing manual snippets when selectedConfigMethod === 'manual' -->
  <!-- render the existing command sequence when selectedConfigMethod === 'automatic' -->
  <!-- render deep-link buttons and fallback copy when selectedConfigMethod === 'ccswitch' -->
</section>
```

- [ ] **Step 2: Rewrite the touched `/user` i18n keys around the new flow**

```ts
'user.subtitle': 'Choose an access group, create your API key, confirm the connection, then configure your tool.',
'user.primaryGoalEyebrow': 'Primary goal',
'user.createMyApiKey': 'Create my API key',
'user.primaryFlowHelp': 'Start with the selected access group. Create a personal API key, run a real connection test, then choose how you want to configure your AI tool.',
'user.accessGroupHelp': 'Access groups decide which API key you can create and which AI tools you can connect.',
'user.apiKeyStageHelp': 'Keep your key hidden by default. After creating or regenerating it, run a connection test before choosing a configuration method.',
'user.runConnectionTest': 'Run connection test',
'user.configurationMethodsTitle': 'Choose a configuration method',
'user.configurationMethodsHelp': 'These options appear only after a successful connection test.',
'user.manualConfigMethodTitle': 'Manual configuration',
'user.automaticConfigMethodTitle': 'Automatic configuration',
'user.ccSwitchConfigMethodTitle': 'CC Switch configuration',
'user.importToCodex': 'Import to Codex',
'user.importToClaude': 'Import to Claude',
'user.importToGemini': 'Import to Gemini',
'user.ccSwitchFallback': 'If CC Switch does not open, install it or re-register the ccswitch:// protocol, then retry or fall back to manual configuration.',
```

```ts
'user.subtitle': '先选择接入组，创建你的 API Key，确认连接可用后再配置工具。',
'user.primaryGoalEyebrow': '主要目标',
'user.createMyApiKey': '创建我的 API Key',
'user.primaryFlowHelp': '从当前接入组开始。先创建个人 API Key，运行一次真实连接测试，再选择工具配置方式。',
'user.accessGroupHelp': '接入组决定你能创建哪类 API Key，以及可连接哪些 AI 工具。',
'user.apiKeyStageHelp': '默认隐藏你的密钥。创建或重新生成后，先运行连接测试，再选择配置方式。',
'user.runConnectionTest': '运行连接测试',
'user.configurationMethodsTitle': '选择配置方式',
'user.configurationMethodsHelp': '只有连接测试成功后才会显示这些方式。',
'user.manualConfigMethodTitle': '手动配置',
'user.automaticConfigMethodTitle': '自动配置',
'user.ccSwitchConfigMethodTitle': 'CC Switch 配置',
'user.importToCodex': '导入到 Codex',
'user.importToClaude': '导入到 Claude',
'user.importToGemini': '导入到 Gemini',
'user.ccSwitchFallback': '如果 CC Switch 没有打开，请先安装或重新注册 ccswitch:// 协议，然后重试，或回退到手动配置。',
```

- [ ] **Step 3: Run the focused frontend tests to verify the new flow passes**

Run: `cd frontend && pnpm test user-setup-review.test.ts user-view.test.ts`

Expected: PASS, including the new deep-link helper coverage and the new `/user` state/visibility assertions.

- [ ] **Step 4: Commit the configuration-method and copy changes**

```bash
git add frontend/src/views/UserView.vue frontend/src/i18n.ts frontend/src/__tests__/user-view.test.ts frontend/src/__tests__/user-setup-review.test.ts
git commit -m "feat(frontend): add post-test onboarding configuration methods"
```

## Task 5: Refresh Architecture Docs And Remove The Old `/user` Narrative

**Files:**
- Modify: `docs/architecture.md`

- [ ] **Step 1: Update the `/user` architecture description to match the new flow**

```md
- The embedded SPA now exposes a regular-user `/user` surface as a personal AI onboarding workbench. The page keeps provider-first, group-second credential self-serve backed by `/api/v1/user/providers`, but its primary flow is now group-scoped and API-key-first: users select an access group, create or regenerate a personal key, run a real connection test with the selected group platform and model, and only after a successful test choose between manual configuration, automatic `ae-cli discover`, or app-specific `CC Switch` provider import links. The old developer / non-developer split and progress-checklist framing are no longer part of the active `/user` contract.
```

- [ ] **Step 2: Run a narrow diff check to verify only the intended `/user` architecture wording changed**

Run: `git diff -- docs/architecture.md`

Expected: A focused wording-only diff around the `/user` surface description, with no unrelated architecture churn.

- [ ] **Step 3: Commit the doc sync**

```bash
git add docs/architecture.md
git commit -m "docs(architecture): refresh user onboarding surface"
```

## Task 6: Final Verification

**Files:**
- Verify: `frontend/src/views/UserView.vue`
- Verify: `frontend/src/utils/userSetupReview.ts`
- Verify: `frontend/src/i18n.ts`
- Verify: `frontend/src/__tests__/user-view.test.ts`
- Verify: `frontend/src/__tests__/user-setup-review.test.ts`
- Verify: `docs/architecture.md`

- [ ] **Step 1: Run targeted frontend tests**

Run: `cd frontend && pnpm test user-setup-review.test.ts user-view.test.ts`

Expected: PASS.

- [ ] **Step 2: Run frontend type-check**

Run: `cd frontend && pnpm exec vue-tsc -b --pretty false`

Expected: PASS.

- [ ] **Step 3: Run the full frontend test suite**

Run: `cd frontend && pnpm test`

Expected: PASS.

- [ ] **Step 4: Run the existing role regression**

Run: `cd frontend && pnpm run test:e2e:role`

Expected: PASS.

- [ ] **Step 5: Manually verify the `/user` flow in the browser**

Run:

```bash
cd frontend && pnpm dev
```

Expected:

- `/user` no longer shows `I'm a developer` / `I'm not a developer`
- `/user` no longer uses `Setup progress` as its main title
- a group with no key emphasizes `Create my API key`
- configuration methods stay hidden before a successful test
- after a successful test, the page shows manual, automatic, and matching `CC Switch` options
- the `CC Switch` button target starts with `ccswitch://v1/import?resource=provider&app=...`

- [ ] **Step 6: Final commit if verification required follow-up fixes**

```bash
git status --short
```

Expected: clean working tree, or only intended follow-up fixes still uncommitted and ready for a final small commit.

## Self-Review

Spec coverage:

- API-key-first primary flow: covered by Tasks 2-4.
- Remove developer/non-developer split: covered by Tasks 2-4.
- Gate configuration methods on successful connection test: covered by Tasks 2-4.
- Add `CC Switch` app-specific deep links only: covered by Tasks 1 and 4.
- Keep backend/API contracts unchanged: preserved by the scope boundary and by Tasks 3-4 reusing the existing APIs.
- Update architecture docs to match the new `/user` contract: covered by Task 5.

Placeholder scan:

- No `TBD`, `TODO`, “appropriate handling”, or “similar to Task N” shortcuts remain.

Type consistency:

- `resolveCCSwitchAppForPlatform`, `buildCCSwitchProviderImportLink`, `selectedConfigMethod`, `showConfigurationMethods`, and the new i18n keys are named consistently across helper, view, and tests.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-06-14-user-api-key-first-onboarding.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?
