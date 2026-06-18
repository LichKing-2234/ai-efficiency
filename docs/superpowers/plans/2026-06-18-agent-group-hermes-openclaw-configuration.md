# Agent Group Hermes/OpenClaw Configuration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an `Agent-` access-group configuration branch on `/user` that focuses on Hermes Agent, OpenClaw, and Custom Agent instead of Codex, Claude Code, Gemini, or `ae-cli discover`.

**Architecture:** Keep the backend and `ae-cli` contracts unchanged. Add focused frontend helpers in `userSetupReview.ts` for strict `Agent-` classification, Agent manual snippets, and Hermes/OpenClaw CC Switch import links; then make `UserView.vue` render different configuration cards from those helpers while preserving the current ordinary-group flow.

**Tech Stack:** Vue 3, TypeScript, TailwindCSS, Vitest, Vue Test Utils, existing `frontend/src/i18n.ts`, Markdown docs.

**Status:** Not started. Spec committed in this worktree as `d55ad0c1`.

## Global Constraints

- Agent configuration is enabled only when `group_name.startsWith("Agent-")` with case-sensitive matching.
- Ordinary access groups continue to show Codex, Claude Code, Gemini manual snippets, CC Switch imports, and `ae-cli` automatic configuration.
- `Agent-` access groups must not show Codex, Claude Code, Gemini manual snippets or import links.
- `Agent-` access groups must not show the `ae-cli` automatic configuration card.
- `Agent-` manual configuration must include Hermes Agent, OpenClaw, and Custom Agent.
- `Agent-` app import must include only Hermes Agent and OpenClaw.
- Hermes/OpenClaw CC Switch import links use `ccswitch://v1/import` provider import with URL parameters `resource`, `app`, `name`, `endpoint`, `apiKey`, `model`, and `enabled`; do not reuse Codex/Claude/Gemini app-specific config payloads for these apps.
- For `Agent-` `anthropic` or `gemini` groups, the UI must tell users to confirm or adjust OpenClaw `api` or Hermes `api_mode` inside CC Switch after import.
- Claude Desktop must not be generated as a deep-link target.
- API key hiding and copy-confirmation behavior must stay unchanged.
- Do not add backend APIs, browser-to-local CLI execution, or `ae-cli discover` support for Hermes/OpenClaw.

---

## Scope Boundary

Included:

- Helper-level strict `Agent-` group classification.
- Agent manual snippets for Hermes Agent, OpenClaw, and Custom Agent.
- Hermes/OpenClaw CC Switch import link generation for `Agent-` groups.
- `/user` configuration-method card branching for ordinary groups versus Agent groups.
- Bilingual `/user` copy for Agent manual configuration, app import, compatibility warnings, and protocol-mode adjustment.
- Focused frontend unit and view tests.
- `docs/architecture.md` update for the new current `/user` behavior.

Excluded:

- Backend changes.
- `ae-cli` behavior changes.
- Executing Hermes/OpenClaw commands from the browser.
- Claude Desktop deep-link generation.
- Full visual redesign of `/user`.

## File Map

Implementation files:

- Modify: `frontend/src/utils/userSetupReview.ts`
  Owns configuration-method helper functions, Agent group classification, manual snippet generation, and CC Switch link builders.
- Modify: `frontend/src/views/UserView.vue`
  Consumes helper output to render ordinary versus Agent configuration cards and panels.
- Modify: `frontend/src/i18n.ts`
  Adds English and Chinese strings for Agent configuration and import warnings.
- Modify: `docs/architecture.md`
  Updates the current `/user` architecture description so Agent group behavior is documented as current state.

Tests:

- Modify: `frontend/src/__tests__/user-setup-review.test.ts`
  Covers helper behavior and generated links.
- Modify: `frontend/src/__tests__/user-view.test.ts`
  Covers the rendered ordinary and Agent group flows.

## Task 1: Add Agent Configuration Helpers

**Files:**
- Modify: `frontend/src/utils/userSetupReview.ts`
- Test: `frontend/src/__tests__/user-setup-review.test.ts`

**Interfaces:**
- Produces: `isAgentAccessGroup(groupName: string | null | undefined): boolean`
- Produces: `resolveCCSwitchAppsForGroup(platform: string, groupName?: string | null): CCSwitchApp[]`
- Produces: `ManualConfigSnippetInput.groupName?: string`
- Produces: `ManualConfigSnippetInput.model?: string`
- Produces: `ManualConfigSnippetKey` values `hermes-agent`, `openclaw-agent`, `custom-agent-env`, `custom-agent-json`
- Produces: `CCSwitchApp` values `hermes` and `openclaw`
- Consumes: existing `buildManualConfigSnippets`, `buildCCSwitchProviderImportLink`, `resolveCCSwitchAppForPlatform`

- [ ] **Step 1: Write failing tests for strict Agent group classification**

Add these imports in `frontend/src/__tests__/user-setup-review.test.ts`:

