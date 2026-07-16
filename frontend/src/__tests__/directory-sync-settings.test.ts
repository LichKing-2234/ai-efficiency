import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import DirectorySyncSettings from '@/components/settings/DirectorySyncSettings.vue'
import { setLocale } from '@/i18n'
import { resetToastsForTest, useToast } from '@/composables/useToast'
import { useWorkItemsStore } from '@/stores/workItems'

vi.mock('@/api/directory', () => ({
  listDirectorySources: vi.fn(),
  createDirectorySource: vi.fn(),
  updateDirectorySource: vi.fn(),
  validateDirectorySource: vi.fn(),
  listDirectoryRuns: vi.fn(),
  previewDirectorySource: vi.fn(),
  startDirectoryRun: vi.fn(),
  getDirectoryRun: vi.fn(),
}))

vi.mock('@/api/workItems', () => ({
  getWorkItemCounts: vi.fn(),
}))

Object.assign(navigator, {
  clipboard: {
    writeText: vi.fn(),
  },
})

function warnings(code: string, count: number) {
  return Array.from({ length: count }, () => ({ code, message: code, step_id: 'members' }))
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((promiseResolve, promiseReject) => {
    resolve = promiseResolve
    reject = promiseReject
  })
  return { promise, resolve, reject }
}

function runSummary(overrides: Record<string, unknown> = {}) {
  return {
    id: 100,
    source_id: 1,
    mode: 'apply',
    trigger: 'manual',
    status: 'completed',
    phase: 'completed',
    started_at: '2026-07-15T01:00:00Z',
    completed_at: '2026-07-15T01:01:00Z',
    http_request_count: 2,
    department_count: 4,
    member_count: 9,
    invalid_member_count: 0,
    warning_count: 0,
    ...overrides,
  }
}

function runPage(items: any[] = [], overrides: Record<string, unknown> = {}) {
  return {
    items,
    total: items.length,
    page: 0,
    page_size: 20,
    latest_active_run: null,
    ...overrides,
  }
}

function apiResponse(data: unknown) {
  return { data: { data } }
}

function countsResponse(total: number) {
  return {
    data: {
      data: {
        quota_reset_approval_count: 0,
        quota_reset_admin_count: 0,
        ai_access_setup_count: 0,
        offboarding_count: total,
        total_count: total,
      },
    },
  }
}

async function mountDirectorySyncSettings(configureMocks?: (api: any) => void) {
  const api = await import('@/api/directory') as any
  const workItemsApi = await import('@/api/workItems') as any
  api.listDirectorySources.mockResolvedValue({
    data: {
      data: {
        items: [
          {
            id: 1,
            name: 'Example Directory',
            description: 'Synthetic directory source',
            scope: 'full_company',
            enabled: true,
            dsl: 'version: 1\nscope: full_company\n',
            schedule_enabled: false,
            schedule_interval: 'daily',
            schedule_timezone: 'UTC',
          },
        ],
      },
    },
  })
  api.createDirectorySource.mockResolvedValue({ data: { data: { id: 2, name: 'New Directory' } } })
  api.updateDirectorySource.mockResolvedValue({ data: { data: { id: 1, name: 'Example Directory' } } })
  api.validateDirectorySource.mockResolvedValue({ data: { data: { valid: true, issues: [] } } })
  api.listDirectoryRuns.mockResolvedValue(apiResponse(runPage()))
  api.previewDirectorySource.mockResolvedValue({ data: { data: { id: 10, mode: 'preview', status: 'completed' } } })
  api.startDirectoryRun.mockResolvedValue({ data: { data: { id: 11, mode: 'apply', status: 'completed' } } })
  api.getDirectoryRun.mockResolvedValue({ data: { data: { id: 10, mode: 'preview', status: 'completed' } } })
  workItemsApi.getWorkItemCounts.mockResolvedValue(countsResponse(0))
  configureMocks?.(api)

  const pinia = createPinia()
  setActivePinia(pinia)
  const wrapper = mount(DirectorySyncSettings, {
    global: { plugins: [pinia] },
  })
  await flushPromises()
  return { wrapper, api, workItems: useWorkItemsStore(pinia) }
}

