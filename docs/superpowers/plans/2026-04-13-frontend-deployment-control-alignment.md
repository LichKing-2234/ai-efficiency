# Frontend Deployment Control Alignment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Align the settings-page deployment controls with the approved `sub2api`-style interaction model while preserving `ai-efficiency`'s mode-aware backend behavior.

**Architecture:** Keep deployment controls in `frontend/src/views/SettingsView.vue` and centralize page-recovery logic in a focused utility module. The frontend will not introduce a global deployment store or background phase polling; it will only probe `/health` after actions that are expected to make the service briefly unavailable, and it will keep router-level chunk reload protection.

**Tech Stack:** Vue 3, Vue Router, Vitest, Vue Test Utils, TypeScript, Vite

**Status:** ✅ 已完成（2026-04-15）

**Replay Status:** 历史完成记录。不要直接按本文逐 task 重跑；如需再次执行或扩展，请基于当前前端实现、router/recovery 辅助模块和最新 spec 重写执行计划。

> **Updated:** 2026-04-15 — 基于 focused deployment/frontend tests 与当前实现回填 checkbox。

---

## File Structure

- `frontend/src/views/SettingsView.vue`
  Responsibility: the only deployment control surface; it fetches deployment status once on page load, lets admins trigger check/apply/rollback/restart, and delegates recovery probing to a helper.
- `frontend/src/utils/deploymentRecovery.ts`
  Responsibility: shared frontend recovery primitives for `/health` polling and one-shot chunk reload protection.
- `frontend/src/router/index.ts`
  Responsibility: route error handling; it should only invoke the chunk reload helper and log non-chunk errors.
- `frontend/src/__tests__/deployment-recovery.test.ts`
  Responsibility: unit coverage for recovery polling and chunk reload protection.
- `frontend/src/__tests__/settings-view.test.ts`
  Responsibility: settings page behavior coverage for compose vs systemd action branching.
- `frontend/src/__tests__/router.test.ts`
  Responsibility: existing router guard coverage, plus safety against the new error hook breaking the router import.
- `frontend/vitest.setup.ts`
  Responsibility: normalize `localStorage` and `sessionStorage` in the Vitest/jsdom environment so browser-dependent tests execute reliably.
- `frontend/vite.config.ts`
  Responsibility: register the Vitest setup file.

### Task 1: Stabilize the frontend test environment

**Files:**
- Create: `frontend/vitest.setup.ts`
- Modify: `frontend/vite.config.ts:24-27`
- Test: `frontend/src/__tests__/auth-store.test.ts`

- [x] **Step 1: Write the failing test command**

Run:

```bash
cd frontend && pnpm test auth-store.test.ts
```

Expected: FAIL with `TypeError: localStorage.clear is not a function` in `src/__tests__/auth-store.test.ts`.

- [x] **Step 2: Add a deterministic in-memory storage setup**

Create `frontend/vitest.setup.ts` with:

```ts
function createMemoryStorage(): Storage {
  const data = new Map<string, string>()

  return {
    get length() {
      return data.size
    },
    clear() {
      data.clear()
    },
    getItem(key: string) {
      return data.has(key) ? data.get(key)! : null
    },
    key(index: number) {
      return Array.from(data.keys())[index] ?? null
    },
    removeItem(key: string) {
      data.delete(key)
    },
    setItem(key: string, value: string) {
      data.set(key, String(value))
    },
  }
}

const local = createMemoryStorage()
const session = createMemoryStorage()

Object.defineProperty(globalThis, 'localStorage', {
  value: local,
  configurable: true,
})

Object.defineProperty(globalThis, 'sessionStorage', {
  value: session,
  configurable: true,
})

if (typeof window !== 'undefined') {
  Object.defineProperty(window, 'localStorage', {
    value: local,
    configurable: true,
  })

  Object.defineProperty(window, 'sessionStorage', {
    value: session,
    configurable: true,
  })
}
```

