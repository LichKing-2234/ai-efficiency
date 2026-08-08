import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import QuotaResetApprovalSettings from '@/components/settings/QuotaResetApprovalSettings.vue'
import { setLocale } from '@/i18n'

vi.mock('@/api/quotaReset', () => ({
  getQuotaResetApproverConfigs: vi.fn(),
  listQuotaResetApproverCandidates: vi.fn(),
  saveQuotaResetApproverConfigs: vi.fn(),
  getQuotaResetNotificationSettings: vi.fn(),
  updateQuotaResetNotificationSettings: vi.fn(),
  testQuotaResetNotificationSettings: vi.fn(),
}))

vi.mock('@/api/directory', () => ({
  listDirectorySources: vi.fn(),
  listDirectoryDepartments: vi.fn(),
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

async function openElementPlusSelect(wrapper: ReturnType<typeof mount>, testId: string) {
  await wrapper.get(`[data-testid="${testId}"] .el-select__wrapper`).trigger('click')
  await flushPromises()
}

function elementPlusSelectInput(wrapper: ReturnType<typeof mount>, testId: string) {
  return wrapper.get(`[data-testid="${testId}"] input[role="combobox"]`)
}

function installMatchMedia(initialMatches: boolean) {
  const listeners = new Set<(event: { matches: boolean; media: string }) => void>()
  const mediaQuery = {
    matches: initialMatches,
    media: '(min-width: 1280px)',
    onchange: null,
    addEventListener: vi.fn((type: string, listener: (event: { matches: boolean; media: string }) => void) => {
      if (type === 'change') listeners.add(listener)
    }),
    removeEventListener: vi.fn((type: string, listener: (event: { matches: boolean; media: string }) => void) => {
      if (type === 'change') listeners.delete(listener)
    }),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(() => true),
  }
  const matchMedia = vi.fn(() => mediaQuery)
  Object.defineProperty(window, 'matchMedia', { configurable: true, value: matchMedia })

  return {
    mediaQuery,
    matchMedia,
    change(matches: boolean) {
      mediaQuery.matches = matches
      for (const listener of listeners) listener({ matches, media: mediaQuery.media })
    },
  }
}

let matchMediaController: ReturnType<typeof installMatchMedia>

beforeEach(async () => {
  setActivePinia(createPinia())
  setLocale('en-US')
  vi.clearAllMocks()
  matchMediaController = installMatchMedia(true)
  const api = await import('@/api/quotaReset') as any
  api.getQuotaResetApproverConfigs.mockResolvedValue({ data: { data: { current_directory_source_id: 1, items: [] } } })
  api.listQuotaResetApproverCandidates.mockResolvedValue({
    data: {
      data: {
        items: [
          {
            user_id: 12,
            username: 'lead-alpha',
            email: 'lead-alpha@example.com',
            display_name: 'Lead Alpha',
            directory_member_external_id: 'member-alpha-lead',
          },
        ],
      },
    },
  })
  api.saveQuotaResetApproverConfigs.mockResolvedValue({ data: { data: { items: [] } } })
  api.getQuotaResetNotificationSettings.mockResolvedValue({ data: { data: { enabled: false, channel: 'generic_webhook', url: '', auth_type: 'none' } } })
  api.updateQuotaResetNotificationSettings.mockResolvedValue({
    data: { data: { enabled: true, channel: 'wecom_group_robot', url: 'https://hooks.example.com/ai-efficiency', auth_type: 'none' } },
  })
  api.testQuotaResetNotificationSettings.mockResolvedValue({ data: { data: { message: 'ok' } } })
  const directory = await import('@/api/directory') as any
  directory.listDirectorySources.mockResolvedValue({
    data: {
      data: {
        items: [
          {
            id: 1,
            name: 'Directory Alpha',
            description: '',
            scope: 'full_company',
            enabled: true,
            dsl: '',
            schedule_enabled: false,
            schedule_interval: 'daily',
            schedule_timezone: 'UTC',
            last_successful_run_id: 10,
          },
        ],
      },
    },
  })
  directory.listDirectoryDepartments.mockResolvedValue({
    data: {
      data: {
        items: [
          {
            id: 11,
            source_id: 1,
            external_id: 'dept-alpha',
            name: 'Platform',
            path: '1.488797.1684075.1684077.1684207',
            display_path: 'Department Alpha / Platform',
          },
        ],
      },
    },
  })
})

describe('QuotaResetApprovalSettings', () => {
  it('renders quota-reset policy actions with Element Plus controls', async () => {
    const wrapper = mount(QuotaResetApprovalSettings, { props: { credentials: [] } })
    await flushPromises()

    expect(wrapper.get('[data-testid="quota-reset-save-approvers"]').classes()).toContain('el-button')
  })

  it('uses Element Plus selects for department and approver choices', async () => {
    const wrapper = mount(QuotaResetApprovalSettings, { props: { credentials: [] } })
    await flushPromises()

    expect(wrapper.get('[data-testid="quota-reset-department-select"]').classes()).toContain('el-select')
    expect(wrapper.get('[data-testid="quota-reset-approver-select"]').classes()).toContain('el-select')
  })

  it('shows a settings load failure without a contradictory no-approvers state', async () => {
    const api = await import('@/api/quotaReset') as any
    api.getQuotaResetApproverConfigs.mockRejectedValueOnce(new Error('configs unavailable'))
    const wrapper = mount(QuotaResetApprovalSettings, { props: { credentials: [] } })
    await flushPromises()

    expect(wrapper.text()).toContain('Failed to load quota reset approval settings')
    expect(wrapper.text()).not.toContain('No approver configs yet.')
  })

  it('removes existing approver rows with explicit full replacement save', async () => {
    const api = await import('@/api/quotaReset') as any
    api.getQuotaResetApproverConfigs.mockResolvedValueOnce({
      data: {
        data: {
          items: [
            {
              id: 7,
              directory_source_id: 1,
              department_external_id: 'dept-alpha',
              department_display_path: 'Department Alpha',
              approver_user_id: 12,
              approver_username: 'lead',
              approver_email: 'lead@example.com',
              enabled: true,
              created_at: '',
              updated_at: '',
            },
          ],
        },
      },
    })
    api.saveQuotaResetApproverConfigs.mockResolvedValueOnce({ data: { data: { items: [] } } })
    const wrapper = mount(QuotaResetApprovalSettings, { props: { credentials: [] } })
    await flushPromises()

    expect(wrapper.text()).toContain('Department Alpha')
    expect(wrapper.find('input[aria-label="Display path"]').exists()).toBe(false)
    await wrapper.find('[data-testid="quota-reset-config-remove-7"]').trigger('click')
    await wrapper.find('[data-testid="quota-reset-save-approvers"]').trigger('click')
    await flushPromises()

    expect(api.saveQuotaResetApproverConfigs).toHaveBeenCalledWith([], 'replace_all')
  })

  it('mounts one responsive approver-config representation without horizontal scrolling', async () => {
    const api = await import('@/api/quotaReset') as any
    api.getQuotaResetApproverConfigs.mockResolvedValueOnce({
      data: {
        data: {
          current_directory_source_id: 1,
          items: [
            {
              id: 7,
              directory_source_id: 1,
              department_external_id: 'dept-alpha',
              department_display_path: 'Department Alpha',
              approver_user_id: 12,
              approver_username: 'lead',
              approver_email: 'lead@example.com',
              enabled: true,
              created_at: '',
              updated_at: '',
            },
          ],
        },
      },
    })
    const wrapper = mount(QuotaResetApprovalSettings, { props: { credentials: [] } })
    await flushPromises()

    expect(wrapper.find('[data-approver-config-list="desktop"]').exists()).toBe(true)
    expect(wrapper.find('[data-approver-config-list="mobile"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-approver-config-row]')).toHaveLength(1)
    expect(matchMediaController.matchMedia).toHaveBeenCalledWith('(min-width: 1280px)')

    matchMediaController.change(false)
    await wrapper.vm.$nextTick()

    expect(wrapper.find('[data-approver-config-list="desktop"]').exists()).toBe(false)
    expect(wrapper.find('[data-approver-config-list="mobile"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-approver-config-row]')).toHaveLength(1)
    expect(wrapper.find('[data-approver-config-list="mobile"]').classes()).not.toContain('overflow-x-auto')
  })

  it('saves webhook settings without exposing credential secrets', async () => {
    const wrapper = mount(QuotaResetApprovalSettings, {
      props: {
        credentials: [
          {
            id: 1,
            name: 'Webhook token',
            description: '',
            kind: 'secret_text',
            usage_count: 0,
            summary: { preview: 'tes****oken' },
            created_at: '',
            updated_at: '',
          },
        ],
      },
    })
    await flushPromises()

    await wrapper.get('[data-testid="quota-reset-webhook-enabled"]').trigger('click')
    await wrapper.find('input[data-testid="quota-reset-webhook-url"]').setValue('https://hooks.example.com/ai-efficiency')
    await wrapper.find('button[data-testid="quota-reset-save-notification"]').trigger('click')
    await flushPromises()

    const api = await import('@/api/quotaReset')
    expect(api.updateQuotaResetNotificationSettings).toHaveBeenCalledWith(expect.objectContaining({
      enabled: true,
      channel: 'generic_webhook',
      url: 'https://hooks.example.com/ai-efficiency',
    }))
    expect(wrapper.text()).toContain('Webhook token')
    expect(wrapper.text()).toContain('tes****oken')
    expect(wrapper.text()).not.toContain('test-token')
  })

  it('does not render subscription group approval chain settings', async () => {
    const wrapper = mount(QuotaResetApprovalSettings, { props: { credentials: [] } })
    await flushPromises()

    expect(wrapper.find('[data-testid="quota-reset-chain-group"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quota-reset-chain-save"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Subscription group approval chains')
  })

  it('shows backend webhook test failure reason', async () => {
    const api = await import('@/api/quotaReset') as any
    api.testQuotaResetNotificationSettings.mockRejectedValueOnce({
      response: {
        data: {
          message: 'webhook returned errcode 40008: invalid message type',
        },
      },
    })
    const wrapper = mount(QuotaResetApprovalSettings, { props: { credentials: [] } })
    await flushPromises()

    const testButton = wrapper.findAll('button').find((button) => button.text() === 'Test webhook')
    expect(testButton).toBeTruthy()
    await testButton!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('webhook returned errcode 40008: invalid message type')
  })

  it('keeps the selected approver visible while filtering the opened picker', async () => {
    const quotaReset = await import('@/api/quotaReset') as any
    quotaReset.listQuotaResetApproverCandidates.mockResolvedValueOnce({
      data: {
        data: {
          items: [
            {
              user_id: 12,
              username: 'lead-alpha',
              email: 'lead-alpha@example.com',
              display_name: 'Lead Alpha',
              directory_member_external_id: 'member-alpha-lead',
              representative: true,
            },
            {
              user_id: 13,
              username: 'reviewer-beta',
              email: 'reviewer-beta@example.com',
              display_name: 'Reviewer Beta',
              directory_member_external_id: 'member-beta-reviewer',
              representative: false,
            },
          ],
        },
      },
    })
    const wrapper = mount(QuotaResetApprovalSettings, { props: { credentials: [] } })
    await flushPromises()

    await openElementPlusSelect(wrapper, 'quota-reset-department-select')
    await elementPlusSelectInput(wrapper, 'quota-reset-department-select').setValue('Platform')
    const directory = await import('@/api/directory') as any
    await vi.waitFor(() => {
      expect(directory.listDirectoryDepartments).toHaveBeenCalledWith({ source_id: 1, q: 'Platform' })
    })
    await wrapper.find('[data-testid="quota-reset-department-option-dept-alpha"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="quota-reset-approver-ids"]').exists()).toBe(false)

    const picker = wrapper.get('[data-testid="quota-reset-approver-select"]')
    await openElementPlusSelect(wrapper, 'quota-reset-approver-select')
    await wrapper.get('[data-testid="quota-reset-approver-option-12"]').trigger('click')

    expect(elementPlusSelectInput(wrapper, 'quota-reset-approver-select').attributes('aria-expanded')).toBe('false')
    expect(picker.text()).toContain('Lead Alpha')
    expect(picker.text()).toContain('lead-alpha@example.com')
    expect(picker.text()).toContain('Representative')

    await openElementPlusSelect(wrapper, 'quota-reset-approver-select')
    await elementPlusSelectInput(wrapper, 'quota-reset-approver-select').setValue('beta')
    await flushPromises()
    expect(wrapper.find('[data-testid="quota-reset-approver-option-12"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quota-reset-approver-option-13"]').exists()).toBe(true)
    expect(picker.text()).toContain('Lead Alpha')
    expect(picker.text()).toContain('Representative')

    await wrapper.find('[data-testid="quota-reset-save-approvers"]').trigger('click')
    await flushPromises()

    expect(directory.listDirectoryDepartments).toHaveBeenCalledWith({ source_id: 1, q: 'Platform' })
    expect(quotaReset.listQuotaResetApproverCandidates).toHaveBeenCalledWith({
      source_id: 1,
      department_external_id: 'dept-alpha',
    })
    expect(quotaReset.saveQuotaResetApproverConfigs).toHaveBeenCalledWith([
      {
        department_external_id: 'dept-alpha',
        department_display_path: 'Department Alpha / Platform',
        approver_user_id: 12,
        enabled: true,
      },
    ], 'replace_all')
  })

  it('uses the backend-selected current directory source', async () => {
    const api = await import('@/api/quotaReset') as any
    const directory = await import('@/api/directory') as any
    api.getQuotaResetApproverConfigs.mockResolvedValueOnce({
      data: { data: { current_directory_source_id: 2, items: [] } },
    })
    directory.listDirectorySources.mockResolvedValueOnce({
      data: {
        data: {
          items: [
            { id: 1, name: 'Old Directory', last_successful_run_id: 20 },
            { id: 2, name: 'Current Directory', last_successful_run_id: 21 },
          ],
        },
      },
    })

    const wrapper = mount(QuotaResetApprovalSettings, { props: { credentials: [] } })
    await flushPromises()
    await openElementPlusSelect(wrapper, 'quota-reset-department-select')

    expect(directory.listDirectoryDepartments).toHaveBeenCalledWith(expect.objectContaining({ source_id: 2 }))
    expect(wrapper.text()).not.toContain('Old Directory')
  })

  it('filters member picker options after opening the dropdown', async () => {
    const api = await import('@/api/quotaReset') as any
    api.listQuotaResetApproverCandidates.mockResolvedValueOnce({
      data: {
        data: {
          items: [
            {
              user_id: 12,
              username: 'lead-alpha',
              email: 'lead-alpha@example.com',
              display_name: 'Lead Alpha',
              directory_member_external_id: 'member-alpha-lead',
              representative: true,
            },
            {
              user_id: 13,
              username: 'reviewer-beta',
              email: 'reviewer-beta@example.com',
              display_name: 'Reviewer Beta',
              directory_member_external_id: 'member-beta-reviewer',
              representative: false,
            },
          ],
        },
      },
    })
    const wrapper = mount(QuotaResetApprovalSettings, { props: { credentials: [] } })
    await flushPromises()
    await openElementPlusSelect(wrapper, 'quota-reset-department-select')
    await wrapper.get('[data-testid="quota-reset-department-option-dept-alpha"]').trigger('click')
    await flushPromises()

    await openElementPlusSelect(wrapper, 'quota-reset-approver-select')
    await elementPlusSelectInput(wrapper, 'quota-reset-approver-select').setValue('beta')
    await flushPromises()

    expect(wrapper.find('[data-testid="quota-reset-approver-option-12"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quota-reset-approver-option-13"]').exists()).toBe(true)
  })

  it('keeps the latest filtered departments when the initial unfiltered request resolves later', async () => {
    const directory = await import('@/api/directory') as any
    const unfiltered = deferred<any>()
    const filtered = deferred<any>()
    directory.listDirectoryDepartments.mockImplementation(({ q }: { q: string }) => (
      q === 'Platform' ? filtered.promise : unfiltered.promise
    ))

    const wrapper = mount(QuotaResetApprovalSettings, { props: { credentials: [] } })
    await flushPromises()

    await openElementPlusSelect(wrapper, 'quota-reset-department-select')
    await elementPlusSelectInput(wrapper, 'quota-reset-department-select').setValue('Platform')
    await vi.waitFor(() => {
      expect(directory.listDirectoryDepartments).toHaveBeenCalledWith({ source_id: 1, q: 'Platform' })
    })

    filtered.resolve({
      data: {
        data: {
          items: [
            {
              id: 11,
              source_id: 1,
              external_id: 'dept-platform',
              name: 'Platform',
              path: '1.2',
              display_path: 'Department Alpha / Platform',
            },
          ],
        },
      },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="quota-reset-department-option-dept-platform"]').exists()).toBe(true)

    unfiltered.resolve({
      data: {
        data: {
          items: [
            {
              id: 12,
              source_id: 1,
              external_id: 'dept-all',
              name: 'All Departments',
              path: '1',
              display_path: 'All Departments',
            },
          ],
        },
      },
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="quota-reset-department-option-dept-platform"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="quota-reset-department-option-dept-all"]').exists()).toBe(false)
  })

  it('explains when directory representatives are not matched to local users', async () => {
    const quotaReset = await import('@/api/quotaReset') as any
    quotaReset.listQuotaResetApproverCandidates.mockResolvedValueOnce({
      data: {
        data: {
          items: [],
          unmatched_representatives: [
            {
              directory_member_external_id: 'member-alpha-lead',
              display_name: 'Lead Alpha',
              email: 'lead-alpha@example.com',
            },
          ],
        },
      },
    })
    const wrapper = mount(QuotaResetApprovalSettings, { props: { credentials: [] } })
    await flushPromises()

    await openElementPlusSelect(wrapper, 'quota-reset-department-select')
    await wrapper.find('[data-testid="quota-reset-department-option-dept-alpha"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Directory representatives not matched to local login users: 1')
    expect(wrapper.text()).toContain('Lead Alpha')
    expect(wrapper.text()).toContain('lead-alpha@example.com')
  })
})