```ts
import {
  isAgentAccessGroup,
  resolveCCSwitchAppsForGroup,
} from '@/utils/userSetupReview'
```

Add this test inside `describe('userSetupReview command builders', () => { ... })`:

```ts
it('classifies Agent access groups with a strict Agent- prefix', () => {
  expect(isAgentAccessGroup('Agent-openai')).toBe(true)
  expect(isAgentAccessGroup('Agent-anthropic')).toBe(true)
  expect(isAgentAccessGroup('Agent-gemini')).toBe(true)
  expect(isAgentAccessGroup('Agent-Alpha')).toBe(true)
  expect(isAgentAccessGroup('Agent')).toBe(false)
  expect(isAgentAccessGroup('Agentic-openai')).toBe(false)
  expect(isAgentAccessGroup('agent-openai')).toBe(false)
  expect(isAgentAccessGroup('My-Agent-openai')).toBe(false)
  expect(isAgentAccessGroup(null)).toBe(false)
  expect(isAgentAccessGroup(undefined)).toBe(false)
})
```

- [ ] **Step 2: Run helper tests to verify the classification test fails**

Run: `cd frontend && pnpm test user-setup-review.test.ts`

Expected: FAIL with an import error for `isAgentAccessGroup` and `resolveCCSwitchAppsForGroup`.

- [ ] **Step 3: Write failing tests for ordinary versus Agent manual snippets**

Add this test to `frontend/src/__tests__/user-setup-review.test.ts`:

```ts
it('switches manual snippets from normal tools to Agent clients for Agent groups', () => {
  expect(buildManualConfigSnippets({
    providerName: 'prod',
    baseUrl: 'https://prod.example.com',
    platform: 'openai',
    apiKey: 'sk-openai',
    model: 'gpt-5.4',
    groupName: 'Group Alpha',
  }).map((snippet) => snippet.key)).toEqual(['codex-config', 'codex-auth'])

  expect(buildManualConfigSnippets({
    providerName: 'prod',
    baseUrl: 'https://prod.example.com',
    platform: 'openai',
    apiKey: 'sk-openai',
    model: 'gpt-5.4',
    groupName: 'Agent-openai',
  }).map((snippet) => snippet.key)).toEqual([
    'hermes-agent',
    'openclaw-agent',
    'custom-agent-env',
    'custom-agent-json',
  ])

  expect(buildManualConfigSnippets({
    providerName: 'prod',
    baseUrl: 'https://prod.example.com',
    platform: 'anthropic',
    apiKey: 'sk-claude',
    model: 'claude-sonnet-4-6',
    groupName: 'Agent-anthropic',
  }).map((snippet) => snippet.key)).toEqual([
    'hermes-agent',
    'openclaw-agent',
    'custom-agent-env',
    'custom-agent-json',
  ])

  expect(buildManualConfigSnippets({
    providerName: 'prod',
    baseUrl: 'https://prod.example.com',
    platform: 'gemini',
    apiKey: 'sk-gemini',
    model: 'gemini-3.1-pro-preview',
    groupName: 'Agent-gemini',
  }).map((snippet) => snippet.key)).toEqual([
    'hermes-agent',
    'openclaw-agent',
    'custom-agent-env',
    'custom-agent-json',
  ])
})
```

Add this test to assert platform-specific content:

```ts
it('builds Agent manual snippets without mixing platform credential names', () => {
  const anthropicSnippets = buildManualConfigSnippets({
    providerName: 'prod',
    baseUrl: 'https://prod.example.com',
    platform: 'anthropic',
    apiKey: 'sk-claude',
    model: 'claude-sonnet-4-6',
    groupName: 'Agent-anthropic',
  })
  const anthropicBody = anthropicSnippets.map((snippet) => snippet.body).join('\n')
  expect(anthropicBody).toContain('ANTHROPIC_AUTH_TOKEN')
  expect(anthropicBody).toContain('ANTHROPIC_BASE_URL')
  expect(anthropicBody).toContain('anthropic-messages')
  expect(anthropicBody).toContain('anthropic_messages')
  expect(anthropicBody).not.toContain('OPENAI_API_KEY')
  expect(anthropicBody).not.toContain('GEMINI_API_KEY')

  const geminiSnippets = buildManualConfigSnippets({
    providerName: 'prod',
    baseUrl: 'https://prod.example.com',
    platform: 'gemini',
    apiKey: 'sk-gemini',
    model: 'gemini-3.1-pro-preview',
    groupName: 'Agent-gemini',
  })
  const geminiBody = geminiSnippets.map((snippet) => snippet.body).join('\n')
  expect(geminiBody).toContain('GEMINI_API_KEY')
  expect(geminiBody).toContain('GOOGLE_GEMINI_BASE_URL')
  expect(geminiBody).toContain('google-generative-ai')
  expect(geminiBody).not.toContain('OPENAI_API_KEY')
  expect(geminiBody).not.toContain('ANTHROPIC_AUTH_TOKEN')
})
```