- [x] **Step 3: Register the setup file in Vitest**

Update the `test` block in `frontend/vite.config.ts` to:

```ts
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./vitest.setup.ts'],
  },
```

- [x] **Step 4: Run the storage verification test**

Run:

```bash
cd frontend && pnpm test auth-store.test.ts
```

Expected: PASS with all auth store tests green.

- [x] **Step 5: Commit**

```bash
git add frontend/vitest.setup.ts frontend/vite.config.ts
git commit -m "test(frontend): stabilize browser storage in vitest"
```

### Task 2: Build and verify the deployment recovery helper

**Files:**
- Create: `frontend/src/utils/deploymentRecovery.ts`
- Create: `frontend/src/__tests__/deployment-recovery.test.ts`

- [x] **Step 1: Write the failing helper tests**

Create `frontend/src/__tests__/deployment-recovery.test.ts` with:

```ts
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { isChunkLoadError, reloadOnceForChunkError, waitForServiceRecovery } from '@/utils/deploymentRecovery'

describe('deploymentRecovery', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('waits for /health to recover and then reloads', async () => {
    const fetchImpl = vi.fn()
      .mockRejectedValueOnce(new Error('down'))
      .mockResolvedValueOnce({ ok: true })
    const reload = vi.fn()

    const promise = waitForServiceRecovery({
      fetchImpl,
      reload,
      retryDelayMs: 1000,
      maxRetries: 3,
    })

    await vi.runAllTimersAsync()
    await promise

    expect(fetchImpl).toHaveBeenCalledTimes(2)
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('reloads anyway after retries are exhausted', async () => {
    const fetchImpl = vi.fn().mockRejectedValue(new Error('still down'))
    const reload = vi.fn()

    const promise = waitForServiceRecovery({
      fetchImpl,
      reload,
      retryDelayMs: 1000,
      maxRetries: 3,
    })

    await vi.runAllTimersAsync()
    await promise

    expect(fetchImpl).toHaveBeenCalledTimes(3)
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('detects dynamic import chunk failures', () => {
    expect(isChunkLoadError(new Error('Loading chunk 12 failed'))).toBe(true)
    expect(isChunkLoadError(new Error('Loading CSS chunk 7 failed'))).toBe(true)
    expect(isChunkLoadError(new Error('boom'))).toBe(false)
  })

  it('reloads only once within the chunk reload window', () => {
    const reload = vi.fn()
    const error = new Error('Loading chunk 12 failed')

    expect(reloadOnceForChunkError(error, { reload, now: () => 1000 })).toBe(true)
    expect(reloadOnceForChunkError(error, { reload, now: () => 2000 })).toBe(false)
    expect(reloadOnceForChunkError(error, { reload, now: () => 12001 })).toBe(true)
    expect(reload).toHaveBeenCalledTimes(2)
  })
})
```

- [x] **Step 2: Run the helper test to verify it fails**

Run:

```bash
cd frontend && pnpm test deployment-recovery.test.ts
```

Expected: FAIL because `@/utils/deploymentRecovery` does not exist yet.

- [x] **Step 3: Implement the minimal helper**

Create `frontend/src/utils/deploymentRecovery.ts` with:

```ts
const CHUNK_RELOAD_KEY = 'chunk_reload_attempted'
const CHUNK_RELOAD_WINDOW_MS = 10000

type FetchLike = (input: RequestInfo | URL, init?: RequestInit) => Promise<{ ok: boolean }>

interface RecoveryOptions {
  fetchImpl?: FetchLike
  reload?: () => void
  healthUrl?: string
  retryDelayMs?: number
  maxRetries?: number
}

interface ChunkReloadOptions {
  reload?: () => void
  now?: () => number
  storage?: Pick<Storage, 'getItem' | 'setItem'>
}

function sleep(ms: number) {
  return new Promise<void>((resolve) => {
    window.setTimeout(resolve, ms)
  })
}

export function isChunkLoadError(error: unknown): boolean {
  const err = error as { message?: string; name?: string }
  const message = typeof err?.message === 'string' ? err.message : String(error ?? '')
  return (
    message.includes('Failed to fetch dynamically imported module') ||
    message.includes('Loading chunk') ||
    message.includes('Loading CSS chunk') ||
    err?.name === 'ChunkLoadError'
  )
}

export async function waitForServiceRecovery(options: RecoveryOptions = {}) {
  const fetchImpl = options.fetchImpl ?? (fetch as FetchLike)
  const reload = options.reload ?? (() => window.location.reload())
  const healthUrl = options.healthUrl ?? '/health'
  const retryDelayMs = options.retryDelayMs ?? 1000
  const maxRetries = options.maxRetries ?? 5

  for (let attempt = 0; attempt < maxRetries; attempt += 1) {
    try {
      const response = await fetchImpl(healthUrl, {
        method: 'GET',
        cache: 'no-cache',
      })
      if (response.ok) {
        reload()
        return
      }
    } catch {
      // Service is still restarting.
    }

    if (attempt < maxRetries - 1) {
      await sleep(retryDelayMs)
    }
  }

  reload()
}

export function reloadOnceForChunkError(error: unknown, options: ChunkReloadOptions = {}) {
  if (!isChunkLoadError(error)) {
    return false
  }

  const storage = options.storage ?? sessionStorage
  const now = options.now ?? (() => Date.now())
  const reload = options.reload ?? (() => window.location.reload())
  const lastReload = storage.getItem(CHUNK_RELOAD_KEY)
  const currentTime = now()

  if (!lastReload || currentTime - Number.parseInt(lastReload, 10) > CHUNK_RELOAD_WINDOW_MS) {
    storage.setItem(CHUNK_RELOAD_KEY, String(currentTime))
    reload()
    return true
  }

  return false
}
```

- [x] **Step 4: Run the helper tests again**

Run:

```bash
cd frontend && pnpm test deployment-recovery.test.ts
```

Expected: PASS with all recovery helper tests green.

- [x] **Step 5: Commit**

```bash
git add frontend/src/utils/deploymentRecovery.ts frontend/src/__tests__/deployment-recovery.test.ts
git commit -m "fix(frontend): add deployment recovery helpers"
```

### Task 3: Wire settings-page deployment controls to the approved mode-aware behavior

**Files:**
- Modify: `frontend/src/views/SettingsView.vue:1-320`
- Modify: `frontend/src/__tests__/settings-view.test.ts`

- [x] **Step 1: Write the failing settings-page branching tests**

Add this mock near the top of `frontend/src/__tests__/settings-view.test.ts`:

```ts
vi.mock('@/utils/deploymentRecovery', () => ({
  waitForServiceRecovery: vi.fn().mockResolvedValue(undefined),
}))
```

Add these tests:

```ts
it('calls restart deployment when restart control is clicked', async () => {
  const { restartDeployment } = await import('@/api/deployment')
  const { waitForServiceRecovery } = await import('@/utils/deploymentRecovery')
  ;(restartDeployment as any).mockResolvedValue({ data: { data: { phase: 'restart_requested' } } })

  const wrapper = await mountSettings({
    deploymentStatus: {
      version: { version: 'v0.4.0', commit: 'abc1234', build_time: '2026-04-08T12:00:00Z' },
      mode: 'systemd',
      update_available: true,
      latest_release: { version: 'v0.5.0', url: 'https://example.com/v0.5.0' },
      update_status: { phase: 'idle' },
    },
  })

  const button = wrapper.findAll('button').find((b) => b.text().includes('Restart Service'))
  await button!.trigger('click')
  await flushPromises()

  expect(restartDeployment).toHaveBeenCalled()
  expect(waitForServiceRecovery).toHaveBeenCalled()
})

it('waits for recovery after bundled apply update', async () => {
  const { applyUpdate } = await import('@/api/deployment')
  const { waitForServiceRecovery } = await import('@/utils/deploymentRecovery')
  ;(applyUpdate as any).mockResolvedValue({ data: { data: { phase: 'updating' } } })

  const wrapper = await mountSettings({
    deploymentStatus: {
      version: { version: 'v0.4.0', commit: 'abc1234', build_time: '2026-04-08T12:00:00Z' },
      mode: 'bundled',
      update_available: true,
      latest_release: { version: 'v0.5.0', url: 'https://example.com/v0.5.0' },
      update_status: { phase: 'idle' },
    },
  })

  const button = wrapper.findAll('button').find((b) => b.text().includes('Apply Update'))
  await button!.trigger('click')
  await flushPromises()

  expect(applyUpdate).toHaveBeenCalledWith({ target_version: 'v0.5.0' })
  expect(waitForServiceRecovery).toHaveBeenCalled()
})

it('does not wait for recovery after systemd apply update', async () => {
  const { applyUpdate } = await import('@/api/deployment')
  const { waitForServiceRecovery } = await import('@/utils/deploymentRecovery')
  ;(applyUpdate as any).mockResolvedValue({ data: { data: { phase: 'updated' } } })

  const wrapper = await mountSettings({
    deploymentStatus: {
      version: { version: 'v0.4.0', commit: 'abc1234', build_time: '2026-04-08T12:00:00Z' },
      mode: 'systemd',
      update_available: true,
      latest_release: { version: 'v0.5.0', url: 'https://example.com/v0.5.0' },
      update_status: { phase: 'idle' },
    },
  })

  const button = wrapper.findAll('button').find((b) => b.text().includes('Apply Update'))
  await button!.trigger('click')
  await flushPromises()

  expect(applyUpdate).toHaveBeenCalledWith({ target_version: 'v0.5.0' })
  expect(waitForServiceRecovery).not.toHaveBeenCalled()
})
```

- [x] **Step 2: Run the settings test to verify it fails**

Run:

```bash
cd frontend && pnpm test settings-view.test.ts
```

Expected: FAIL because the settings page does not yet branch recovery behavior through the helper.

- [x] **Step 3: Implement the minimal settings-page wiring**

Update the deployment section of `frontend/src/views/SettingsView.vue` with:

```ts
import { waitForServiceRecovery } from '@/utils/deploymentRecovery'

function shouldWaitForRecovery(action: 'apply' | 'rollback' | 'restart') {
  if (!deployment.value) return false
  if (action === 'restart') return true
  return deployment.value.mode !== 'systemd'
}

async function handleApplyUpdate() {
  const targetVersion = deployment.value?.latest_release?.version?.trim()
  if (!targetVersion) {
    setDeploymentMessage('error', 'No target version available')
    return
  }

  deploymentActionLoading.value = true
  deploymentMessage.value = ''
  deploymentMessageKind.value = ''
  try {
    const res = await applyUpdate({ target_version: targetVersion })
    applyDeploymentUpdateStatus(res.data.data ?? { phase: 'unknown' })
    setDeploymentMessage('success', 'Update request submitted')
    if (shouldWaitForRecovery('apply')) {
      setDeploymentMessage('success', 'Update submitted. Waiting for service recovery...')
      await waitForServiceRecovery()
    }
  } catch (e: any) {
    setDeploymentMessage('error', e.response?.data?.message || 'Failed to apply update')
  } finally {
    deploymentActionLoading.value = false
  }
}

async function handleRollbackUpdate() {
  deploymentActionLoading.value = true
  deploymentMessage.value = ''
  deploymentMessageKind.value = ''
  try {
    const res = await rollbackUpdate()
    applyDeploymentUpdateStatus(res.data.data ?? { phase: 'unknown' })
    setDeploymentMessage('success', 'Rollback request submitted')
    if (shouldWaitForRecovery('rollback')) {
      setDeploymentMessage('success', 'Rollback submitted. Waiting for service recovery...')
      await waitForServiceRecovery()
    }
  } catch (e: any) {
    setDeploymentMessage('error', e.response?.data?.message || 'Failed to rollback update')
  } finally {
    deploymentActionLoading.value = false
  }
}

async function handleRestartDeployment() {
  deploymentActionLoading.value = true
  deploymentMessage.value = ''
  deploymentMessageKind.value = ''
  try {
    const res = await restartDeployment()
    applyDeploymentUpdateStatus(res.data.data ?? { phase: 'restart_requested' })
    setDeploymentMessage('success', 'Restart request submitted')
    if (shouldWaitForRecovery('restart')) {
      setDeploymentMessage('success', 'Restart requested. Waiting for service recovery...')
      await waitForServiceRecovery()
    }
  } catch (e: any) {
    setDeploymentMessage('error', e.response?.data?.message || 'Failed to restart service')
  } finally {
    deploymentActionLoading.value = false
  }
}
```

