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

beforeEach(async () => {
  setActivePinia(createPinia())
  setLocale('en-US')
  vi.clearAllMocks()
  const api = await import('@/api/quotaReset') as any
  api.getQuotaResetApproverConfigs.mockResolvedValue({ data: { data: { items: [] } } })
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
  api.getQuotaResetNotificationSettings.mockResolvedValue({ data: { data: { enabled: false, url: '', auth_type: 'none' } } })
  api.updateQuotaResetNotificationSettings.mockResolvedValue({
    data: { data: { enabled: true, url: 'https://hooks.example.com/ai-efficiency', auth_type: 'none' } },
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

    await wrapper.find('input[data-testid="quota-reset-webhook-enabled"]').setValue(true)
    await wrapper.find('input[data-testid="quota-reset-webhook-url"]').setValue('https://hooks.example.com/ai-efficiency')
    await wrapper.find('button[data-testid="quota-reset-save-notification"]').trigger('click')
    await flushPromises()

    const api = await import('@/api/quotaReset')
    expect(api.updateQuotaResetNotificationSettings).toHaveBeenCalledWith(expect.objectContaining({
      enabled: true,
      url: 'https://hooks.example.com/ai-efficiency',
    }))
    expect(wrapper.text()).toContain('Webhook token')
    expect(wrapper.text()).toContain('tes****oken')
    expect(wrapper.text()).not.toContain('test-token')
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

  it('selects approver department and corresponding representative through dropdowns', async () => {
    const wrapper = mount(QuotaResetApprovalSettings, { props: { credentials: [] } })
    await flushPromises()

    expect(wrapper.find('[data-testid="quota-reset-department-search"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quota-reset-department-search-button"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quota-reset-department-filter"]').exists()).toBe(false)

    await wrapper.find('[data-testid="quota-reset-department-select"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="quota-reset-department-filter"]').exists()).toBe(true)

    await wrapper.find('[data-testid="quota-reset-department-filter"]').setValue('Platform')
    await flushPromises()
    await wrapper.find('[data-testid="quota-reset-department-option-dept-alpha"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="quota-reset-approver-ids"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('lead-alpha@example.com')
    await wrapper.find('[data-testid="quota-reset-approver-select"]').setValue('12')
    await wrapper.find('[data-testid="quota-reset-save-approvers"]').trigger('click')
    await flushPromises()

    const quotaReset = await import('@/api/quotaReset') as any
    const directory = await import('@/api/directory') as any
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

  it('keeps the latest filtered departments when the initial unfiltered request resolves later', async () => {
    const directory = await import('@/api/directory') as any
    const unfiltered = deferred<any>()
    const filtered = deferred<any>()
    directory.listDirectoryDepartments.mockImplementation(({ q }: { q: string }) => (
      q === 'Platform' ? filtered.promise : unfiltered.promise
    ))

    const wrapper = mount(QuotaResetApprovalSettings, { props: { credentials: [] } })
    await flushPromises()

    await wrapper.find('[data-testid="quota-reset-department-select"]').trigger('click')
    await wrapper.find('[data-testid="quota-reset-department-filter"]').setValue('Platform')

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

    await wrapper.find('[data-testid="quota-reset-department-select"]').trigger('click')
    await flushPromises()
    await wrapper.find('[data-testid="quota-reset-department-option-dept-alpha"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Directory representatives not matched to local login users: 1')
    expect(wrapper.text()).toContain('Lead Alpha')
    expect(wrapper.text()).toContain('lead-alpha@example.com')
  })
})
