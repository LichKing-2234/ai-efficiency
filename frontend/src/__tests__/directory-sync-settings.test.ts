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
  api.listDirectoryRuns.mockResolvedValue({ data: { data: { items: [] } } })
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
      expect(workItemsApi.getWorkItemCounts).not.toHaveBeenCalled()
    } finally {
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
      expect(workItemsApi.getWorkItemCounts).toHaveBeenCalledTimes(1)
    } finally {
      vi.useRealTimers()
    }
  })

  it('keeps polling an observed apply after switching sources and clearing feedback', async () => {
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

      expect(api.getDirectoryRun).toHaveBeenCalledTimes(2)
      expect(api.getDirectoryRun).toHaveBeenLastCalledWith(42)
      expect(invalidateCounts).toHaveBeenCalledTimes(1)
      expect(loadCounts).toHaveBeenCalledTimes(1)
      expect(loadCounts).toHaveBeenCalledWith({ force: true })
      expect(workItemsApi.getWorkItemCounts).toHaveBeenCalledTimes(1)

      await vi.advanceTimersByTimeAsync(3000)
      await flushPromises()
      expect(api.getDirectoryRun).toHaveBeenCalledTimes(2)
      expect(invalidateCounts).toHaveBeenCalledTimes(1)
      expect(workItemsApi.getWorkItemCounts).toHaveBeenCalledTimes(1)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('polls an active apply independently when mount recovery displays an active preview first', async () => {
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

      await vi.advanceTimersByTimeAsync(1500)
      await flushPromises()

      const polledRunIDs = api.getDirectoryRun.mock.calls.map(([runID]: [number]) => runID)
      expect(polledRunIDs[0]).toBe(54)
      expect(polledRunIDs.filter((runID: number) => runID === 54)).toHaveLength(1)
      expect(polledRunIDs.filter((runID: number) => runID === 53)).toHaveLength(1)
      expect(invalidateCounts).toHaveBeenCalledTimes(1)
      expect(loadCounts).toHaveBeenCalledTimes(1)
      expect(loadCounts).toHaveBeenCalledWith({ force: true })
      expect(workItemsApi.getWorkItemCounts).toHaveBeenCalledTimes(1)

      await vi.advanceTimersByTimeAsync(3000)
      await flushPromises()
      expect(api.getDirectoryRun.mock.calls.filter(([runID]: [number]) => runID === 54)).toHaveLength(1)
      expect(invalidateCounts).toHaveBeenCalledTimes(1)
      expect(workItemsApi.getWorkItemCounts).toHaveBeenCalledTimes(1)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('recovers the latest active directory apply run on mount and keeps polling it', async () => {
    vi.useFakeTimers()
    const workItemsApi = await import('@/api/workItems') as any
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns.mockResolvedValueOnce({
        data: {
          data: {
            items: [
              { id: 50, source_id: 1, mode: 'apply', status: 'running', phase: 'applying', department_count: 12, member_count: 34, warning_count: 5 },
            ],
          },
        },
      })
      api.getDirectoryRun.mockResolvedValueOnce({
        data: {
          data: { id: 50, source_id: 1, mode: 'apply', status: 'completed', phase: 'completed', department_count: 12, member_count: 34, warning_count: 0 },
        },
      })
    })

    try {
      await wrapper.vm.$nextTick()
      await flushPromises()

      expect(api.listDirectoryRuns).toHaveBeenCalledWith(1)
      expect(wrapper.text()).toContain('Applying directory facts')
      expect(wrapper.text()).toContain('12 departments · 34 members · 5 skipped records')

      await vi.runOnlyPendingTimersAsync()
      await flushPromises()

      expect(api.getDirectoryRun).toHaveBeenCalledWith(50)
      expect(wrapper.text()).toContain('Run completed: kept 34 valid members; 12 departments.')
      expect(workItemsApi.getWorkItemCounts).toHaveBeenCalledTimes(1)
    } finally {
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
        .mockResolvedValueOnce({ data: { data: { items: [] } } })
        .mockResolvedValueOnce({
          data: {
            data: {
              items: [
                { id: 51, source_id: 1, mode: 'apply', status: 'running', phase: 'executing', department_count: 0, member_count: 0, warning_count: 0 },
              ],
            },
          },
        })
      api.getDirectoryRun.mockResolvedValueOnce({
        data: {
          data: { id: 51, source_id: 1, mode: 'apply', status: 'completed', phase: 'completed', department_count: 2, member_count: 3, warning_count: 0 },
        },
      })
    })

    try {
      await wrapper.get('[data-testid="directory-run-now"]').trigger('click')
      await flushPromises()

      expect(api.listDirectoryRuns).toHaveBeenCalledWith(1)
      expect(wrapper.text()).toContain('Reading directory API')
      expect(wrapper.text()).not.toContain('another full-company apply sync')

      await vi.runOnlyPendingTimersAsync()
      await flushPromises()

      expect(api.getDirectoryRun).toHaveBeenCalledWith(51)
      expect(wrapper.text()).toContain('Run completed: kept 3 valid members; 2 departments.')
    } finally {
      vi.useRealTimers()
    }
  })

  it('shows the latest completed directory apply run on mount without polling', async () => {
    vi.useFakeTimers()
    const workItemsApi = await import('@/api/workItems') as any
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns.mockResolvedValueOnce({
        data: {
          data: {
            items: [
              { id: 52, source_id: 1, mode: 'apply', status: 'completed', phase: 'completed', department_count: 4, member_count: 9, warning_count: 0 },
            ],
          },
        },
      })
    })

    try {
      await wrapper.vm.$nextTick()
      await flushPromises()

      expect(api.listDirectoryRuns).toHaveBeenCalledWith(1)
      expect(wrapper.text()).toContain('Run completed: kept 9 valid members; 4 departments.')

      await vi.runOnlyPendingTimersAsync()
      await flushPromises()

      expect(api.getDirectoryRun).not.toHaveBeenCalledWith(52)
      expect(workItemsApi.getWorkItemCounts).not.toHaveBeenCalled()
    } finally {
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

    secondRun.resolve({
      data: {
        data: {
          items: [
            { id: 62, source_id: 2, mode: 'apply', status: 'completed', phase: 'completed', department_count: 2, member_count: 8, warning_count: 0 },
          ],
        },
      },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Run completed: kept 8 valid members; 2 departments.')

    firstRun.resolve({
      data: {
        data: {
          items: [
            { id: 61, source_id: 1, mode: 'apply', status: 'completed', phase: 'completed', department_count: 9, member_count: 99, warning_count: 0 },
          ],
        },
      },
    })
    await flushPromises()

    expect(api.listDirectoryRuns).toHaveBeenCalledWith(1)
    expect(api.listDirectoryRuns).toHaveBeenCalledWith(2)
    expect(wrapper.text()).toContain('Run completed: kept 8 valid members; 2 departments.')
    expect(wrapper.text()).not.toContain('Run completed: kept 99 valid members; 9 departments.')
  })

  it('prefers an active directory run over a newer terminal preview on mount', async () => {
    vi.useFakeTimers()
    const { wrapper, api } = await mountDirectorySyncSettings((api) => {
      api.listDirectoryRuns.mockResolvedValueOnce({
        data: {
          data: {
            items: [
              { id: 63, source_id: 1, mode: 'preview', status: 'completed', phase: 'completed', department_count: 1, member_count: 5, warning_count: 0 },
              { id: 64, source_id: 1, mode: 'apply', status: 'running', phase: 'applying', department_count: 7, member_count: 11, warning_count: 0 },
            ],
          },
        },
      })
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
    const { wrapper, workItems } = await mountDirectorySyncSettings()
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
    const { wrapper, workItems } = await mountDirectorySyncSettings()
    workItemsApi.getWorkItemCounts.mockReset()
      .mockResolvedValueOnce(countsResponse(1))
      .mockReturnValueOnce(previousCounts.promise)
      .mockReturnValueOnce(freshCounts.promise)
    await workItems.loadCounts()
    const previousLoad = workItems.loadCounts({ force: true })

    await wrapper.get('[data-testid="directory-run-now"]').trigger('click')
    await flushPromises()

    expect(workItemsApi.getWorkItemCounts).toHaveBeenCalledTimes(3)
    previousCounts.resolve(countsResponse(9))
    await previousLoad
    expect(workItems.totalCount).toBe(1)
    expect(workItems.loading).toBe(true)

    freshCounts.resolve(countsResponse(0))
    await flushPromises()
    expect(workItemsApi.getWorkItemCounts).toHaveBeenCalledTimes(3)
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
    expect(prompt).toContain('directory.example.com')
  })
})