- [x] **Step 4: Run the settings-page test suite**

Run:

```bash
cd frontend && pnpm test settings-view.test.ts
```

Expected: PASS with all settings view tests green.

- [x] **Step 5: Commit**

```bash
git add frontend/src/views/SettingsView.vue frontend/src/__tests__/settings-view.test.ts
git commit -m "fix(frontend): align settings deployment controls"
```

### Task 4: Preserve router-level bundle upgrade safety

**Files:**
- Modify: `frontend/src/router/index.ts:1-90`
- Test: `frontend/src/__tests__/router.test.ts`

- [x] **Step 1: Write the router-level smoke verification**

Run:

```bash
cd frontend && pnpm test router.test.ts
```

Expected: PASS before and after the router change; this suite protects the router import and guard wiring.

- [x] **Step 2: Add the chunk reload hook**

Update `frontend/src/router/index.ts` to:

```ts
import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { reloadOnceForChunkError } from '@/utils/deploymentRecovery'

// existing router config and beforeEach guard

router.onError((error) => {
  if (reloadOnceForChunkError(error)) {
    return
  }
  console.error('Router error:', error)
})

export default router
```

- [x] **Step 3: Run the router smoke test again**

Run:

```bash
cd frontend && pnpm test router.test.ts
```

Expected: PASS with all router tests green.

- [x] **Step 4: Run the focused deployment-related tests together**

Run:

```bash
cd frontend && pnpm test deployment-recovery.test.ts settings-view.test.ts router.test.ts
```

Expected: PASS

- [x] **Step 5: Commit**

```bash
git add frontend/src/router/index.ts
git commit -m "fix(frontend): reload once after upgraded chunks expire"
```

### Task 5: Final verification and documentation sync

**Files:**
- Modify: `docs/superpowers/specs/2026-04-13-frontend-deployment-control-alignment-design.md`
- Modify: `docs/architecture.md` (only if implementation changed the project-level architecture wording; otherwise no change)

- [x] **Step 1: Re-read the approved spec against the implementation**

Checklist:

```text
- Settings page is still the only deployment control entry
- No global deployment store was added
- No automatic phase polling was added
- Compose apply/rollback wait for /health recovery
- Systemd apply/rollback do not wait for /health recovery
- Restart waits for /health recovery
- Router keeps chunk reload protection
```

- [x] **Step 2: Run the full frontend verification**

Run:

```bash
cd frontend && pnpm test
cd frontend && pnpm build
```

Expected:

```text
All frontend tests pass
Production build succeeds
```

- [x] **Step 3: Update docs only if the code diverged from the approved spec**

If no divergence is found, leave `docs/architecture.md` unchanged and keep the new spec as the contract. If divergence is found, fix the spec inline before finishing.

- [x] **Step 4: Commit**

```bash
git add docs/superpowers/specs/2026-04-13-frontend-deployment-control-alignment-design.md docs/superpowers/plans/2026-04-13-frontend-deployment-control-alignment.md
git commit -m "docs(frontend): record deployment control alignment"
```