- [ ] **Step 4: Write failing tests for ordinary versus Agent CC Switch app selection**

Add this test:

```ts
it('resolves CC Switch apps by ordinary versus Agent group', () => {
  expect(resolveCCSwitchAppsForGroup('openai', 'Group Alpha')).toEqual(['codex'])
  expect(resolveCCSwitchAppsForGroup('anthropic', 'Group Beta')).toEqual(['claude'])
  expect(resolveCCSwitchAppsForGroup('gemini', 'Group Delta')).toEqual(['gemini'])
  expect(resolveCCSwitchAppsForGroup('openai', 'Agent-openai')).toEqual(['hermes', 'openclaw'])
  expect(resolveCCSwitchAppsForGroup('anthropic', 'Agent-anthropic')).toEqual(['hermes', 'openclaw'])
  expect(resolveCCSwitchAppsForGroup('gemini', 'Agent-gemini')).toEqual(['hermes', 'openclaw'])
  expect(resolveCCSwitchAppsForGroup('openai', 'Agent')).toEqual(['codex'])
  expect(resolveCCSwitchAppsForGroup('unknown', 'Agent-unknown')).toEqual([])
})
```

Add this test:

```ts
it('builds Hermes and OpenClaw provider links with URL params instead of app-specific config payloads', () => {
  for (const app of ['hermes', 'openclaw'] as const) {
    const link = buildCCSwitchProviderImportLink({
      app,
      name: 'Production / Agent-openai',
      endpoint: 'https://prod.example.com',
      apiKey: 'sk-agent',
      model: 'gpt-5.4',
    })
    const url = new URL(link)
    expect(`${url.protocol}//${url.host}${url.pathname}`).toBe('ccswitch://v1/import')
    expect(url.searchParams.get('resource')).toBe('provider')
    expect(url.searchParams.get('app')).toBe(app)
    expect(url.searchParams.get('name')).toBe('Production / Agent-openai')
    expect(url.searchParams.get('endpoint')).toBe('https://prod.example.com')
    expect(url.searchParams.get('apiKey')).toBe('sk-agent')
    expect(url.searchParams.get('model')).toBe('gpt-5.4')
    expect(url.searchParams.get('enabled')).toBe('true')
    expect(url.searchParams.get('configFormat')).toBeNull()
    expect(url.searchParams.get('config')).toBeNull()
  }
})
```

- [ ] **Step 5: Run helper tests to verify the new tests fail**

Run: `cd frontend && pnpm test user-setup-review.test.ts`

Expected: FAIL with missing helper/type support for Agent snippets and app selection.

- [ ] **Step 6: Add Agent helper types and classification**

Modify the top of `frontend/src/utils/userSetupReview.ts`:

```ts
export type CCSwitchApp = 'codex' | 'claude' | 'gemini' | 'hermes' | 'openclaw'

export type ManualConfigSnippetKey =
  | 'codex-config'
  | 'codex-auth'
  | 'claude-settings'
  | 'gemini-env'
  | 'gemini-reload'
  | 'gemini-model'
  | 'hermes-agent'
  | 'openclaw-agent'
  | 'custom-agent-env'
  | 'custom-agent-json'

export type ManualConfigSnippetInput = {
  providerName: string
  baseUrl: string
  platform: string
  apiKey: string
  groupName?: string | null
  model?: string
}
```

Add these helper types and functions after `const GEMINI_MODEL = 'gemini-3.1-pro-preview'`:

```ts
type AgentPlatform = 'openai' | 'anthropic' | 'gemini'

type AgentPlatformProfile = {
  platform: AgentPlatform
  displayName: string
  env: Record<string, string>
  openClawApi: 'openai-completions' | 'anthropic-messages' | 'google-generative-ai'
  hermesApiMode: 'chat_completions' | 'anthropic_messages' | null
}

export function isAgentAccessGroup(groupName: string | null | undefined) {
  return Boolean(groupName?.startsWith('Agent-'))
}

function normalizeAgentPlatform(platform: string): AgentPlatform | null {
  const normalized = platform.trim().toLowerCase()
  if (normalized === 'openai') return 'openai'
  if (normalized === 'anthropic') return 'anthropic'
  if (normalized === 'gemini') return 'gemini'
  return null
}