describe('DirectorySyncSettings', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    resetToastsForTest()
    setLocale('en-US')
  })

  it('renders safe templates and copies an AI prompt with safety guidance', async () => {
    const { wrapper } = await mountDirectorySyncSettings()

    expect(wrapper.text()).toContain('Directory Sync')
    expect(wrapper.text()).toContain('Departments then members')
    expect(wrapper.text()).toContain('Single members endpoint')
    expect(wrapper.text()).toContain('Paged members endpoint')
    expect(wrapper.text()).toContain('API documentation or endpoint notes')
    expect(wrapper.text()).toContain('If there is no API documentation, provide these interfaces one by one')
    expect(wrapper.text()).toContain('directory.example.com')

    await wrapper.get('[data-testid="directory-ai-context"]').setValue('GET /departments returns data.departments; GET /users returns data.users.')
    await wrapper.get('[data-testid="directory-copy-ai-prompt"]').trigger('click')

    expect(navigator.clipboard.writeText).toHaveBeenCalled()
    expect(useToast().toast.message).toBe('AI prompt copied')
    const prompt = (navigator.clipboard.writeText as any).mock.calls[0][0]
    expect(prompt).toContain('Target YAML contract')
    expect(prompt).toContain('Normalized output structures')
    expect(prompt).toContain('Use steps[].id')
    expect(prompt).toContain('do not use steps[].name')
    expect(prompt).toContain('department.external_id')
    expect(prompt).toContain('member.email')
    expect(prompt).toContain('Choose member.external_id from a stable non-empty field')
    expect(prompt).toContain('email has complete non-empty coverage')
    expect(prompt).toContain('summarize endpoint, method, root type, item count, field paths, email coverage, and pagination signals')
    expect(prompt).toContain('Do not paste raw response rows')
    expect(prompt).toContain('If you can read docs or call endpoints with tools, do so in read-only mode')
    expect(prompt).toContain('If pagination is not visible in the response body or headers, do not invent it')
    expect(prompt).toContain('Do not output YAML until the required fields are known')
    expect(prompt).toContain('Preserve YAML indentation')
    expect(prompt).toContain('do not flatten nested keys')
    expect(prompt).toContain('Use extract.items: $ only when the response root is the array')
    expect(prompt).toContain('GET /departments returns data.departments')
    expect(prompt).toContain('Use provided production endpoint URLs, field names, and non-secret header names in the final YAML')
    expect(prompt).toContain('Do not include API keys, bearer tokens, passwords')
    expect(prompt).not.toContain('real company domains')
    expect(prompt).not.toContain('real internal URLs')
    expect(prompt).toContain('directory.example.com')
  })

  it('copies a step-by-step prompt when API documentation is missing', async () => {
    const { wrapper } = await mountDirectorySyncSettings()

    await wrapper.get('[data-testid="directory-copy-ai-prompt"]').trigger('click')

    const prompt = (navigator.clipboard.writeText as any).mock.calls[0][0]
    expect(prompt).toContain('No API documentation was provided')
    expect(prompt).toContain('Ask the configurator for each interface in this order')
    expect(prompt).toContain('1. Department list endpoint')
    expect(prompt).toContain('2. Member list endpoint')
    expect(prompt).toContain('3. Pagination')
  })

  it('shows toast feedback when copying the AI prompt fails', async () => {
    ;(navigator.clipboard.writeText as any).mockRejectedValueOnce(new Error('clipboard unavailable'))
    const { wrapper } = await mountDirectorySyncSettings()

    await wrapper.get('[data-testid="directory-copy-ai-prompt"]').trigger('click')
    await flushPromises()

    expect(useToast().toast.message).toBe('Copy failed')
    expect(useToast().toast.tone).toBe('error')
  })

  it('shows validation issue details', async () => {
    const { wrapper, api } = await mountDirectorySyncSettings()
    api.validateDirectorySource.mockResolvedValueOnce({
      data: {
        data: {
          valid: false,
          issues: [
            { path: 'auth.type', message: 'auth.type must be header' },
            { path: 'steps[0].request.url', message: 'url host is required' },
          ],
        },
      },
    })

    await wrapper.get('[data-testid="directory-validate"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Validation found 2 issue(s)')
    expect(wrapper.text()).toContain('auth.type')
    expect(wrapper.text()).toContain('auth.type must be header')
    expect(wrapper.text()).toContain('steps[0].request.url')
    expect(wrapper.text()).toContain('url host is required')
  })

  it('shows the credential ref from the current DSL', async () => {
    const { wrapper } = await mountDirectorySyncSettings()

    await wrapper.get('[data-testid="directory-dsl"]').setValue(`version: 1
scope: full_company
auth:
  type: header
  header: X-Directory-API-Key
  credential_ref: custom_directory_key
`)

    expect(wrapper.text()).toContain('Credential ref: custom_directory_key')
  })

  it('shows preview and apply run failure details', async () => {
    const workItemsApi = await import('@/api/workItems') as any
    const { wrapper, api } = await mountDirectorySyncSettings()
    api.previewDirectorySource.mockResolvedValueOnce({
      data: { data: { id: 20, mode: 'preview', status: 'failed', error_message: 'steps[0].request.url: url host is required' } },
    })
    api.startDirectoryRun.mockResolvedValueOnce({
      data: { data: { id: 21, mode: 'apply', status: 'failed', error_message: 'steps[0].map: department or member mapping is required' } },
    })

    await wrapper.get('[data-testid="directory-preview"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Preview run failed')
    expect(wrapper.text()).toContain('steps[0].request.url: url host is required')

    await wrapper.get('[data-testid="directory-run-now"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Apply run failed')
    expect(wrapper.text()).toContain('steps[0].map: department or member mapping is required')
    expect(workItemsApi.getWorkItemCounts).not.toHaveBeenCalled()
  })

  it('shows completed preview and run counts when the backend returns warnings', async () => {
    const { wrapper, api } = await mountDirectorySyncSettings()
    api.previewDirectorySource.mockResolvedValueOnce({
      data: {
        data: {
          id: 30,
          mode: 'preview',
          status: 'completed_with_warnings',
          department_count: 184,
          member_count: 631,
          warning_count: 3759,
          warnings: [...warnings('duplicate_member_email', 3758), ...warnings('invalid_member_email', 1)],
        },
      },
    })
    api.startDirectoryRun.mockResolvedValueOnce({
      data: {
        data: {
          id: 31,
          mode: 'apply',
          status: 'completed_with_warnings',
          department_count: 184,
          member_count: 631,
          warning_count: 3759,
          warnings: [...warnings('duplicate_member_email', 3758), ...warnings('invalid_member_email', 1)],
        },
      },
    })

    await wrapper.get('[data-testid="directory-preview"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Preview completed: kept 631 valid members, skipped 3759 records; 184 departments.')
    expect(wrapper.text()).toContain('Skipped record reasons')
    expect(wrapper.text()).toContain('Duplicate email: 3758 records')
    expect(wrapper.text()).toContain('Missing or invalid email: 1 record')

    await wrapper.get('[data-testid="directory-run-now"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Run completed: kept 631 valid members, skipped 3759 records; 184 departments.')
    expect(wrapper.text()).toContain('Duplicate email: 3758 records')
  })

  it('shows localized completed-with-warnings run counts in Chinese', async () => {
    setLocale('zh-CN')
    const { wrapper, api } = await mountDirectorySyncSettings()
    api.previewDirectorySource.mockResolvedValueOnce({
      data: {
        data: {
          id: 32,
          mode: 'preview',
          status: 'completed_with_warnings',
          department_count: 184,
          member_count: 631,
          warning_count: 3759,
          warnings: [...warnings('duplicate_member_email', 3758), ...warnings('invalid_member_email', 1)],
        },
      },
    })
    api.startDirectoryRun.mockResolvedValueOnce({
      data: {
        data: {
          id: 33,
          mode: 'apply',
          status: 'completed_with_warnings',
          department_count: 184,
          member_count: 631,
          warning_count: 3759,
          warnings: [...warnings('duplicate_member_email', 3758), ...warnings('invalid_member_email', 1)],
        },
      },
    })

    await wrapper.get('[data-testid="directory-preview"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('预览已完成：已保留 631 个有效成员，跳过 3759 条记录；部门 184 个。')
    expect(wrapper.text()).toContain('跳过原因')
    expect(wrapper.text()).toContain('重复邮箱：3758 条')
    expect(wrapper.text()).toContain('缺失或无效邮箱：1 条')
    expect(wrapper.text()).toContain('通常表示同一成员从多个部门结果重复返回')

    await wrapper.get('[data-testid="directory-run-now"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('运行已完成：已保留 631 个有效成员，跳过 3759 条记录；部门 184 个。')
    expect(wrapper.text()).toContain('重复邮箱：3758 条')
  })

  it('loads bounded run summaries and navigates with limit and offset metadata', async () => {
    const firstPage = runPage([
      runSummary({ id: 220, member_count: 20 }),
      runSummary({ id: 219, mode: 'preview', member_count: 19 }),
    ], { total: 41, page: 0 })
    const secondPage = runPage([
      runSummary({ id: 200, member_count: 8 }),
    ], { total: 41, page: 1 })
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns
        .mockResolvedValueOnce(apiResponse(firstPage))
        .mockResolvedValueOnce(apiResponse(secondPage))
        .mockResolvedValueOnce(apiResponse(firstPage))
    })

    expect(api.listDirectoryRuns).toHaveBeenNthCalledWith(1, 1, { limit: 20, offset: 0 })
    expect(wrapper.get('[data-testid="directory-run-row-220"]').text()).toContain('#220')
    expect(wrapper.get('[data-testid="directory-run-row-219"]').text()).toContain('#219')
    expect(wrapper.text()).not.toContain('#200')
    expect(wrapper.get('[data-testid="directory-run-page-meta"]').text()).toContain('Page 1 of 3')

    await wrapper.get('[data-testid="directory-run-next"]').trigger('click')
    await flushPromises()

    expect(api.listDirectoryRuns).toHaveBeenNthCalledWith(2, 1, { limit: 20, offset: 20 })
    expect(wrapper.get('[data-testid="directory-run-row-200"]').text()).toContain('#200')
    expect(wrapper.text()).not.toContain('#220')
    expect(wrapper.get('[data-testid="directory-run-page-meta"]').text()).toContain('Page 2 of 3')

    await wrapper.get('[data-testid="directory-run-prev"]').trigger('click')
    await flushPromises()

    expect(api.listDirectoryRuns).toHaveBeenNthCalledWith(3, 1, { limit: 20, offset: 0 })
    expect(wrapper.get('[data-testid="directory-run-page-meta"]').text()).toContain('Page 1 of 3')
  })

  it('formats run timestamps with the reactive selected locale', async () => {
    const startedAt = '2026-07-15T01:00:00Z'
    const summary = runSummary({ id: 221, started_at: startedAt })
    const { wrapper } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns.mockResolvedValueOnce(apiResponse(runPage([summary])))
    })
    const row = wrapper.get(`[data-testid="directory-run-row-${summary.id}"]`)
    const englishTimestamp = new Date(startedAt).toLocaleString('en-US')
    const chineseTimestamp = new Date(startedAt).toLocaleString('zh-CN')

    expect(englishTimestamp).not.toBe(chineseTimestamp)
    expect(row.text()).toContain(englishTimestamp)

    setLocale('zh-CN')
    await wrapper.vm.$nextTick()

    expect(row.text()).toContain(chineseTimestamp)
    expect(row.text()).not.toContain(englishTimestamp)
  })

  it('loads complete diagnostics once for a selected terminal summary without polling it', async () => {
    vi.useFakeTimers()
    const selected = runSummary({ id: 230, mode: 'preview', status: 'failed', phase: 'failed', warning_count: 1 })
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns.mockResolvedValueOnce(apiResponse(runPage([selected])))
      api.getDirectoryRun.mockResolvedValueOnce(apiResponse({
        ...selected,
        warnings: [{ code: 'synthetic_warning', message: 'warning-detail-marker', step_id: 'members' }],
        summary: { marker: 'summary-detail-marker' },
        preview_diff: { marker: 'diff-detail-marker' },
        error_message: 'error-detail-marker',
      }))
    })

    try {
      await wrapper.get('[data-testid="directory-run-row-230"]').trigger('click')
      await flushPromises()

      expect(api.getDirectoryRun).toHaveBeenCalledTimes(1)
      expect(api.getDirectoryRun).toHaveBeenCalledWith(230)
      expect(wrapper.get('[data-testid="directory-run-detail"]').text()).toContain('warning-detail-marker')
      expect(wrapper.get('[data-testid="directory-run-detail"]').text()).toContain('summary-detail-marker')
      expect(wrapper.get('[data-testid="directory-run-detail"]').text()).toContain('diff-detail-marker')
      expect(wrapper.get('[data-testid="directory-run-detail"]').text()).toContain('error-detail-marker')
      expect(vi.getTimerCount()).toBe(0)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('loads an older queued detail with omitted counts once and never polls that history selection', async () => {
    vi.useFakeTimers()
    const queued = runSummary({
      id: 231,
      status: 'queued',
      phase: 'validating',
      started_at: null,
      completed_at: null,
      http_request_count: 0,
      department_count: 0,
      member_count: 0,
      invalid_member_count: 0,
      warning_count: 0,
    })
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns.mockResolvedValueOnce(apiResponse(runPage([queued])))
      api.getDirectoryRun.mockResolvedValueOnce(apiResponse({
        id: 231,
        source_id: 1,
        mode: 'apply',
        trigger: 'manual',
        status: 'queued',
        phase: 'validating',
      }))
    })

    try {
      await wrapper.get('[data-testid="directory-run-row-231"]').trigger('click')
      await flushPromises()

      expect(api.getDirectoryRun).toHaveBeenCalledTimes(1)
      expect(wrapper.get('[data-testid="directory-run-detail"]').text()).toContain('#231')
      expect(vi.getTimerCount()).toBe(0)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('uses latest_active_run outside the page as the only recovery poll target', async () => {
    vi.useFakeTimers()
    const newerTerminal = runSummary({ id: 240, mode: 'preview', member_count: 40 })
    const active = runSummary({
      id: 239,
      mode: 'apply',
      status: 'running',
      phase: 'applying',
      completed_at: null,
      member_count: 39,
    })
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns
        .mockResolvedValueOnce(apiResponse(runPage([newerTerminal], { latest_active_run: active })))
        .mockResolvedValueOnce(apiResponse(runPage([runSummary({ id: 239, member_count: 39 })])))
      api.getDirectoryRun.mockResolvedValueOnce(apiResponse({ ...active, status: 'completed', phase: 'completed' }))
    })

    try {
      expect(wrapper.text()).toContain('Applying directory facts')
      expect(wrapper.text()).not.toContain('Preview completed: kept 40 valid members')

      await vi.runOnlyPendingTimersAsync()
      await flushPromises()

      expect(api.getDirectoryRun).toHaveBeenCalledTimes(1)
      expect(api.getDirectoryRun).toHaveBeenCalledWith(239)
      expect(api.getDirectoryRun).not.toHaveBeenCalledWith(240)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('serializes same-source page navigation while a request is pending', async () => {
    const slowPageZero = deferred<any>()
    const initialPage = runPage([runSummary({ id: 260 })], { total: 41, page: 0 })
    const pageOne = runPage([runSummary({ id: 240 })], { total: 41, page: 1 })
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns
        .mockResolvedValueOnce(apiResponse(initialPage))
        .mockResolvedValueOnce(apiResponse(pageOne))
        .mockImplementationOnce(() => slowPageZero.promise)
        .mockResolvedValueOnce(apiResponse(pageOne))
    })

    await wrapper.get('[data-testid="directory-run-next"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('#240')

    await wrapper.get('[data-testid="directory-run-prev"]').trigger('click')
    expect(wrapper.get('[data-testid="directory-run-next"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="directory-run-next"]').trigger('click')
    await flushPromises()
    expect(api.listDirectoryRuns).toHaveBeenCalledTimes(3)

    slowPageZero.resolve(apiResponse(runPage([runSummary({ id: 259 })], { total: 41, page: 0 })))
    await flushPromises()

    expect(wrapper.text()).toContain('#259')
    expect(wrapper.get('[data-testid="directory-run-page-meta"]').text()).toContain('Page 1 of 3')

    await wrapper.get('[data-testid="directory-run-next"]').trigger('click')
    await flushPromises()

    expect(api.listDirectoryRuns).toHaveBeenNthCalledWith(4, 1, { limit: 20, offset: 20 })
    expect(wrapper.text()).toContain('#240')
    expect(wrapper.text()).not.toContain('#259')
    expect(wrapper.get('[data-testid="directory-run-page-meta"]').text()).toContain('Page 2 of 3')
  })

  it('keeps detail B when a slower detail A resolves last', async () => {
    const detailA = deferred<any>()
    const detailB = deferred<any>()
    const runA = runSummary({ id: 270, mode: 'preview' })
    const runB = runSummary({ id: 269, mode: 'apply' })
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns.mockResolvedValueOnce(apiResponse(runPage([runA, runB])))
      api.getDirectoryRun
        .mockImplementationOnce(() => detailA.promise)
        .mockImplementationOnce(() => detailB.promise)
    })

    await wrapper.get('[data-testid="directory-run-row-270"]').trigger('click')
    await wrapper.get('[data-testid="directory-run-row-269"]').trigger('click')

    detailB.resolve(apiResponse({ ...runB, summary: { marker: 'detail-b-marker' } }))
    await flushPromises()
    expect(wrapper.get('[data-testid="directory-run-detail"]').text()).toContain('detail-b-marker')

    detailA.resolve(apiResponse({ ...runA, summary: { marker: 'detail-a-marker' } }))
    await flushPromises()
    expect(wrapper.get('[data-testid="directory-run-detail"]').text()).toContain('detail-b-marker')
    expect(wrapper.get('[data-testid="directory-run-detail"]').text()).not.toContain('detail-a-marker')
  })

  it('keeps active A as the only poll target while terminal B is selected and displayed', async () => {
    vi.useFakeTimers()
    const activeA = runSummary({
      id: 280,
      mode: 'apply',
      status: 'running',
      phase: 'applying',
      completed_at: null,
    })
    const terminalB = runSummary({ id: 279, mode: 'preview', status: 'completed_with_warnings', warning_count: 1 })
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns
        .mockResolvedValueOnce(apiResponse(runPage([terminalB], { latest_active_run: activeA })))
        .mockResolvedValueOnce(apiResponse(runPage([
          runSummary({ id: 280 }),
          terminalB,
        ])))
      api.getDirectoryRun.mockImplementation((id: number) => {
        if (id === 279) {
          return Promise.resolve(apiResponse({ ...terminalB, summary: { marker: 'terminal-b-detail' } }))
        }
        return Promise.resolve(apiResponse({
          ...activeA,
          status: 'completed',
          phase: 'completed',
          summary: { marker: 'active-a-detail' },
        }))
      })
    })

    try {
      await wrapper.get('[data-testid="directory-run-row-279"]').trigger('click')
      await flushPromises()
      expect(wrapper.get('[data-testid="directory-run-detail"]').text()).toContain('terminal-b-detail')

      await vi.runOnlyPendingTimersAsync()
      await flushPromises()

      expect(api.getDirectoryRun.mock.calls.map((call: any[]) => call[0])).toEqual([279, 280])
      expect(api.listDirectoryRuns).toHaveBeenNthCalledWith(2, 1, { limit: 20, offset: 0 })
      expect(wrapper.get('[data-testid="directory-run-detail"]').text()).toContain('terminal-b-detail')
      expect(wrapper.get('[data-testid="directory-run-detail"]').text()).not.toContain('active-a-detail')
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('keeps selected active run terminal when detail resolves before the terminal poll', async () => {
    vi.useFakeTimers()
    const detailA = deferred<any>()
    const pollA = deferred<any>()
    const activeA = runSummary({
      id: 281,
      mode: 'apply',
      status: 'running',
      phase: 'applying',
      completed_at: null,
      department_count: 4,
      member_count: 8,
    })
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns.mockResolvedValueOnce(apiResponse(runPage([activeA], { latest_active_run: activeA })))
      api.getDirectoryRun
        .mockImplementationOnce(() => detailA.promise)
        .mockImplementationOnce(() => pollA.promise)
    })

    try {
      await wrapper.get(`[data-testid="directory-run-row-${activeA.id}"]`).trigger('click')
      await vi.runOnlyPendingTimersAsync()
      expect(api.getDirectoryRun.mock.calls.map((call: any[]) => call[0])).toEqual([activeA.id, activeA.id])

      detailA.resolve(apiResponse({
        ...activeA,
        phase: 'executing',
        department_count: 5,
        member_count: 9,
        summary: { marker: 'running-detail-before-terminal' },
      }))
      await flushPromises()
      expect(wrapper.get('[data-testid="directory-run-detail"]').text()).toContain('running-detail-before-terminal')

      pollA.resolve(apiResponse({
        ...activeA,
        status: 'completed',
        phase: 'completed',
        completed_at: '2026-07-15T01:02:00Z',
        department_count: 23,
        member_count: 44,
        summary: { marker: 'terminal-poll-after-detail' },
      }))
      await flushPromises()

      const detail = wrapper.get('[data-testid="directory-run-detail"]')
      expect(detail.find('span').text()).toBe('Completed')
      expect(detail.text()).toContain('23 departments · 44 members')
      expect(detail.text()).toContain('terminal-poll-after-detail')
      expect(detail.text()).not.toContain('running-detail-before-terminal')
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('keeps selected active run terminal when selection starts during an in-flight poll', async () => {
    vi.useFakeTimers()
    const pollA = deferred<any>()
    const detailA = deferred<any>()
    const activeA = runSummary({
      id: 282,
      mode: 'preview',
      status: 'running',
      phase: 'normalizing',
      completed_at: null,
      department_count: 6,
      member_count: 12,
    })
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns.mockResolvedValueOnce(apiResponse(runPage([activeA], { latest_active_run: activeA })))
      api.getDirectoryRun
        .mockImplementationOnce(() => pollA.promise)
        .mockImplementationOnce(() => detailA.promise)
    })

    try {
      await vi.runOnlyPendingTimersAsync()
      expect(api.getDirectoryRun).toHaveBeenCalledWith(activeA.id)

      await wrapper.get(`[data-testid="directory-run-row-${activeA.id}"]`).trigger('click')
      expect(api.getDirectoryRun.mock.calls.map((call: any[]) => call[0])).toEqual([activeA.id, activeA.id])

      pollA.resolve(apiResponse({
        ...activeA,
        status: 'completed_with_warnings',
        phase: 'completed',
        completed_at: '2026-07-15T01:03:00Z',
        department_count: 31,
        member_count: 55,
        warning_count: 2,
        summary: { marker: 'terminal-poll-before-detail' },
      }))
      await flushPromises()

      let detail = wrapper.get('[data-testid="directory-run-detail"]')
      expect(detail.find('span').text()).toBe('Completed with warnings')
      expect(detail.text()).toContain('31 departments · 55 members · 2 skipped records')
      expect(detail.text()).toContain('terminal-poll-before-detail')

      detailA.resolve(apiResponse({
        ...activeA,
        phase: 'executing',
        department_count: 7,
        member_count: 13,
        summary: { marker: 'stale-running-detail-after-terminal' },
      }))
      await flushPromises()

      detail = wrapper.get('[data-testid="directory-run-detail"]')
      expect(detail.find('span').text()).toBe('Completed with warnings')
      expect(detail.text()).toContain('terminal-poll-before-detail')
      expect(detail.text()).not.toContain('stale-running-detail-after-terminal')
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('keeps the committed non-first page coherent when active A completes beside selected terminal B', async () => {
    vi.useFakeTimers()
    const workItemsApi = await import('@/api/workItems') as any
    const refreshCounts = deferred<any>()
    const activeA = runSummary({
      id: 285,
      mode: 'apply',
      status: 'running',
      phase: 'applying',
      completed_at: null,
      department_count: 12,
      member_count: 24,
    })
    const terminalB = runSummary({ id: 264, mode: 'preview', department_count: 4, member_count: 8 })
    const oldTerminal = runSummary({ id: 284, mode: 'apply', department_count: 2, member_count: 3 })
    const pageZero = runPage([oldTerminal], { total: 41, page: 0, latest_active_run: activeA })
    const pageOne = runPage([terminalB], { total: 41, page: 1, latest_active_run: activeA })
    const refreshedPageOne = runPage([terminalB], { total: 41, page: 1 })
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns.mockImplementation((_sourceID: number, params: { offset: number }) => {
        if (api.listDirectoryRuns.mock.calls.length === 1) return Promise.resolve(apiResponse(pageZero))
        if (params.offset === 20) {
          return Promise.resolve(apiResponse(
            api.listDirectoryRuns.mock.calls.length === 2 ? pageOne : refreshedPageOne,
          ))
        }
        return Promise.resolve(apiResponse(runPage([oldTerminal], { total: 41, page: 0 })))
      })
      api.getDirectoryRun.mockImplementation((id: number) => {
        if (id === terminalB.id) {
          return Promise.resolve(apiResponse({ ...terminalB, summary: { marker: 'terminal-b-page-detail' } }))
        }
        return Promise.resolve(apiResponse({
          ...activeA,
          status: 'completed',
          phase: 'completed',
          completed_at: '2026-07-15T01:02:00Z',
          department_count: 15,
          member_count: 31,
        }))
      })
    })
    workItemsApi.getWorkItemCounts.mockReset().mockReturnValueOnce(refreshCounts.promise)

    try {
      await wrapper.get('[data-testid="directory-run-next"]').trigger('click')
      await flushPromises()
      await wrapper.get(`[data-testid="directory-run-row-${terminalB.id}"]`).trigger('click')
      await flushPromises()

      expect(wrapper.get('[data-testid="directory-run-page-meta"]').text()).toContain('Page 2 of 3')
      expect(wrapper.get('[data-testid="directory-run-detail"]').text()).toContain('terminal-b-page-detail')

      await vi.runOnlyPendingTimersAsync()
      await flushPromises()

      expect(api.listDirectoryRuns).toHaveBeenCalledTimes(3)
      expect(api.listDirectoryRuns).toHaveBeenLastCalledWith(1, { limit: 20, offset: 20 })
      expect(wrapper.get('[data-testid="directory-run-page-meta"]').text()).toContain('Page 2 of 3')
      expect(wrapper.find(`[data-testid="directory-run-row-${terminalB.id}"]`).exists()).toBe(true)
      expect(wrapper.find(`[data-testid="directory-run-row-${oldTerminal.id}"]`).exists()).toBe(false)
      expect(wrapper.get('[data-testid="directory-run-detail"]').text()).toContain('terminal-b-page-detail')
      expect(wrapper.text()).toContain('Run completed: kept 31 valid members; 15 departments.')
      expect(wrapper.text()).not.toContain('Run completed: kept 8 valid members; 4 departments.')
      expect(wrapper.text()).not.toContain('Run completed: kept 3 valid members; 2 departments.')

      refreshCounts.resolve(countsResponse(0))
      await flushPromises()
      expect(api.listDirectoryRuns).toHaveBeenCalledTimes(3)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('keeps committed pagination atomic while a failed next page is pending and retryable', async () => {
    const failedPageOne = deferred<any>()
    const retryPageOne = deferred<any>()
    const pageZeroRun = runSummary({ id: 286, member_count: 20 })
    const pageOneRun = runSummary({ id: 266, member_count: 10 })
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns.mockImplementation((_sourceID: number, params: { offset: number }) => {
        if (params.offset === 0) {
          return Promise.resolve(apiResponse(runPage([pageZeroRun], { total: 61, page: 0 })))
        }
        if (params.offset === 20 && api.listDirectoryRuns.mock.calls.filter((call: any[]) => call[1].offset === 20).length === 1) {
          return failedPageOne.promise
        }
        if (params.offset === 20) return retryPageOne.promise
        return Promise.resolve(apiResponse(runPage([runSummary({ id: 246 })], { total: 61, page: 2 })))
      })
    })

    const next = wrapper.get('[data-testid="directory-run-next"]')
    await next.trigger('click')
    expect(next.attributes('disabled')).toBeDefined()
    await next.trigger('click')
    expect(api.listDirectoryRuns).toHaveBeenCalledTimes(2)

    failedPageOne.reject(new Error('synthetic page one failure'))
    await flushPromises()

    expect(wrapper.get('[data-testid="directory-run-page-meta"]').text()).toContain('Page 1 of 4')
    expect(wrapper.find(`[data-testid="directory-run-row-${pageZeroRun.id}"]`).exists()).toBe(true)
    expect(wrapper.get('[data-testid="directory-run-prev"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="directory-run-next"]').attributes('disabled')).toBeUndefined()

    await wrapper.get('[data-testid="directory-run-next"]').trigger('click')
    expect(api.listDirectoryRuns).toHaveBeenLastCalledWith(1, { limit: 20, offset: 20 })
    expect(wrapper.get('[data-testid="directory-run-next"]').attributes('disabled')).toBeDefined()

    retryPageOne.resolve(apiResponse(runPage([pageOneRun], { total: 61, page: 1 })))
    await flushPromises()

    expect(api.listDirectoryRuns.mock.calls.map((call: any[]) => call[1].offset)).toEqual([0, 20, 20])
    expect(wrapper.get('[data-testid="directory-run-page-meta"]').text()).toContain('Page 2 of 4')
    expect(wrapper.find(`[data-testid="directory-run-row-${pageOneRun.id}"]`).exists()).toBe(true)
    expect(wrapper.find(`[data-testid="directory-run-row-${pageZeroRun.id}"]`).exists()).toBe(false)
  })

  it.each([
    'preview' as const,
    'apply' as const,
  ])('keeps polling authoritative when a no-active page started during the $action POST resolves', async (action) => {
    vi.useFakeTimers()
    const actionResponse = deferred<any>()
    const ordinaryPage = deferred<any>()
    const initialRun = runSummary({ id: 421, member_count: 19 })
    const pageTwoRun = runSummary({ id: 401, member_count: 9 })
    const createdRun = runSummary({
      id: 422,
      mode: action,
      status: 'queued',
      phase: 'validating',
      started_at: null,
      completed_at: null,
      department_count: 0,
      member_count: 0,
    })
    const runningCreatedRun = {
      ...createdRun,
      status: 'running',
      phase: 'executing',
      department_count: 7,
      member_count: 13,
    }
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns
        .mockResolvedValueOnce(apiResponse(runPage([initialRun], { total: 41 })))
        .mockImplementationOnce(() => ordinaryPage.promise)
      const actionAPI = action === 'preview' ? api.previewDirectorySource : api.startDirectoryRun
      actionAPI.mockImplementationOnce(() => actionResponse.promise)
      api.getDirectoryRun.mockResolvedValue(apiResponse(runningCreatedRun))
    })

    try {
      const actionSelector = action === 'preview' ? '[data-testid="directory-preview"]' : '[data-testid="directory-run-now"]'
      await wrapper.get(actionSelector).trigger('click')
      await wrapper.get('[data-testid="directory-run-next"]').trigger('click')

      expect(api.listDirectoryRuns).toHaveBeenCalledTimes(2)
      expect(wrapper.get('[data-testid="directory-run-next"]').attributes('disabled')).toBeDefined()

      actionResponse.resolve(apiResponse(createdRun))
      await flushPromises()

      expect(api.getDirectoryRun).toHaveBeenCalledTimes(1)
      expect(api.getDirectoryRun).toHaveBeenCalledWith(createdRun.id)
      expect(vi.getTimerCount()).toBe(1)

      ordinaryPage.resolve(apiResponse(runPage([pageTwoRun], { total: 41, page: 1 })))
      await flushPromises()

      expect(wrapper.get('[data-testid="directory-run-page-meta"]').text()).toContain('Page 2 of 3')
      expect(wrapper.find(`[data-testid="directory-run-row-${pageTwoRun.id}"]`).exists()).toBe(true)
      expect(vi.getTimerCount()).toBe(1)

      await vi.runOnlyPendingTimersAsync()
      await flushPromises()

      expect(api.getDirectoryRun.mock.calls.map((call: any[]) => call[0])).toEqual([createdRun.id, createdRun.id])
      expect(wrapper.text()).toContain('Reading directory API')
      expect(wrapper.text()).toContain('7 departments · 13 members')
      expect(wrapper.get('[data-testid="directory-run-page-meta"]').text()).toContain('Page 2 of 3')
      expect(wrapper.find(`[data-testid="directory-run-row-${pageTwoRun.id}"]`).exists()).toBe(true)
      expect(vi.getTimerCount()).toBe(1)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it.each([
    'preview' as const,
    'apply' as const,
  ])('keeps the just-created $action poll when an older-active page started during its POST resolves', async (action) => {
    vi.useFakeTimers()
    const actionResponse = deferred<any>()
    const stalePage = deferred<any>()
    const initialRun = runSummary({ id: 450, member_count: 19 })
    const olderActiveRun = runSummary({
      id: 430,
      mode: 'apply',
      status: 'running',
      phase: 'applying',
      completed_at: null,
      department_count: 4,
      member_count: 8,
    })
    const createdRun = runSummary({
      id: 431,
      mode: action,
      status: 'queued',
      phase: 'validating',
      started_at: null,
      completed_at: null,
      department_count: 0,
      member_count: 0,
    })
    const runningCreatedRun = {
      ...createdRun,
      status: 'running',
      phase: 'executing',
      department_count: 7,
      member_count: 13,
    }
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns
        .mockResolvedValueOnce(apiResponse(runPage([initialRun], { total: 41 })))
        .mockImplementationOnce(() => stalePage.promise)
      const actionAPI = action === 'preview' ? api.previewDirectorySource : api.startDirectoryRun
      actionAPI.mockImplementationOnce(() => actionResponse.promise)
      api.getDirectoryRun.mockImplementation((id: number) => Promise.resolve(apiResponse(
        id === createdRun.id ? runningCreatedRun : olderActiveRun,
      )))
    })

    try {
      const actionSelector = action === 'preview' ? '[data-testid="directory-preview"]' : '[data-testid="directory-run-now"]'
      await wrapper.get(actionSelector).trigger('click')
      await wrapper.get('[data-testid="directory-run-next"]').trigger('click')

      expect(api.listDirectoryRuns).toHaveBeenCalledTimes(2)

      actionResponse.resolve(apiResponse(createdRun))
      await flushPromises()

      expect(api.getDirectoryRun.mock.calls.map((call: any[]) => call[0])).toEqual([createdRun.id])
      expect(vi.getTimerCount()).toBe(1)

      stalePage.resolve(apiResponse(runPage([initialRun], {
        total: 41,
        page: 1,
        latest_active_run: olderActiveRun,
      })))
      await flushPromises()

      expect(vi.getTimerCount()).toBe(1)

      await vi.runOnlyPendingTimersAsync()
      await flushPromises()

      expect(api.getDirectoryRun.mock.calls.map((call: any[]) => call[0])).toEqual([createdRun.id, createdRun.id])
      expect(wrapper.text()).toContain('Reading directory API')
      expect(wrapper.text()).toContain('7 departments · 13 members')
      expect(vi.getTimerCount()).toBe(1)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it.each([
    { first: 'preview' as const, second: 'apply' as const },
    { first: 'apply' as const, second: 'preview' as const },
  ])('serializes $first then $second while the first action POST is pending', async ({ first, second }) => {
    vi.useFakeTimers()
    const actionResponse = deferred<any>()
    const createdRun = runSummary({
      id: 441,
      mode: first,
      status: 'queued',
      phase: 'validating',
      started_at: null,
      completed_at: null,
      department_count: 0,
      member_count: 0,
    })
    const runningCreatedRun = {
      ...createdRun,
      status: 'running',
      phase: 'executing',
      department_count: 7,
      member_count: 13,
    }
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      const firstAPI = first === 'preview' ? api.previewDirectorySource : api.startDirectoryRun
      firstAPI.mockImplementationOnce(() => actionResponse.promise)
      api.getDirectoryRun.mockResolvedValue(apiResponse(runningCreatedRun))
    })

    try {
      const firstSelector = first === 'preview' ? '[data-testid="directory-preview"]' : '[data-testid="directory-run-now"]'
      const secondSelector = second === 'preview' ? '[data-testid="directory-preview"]' : '[data-testid="directory-run-now"]'
      const firstAPI = first === 'preview' ? api.previewDirectorySource : api.startDirectoryRun
      const secondAPI = second === 'preview' ? api.previewDirectorySource : api.startDirectoryRun

      await wrapper.get(firstSelector).trigger('click')

      expect.soft(wrapper.get('[data-testid="directory-preview"]').attributes('disabled')).toBeDefined()
      expect.soft(wrapper.get('[data-testid="directory-run-now"]').attributes('disabled')).toBeDefined()

      await wrapper.get(secondSelector).trigger('click')
      await flushPromises()

      expect.soft(firstAPI).toHaveBeenCalledTimes(1)
      expect.soft(secondAPI).not.toHaveBeenCalled()

      actionResponse.resolve(apiResponse(createdRun))
      await flushPromises()

      expect.soft(api.getDirectoryRun.mock.calls.map((call: any[]) => call[0])).toEqual([createdRun.id])
      expect.soft(vi.getTimerCount()).toBe(1)
      expect.soft(wrapper.text()).toContain('Reading directory API')
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it.each([
    'preview' as const,
    'apply' as const,
  ])('keeps a pending $action owned when Save reloads the same source first', async (action) => {
    vi.useFakeTimers()
    const actionResponse = deferred<any>()
    const createdRun = runSummary({
      id: 501,
      mode: action,
      status: 'queued',
      phase: 'validating',
      started_at: null,
      completed_at: null,
      department_count: 0,
      member_count: 0,
    })
    const runningCreatedRun = {
      ...createdRun,
      status: 'running',
      phase: 'executing',
      department_count: 7,
      member_count: 13,
    }
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns
        .mockResolvedValueOnce(apiResponse(runPage()))
        .mockResolvedValueOnce(apiResponse(runPage()))
      const actionAPI = action === 'preview' ? api.previewDirectorySource : api.startDirectoryRun
      actionAPI.mockImplementationOnce(() => actionResponse.promise)
      api.getDirectoryRun.mockResolvedValue(apiResponse(runningCreatedRun))
    })

    try {
      const actionSelector = action === 'preview' ? '[data-testid="directory-preview"]' : '[data-testid="directory-run-now"]'
      await wrapper.get(actionSelector).trigger('click')
      await wrapper.get('[data-testid="directory-save"]').trigger('click')
      await flushPromises()

      expect(api.updateDirectorySource).toHaveBeenCalledTimes(1)
      expect(api.listDirectorySources).toHaveBeenCalledTimes(2)
      expect(api.listDirectoryRuns).toHaveBeenCalledTimes(2)
      expect(api.getDirectoryRun).not.toHaveBeenCalled()

      actionResponse.resolve(apiResponse(createdRun))
      await flushPromises()

      expect(api.getDirectoryRun.mock.calls.map((call: any[]) => call[0])).toEqual([createdRun.id])
      expect(vi.getTimerCount()).toBe(1)
      expect(wrapper.text()).toContain('Reading directory API')
      expect(wrapper.get('[data-testid="directory-preview"]').attributes('disabled')).toBeDefined()
      expect(wrapper.get('[data-testid="directory-run-now"]').attributes('disabled')).toBeDefined()
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('keeps polling authoritative while a no-active non-first page commits', async () => {
    vi.useFakeTimers()
    const activeA = runSummary({
      id: 425,
      mode: 'apply',
      status: 'running',
      phase: 'applying',
      completed_at: null,
      department_count: 12,
      member_count: 24,
    })
    const terminalB = runSummary({ id: 404, mode: 'preview', department_count: 4, member_count: 8 })
    const oldTerminal = runSummary({ id: 424, mode: 'apply', department_count: 2, member_count: 3 })
    const terminalA = {
      ...activeA,
      status: 'completed',
      phase: 'completed',
      completed_at: '2026-07-15T01:02:00Z',
      department_count: 15,
      member_count: 31,
    }
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns.mockImplementation((_sourceID: number, params: { offset: number }) => {
        if (params.offset === 0) {
          return Promise.resolve(apiResponse(runPage([oldTerminal], { total: 41, latest_active_run: activeA })))
        }
        return Promise.resolve(apiResponse(runPage([terminalB], { total: 41, page: 1 })))
      })
      api.getDirectoryRun.mockImplementation((id: number) => {
        if (id === terminalB.id) {
          return Promise.resolve(apiResponse({ ...terminalB, summary: { marker: 'terminal-b-authoritative-poll' } }))
        }
        return Promise.resolve(apiResponse(terminalA))
      })
    })

    try {
      await wrapper.get('[data-testid="directory-run-next"]').trigger('click')
      await flushPromises()

      expect(wrapper.get('[data-testid="directory-run-page-meta"]').text()).toContain('Page 2 of 3')
      expect(wrapper.find(`[data-testid="directory-run-row-${terminalB.id}"]`).exists()).toBe(true)
      expect(wrapper.text()).toContain('Applying directory facts')
      expect(wrapper.get('[data-testid="directory-preview"]').attributes('disabled')).toBeDefined()
      expect(wrapper.get('[data-testid="directory-run-now"]').attributes('disabled')).toBeDefined()
      expect(vi.getTimerCount()).toBe(1)

      await wrapper.get(`[data-testid="directory-run-row-${terminalB.id}"]`).trigger('click')
      await flushPromises()
      expect(wrapper.get('[data-testid="directory-run-detail"]').text()).toContain('terminal-b-authoritative-poll')

      await vi.runOnlyPendingTimersAsync()
      await flushPromises()

      expect(api.getDirectoryRun.mock.calls.map((call: any[]) => call[0])).toEqual([terminalB.id, activeA.id])
      expect(api.listDirectoryRuns).toHaveBeenLastCalledWith(1, { limit: 20, offset: 20 })
      expect(wrapper.get('[data-testid="directory-run-page-meta"]').text()).toContain('Page 2 of 3')
      expect(wrapper.find(`[data-testid="directory-run-row-${terminalB.id}"]`).exists()).toBe(true)
      expect(wrapper.get('[data-testid="directory-run-detail"]').text()).toContain('terminal-b-authoritative-poll')
      expect(wrapper.text()).toContain('Run completed: kept 31 valid members; 15 departments.')
      expect(wrapper.text()).not.toContain('Applying directory facts')
      expect(wrapper.get('[data-testid="directory-preview"]').attributes('disabled')).toBeUndefined()
      expect(wrapper.get('[data-testid="directory-run-now"]').attributes('disabled')).toBeUndefined()
      expect(vi.getTimerCount()).toBe(0)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it.each([
    'preview' as const,
    'apply' as const,
  ])('keeps a newer same-source $action poll when an older ordinary page resolves', async (action) => {
    vi.useFakeTimers()
    const staleOrdinaryPage = deferred<any>()
    const initialRun = runSummary({ id: 411, member_count: 19 })
    const stalePageRun = runSummary({ id: 390, member_count: 99 })
    const newerRun = runSummary({
      id: 412,
      mode: action,
      status: 'queued',
      phase: 'validating',
      started_at: null,
      completed_at: null,
      department_count: 0,
      member_count: 0,
    })
    const runningNewerRun = {
      ...newerRun,
      status: 'running',
      phase: 'executing',
      department_count: 7,
      member_count: 13,
    }
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns
        .mockResolvedValueOnce(apiResponse(runPage([initialRun], { total: 41 })))
        .mockImplementationOnce(() => staleOrdinaryPage.promise)
      const actionAPI = action === 'preview' ? api.previewDirectorySource : api.startDirectoryRun
      actionAPI.mockResolvedValueOnce(apiResponse(newerRun))
      api.getDirectoryRun.mockResolvedValue(apiResponse(runningNewerRun))
    })

    try {
      const actionSelector = action === 'preview' ? '[data-testid="directory-preview"]' : '[data-testid="directory-run-now"]'
      await wrapper.get('[data-testid="directory-run-next"]').trigger('click')

      expect(api.listDirectoryRuns).toHaveBeenCalledTimes(2)
      expect(wrapper.get('[data-testid="directory-run-next"]').attributes('disabled')).toBeDefined()

      await wrapper.get(actionSelector).trigger('click')
      await flushPromises()

      expect(api.getDirectoryRun).toHaveBeenCalledTimes(1)
      expect(api.getDirectoryRun).toHaveBeenCalledWith(newerRun.id)
      expect(vi.getTimerCount()).toBe(1)
      expect(wrapper.get('[data-testid="directory-run-next"]').attributes('disabled')).toBeUndefined()

      staleOrdinaryPage.resolve(apiResponse(runPage([stalePageRun], { total: 1, page: 1 })))
      await flushPromises()

      expect(vi.getTimerCount()).toBe(1)
      expect(wrapper.find(`[data-testid="directory-run-row-${initialRun.id}"]`).exists()).toBe(true)
      expect(wrapper.find(`[data-testid="directory-run-row-${stalePageRun.id}"]`).exists()).toBe(false)
      expect(wrapper.get('[data-testid="directory-run-page-meta"]').text()).toContain('Page 1 of 3')
      expect(wrapper.get('[data-testid="directory-run-next"]').attributes('disabled')).toBeUndefined()

      await vi.runOnlyPendingTimersAsync()
      await flushPromises()

      expect(api.getDirectoryRun.mock.calls.map((call: any[]) => call[0])).toEqual([newerRun.id, newerRun.id])
      expect(wrapper.text()).toContain('Reading directory API')
      expect(wrapper.text()).toContain('7 departments · 13 members')
      expect(vi.getTimerCount()).toBe(1)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it.each([
    'preview' as const,
    'apply' as const,
  ])('keeps a newer same-source $action poll when an older conflict recovery page resolves', async (action) => {
    vi.useFakeTimers()
    const staleRecoveryPage = deferred<any>()
    const initialRun = runSummary({ id: 401, member_count: 19 })
    const staleRecoveryRun = runSummary({ id: 400, member_count: 99 })
    const newerRun = runSummary({
      id: 402,
      mode: action,
      status: 'queued',
      phase: 'validating',
      started_at: null,
      completed_at: null,
      department_count: 0,
      member_count: 0,
    })
    const runningNewerRun = {
      ...newerRun,
      status: 'running',
      phase: 'executing',
      department_count: 7,
      member_count: 13,
    }
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns
        .mockResolvedValueOnce(apiResponse(runPage([initialRun], { total: 41 })))
        .mockImplementationOnce(() => staleRecoveryPage.promise)
      const actionAPI = action === 'preview' ? api.previewDirectorySource : api.startDirectoryRun
      actionAPI
        .mockRejectedValueOnce({ response: { status: 409, data: { message: 'synthetic conflict' } } })
        .mockResolvedValueOnce(apiResponse(newerRun))
      api.getDirectoryRun.mockResolvedValue(apiResponse(runningNewerRun))
    })

    try {
      const actionSelector = action === 'preview' ? '[data-testid="directory-preview"]' : '[data-testid="directory-run-now"]'
      await wrapper.get(actionSelector).trigger('click')
      await flushPromises()

      expect(api.listDirectoryRuns).toHaveBeenCalledTimes(2)

      await wrapper.get(actionSelector).trigger('click')
      await flushPromises()

      expect(api.getDirectoryRun).toHaveBeenCalledTimes(1)
      expect(api.getDirectoryRun).toHaveBeenCalledWith(newerRun.id)
      expect(vi.getTimerCount()).toBe(1)

      staleRecoveryPage.resolve(apiResponse(runPage([staleRecoveryRun], { total: 1 })))
      await flushPromises()

      expect(vi.getTimerCount()).toBe(1)
      expect(wrapper.find(`[data-testid="directory-run-row-${initialRun.id}"]`).exists()).toBe(true)
      expect(wrapper.find(`[data-testid="directory-run-row-${staleRecoveryRun.id}"]`).exists()).toBe(false)
      expect(wrapper.get('[data-testid="directory-run-page-meta"]').text()).toContain('Page 1 of 3')
      expect(wrapper.get('[data-testid="directory-run-next"]').attributes('disabled')).toBeUndefined()

      await vi.runOnlyPendingTimersAsync()
      await flushPromises()

      expect(api.getDirectoryRun.mock.calls.map((call: any[]) => call[0])).toEqual([newerRun.id, newerRun.id])
      expect(wrapper.text()).toContain('Reading directory API')
      expect(wrapper.text()).toContain('7 departments · 13 members')
      expect(vi.getTimerCount()).toBe(1)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it.each([
    { action: 'preview' as const, outcome: 'success' as const },
    { action: 'preview' as const, outcome: 'conflict' as const },
    { action: 'preview' as const, outcome: 'error' as const },
    { action: 'apply' as const, outcome: 'success' as const },
    { action: 'apply' as const, outcome: 'conflict' as const },
    { action: 'apply' as const, outcome: 'error' as const },
  ])('ignores stale source action response while source B owns poll and page ($action, $outcome)', async ({ action, outcome }) => {
    vi.useFakeTimers()
    const actionResponse = deferred<any>()
    const pageB = deferred<any>()
    const pollB = deferred<any>()
    const sourceA = {
      id: 1,
      name: 'First Directory',
      description: 'First source',
      scope: 'full_company',
      enabled: true,
      dsl: 'version: 1\nscope: full_company\n',
      schedule_enabled: false,
      schedule_interval: 'daily',
      schedule_timezone: 'UTC',
    }
    const sourceB = { ...sourceA, id: 2, name: 'Second Directory', description: 'Second source' }
    const activeB = runSummary({
      id: 390,
      source_id: 2,
      mode: 'apply',
      status: 'running',
      phase: 'applying',
      completed_at: null,
      department_count: 12,
      member_count: 22,
    })
    const pageBRun = runSummary({ id: 369, source_id: 2, member_count: 17 })
    const staleMarker = `stale-${action}-${outcome}-marker`
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectorySources.mockResolvedValue(apiResponse({ items: [sourceA, sourceB] }))
      api.listDirectoryRuns.mockImplementation((sourceID: number, params: { offset: number }) => {
        if (sourceID === 1) return Promise.resolve(apiResponse(runPage()))
        if (params.offset === 0) {
          return Promise.resolve(apiResponse(runPage([runSummary({ id: 389, source_id: 2 })], {
            total: 41,
            latest_active_run: activeB,
          })))
        }
        if (params.offset === 20) return pageB.promise
        return Promise.resolve(apiResponse(runPage()))
      })
      api.previewDirectorySource.mockImplementationOnce(() => actionResponse.promise)
      api.startDirectoryRun.mockImplementationOnce(() => actionResponse.promise)
      api.getDirectoryRun.mockImplementation((id: number) => {
        if (id === activeB.id) return pollB.promise
        return Promise.resolve(apiResponse(runSummary({ id, source_id: 1 })))
      })
    })

    try {
      const actionSelector = action === 'preview' ? '[data-testid="directory-preview"]' : '[data-testid="directory-run-now"]'
      await wrapper.get(actionSelector).trigger('click')

      const secondSourceButton = wrapper.findAll('button').find((button) => button.text().includes('Second Directory'))
      await secondSourceButton!.trigger('click')
      await flushPromises()

      await vi.runOnlyPendingTimersAsync()
      expect(api.getDirectoryRun).toHaveBeenCalledWith(activeB.id)

      await wrapper.get('[data-testid="directory-run-next"]').trigger('click')
      expect(wrapper.get('[data-testid="directory-run-next"]').attributes('disabled')).toBeDefined()

      if (outcome === 'success') {
        actionResponse.resolve(apiResponse(runSummary({
          id: 380,
          source_id: 1,
          mode: action,
          status: 'queued',
          phase: 'validating',
          started_at: null,
          completed_at: null,
          department_count: 999,
          member_count: 999,
        })))
      } else {
        actionResponse.reject({
          response: {
            status: outcome === 'conflict' ? 409 : 500,
            data: { message: staleMarker },
          },
        })
      }
      await flushPromises()

      pageB.resolve(apiResponse(runPage([pageBRun], { total: 41, page: 1, latest_active_run: activeB })))
      pollB.resolve(apiResponse({ ...activeB, phase: 'normalizing', department_count: 23, member_count: 33 }))
      await flushPromises()

      expect(api.listDirectoryRuns.mock.calls.filter((call: any[]) => call[0] === 1)).toHaveLength(1)
      expect(wrapper.get('[data-testid="directory-run-page-meta"]').text()).toContain('Page 2 of 3')
      expect(wrapper.find(`[data-testid="directory-run-row-${pageBRun.id}"]`).exists()).toBe(true)
      expect(wrapper.text()).toContain('Normalizing members')
      expect(wrapper.text()).toContain('23 departments · 33 members')
      expect(wrapper.text()).not.toContain('999 departments · 999 members')
      expect(wrapper.text()).not.toContain(staleMarker)
      expect(vi.getTimerCount()).toBe(1)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it.each([
    'preview' as const,
    'apply' as const,
  ])('keeps a recovered active $action poll while applying a template', async (action) => {
    vi.useFakeTimers()
    const activeRun = runSummary({
      id: 426,
      mode: action,
      status: 'running',
      phase: action === 'preview' ? 'executing' : 'applying',
      completed_at: null,
      department_count: 7,
      member_count: 13,
    })
    const terminalRun = {
      ...activeRun,
      status: 'completed',
      phase: 'completed',
      completed_at: '2026-07-15T01:02:00Z',
      department_count: 9,
      member_count: 17,
    }
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns
        .mockResolvedValueOnce(apiResponse(runPage([], { latest_active_run: activeRun })))
        .mockResolvedValueOnce(apiResponse(runPage([terminalRun])))
      api.getDirectoryRun.mockResolvedValueOnce(apiResponse(terminalRun))
    })

    try {
      const startedMessage = action === 'preview' ? 'Starting preview...' : 'Starting run...'
      const phaseMessage = action === 'preview' ? 'Reading directory API' : 'Applying directory facts'
      const terminalMessage = action === 'preview'
        ? 'Preview completed: kept 17 valid members; 9 departments.'
        : 'Run completed: kept 17 valid members; 9 departments.'

      expect(vi.getTimerCount()).toBe(1)
      expect(wrapper.text()).toContain(startedMessage)
      expect(wrapper.text()).toContain(phaseMessage)
      expect(wrapper.get('[data-testid="directory-preview"]').attributes('disabled')).toBeDefined()
      expect(wrapper.get('[data-testid="directory-run-now"]').attributes('disabled')).toBeDefined()

      const templateButton = wrapper.findAll('button').find((button) => button.text().includes('Departments then members'))
      expect(templateButton).toBeTruthy()
      await templateButton!.trigger('click')

      expect(vi.getTimerCount()).toBe(1)
      expect(wrapper.text()).toContain(startedMessage)
      expect(wrapper.text()).toContain(phaseMessage)
      expect(wrapper.get('[data-testid="directory-preview"]').attributes('disabled')).toBeDefined()
      expect(wrapper.get('[data-testid="directory-run-now"]').attributes('disabled')).toBeDefined()
      expect(api.getDirectoryRun).not.toHaveBeenCalled()

      await vi.runOnlyPendingTimersAsync()
      await flushPromises()

      expect(api.getDirectoryRun).toHaveBeenCalledTimes(1)
      expect(api.getDirectoryRun).toHaveBeenCalledWith(activeRun.id)
      expect(wrapper.text()).toContain(terminalMessage)
      expect(wrapper.text()).not.toContain(phaseMessage)
      expect(wrapper.get('[data-testid="directory-preview"]').attributes('disabled')).toBeUndefined()
      expect(wrapper.get('[data-testid="directory-run-now"]').attributes('disabled')).toBeUndefined()
      expect(vi.getTimerCount()).toBe(0)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('keeps a recovered active poll through save and same-source reload', async () => {
    vi.useFakeTimers()
    const saveResponse = deferred<any>()
    const sourceA = {
      id: 1,
      name: 'First Directory',
      description: 'First source',
      scope: 'full_company',
      enabled: true,
      dsl: 'version: 1\nscope: full_company\n',
      schedule_enabled: false,
      schedule_interval: 'daily',
      schedule_timezone: 'UTC',
    }
    const sourceB = { ...sourceA, id: 2, name: 'Second Directory', description: 'Second source' }
    const activeRun = runSummary({
      id: 427,
      source_id: 2,
      status: 'running',
      phase: 'applying',
      completed_at: null,
      department_count: 8,
      member_count: 14,
    })
    const terminalRun = {
      ...activeRun,
      status: 'completed',
      phase: 'completed',
      completed_at: '2026-07-15T01:03:00Z',
      department_count: 10,
      member_count: 19,
    }
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectorySources.mockResolvedValue(apiResponse({ items: [sourceA, sourceB] }))
      api.updateDirectorySource.mockImplementationOnce(() => saveResponse.promise)
      api.listDirectoryRuns
        .mockResolvedValueOnce(apiResponse(runPage()))
        .mockResolvedValueOnce(apiResponse(runPage([], { latest_active_run: activeRun })))
        .mockResolvedValueOnce(apiResponse(runPage([], { latest_active_run: activeRun })))
        .mockResolvedValueOnce(apiResponse(runPage([terminalRun])))
      api.getDirectoryRun.mockResolvedValueOnce(apiResponse(terminalRun))
    })

    try {
      const secondSourceButton = wrapper.findAll('button').find((button) => button.text().includes('Second Directory'))
      expect(secondSourceButton).toBeTruthy()
      await secondSourceButton!.trigger('click')
      await flushPromises()

      expect(vi.getTimerCount()).toBe(1)
      expect(wrapper.text()).toContain('Starting run...')
      expect(wrapper.text()).toContain('Applying directory facts')

      await wrapper.get('[data-testid="directory-save"]').trigger('click')
      await flushPromises()

      expect(api.updateDirectorySource).toHaveBeenCalledTimes(1)
      expect(api.updateDirectorySource).toHaveBeenCalledWith(2, expect.any(Object))
      expect(wrapper.get('[data-testid="directory-save"]').attributes('disabled')).toBeDefined()
      expect(vi.getTimerCount()).toBe(1)
      expect(wrapper.text()).toContain('Starting run...')
      expect(wrapper.text()).toContain('Applying directory facts')
      expect(api.getDirectoryRun).not.toHaveBeenCalled()

      saveResponse.resolve(apiResponse({ id: 1, name: 'Example Directory' }))
      await flushPromises()

      expect(api.listDirectorySources).toHaveBeenCalledTimes(2)
      expect(api.listDirectoryRuns).toHaveBeenCalledTimes(3)
      expect(api.listDirectoryRuns).toHaveBeenLastCalledWith(2, { limit: 20, offset: 0 })
      expect(wrapper.get('[data-testid="directory-save"]').attributes('disabled')).toBeUndefined()
      expect(vi.getTimerCount()).toBe(1)
      expect(wrapper.text()).toContain('Starting run...')
      expect(wrapper.text()).toContain('Applying directory facts')
      expect(api.getDirectoryRun).not.toHaveBeenCalled()

      await vi.runOnlyPendingTimersAsync()
      await flushPromises()

      expect(api.getDirectoryRun).toHaveBeenCalledTimes(1)
      expect(api.getDirectoryRun).toHaveBeenCalledWith(activeRun.id)
      expect(wrapper.text()).toContain('Run completed: kept 19 valid members; 10 departments.')
      expect(wrapper.text()).not.toContain('Applying directory facts')
      expect(vi.getTimerCount()).toBe(0)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('invalidates an in-flight active poll when switching sources', async () => {
    vi.useFakeTimers()
    const pollA = deferred<any>()
    const sourceA = {
      id: 1,
      name: 'First Directory',
      description: 'First source',
      scope: 'full_company',
      enabled: true,
      dsl: 'version: 1\nscope: full_company\n',
      schedule_enabled: false,
      schedule_interval: 'daily',
      schedule_timezone: 'UTC',
    }
    const sourceB = { ...sourceA, id: 2, name: 'Second Directory', description: 'Second source' }
    const activeA = runSummary({ id: 290, status: 'running', phase: 'executing', completed_at: null })
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectorySources.mockResolvedValue(apiResponse({ items: [sourceA, sourceB] }))
      api.listDirectoryRuns.mockImplementation((sourceID: number) => Promise.resolve(apiResponse(
        sourceID === 1 ? runPage([], { latest_active_run: activeA }) : runPage(),
      )))
      api.getDirectoryRun.mockImplementationOnce(() => pollA.promise)
    })

    try {
      await vi.runOnlyPendingTimersAsync()
      expect(api.getDirectoryRun).toHaveBeenCalledWith(290)

      const secondSourceButton = wrapper.findAll('button').find((button) => button.text().includes('Second Directory'))
      await secondSourceButton!.trigger('click')
      await flushPromises()

      pollA.resolve(apiResponse({
        ...activeA,
        status: 'failed',
        phase: 'failed',
        error_message: 'stale-source-poll-marker',
      }))
      await flushPromises()

      expect(wrapper.text()).not.toContain('stale-source-poll-marker')
      expect(wrapper.text()).toContain('Second Directory')
      expect(vi.getTimerCount()).toBe(0)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('invalidates stale detail when switching sources', async () => {
    const detailA = deferred<any>()
    const sourceA = {
      id: 1,
      name: 'First Directory',
      description: 'First source',
      scope: 'full_company',
      enabled: true,
      dsl: 'version: 1\nscope: full_company\n',
      schedule_enabled: false,
      schedule_interval: 'daily',
      schedule_timezone: 'UTC',
    }
    const sourceB = { ...sourceA, id: 2, name: 'Second Directory', description: 'Second source' }
    const summaryA = runSummary({ id: 300, source_id: 1 })
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectorySources.mockResolvedValue(apiResponse({ items: [sourceA, sourceB] }))
      api.listDirectoryRuns.mockImplementation((sourceID: number) => Promise.resolve(apiResponse(
        sourceID === 1 ? runPage([summaryA]) : runPage(),
      )))
      api.getDirectoryRun.mockImplementationOnce(() => detailA.promise)
    })

    await wrapper.get('[data-testid="directory-run-row-300"]').trigger('click')
    const secondSourceButton = wrapper.findAll('button').find((button) => button.text().includes('Second Directory'))
    await secondSourceButton!.trigger('click')
    await flushPromises()

    detailA.resolve(apiResponse({ ...summaryA, summary: { marker: 'stale-source-detail-marker' } }))
    await flushPromises()

    expect(wrapper.text()).not.toContain('stale-source-detail-marker')
    expect(wrapper.find('[data-testid="directory-run-detail"]').exists()).toBe(false)
  })

  it('switches recovery to a newer active ID and invalidates the older timer', async () => {
    vi.useFakeTimers()
    const activeA = runSummary({ id: 310, status: 'running', phase: 'executing', completed_at: null })
    const activeC = runSummary({ id: 311, status: 'running', phase: 'normalizing', completed_at: null })
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns
        .mockResolvedValueOnce(apiResponse(runPage([runSummary({ id: 309 })], { total: 41, latest_active_run: activeA })))
        .mockResolvedValueOnce(apiResponse(runPage([runSummary({ id: 289 })], { total: 41, page: 1, latest_active_run: activeC })))
      api.getDirectoryRun.mockResolvedValueOnce(apiResponse(activeC))
    })

    try {
      await wrapper.get('[data-testid="directory-run-next"]').trigger('click')
      await flushPromises()
      await vi.runOnlyPendingTimersAsync()
      await flushPromises()

      expect(api.getDirectoryRun).toHaveBeenCalledTimes(1)
      expect(api.getDirectoryRun).toHaveBeenCalledWith(311)
      expect(api.getDirectoryRun).not.toHaveBeenCalledWith(310)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('clears active polling timers on unmount', async () => {
    vi.useFakeTimers()
    const active = runSummary({ id: 320, status: 'running', phase: 'executing', completed_at: null })
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns.mockResolvedValueOnce(apiResponse(runPage([], { latest_active_run: active })))
    })

    try {
      expect(vi.getTimerCount()).toBe(1)
      wrapper.unmount()
      await vi.runOnlyPendingTimersAsync()
      expect(api.getDirectoryRun).not.toHaveBeenCalled()
    } finally {
      vi.useRealTimers()
    }
  })

  it('polls preview progress until the run completes', async () => {
    vi.useFakeTimers()
    const workItemsApi = await import('@/api/workItems') as any
    const { wrapper, api } = await mountDirectorySyncSettings()
    api.previewDirectorySource.mockResolvedValueOnce({
      data: { data: { id: 40, mode: 'preview', status: 'queued', phase: 'validating', department_count: 0, member_count: 0, warning_count: 0 } },
    })
    api.getDirectoryRun
      .mockResolvedValueOnce({ data: { data: { id: 40, mode: 'preview', status: 'running', phase: 'executing', department_count: 0, member_count: 0, warning_count: 0 } } })
      .mockResolvedValueOnce({ data: { data: { id: 40, mode: 'preview', status: 'completed', phase: 'completed', department_count: 2, member_count: 3, warning_count: 0 } } })

    try {
      await wrapper.get('[data-testid="directory-preview"]').trigger('click')
      expect(wrapper.text()).toContain('Starting preview...')
      await flushPromises()
      expect(wrapper.text()).toContain('Reading directory API')
      expect(api.getDirectoryRun).toHaveBeenCalledWith(40)

      await vi.runOnlyPendingTimersAsync()
      await flushPromises()
      expect(wrapper.text()).toContain('Preview completed: kept 3 valid members; 2 departments.')
      expect(wrapper.text()).not.toContain('Reading directory API')
      expect(api.listDirectoryRuns).toHaveBeenCalledTimes(2)
      expect(api.listDirectoryRuns).toHaveBeenLastCalledWith(1, { limit: 20, offset: 0 })
      expect(workItemsApi.getWorkItemCounts).not.toHaveBeenCalled()
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('shows localized apply progress while polling the run', async () => {
    vi.useFakeTimers()
    setLocale('zh-CN')
    const workItemsApi = await import('@/api/workItems') as any
    const { wrapper, api } = await mountDirectorySyncSettings()
    api.startDirectoryRun.mockResolvedValueOnce({
      data: { data: { id: 41, mode: 'apply', status: 'queued', phase: 'validating', department_count: 0, member_count: 0, warning_count: 0 } },
    })
    api.getDirectoryRun
      .mockResolvedValueOnce({ data: { data: { id: 41, mode: 'apply', status: 'running', phase: 'executing', department_count: 0, member_count: 0, warning_count: 0 } } })
      .mockResolvedValueOnce({
        data: {
          data: {
            id: 41,
            mode: 'apply',
            status: 'completed_with_warnings',
            phase: 'completed',
            department_count: 184,
            member_count: 633,
            warning_count: 3769,
            warnings: warnings('duplicate_member_email', 3769),
          },
        },
      })

    try {
      await wrapper.get('[data-testid="directory-run-now"]').trigger('click')
      expect(wrapper.text()).toContain('正在启动运行...')
      await flushPromises()
      expect(wrapper.text()).toContain('正在读取组织架构接口')
      expect(api.getDirectoryRun).toHaveBeenCalledWith(41)
      expect(workItemsApi.getWorkItemCounts).not.toHaveBeenCalled()

      await vi.runOnlyPendingTimersAsync()
      await flushPromises()
      expect(wrapper.text()).toContain('运行已完成：已保留 633 个有效成员，跳过 3769 条记录；部门 184 个。')
      expect(wrapper.text()).toContain('重复邮箱：3769 条')
      expect(wrapper.text()).not.toContain('正在读取组织架构接口')
      expect(api.listDirectoryRuns).toHaveBeenCalledTimes(2)
      expect(api.listDirectoryRuns).toHaveBeenLastCalledWith(1, { limit: 20, offset: 0 })
      expect(workItemsApi.getWorkItemCounts).toHaveBeenCalledTimes(1)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('cancels an observed apply poll after switching sources', async () => {
    vi.useFakeTimers()
    const workItemsApi = await import('@/api/workItems') as any
    const { wrapper, api, workItems } = await mountDirectorySyncSettings((api) => {
      api.listDirectorySources.mockResolvedValue({
        data: {
          data: {
            items: [
              {
                id: 1,
                name: 'First Directory',
                description: 'First source',
                scope: 'full_company',
                enabled: true,
                dsl: 'version: 1\nscope: full_company\n',
                schedule_enabled: false,
                schedule_interval: 'daily',
                schedule_timezone: 'UTC',
              },
              {
                id: 2,
                name: 'Second Directory',
                description: 'Second source',
                scope: 'full_company',
                enabled: true,
                dsl: 'version: 1\nscope: full_company\n',
                schedule_enabled: false,
                schedule_interval: 'daily',
                schedule_timezone: 'UTC',
              },
            ],
          },
        },
      })
      api.startDirectoryRun.mockResolvedValueOnce({
        data: { data: { id: 42, source_id: 1, mode: 'apply', status: 'queued', phase: 'validating' } },
      })
      api.getDirectoryRun
        .mockResolvedValueOnce({
          data: { data: { id: 42, source_id: 1, mode: 'apply', status: 'running', phase: 'executing' } },
        })
        .mockResolvedValueOnce({
          data: { data: { id: 42, source_id: 1, mode: 'apply', status: 'completed', phase: 'completed', department_count: 2, member_count: 5, warning_count: 0 } },
        })
    })
    const invalidateCounts = vi.spyOn(workItems, 'invalidateCounts')
    const loadCounts = vi.spyOn(workItems, 'loadCounts')

    try {
      await wrapper.get('[data-testid="directory-run-now"]').trigger('click')
      await flushPromises()
      expect(api.getDirectoryRun).toHaveBeenCalledTimes(1)

      const secondSourceButton = wrapper.findAll('button').find((button) => button.text().includes('Second Directory'))
      expect(secondSourceButton).toBeTruthy()
      await secondSourceButton!.trigger('click')
      await flushPromises()
      await wrapper.get('[data-testid="directory-validate"]').trigger('click')
      await flushPromises()

      await vi.advanceTimersByTimeAsync(1500)
      await flushPromises()

      expect(api.getDirectoryRun).toHaveBeenCalledTimes(1)
      expect(invalidateCounts).not.toHaveBeenCalled()
      expect(loadCounts).not.toHaveBeenCalled()
      expect(workItemsApi.getWorkItemCounts).not.toHaveBeenCalled()

      await vi.advanceTimersByTimeAsync(3000)
      await flushPromises()
      expect(api.getDirectoryRun).toHaveBeenCalledTimes(1)
      expect(invalidateCounts).not.toHaveBeenCalled()
      expect(workItemsApi.getWorkItemCounts).not.toHaveBeenCalled()
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('does not let source A apply refresh cancel source B polling', async () => {
    vi.useFakeTimers()
    const workItemsApi = await import('@/api/workItems') as any
    const refreshCounts = deferred<any>()
    const activeSourceB = runSummary({
      id: 43,
      source_id: 2,
      mode: 'preview',
      status: 'running',
      phase: 'executing',
      completed_at: null,
    })
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectorySources.mockResolvedValue({
        data: {
          data: {
            items: [
              {
                id: 1,
                name: 'First Directory',
                description: 'First source',
                scope: 'full_company',
                enabled: true,
                dsl: 'version: 1\nscope: full_company\n',
                schedule_enabled: false,
                schedule_interval: 'daily',
                schedule_timezone: 'UTC',
              },
              {
                id: 2,
                name: 'Second Directory',
                description: 'Second source',
                scope: 'full_company',
                enabled: true,
                dsl: 'version: 1\nscope: full_company\n',
                schedule_enabled: false,
                schedule_interval: 'daily',
                schedule_timezone: 'UTC',
              },
            ],
          },
        },
      })
      api.listDirectoryRuns.mockImplementation((sourceID: number) => Promise.resolve(apiResponse(
        sourceID === 2 ? runPage([], { latest_active_run: activeSourceB }) : runPage(),
      )))
      api.startDirectoryRun.mockResolvedValueOnce({
        data: { data: { id: 42, source_id: 1, mode: 'apply', status: 'queued', phase: 'validating' } },
      })
      api.getDirectoryRun.mockImplementation((runID: number) => Promise.resolve({
        data: {
          data: runID === 42
            ? { id: 42, source_id: 1, mode: 'apply', status: 'completed', phase: 'completed', department_count: 2, member_count: 5, warning_count: 0 }
            : { id: 43, source_id: 2, mode: 'preview', status: 'completed', phase: 'completed', department_count: 3, member_count: 6, warning_count: 0 },
        },
      }))
    })
    workItemsApi.getWorkItemCounts.mockReset().mockReturnValueOnce(refreshCounts.promise)

    try {
      await wrapper.get('[data-testid="directory-run-now"]').trigger('click')
      await flushPromises()
      expect(api.getDirectoryRun).toHaveBeenCalledWith(42)
      expect(workItemsApi.getWorkItemCounts).toHaveBeenCalledTimes(1)

      const secondSourceButton = wrapper.findAll('button').find((button) => button.text().includes('Second Directory'))
      expect(secondSourceButton).toBeTruthy()
      await secondSourceButton!.trigger('click')
      await flushPromises()
      expect(vi.getTimerCount()).toBe(1)

      refreshCounts.resolve(countsResponse(0))
      await flushPromises()
      expect(vi.getTimerCount()).toBe(1)

      await vi.runOnlyPendingTimersAsync()
      await flushPromises()
      expect(api.getDirectoryRun).toHaveBeenCalledWith(43)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('does not infer parallel polling targets from history summaries', async () => {
    vi.useFakeTimers()
    const workItemsApi = await import('@/api/workItems') as any
    const { wrapper, api, workItems } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns.mockResolvedValueOnce({
        data: {
          data: {
            items: [
              { id: 53, source_id: 1, mode: 'preview', status: 'running', phase: 'executing', department_count: 1, member_count: 2, warning_count: 0 },
              { id: 54, source_id: 1, mode: 'apply', status: 'running', phase: 'applying', department_count: 3, member_count: 4, warning_count: 0 },
            ],
          },
        },
      })
      api.getDirectoryRun.mockImplementation((runID: number) => {
        if (runID === 54) {
          return Promise.resolve({
            data: { data: { id: 54, source_id: 1, mode: 'apply', status: 'completed', phase: 'completed', department_count: 3, member_count: 4, warning_count: 0 } },
          })
        }
        return Promise.resolve({
          data: { data: { id: 53, source_id: 1, mode: 'preview', status: 'completed', phase: 'completed', department_count: 1, member_count: 2, warning_count: 0 } },
        })
      })
    })
    const invalidateCounts = vi.spyOn(workItems, 'invalidateCounts')
    const loadCounts = vi.spyOn(workItems, 'loadCounts')

    try {
      expect(wrapper.text()).toContain('Reading directory API')
      expect(vi.getTimerCount()).toBe(0)

      await vi.advanceTimersByTimeAsync(1500)
      await flushPromises()

      const polledRunIDs = api.getDirectoryRun.mock.calls.map(([runID]: [number]) => runID)
      expect(polledRunIDs).toEqual([])
      expect(invalidateCounts).not.toHaveBeenCalled()
      expect(loadCounts).not.toHaveBeenCalled()
      expect(workItemsApi.getWorkItemCounts).not.toHaveBeenCalled()

      await vi.advanceTimersByTimeAsync(3000)
      await flushPromises()
      expect(api.getDirectoryRun).not.toHaveBeenCalled()
      expect(invalidateCounts).not.toHaveBeenCalled()
      expect(workItemsApi.getWorkItemCounts).not.toHaveBeenCalled()
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('recovers the latest active directory apply run on mount and keeps polling it', async () => {
    vi.useFakeTimers()
    const workItemsApi = await import('@/api/workItems') as any
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns.mockResolvedValueOnce(apiResponse(runPage([], {
        latest_active_run: runSummary({
          id: 50,
          mode: 'apply',
          status: 'running',
          phase: 'applying',
          completed_at: null,
          department_count: 12,
          member_count: 34,
          warning_count: 5,
        }),
      })))
      api.getDirectoryRun.mockResolvedValueOnce({
        data: {
          data: { id: 50, source_id: 1, mode: 'apply', status: 'completed', phase: 'completed', department_count: 12, member_count: 34, warning_count: 0 },
        },
      })
    })

    try {
      await wrapper.vm.$nextTick()
      await flushPromises()

      expect(api.listDirectoryRuns).toHaveBeenCalledWith(1, { limit: 20, offset: 0 })
      expect(wrapper.text()).toContain('Applying directory facts')
      expect(wrapper.text()).toContain('12 departments · 34 members · 5 skipped records')

      await vi.runOnlyPendingTimersAsync()
      await flushPromises()

      expect(api.getDirectoryRun).toHaveBeenCalledWith(50)
      expect(wrapper.text()).toContain('Run completed: kept 34 valid members; 12 departments.')
      expect(workItemsApi.getWorkItemCounts).toHaveBeenCalledTimes(1)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('recovers the latest active apply run when starting a run returns a conflict', async () => {
    vi.useFakeTimers()
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.startDirectoryRun.mockRejectedValueOnce({
        response: {
          status: 409,
          data: { message: 'another full-company apply sync is already queued or running for this source' },
        },
      })
      api.listDirectoryRuns
        .mockResolvedValueOnce(apiResponse(runPage()))
        .mockResolvedValueOnce(apiResponse(runPage([], {
          latest_active_run: runSummary({
            id: 51,
            status: 'running',
            phase: 'executing',
            completed_at: null,
            department_count: 0,
            member_count: 0,
            warning_count: 0,
          }),
        })))
      api.getDirectoryRun.mockResolvedValueOnce({
        data: {
          data: { id: 51, source_id: 1, mode: 'apply', status: 'completed', phase: 'completed', department_count: 2, member_count: 3, warning_count: 0 },
        },
      })
    })

    try {
      await wrapper.get('[data-testid="directory-run-now"]').trigger('click')
      await flushPromises()

      expect(api.listDirectoryRuns).toHaveBeenCalledTimes(2)
      expect(api.listDirectoryRuns).toHaveBeenNthCalledWith(1, 1, { limit: 20, offset: 0 })
      expect(api.listDirectoryRuns).toHaveBeenNthCalledWith(2, 1, { limit: 20, offset: 0 })
      expect(wrapper.text()).toContain('Reading directory API')
      expect(wrapper.text()).not.toContain('another full-company apply sync')

      await vi.runOnlyPendingTimersAsync()
      await flushPromises()

      expect(api.getDirectoryRun).toHaveBeenCalledWith(51)
      expect(wrapper.text()).toContain('Run completed: kept 3 valid members; 2 departments.')
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('shows the latest completed directory apply run on mount without polling', async () => {
    vi.useFakeTimers()
    const workItemsApi = await import('@/api/workItems') as any
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns.mockResolvedValueOnce(apiResponse(runPage([
        runSummary({ id: 52, department_count: 4, member_count: 9 }),
      ])))
    })

    try {
      await wrapper.vm.$nextTick()
      await flushPromises()

      expect(api.listDirectoryRuns).toHaveBeenCalledWith(1, { limit: 20, offset: 0 })
      expect(wrapper.text()).toContain('Run completed: kept 9 valid members; 4 departments.')

      await vi.runOnlyPendingTimersAsync()
      await flushPromises()

      expect(api.getDirectoryRun).not.toHaveBeenCalledWith(52)
      expect(workItemsApi.getWorkItemCounts).not.toHaveBeenCalled()
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('does not let a stale source recovery overwrite the currently selected source', async () => {
    const firstRun = deferred<any>()
    const secondRun = deferred<any>()
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectorySources.mockResolvedValue({
        data: {
          data: {
            items: [
              {
                id: 1,
                name: 'First Directory',
                description: 'First source',
                scope: 'full_company',
                enabled: true,
                dsl: 'version: 1\nscope: full_company\n',
                schedule_enabled: false,
                schedule_interval: 'daily',
                schedule_timezone: 'UTC',
              },
              {
                id: 2,
                name: 'Second Directory',
                description: 'Second source',
                scope: 'full_company',
                enabled: true,
                dsl: 'version: 1\nscope: full_company\n',
                schedule_enabled: false,
                schedule_interval: 'daily',
                schedule_timezone: 'UTC',
              },
            ],
          },
        },
      })
      api.listDirectoryRuns
        .mockImplementationOnce(() => firstRun.promise)
        .mockImplementationOnce(() => secondRun.promise)
    })

    const secondSourceButton = wrapper.findAll('button').find((button) => button.text().includes('Second Directory'))
    expect(secondSourceButton).toBeTruthy()
    await secondSourceButton!.trigger('click')

    secondRun.resolve(apiResponse(runPage([
      runSummary({ id: 62, source_id: 2, department_count: 2, member_count: 8 }),
    ])))
    await flushPromises()

    expect(wrapper.text()).toContain('Run completed: kept 8 valid members; 2 departments.')

    firstRun.resolve(apiResponse(runPage([
      runSummary({ id: 61, source_id: 1, department_count: 9, member_count: 99 }),
    ])))
    await flushPromises()

    expect(api.listDirectoryRuns).toHaveBeenCalledWith(1, { limit: 20, offset: 0 })
    expect(api.listDirectoryRuns).toHaveBeenCalledWith(2, { limit: 20, offset: 0 })
    expect(wrapper.text()).toContain('Run completed: kept 8 valid members; 2 departments.')
    expect(wrapper.text()).not.toContain('Run completed: kept 99 valid members; 9 departments.')
  })

  it('prefers an active directory run over a newer terminal preview on mount', async () => {
    vi.useFakeTimers()
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns.mockResolvedValueOnce(apiResponse(runPage([
        runSummary({ id: 63, mode: 'preview', department_count: 1, member_count: 5 }),
      ], {
        latest_active_run: runSummary({
          id: 64,
          mode: 'apply',
          status: 'running',
          phase: 'applying',
          completed_at: null,
          department_count: 7,
          member_count: 11,
        }),
      })))
      api.getDirectoryRun.mockResolvedValueOnce({
        data: {
          data: { id: 64, source_id: 1, mode: 'apply', status: 'completed', phase: 'completed', department_count: 7, member_count: 11, warning_count: 0 },
        },
      })
    })

    try {
      await wrapper.vm.$nextTick()
      await flushPromises()

      expect(wrapper.text()).toContain('Applying directory facts')
      expect(wrapper.text()).toContain('7 departments · 11 members · 0 skipped records')
      expect(wrapper.text()).not.toContain('Preview completed: kept 5 valid members; 1 departments.')

      await vi.runOnlyPendingTimersAsync()
      await flushPromises()

      expect(api.getDirectoryRun).toHaveBeenCalledWith(64)
      expect(wrapper.text()).toContain('Run completed: kept 11 valid members; 7 departments.')
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('saves, validates, previews, and runs a directory source', async () => {
    const { wrapper, api } = await mountDirectorySyncSettings()

    await wrapper.get('[data-testid="directory-source-name"]').setValue('Updated Directory')
    await wrapper.get('[data-testid="directory-save"]').trigger('click')
    await flushPromises()
    expect(api.updateDirectorySource).toHaveBeenCalledWith(1, expect.objectContaining({ name: 'Updated Directory' }))

    await wrapper.get('[data-testid="directory-validate"]').trigger('click')
    await flushPromises()
    expect(api.validateDirectorySource).toHaveBeenCalledWith(1)
    expect(wrapper.text()).toContain('Validation passed')

    await wrapper.get('[data-testid="directory-preview"]').trigger('click')
    await flushPromises()
    expect(api.previewDirectorySource).toHaveBeenCalledWith(1)

    await wrapper.get('[data-testid="directory-run-now"]').trigger('click')
    await flushPromises()
    expect(api.startDirectoryRun).toHaveBeenCalledWith(1, { mode: 'apply' })
  })

  it('invalidates an older generation and awaits fresh counts after updating an existing source', async () => {
    const workItemsApi = await import('@/api/workItems') as any
    const previousCounts = deferred<any>()
    const freshCounts = deferred<any>()
    const { wrapper, api, workItems } = await mountDirectorySyncSettings()
    workItemsApi.getWorkItemCounts.mockReset()
      .mockResolvedValueOnce(countsResponse(1))
      .mockReturnValueOnce(previousCounts.promise)
      .mockReturnValueOnce(freshCounts.promise)
    await workItems.loadCounts()
    const previousLoad = workItems.loadCounts({ force: true })

    await wrapper.get('[data-testid="directory-source-name"]').setValue('Updated Directory')
    await wrapper.get('[data-testid="directory-save"]').trigger('click')
    await flushPromises()

    expect(workItemsApi.getWorkItemCounts).toHaveBeenCalledTimes(3)
    expect(api.listDirectoryRuns).toHaveBeenCalledTimes(2)
    expect(api.listDirectoryRuns).toHaveBeenLastCalledWith(1, { limit: 20, offset: 0 })
    expect(wrapper.get('[data-testid="directory-save"]').attributes('disabled')).toBeDefined()

    previousCounts.resolve(countsResponse(9))
    await previousLoad
    expect(workItems.totalCount).toBe(1)
    expect(workItems.loading).toBe(true)

    freshCounts.resolve(countsResponse(0))
    await flushPromises()
    expect(workItems.totalCount).toBe(0)
    expect(workItems.loading).toBe(false)
    expect(wrapper.get('[data-testid="directory-save"]').attributes('disabled')).toBeUndefined()
  })

  it('does not refresh work-item counts when creating a source', async () => {
    const workItemsApi = await import('@/api/workItems') as any
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectorySources.mockResolvedValue({ data: { data: { items: [] } } })
    })

    await wrapper.get('[data-testid="directory-save"]').trigger('click')
    await flushPromises()

    expect(api.createDirectorySource).toHaveBeenCalled()
    expect(workItemsApi.getWorkItemCounts).not.toHaveBeenCalled()
  })

  it('refreshes once for an immediately completed apply and ignores the pre-apply response', async () => {
    const workItemsApi = await import('@/api/workItems') as any
    const previousCounts = deferred<any>()
    const freshCounts = deferred<any>()
    const { wrapper, api, workItems } = await mountDirectorySyncSettings()
    workItemsApi.getWorkItemCounts.mockReset()
      .mockResolvedValueOnce(countsResponse(1))
      .mockReturnValueOnce(previousCounts.promise)
      .mockReturnValueOnce(freshCounts.promise)
    await workItems.loadCounts()
    const previousLoad = workItems.loadCounts({ force: true })

    await wrapper.get('[data-testid="directory-run-now"]').trigger('click')
    await flushPromises()

    expect(workItemsApi.getWorkItemCounts).toHaveBeenCalledTimes(3)
    expect(api.listDirectoryRuns).toHaveBeenCalledTimes(2)
    expect(api.listDirectoryRuns).toHaveBeenLastCalledWith(1, { limit: 20, offset: 0 })
    previousCounts.resolve(countsResponse(9))
    await previousLoad
    expect(workItems.totalCount).toBe(1)
    expect(workItems.loading).toBe(true)

    freshCounts.resolve(countsResponse(0))
    await flushPromises()
    expect(workItemsApi.getWorkItemCounts).toHaveBeenCalledTimes(3)
    expect(api.listDirectoryRuns).toHaveBeenCalledTimes(2)
    expect(workItems.totalCount).toBe(0)
    expect(workItems.loading).toBe(false)
  })

  it('switches directory sync copy to Chinese', async () => {
    setLocale('zh-CN')
    const { wrapper } = await mountDirectorySyncSettings()

    expect(wrapper.text()).toContain('组织架构同步')
    expect(wrapper.text()).toContain('先部门后成员')
    expect(wrapper.text()).toContain('复制 AI Prompt')
    expect(wrapper.text()).toContain('接口文档或接口说明')
    expect(wrapper.text()).toContain('没有接口文档时，逐项提供这些接口')

    await wrapper.get('[data-testid="directory-copy-ai-prompt"]').trigger('click')
    const prompt = (navigator.clipboard.writeText as any).mock.calls[0][0]
    expect(prompt).toContain('目标 YAML 合同')
    expect(prompt).toContain('标准化输出结构')
    expect(prompt).toContain('使用 steps[].id')
    expect(prompt).toContain('不要使用 steps[].name')
    expect(prompt).toContain('只读方式读取文档或调用接口')
    expect(prompt).toContain('不要粘贴原始响应行')
    expect(prompt).toContain('邮箱字段必须有完整的非空覆盖')
    expect(prompt).toContain('不要编造分页')
    expect(prompt).toContain('保留 YAML 缩进')
    expect(prompt).toContain('不要把嵌套字段拉平成顶层字段')
    expect(prompt).toContain('只有响应根本身就是数组时才使用 extract.items: $')
    expect(prompt).toContain('最终 YAML 可以使用配置者提供的生产接口 URL、字段名和非密钥 header 名称')
    expect(prompt).toContain('不要包含 API Key、bearer token、密码')
    expect(prompt).toContain('member.metadata.wecom_userid')
    expect(prompt).toContain('required when quota reset approval notifications must @ approvers through WeCom')
    expect(prompt).toContain('never member.external_id, local user ids, or email addresses')
    expect(prompt).toContain('directory.example.com')
  })
})