function resolveAgentPlatformProfile(platform: string, baseUrl: string, apiKey: string, model?: string): AgentPlatformProfile | null {
  const normalized = normalizeAgentPlatform(platform)
  const selectedModel = model?.trim()
  if (normalized === 'openai') {
    return {
      platform: 'openai',
      displayName: 'OpenAI-compatible',
      env: {
        OPENAI_API_KEY: apiKey,
        OPENAI_BASE_URL: baseUrl,
        ...(selectedModel ? { OPENAI_MODEL: selectedModel } : {}),
      },
      openClawApi: 'openai-completions',
      hermesApiMode: 'chat_completions',
    }
  }
  if (normalized === 'anthropic') {
    return {
      platform: 'anthropic',
      displayName: 'Anthropic-compatible',
      env: {
        ANTHROPIC_AUTH_TOKEN: apiKey,
        ANTHROPIC_BASE_URL: baseUrl,
        ...(selectedModel ? { ANTHROPIC_MODEL: selectedModel } : {}),
      },
      openClawApi: 'anthropic-messages',
      hermesApiMode: 'anthropic_messages',
    }
  }
  if (normalized === 'gemini') {
    return {
      platform: 'gemini',
      displayName: 'Gemini-compatible',
      env: {
        GEMINI_API_KEY: apiKey,
        GOOGLE_GEMINI_BASE_URL: baseUrl,
        ...(selectedModel ? { GEMINI_MODEL: selectedModel } : {}),
      },
      openClawApi: 'google-generative-ai',
      hermesApiMode: null,
    }
  }
  return null
}
```

- [ ] **Step 7: Add Agent manual snippet builders**

Add these functions after `buildGeminiModelSnippet()`:

```ts
function envExports(env: Record<string, string>) {
  return Object.entries(env)
    .map(([key, value]) => `export ${key}=${shellString(value)}`)
    .join('\n')
}

function buildHermesAgentSnippet(input: ManualConfigSnippetInput, profile: AgentPlatformProfile) {
  const providerName = input.providerName.trim() || 'ae-agent'
  const model = input.model?.trim()
  if (profile.platform === 'gemini') {
    return [
      '# Hermes Agent does not expose a stable native Gemini provider mode through CC Switch import.',
      '# Use the Hermes setup portal and enter these current access-group values:',
      'hermes setup --portal',
      '',
      `provider_name=${shellString(providerName)}`,
      `provider_protocol=${shellString(profile.displayName)}`,
      `base_url=${shellString(input.baseUrl)}`,
      `api_key=${shellString(input.apiKey)}`,
      ...(model ? [`model=${shellString(model)}`] : []),
    ].join('\n')
  }
  return [
    '# Add this provider through Hermes setup or config, using the same fields.',
    'hermes setup --portal',
    '',
    'custom_providers:',
    `  - name: ${JSON.stringify(providerName)}`,
    `    base_url: ${JSON.stringify(input.baseUrl)}`,
    `    api_key: ${JSON.stringify(input.apiKey)}`,
    `    api_mode: ${JSON.stringify(profile.hermesApiMode)}`,
    ...(model ? [
      '    models:',
      `      ${JSON.stringify(model)}:`,
      '        context_length: 200000',
    ] : []),
  ].join('\n')
}

function buildOpenClawAgentSnippet(input: ManualConfigSnippetInput, profile: AgentPlatformProfile) {
  const model = input.model?.trim()
  return JSON.stringify({
    baseUrl: input.baseUrl,
    apiKey: input.apiKey,
    api: profile.openClawApi,
    ...(model ? { models: [{ id: model, name: model }] } : {}),
  }, null, 2)
}

function buildCustomAgentJSONSnippet(input: ManualConfigSnippetInput, profile: AgentPlatformProfile) {
  return JSON.stringify({
    platform: profile.platform,
    protocol: profile.displayName,
    base_url: input.baseUrl,
    api_key: input.apiKey,
    ...(input.model?.trim() ? { model: input.model.trim() } : {}),
  }, null, 2)
}

function buildAgentManualConfigSnippets(input: ManualConfigSnippetInput): ManualConfigSnippet[] {
  const profile = resolveAgentPlatformProfile(input.platform, input.baseUrl, input.apiKey, input.model)
  if (!profile) {
    return [
      {
        key: 'custom-agent-json',
        path: 'Custom Agent provider values',
        body: JSON.stringify({
          platform: input.platform,
          base_url: input.baseUrl,
          api_key: input.apiKey,
          ...(input.model?.trim() ? { model: input.model.trim() } : {}),
        }, null, 2),
        containsSecret: true,
      },
    ]
  }
  return [
    {
      key: 'hermes-agent',
      path: profile.platform === 'gemini' ? 'Hermes Agent setup values' : '~/.hermes/config.yaml',
      body: buildHermesAgentSnippet(input, profile),
      containsSecret: true,
    },
    {
      key: 'openclaw-agent',
      path: '~/.openclaw/openclaw.json provider entry',
      body: buildOpenClawAgentSnippet(input, profile),
      containsSecret: true,
    },
    {
      key: 'custom-agent-env',
      path: 'Custom Agent environment',
      body: envExports(profile.env),
      containsSecret: true,
    },
    {
      key: 'custom-agent-json',
      path: 'Custom Agent JSON',
      body: buildCustomAgentJSONSnippet(input, profile),
      containsSecret: true,
    },
  ]
}
```

- [ ] **Step 8: Route `buildManualConfigSnippets` through Agent snippets**

Modify the start of `buildManualConfigSnippets`:

```ts
export function buildManualConfigSnippets(input: ManualConfigSnippetInput): ManualConfigSnippet[] {
  if (isAgentAccessGroup(input.groupName)) {
    return buildAgentManualConfigSnippets(input)
  }
  const platform = input.platform.trim().toLowerCase()
  // keep the existing ordinary-group openai/anthropic/gemini branches unchanged
```

- [ ] **Step 9: Add Agent CC Switch app resolution and link support**

Replace `resolveCCSwitchAppForPlatform` and keep its existing behavior for ordinary groups:

```ts
export function resolveCCSwitchAppForPlatform(platform: string): CCSwitchApp | null {
  const normalized = platform.trim().toLowerCase()
  if (normalized === 'openai') return 'codex'
  if (normalized === 'anthropic') return 'claude'
  if (normalized === 'gemini') return 'gemini'
  return null
}

export function resolveCCSwitchAppsForGroup(platform: string, groupName?: string | null): CCSwitchApp[] {
  if (isAgentAccessGroup(groupName)) {
    return normalizeAgentPlatform(platform) ? ['hermes', 'openclaw'] : []
  }
  const app = resolveCCSwitchAppForPlatform(platform)
  return app ? [app] : []
}
```

Leave the existing `buildCCSwitchProviderImportLink` app-specific `claude`, `codex`, and `gemini` branches unchanged. The existing fallback branch will now handle `hermes` and `openclaw` with URL params because `CCSwitchApp` includes those values.

- [ ] **Step 10: Run helper tests to verify they pass**

Run: `cd frontend && pnpm test user-setup-review.test.ts`

Expected: PASS for all `user-setup-review.test.ts` tests.

- [ ] **Step 11: Commit helper work**

```bash
git add frontend/src/utils/userSetupReview.ts frontend/src/__tests__/user-setup-review.test.ts
git commit -m "feat(frontend): add agent group setup helpers"
```

## Task 2: Render Agent Configuration Methods On `/user`

**Files:**
- Modify: `frontend/src/views/UserView.vue`
- Modify: `frontend/src/i18n.ts`
- Test: `frontend/src/__tests__/user-view.test.ts`

**Interfaces:**
- Consumes: `isAgentAccessGroup(groupName)`
- Consumes: `resolveCCSwitchAppsForGroup(platform, groupName)`
- Consumes: Agent `ManualConfigSnippetKey` values from Task 1
- Produces: `selectedConfigMethod` remains `'manual' | 'automatic' | 'ccswitch' | null`
- Produces: `config-method-automatic` is hidden for Agent groups
- Produces: `ccswitch-import-hermes` and `ccswitch-import-openclaw` links for Agent groups

- [ ] **Step 1: Add Agent groups to the view test fixture**

Modify the `providers` array inside `mountUserView()` in `frontend/src/__tests__/user-view.test.ts` by adding these groups to the `prod` provider:

```ts
{
  group_id: '46',
  group_name: 'Agent-openai',
  platform: 'openai',
  credential: { state: 'existing_hidden', api_key_id: 25, name: 'alice', status: 'active', key: 'sk-existing-agent-openai-123456' },
},
{
  group_id: '47',
  group_name: 'Agent-anthropic',
  platform: 'anthropic',
  credential: { state: 'existing_hidden', api_key_id: 26, name: 'alice', status: 'active', key: 'sk-existing-agent-anthropic-123456' },
},
{
  group_id: '48',
  group_name: 'Agent-gemini',
  platform: 'gemini',
  credential: { state: 'existing_hidden', api_key_id: 27, name: 'alice', status: 'active', key: 'sk-existing-agent-gemini-123456' },
},
```

- [ ] **Step 2: Write failing view tests for Agent method cards and manual panel**

Add this test in `frontend/src/__tests__/user-view.test.ts`:

```ts
it('shows Agent-only configuration methods and manual snippets for Agent groups', async () => {
  const { wrapper } = await mountUserView()

  await wrapper.get('[data-testid="group-46"]').trigger('click')
  await flushPromises()

  const methods = wrapper.get('[data-testid="configuration-methods"]').text()
  expect(methods).toContain('Manual configuration')
  expect(methods).toContain('App import')
  expect(methods).not.toContain('Automatic configuration')
  expect(wrapper.find('[data-testid="config-method-automatic"]').exists()).toBe(false)
  expect(wrapper.find('[data-testid="config-method-ccswitch"]').exists()).toBe(true)

  await wrapper.get('[data-testid="config-method-manual"]').trigger('click')
  const manualText = wrapper.text()
  expect(manualText).toContain('Hermes Agent')
  expect(manualText).toContain('OpenClaw')
  expect(manualText).toContain('Custom Agent')
  expect(manualText).not.toContain('Codex config')
  expect(manualText).not.toContain('Codex auth')
  expect(manualText).not.toContain('Claude settings')
  expect(manualText).not.toContain('Gemini env')
})
```

Add this test:

```ts
it('shows only Hermes and OpenClaw imports for Agent groups', async () => {
  const { wrapper } = await mountUserView()

  await wrapper.get('[data-testid="group-46"]').trigger('click')
  await flushPromises()
  await wrapper.get('[data-testid="config-method-ccswitch"]').trigger('click')

  const panelText = wrapper.text()
  expect(panelText).toContain('Import to Hermes Agent')
  expect(panelText).toContain('Import to OpenClaw')
  expect(panelText).not.toContain('Import to Codex')
  expect(panelText).not.toContain('Import to Claude')
  expect(panelText).not.toContain('Import to Gemini')

  const hermesHref = wrapper.get('[data-testid="ccswitch-import-hermes"]').attributes('href') ?? ''
  const openclawHref = wrapper.get('[data-testid="ccswitch-import-openclaw"]').attributes('href') ?? ''
  expect(hermesHref).toContain('app=hermes')
  expect(hermesHref).toContain('endpoint=https%3A%2F%2Fprod.example.com')
  expect(hermesHref).toContain('apiKey=sk-existing-agent-openai-123456')
  expect(hermesHref).not.toContain('configFormat=json')
  expect(openclawHref).toContain('app=openclaw')
  expect(openclawHref).toContain('endpoint=https%3A%2F%2Fprod.example.com')
  expect(openclawHref).toContain('apiKey=sk-existing-agent-openai-123456')
  expect(openclawHref).not.toContain('configFormat=json')
})
```

Add this test:

```ts
it('warns Agent Anthropic and Gemini users to confirm CC Switch protocol mode after import', async () => {
  const { wrapper } = await mountUserView()

  await wrapper.get('[data-testid="group-47"]').trigger('click')
  await flushPromises()
  await wrapper.get('[data-testid="config-method-ccswitch"]').trigger('click')
  expect(wrapper.text()).toContain('Confirm the provider protocol in CC Switch after import')
  expect(wrapper.text()).toContain('OpenClaw api')
  expect(wrapper.text()).toContain('Hermes api_mode')

  await wrapper.get('[data-testid="group-48"]').trigger('click')
  await flushPromises()
  await wrapper.get('[data-testid="config-method-ccswitch"]').trigger('click')
  expect(wrapper.text()).toContain('Confirm the provider protocol in CC Switch after import')
  expect(wrapper.text()).toContain('OpenClaw api')
  expect(wrapper.text()).toContain('Hermes api_mode')
})
```

- [ ] **Step 3: Run view tests to verify they fail**

Run: `cd frontend && pnpm test user-view.test.ts`

Expected: FAIL because the Agent groups still render the ordinary automatic card and ordinary import/manual snippets.

- [ ] **Step 4: Import Agent helpers and compute Agent state in `UserView.vue`**

Modify the import block in `frontend/src/views/UserView.vue`:

```ts
import {
  buildCCSwitchProviderImportLink,
  buildDeviceLoginCommand,
  buildDiscoverCommand,
  buildDoctorCommand,
  buildPreferredGithubConnectivityCommand,
  buildHooksGlobalCommand,
  buildHooksStatusUploadsCommand,
  buildInstallCommand,
  buildLoginCommand,
  buildManualConfigSnippets,
  buildPreferredInstallCommand,
  buildRepoInitCommand,
  resolveCCSwitchAppsForGroup,
  buildSyncCommand,
  buildWindowsInstallCommand,
  detectInstallPlatform,
  isAgentAccessGroup,
} from '@/utils/userSetupReview'
```

Add these computed values after `selectedGroup`:

```ts
const selectedIsAgentGroup = computed(() => isAgentAccessGroup(selectedGroup.value?.group_name))
const showAutomaticConfigMethod = computed(() => !selectedIsAgentGroup.value)
const ccSwitchMethodTitle = computed(() => selectedIsAgentGroup.value ? t('user.appImportMethodTitle') : t('user.ccSwitchConfigMethodTitle'))
const ccSwitchMethodHelp = computed(() => selectedIsAgentGroup.value ? t('user.appImportMethodHelp') : t('user.ccSwitchConfigMethodHelp'))
const ccSwitchMethodAudience = computed(() => selectedIsAgentGroup.value ? t('user.appImportMethodAudience') : t('user.ccSwitchConfigMethodAudience'))
const showAgentImportProtocolWarning = computed(() => {
  if (!selectedIsAgentGroup.value) return false
  const platform = selectedGroup.value?.platform.trim().toLowerCase()
  return platform === 'anthropic' || platform === 'gemini'
})
```

- [ ] **Step 5: Update manual snippet input with group name and model**

Modify `buildSelectedManualConfigSnippets` in `frontend/src/views/UserView.vue`:

```ts
function buildSelectedManualConfigSnippets(apiKey: string) {
  if (!selectedProvider.value || !selectedGroup.value) return []
  return buildManualConfigSnippets({
    providerName: selectedProvider.value.name,
    baseUrl: selectedProvider.value.base_url,
    platform: selectedGroup.value.platform,
    apiKey,
    groupName: selectedGroup.value.group_name,
    model: providerTestModel.value.trim(),
  })
}
```

- [ ] **Step 6: Update CC Switch imports computed output**

Replace the current `ccSwitchImports` computed in `frontend/src/views/UserView.vue`:

```ts
const ccSwitchImports = computed(() => {
  if (!selectedProvider.value || !selectedGroup.value || !selectedKeyValue.value) return []
  const apps = resolveCCSwitchAppsForGroup(selectedGroup.value.platform, selectedGroup.value.group_name)
  const selectedModel = providerTestModel.value.trim()
  return apps.map((app) => ({
    key: app,
    label:
      app === 'codex' ? t('user.importToCodex')
      : app === 'claude' ? t('user.importToClaude')
      : app === 'gemini' ? t('user.importToGemini')
      : app === 'hermes' ? t('user.importToHermes')
      : t('user.importToOpenClaw'),
    href: buildCCSwitchProviderImportLink({
      app,
      name: `${selectedProvider.value.display_name} / ${selectedGroup.value.group_name}`,
      endpoint: selectedProvider.value.base_url,
      apiKey: selectedKeyValue.value,
      model: selectedModel || (app === 'codex' ? 'gpt-5.4' : undefined),
    }),
  }))
})
```

- [ ] **Step 7: Add Agent manual snippet titles**

Extend `manualConfigSnippetTitle` in `frontend/src/views/UserView.vue`:

```ts
    case 'hermes-agent':
      return t('user.manualConfigHermesAgent')
    case 'openclaw-agent':
      return t('user.manualConfigOpenClaw')
    case 'custom-agent-env':
      return t('user.manualConfigCustomAgentEnv')
    case 'custom-agent-json':
      return t('user.manualConfigCustomAgentJson')
```

- [ ] **Step 8: Hide automatic card and retitle app import card**

Modify the template card section:

```vue
<button
  v-if="showAutomaticConfigMethod"
  data-testid="config-method-automatic"
  class="rounded-lg border px-4 py-3 text-left transition"
  :class="selectedConfigMethod === 'automatic' ? 'border-gray-900 bg-gray-50' : 'border-gray-200 hover:border-gray-400'"
  @click="selectedConfigMethod = 'automatic'"
>
  <div class="font-medium text-gray-900">{{ t('user.automaticConfigMethodTitle') }}</div>
  <p class="mt-1 text-sm text-gray-600">{{ t('user.automaticConfigMethodHelp') }}</p>
  <p class="mt-3 text-xs text-gray-500">{{ t('user.automaticConfigMethodAudience') }}</p>
</button>
```

Replace the CC Switch card title/help/audience:

```vue
<div class="font-medium text-gray-900">{{ ccSwitchMethodTitle }}</div>
<p class="mt-1 text-sm text-gray-600">{{ ccSwitchMethodHelp }}</p>
<p class="mt-3 text-xs text-gray-500">{{ ccSwitchMethodAudience }}</p>
```

Replace the CC Switch panel title/help:

```vue
<div class="font-medium text-gray-900">{{ ccSwitchMethodTitle }}</div>
<p class="mt-1 text-sm text-gray-600">{{ ccSwitchMethodHelp }}</p>
```

Add this warning inside the app-import panel before the import buttons:

```vue
<p
  v-if="selectedIsAgentGroup"
  class="mt-3 rounded-md border border-amber-200 bg-amber-50 p-3 text-xs text-amber-900"
>
  {{ t('user.agentImportHermesVersionWarning') }}
</p>
<p
  v-if="showAgentImportProtocolWarning"
  data-testid="agent-import-protocol-warning"
  class="mt-3 rounded-md border border-amber-200 bg-amber-50 p-3 text-xs text-amber-900"
>
  {{ t('user.agentImportProtocolWarning') }}
</p>
```

- [ ] **Step 9: Add English and Chinese i18n strings**

Add English strings near the existing manual/CC Switch strings in `frontend/src/i18n.ts`:

```ts
'user.manualConfigHermesAgent': 'Hermes Agent',
'user.manualConfigOpenClaw': 'OpenClaw',
'user.manualConfigCustomAgentEnv': 'Custom Agent environment',
'user.manualConfigCustomAgentJson': 'Custom Agent JSON',
'user.appImportMethodTitle': 'App import',
'user.appImportMethodHelp': 'Import the current Agent access group into CC Switch-managed Agent clients.',
'user.appImportMethodAudience': 'Best for Agent clients that are managed through CC Switch.',
'user.importToHermes': 'Import to Hermes Agent',
'user.importToOpenClaw': 'Import to OpenClaw',
'user.agentImportHermesVersionWarning': 'Hermes Agent import requires a recent CC Switch version. If import fails, upgrade CC Switch or use manual configuration.',
'user.agentImportProtocolWarning': 'Confirm the provider protocol in CC Switch after import. For this platform, check OpenClaw api and Hermes api_mode before using the provider.',
```

Add Chinese strings near the existing Chinese manual/CC Switch strings:

```ts
'user.manualConfigHermesAgent': 'Hermes Agent',
'user.manualConfigOpenClaw': 'OpenClaw',
'user.manualConfigCustomAgentEnv': '自定义 Agent 环境变量',
'user.manualConfigCustomAgentJson': '自定义 Agent JSON',
'user.appImportMethodTitle': '应用导入',
'user.appImportMethodHelp': '把当前 Agent 接入组导入到由 CC Switch 管理的 Agent 客户端。',
'user.appImportMethodAudience': '适合通过 CC Switch 管理 Hermes Agent 或 OpenClaw 的场景。',
'user.importToHermes': '导入到 Hermes Agent',
'user.importToOpenClaw': '导入到 OpenClaw',
'user.agentImportHermesVersionWarning': 'Hermes Agent 导入需要较新版本的 CC Switch。如果导入失败，请升级 CC Switch 或改用手动配置。',
'user.agentImportProtocolWarning': '导入后请在 CC Switch 内确认 provider 协议。当前平台需要检查 OpenClaw api 和 Hermes api_mode 后再使用。',
```

- [ ] **Step 10: Run view tests to verify they pass**

Run: `cd frontend && pnpm test user-view.test.ts`

Expected: PASS for all `user-view.test.ts` tests.

- [ ] **Step 11: Commit UI work**

```bash
git add frontend/src/views/UserView.vue frontend/src/i18n.ts frontend/src/__tests__/user-view.test.ts
git commit -m "feat(frontend): show agent group configuration paths"
```

## Task 3: Update Architecture Docs And Run Final Verification

**Files:**
- Modify: `docs/architecture.md`
- Test: frontend focused tests and build/type check

**Interfaces:**
- Consumes: Task 1 helper contract and Task 2 rendered behavior.
- Produces: Current architecture documentation that describes ordinary versus `Agent-` `/user` configuration branching.

- [ ] **Step 1: Update `docs/architecture.md` for the new `/user` current behavior**

Modify the long `/user` paragraph in `docs/architecture.md` that currently says users can choose manual configuration, automatic `ae-cli discover`, or app-specific `CC Switch` provider-import links once a key exists.

Replace that sentence with this content:

```md
Once a key exists, ordinary access groups still offer manual local configuration, automatic `ae-cli discover`, and app-specific `CC Switch` provider-import links for Codex, Claude Code, or Gemini according to the selected group platform. Access groups whose names strictly start with `Agent-` instead enter an Agent-client configuration branch: the page hides Codex/Claude/Gemini snippets, hides the `ae-cli` automatic configuration card, shows Hermes Agent, OpenClaw, and Custom Agent manual configuration, and offers only Hermes/OpenClaw CC Switch app-import links. Hermes/OpenClaw app import uses CC Switch provider deep links; for Anthropic or Gemini Agent groups, the UI tells users to confirm OpenClaw `api` or Hermes `api_mode` inside CC Switch after import because the deep link imports endpoint/key/model but does not fully encode every target app protocol field.
```

- [ ] **Step 2: Run plan-required focused tests**

Run:

```bash
cd frontend && pnpm test user-setup-review.test.ts user-view.test.ts
```

Expected: PASS for both test files.

- [ ] **Step 3: Run frontend build/type verification**

Run:

```bash
cd frontend && pnpm run build
```

Expected: PASS with `vue-tsc -b` and `vite build` completing successfully.

- [ ] **Step 4: Check the final diff for whitespace and scope**

Run:

```bash
git diff --check
git status --short
```

Expected:

- `git diff --check` prints no output and exits 0.
- `git status --short` shows only:

```text
 M docs/architecture.md
```

- [ ] **Step 5: Commit docs and final verification state**

```bash
git add docs/architecture.md
git commit -m "docs(frontend): document agent group configuration branch"
```

- [ ] **Step 6: Report final implementation status**

Collect:

```bash
git log --oneline -n 4
git status --short --branch
```

Expected:

- The branch contains the spec commit plus three implementation commits.
- The worktree is clean after commits.

Report:

- Commits created.
- Focused frontend tests result.
- Frontend build result.
- Any manual verification not performed.

## Execution Notes

- Keep the existing untracked `.claude/` directory in the main checkout untouched.
- Work in `/Users/admin/ai-efficiency/.worktrees/agent-group-hermes-openclaw-config-spec` unless the user explicitly chooses another location.
- If `frontend/node_modules` is missing in this worktree, install dependencies with `cd frontend && npm install` before running tests. Do not commit generated dependency directories.
- Do not use real user emails, API keys, provider URLs, group names, passwords, tokens, or company domains in tests. Use `example.com`, `alice@example.com`, `sk-*` fake keys, `Group Alpha`, and `Agent-*` test group names only.
