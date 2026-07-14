import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import QuotaResetApprovalSettings from '@/components/settings/QuotaResetApprovalSettings.vue'
import { setLocale } from '@/i18n'

vi.mock('@/api/quotaReset', () => ({
  getQuotaResetApproverConfigs: vi.fn(),
  listQuotaResetApproverCandidates: vi.fn(),
  saveQuotaResetApproverConfigs: vi.fn(),
  getQuotaResetApprovalChains: vi.fn(),
  getQuotaResetApprovalChainOptions: vi.fn(),
  saveQuotaResetApprovalChains: vi.fn(),
  getQuotaResetNotificationSettings: vi.fn(),
  updateQuotaResetNotificationSettings: vi.fn(),
  testQuotaResetNotificationSettings: vi.fn(),
}))

vi.mock('@/api/directory', () => ({
  listDirectorySources: vi.fn(),
  listDirectoryDepartments: vi.fn(),
}))

const departmentAlpha = {
  id: 11,
  source_id: 1,
  external_id: 'dept-alpha',
  name: 'Department Alpha',
  path: '1.2',
  display_path: 'Department Alpha',
}

const departmentBeta = {
  id: 12,
  source_id: 1,
  external_id: 'dept-beta',
  name: 'Department Beta',
  path: '1.3',
  display_path: 'Department Beta',
}

const aliceCandidate = {
  user_id: 12,
  username: 'alice',
  email: 'alice@example.com',
  display_name: 'Alice',
  directory_member_external_id: 'member-alice',
  department_paths: ['Department Alpha / Platform', 'Department Beta'],
  wecom_mention_available: true,
}

const configuredAliceApprover = {
  id: 7,
  directory_source_id: 1,
  department_external_id: 'dept-alpha',
  department_display_path: 'Department Alpha',
  approver_user_id: 12,
  approver_username: 'Alice',
  approver_email: 'alice@example.com',
  enabled: true,
  created_at: '',
  updated_at: '',
}

const chainOptions = {
  groups: [
    { provider_id: 1, group_id: 'group-alpha', group_name: 'Group Alpha', platform: 'openai' },
    { provider_id: 1, group_id: 'group-beta', group_name: 'Group Beta', platform: 'anthropic' },
  ],
  departments: [
    {
      directory_source_id: 1,
      department_external_id: 'dept-alpha',
      department_display_path: 'Department Alpha',
      approver_count: 1,
    },
    {
      directory_source_id: 1,
      department_external_id: 'dept-beta',
      department_display_path: 'Department Beta',
      approver_count: 2,
    },
  ],
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

async function mountSettings(credentials: any[] = []) {
  const wrapper = mount(QuotaResetApprovalSettings, { props: { credentials } })
  await flushPromises()
  return wrapper
}

async function selectApproverDepartment(
  wrapper: VueWrapper,
  externalID = 'dept-alpha',
  filterLabel = 'Filter departments',
) {
  await wrapper.get('[data-testid="quota-reset-department-select"]').trigger('click')
  await flushPromises()
  expect(wrapper.find('[data-testid="quota-reset-department-filter"]').exists()).toBe(true)
  expect(wrapper.get('[data-testid="quota-reset-department-filter"]').attributes('aria-label')).toBe(filterLabel)
  await wrapper.get(`[data-testid="quota-reset-department-option-${externalID}"]`).trigger('click')
  await flushPromises()
}

async function selectChainGroup(wrapper: VueWrapper, groupID: 'group-alpha' | 'group-beta') {
  await wrapper.get('[data-testid="quota-reset-chain-group-select"]').trigger('click')
  await flushPromises()
  expect(wrapper.find('[data-testid="quota-reset-chain-group-filter"]').exists()).toBe(true)
  await wrapper.get(`[data-testid="quota-reset-chain-group-option-1-${groupID}"]`).trigger('click')
  await flushPromises()
}

async function addChainDepartment(wrapper: VueWrapper, externalID: 'dept-alpha' | 'dept-beta') {
  await wrapper.get('[data-testid="quota-reset-chain-department-select"]').trigger('click')
  await flushPromises()
  expect(wrapper.find('[data-testid="quota-reset-chain-department-filter"]').exists()).toBe(true)
  expect(wrapper.get('[data-testid="quota-reset-chain-department-filter"]').attributes('aria-label')).toBe('Filter departments')
  await wrapper.get(`[data-testid="quota-reset-chain-department-option-${externalID}"]`).trigger('click')
  await flushPromises()
}

beforeEach(async () => {
  setLocale('en-US')
  vi.clearAllMocks()

  const api = await import('@/api/quotaReset') as any
  api.getQuotaResetApproverConfigs.mockResolvedValue({ data: { data: { items: [] } } })
  api.listQuotaResetApproverCandidates.mockResolvedValue({
    data: { data: { items: [aliceCandidate], page: 1, page_size: 20, total: 1 } },
  })
  api.saveQuotaResetApproverConfigs.mockResolvedValue({ data: { data: { items: [] } } })
  api.getQuotaResetApprovalChains.mockResolvedValue({ data: { data: { items: [] } } })
  api.getQuotaResetApprovalChainOptions.mockResolvedValue({ data: { data: chainOptions } })
  api.saveQuotaResetApprovalChains.mockResolvedValue({ data: { data: { items: [] } } })
  api.getQuotaResetNotificationSettings.mockResolvedValue({
    data: {
      data: {
        enabled: true,
        channel_type: 'wecom_group_robot',
        template_version: 1,
        url_configured: true,
        url_preview: 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic...redacted',
        url: 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic-saved-robot-key',
        auth_type: 'none',
        credential_id: null,
      },
    },
  })
  api.updateQuotaResetNotificationSettings.mockResolvedValue({
    data: {
      data: {
        enabled: true,
        channel_type: 'wecom_group_robot',
        template_version: 1,
        url_configured: true,
        url_preview: 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic...redacted',
        auth_type: 'none',
        credential_id: null,
      },
    },
  })
  api.testQuotaResetNotificationSettings.mockResolvedValue({
    data: { data: { delivered: true, recipient_count: 1, missing_recipient_count: 0 } },
  })

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
    data: { data: { items: [departmentAlpha, departmentBeta] } },
  })
})

describe('QuotaResetApprovalSettings', () => {
  it('renders one orchestration section and three non-nested settings surfaces', async () => {
    const wrapper = await mountSettings()

    expect(wrapper.get('[data-testid="quota-reset-approval-settings"]').classes()).not.toContain('shadow')
    expect(wrapper.findAll('[data-testid="department-approver-settings"]')).toHaveLength(1)
    expect(wrapper.findAll('[data-testid="subscription-group-approval-chains"]')).toHaveLength(1)
    expect(wrapper.findAll('[data-testid="quota-reset-notification-settings"]')).toHaveLength(1)
  })

  it('searches all matched users and shows WeCom mention coverage', async () => {
    const wrapper = await mountSettings()
    const api = await import('@/api/quotaReset') as any

    expect(wrapper.find('[data-testid="quota-reset-department-filter"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quota-reset-approver-filter"]').exists()).toBe(false)
    await selectApproverDepartment(wrapper)
    expect(api.listQuotaResetApproverCandidates).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="quota-reset-approver-select"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="quota-reset-approver-filter"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="quota-reset-approver-filter"]').attributes('aria-label')).toBe('Search matched users')
    api.listQuotaResetApproverCandidates.mockClear()

    await wrapper.get('[data-testid="quota-reset-approver-filter"]').setValue('alice')
    await flushPromises()

    expect(api.listQuotaResetApproverCandidates).toHaveBeenCalledTimes(1)
    expect(api.listQuotaResetApproverCandidates).toHaveBeenCalledWith({
      source_id: 1,
      q: 'alice',
      page: 1,
      page_size: 20,
    })
    const row = wrapper.get('[data-testid="quota-reset-approver-option-12"]')
    expect(row.text()).toContain('Alice')
    expect(row.text()).toContain('alice@example.com')
    expect(row.text()).toContain('Department Alpha / Platform')
    expect(row.text()).toContain('Department Beta')
    expect(row.text()).toContain('Can mention in WeCom')

    await row.trigger('click')
    await wrapper.get('[data-testid="quota-reset-save-approvers"]').trigger('click')
    await flushPromises()

    expect(api.saveQuotaResetApproverConfigs).toHaveBeenCalledWith([
      {
        department_external_id: 'dept-alpha',
        department_display_path: 'Department Alpha',
        approver_user_id: 12,
        enabled: true,
      },
    ], 'replace_all')
  })

  it('localizes WeCom mention coverage in Chinese', async () => {
    setLocale('zh-CN')
    const wrapper = await mountSettings()
    await selectApproverDepartment(wrapper, 'dept-alpha', '筛选部门')
    await wrapper.get('[data-testid="quota-reset-approver-select"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="quota-reset-approver-option-12"]').text()).toContain('企业微信可 @')
  })

  it('keeps candidate loading and results owned by the latest dropdown search', async () => {
    const api = await import('@/api/quotaReset') as any
    const unfiltered = deferred<any>()
    const filtered = deferred<any>()
    api.listQuotaResetApproverCandidates.mockImplementation(({ q }: { q: string }) => (
      q === 'alice' ? filtered.promise : unfiltered.promise
    ))
    const wrapper = await mountSettings()
    await selectApproverDepartment(wrapper)

    await wrapper.get('[data-testid="quota-reset-approver-select"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-approver-filter"]').setValue('alice')

    unfiltered.resolve({
      data: {
        data: {
          items: [{ ...aliceCandidate, user_id: 99, username: 'all-user', display_name: 'All User' }],
          page: 1,
          page_size: 20,
          total: 1,
        },
      },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="quota-reset-approver-loading"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('All User')

    filtered.resolve({
      data: { data: { items: [aliceCandidate], page: 1, page_size: 20, total: 1 } },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="quota-reset-approver-loading"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Alice')
    expect(wrapper.text()).not.toContain('All User')
  })

  it('shows readable backend names for existing approver rows without raw ids', async () => {
    const api = await import('@/api/quotaReset') as any
    api.getQuotaResetApproverConfigs.mockResolvedValueOnce({
      data: {
        data: {
          items: [
            {
              id: 7,
              directory_source_id: 1,
              department_external_id: 'dept-alpha-internal-id',
              department_display_path: 'Department Alpha / Platform',
              approver_user_id: 998,
              approver_username: 'Alice Admin',
              approver_email: 'alice.admin@example.com',
              enabled: true,
              created_at: '',
              updated_at: '',
            },
          ],
        },
      },
    })

    const wrapper = await mountSettings()
    const row = wrapper.get('[data-testid="quota-reset-config-row-7"]')
    expect(row.text()).toContain('Department Alpha / Platform')
    expect(row.text()).toContain('Alice Admin')
    expect(row.text()).toContain('alice.admin@example.com')
    expect(row.text()).not.toContain('dept-alpha-internal-id')
    expect(row.text()).not.toContain('User #998')
  })

  it('surfaces backend chain-reference details on full-list approver save', async () => {
    const api = await import('@/api/quotaReset') as any
    api.getQuotaResetApproverConfigs.mockResolvedValueOnce({
      data: { data: { items: [configuredAliceApprover] } },
    })
    api.saveQuotaResetApproverConfigs.mockRejectedValueOnce({
      response: {
        data: {
          message: 'Approval chain references Department Alpha in Group Alpha.',
        },
      },
    })
    const wrapper = await mountSettings()

    await wrapper.get('[data-testid="quota-reset-config-remove-7"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-save-approvers"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Approval chain references Department Alpha in Group Alpha.')
    expect(api.getQuotaResetApprovalChains).toHaveBeenCalledTimes(1)
  })

  it('emits saved after full replacement and reloads chains through the parent', async () => {
    const api = await import('@/api/quotaReset') as any
    api.getQuotaResetApproverConfigs.mockResolvedValueOnce({
      data: { data: { items: [configuredAliceApprover] } },
    })
    api.saveQuotaResetApproverConfigs.mockResolvedValueOnce({ data: { data: { items: [] } } })
    const wrapper = await mountSettings()

    await wrapper.get('[data-testid="quota-reset-config-remove-7"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-save-approvers"]').trigger('click')
    await flushPromises()
    expect(api.saveQuotaResetApproverConfigs).toHaveBeenCalledWith([], 'replace_all')
    expect(api.getQuotaResetApprovalChains).toHaveBeenCalledTimes(2)
  })

  it('adds and reorders configured department nodes for one subscription group', async () => {
    const api = await import('@/api/quotaReset') as any
    const wrapper = await mountSettings()
    expect(wrapper.find('[data-testid="quota-reset-chain-group-filter"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quota-reset-chain-department-filter"]').exists()).toBe(false)

    await selectChainGroup(wrapper, 'group-alpha')
    await wrapper.get('[data-testid="quota-reset-chain-group-select"]').trigger('click')
    expect(wrapper.get('[data-testid="quota-reset-chain-group-filter"]').attributes('aria-label')).toBe('Filter subscription groups')
    await wrapper.get('[data-testid="quota-reset-chain-group-select"]').trigger('click')
    await addChainDepartment(wrapper, 'dept-alpha')
    await addChainDepartment(wrapper, 'dept-beta')

    expect(wrapper.findAll('[data-testid^="quota-reset-chain-node-"]')).toHaveLength(2)
    const moveBetaUp = wrapper.get('[data-testid="quota-reset-chain-move-up-dept-beta"]')
    expect(moveBetaUp.attributes('aria-label')).toBe('Move Department Beta up')
    expect(moveBetaUp.attributes('title')).toBe('Move Department Beta up')
    expect(moveBetaUp.find('svg').exists()).toBe(true)
    const moveAlphaDown = wrapper.get('[data-testid="quota-reset-chain-move-down-dept-alpha"]')
    expect(moveAlphaDown.attributes('aria-label')).toBe('Move Department Alpha down')
    expect(moveAlphaDown.find('svg').exists()).toBe(true)
    const removeBeta = wrapper.get('[data-testid="quota-reset-chain-remove-dept-beta"]')
    expect(removeBeta.attributes('aria-label')).toBe('Remove Department Beta')
    expect(removeBeta.attributes('title')).toBe('Remove Department Beta')
    expect(removeBeta.find('svg').exists()).toBe(true)
    await moveBetaUp.trigger('click')
    await wrapper.get('[data-testid="quota-reset-save-chains"]').trigger('click')
    await flushPromises()

    expect(api.saveQuotaResetApprovalChains).toHaveBeenCalledWith([
      {
        provider_id: 1,
        group_id: 'group-alpha',
        group_name: 'Group Alpha',
        enabled: true,
        nodes: [
          {
            directory_source_id: 1,
            department_external_id: 'dept-beta',
            department_display_path: 'Department Beta',
          },
          {
            directory_source_id: 1,
            department_external_id: 'dept-alpha',
            department_display_path: 'Department Alpha',
          },
        ],
      },
    ])
  })

  it('prevents duplicate departments in one chain', async () => {
    const wrapper = await mountSettings()
    await selectChainGroup(wrapper, 'group-alpha')
    await addChainDepartment(wrapper, 'dept-alpha')

    await wrapper.get('[data-testid="quota-reset-chain-department-select"]').trigger('click')
    await flushPromises()
    const duplicate = wrapper.get('[data-testid="quota-reset-chain-department-option-dept-alpha"]')
    expect(duplicate.attributes()).toHaveProperty('disabled')
    await duplicate.trigger('click')
    await flushPromises()

    expect(wrapper.findAll('[data-testid="quota-reset-chain-node-dept-alpha"]')).toHaveLength(1)
  })

  it('saves every subscription group chain atomically', async () => {
    const api = await import('@/api/quotaReset') as any
    api.getQuotaResetApprovalChains.mockResolvedValueOnce({
      data: {
        data: {
          items: [
            {
              id: 20,
              provider_id: 1,
              group_id: 'group-alpha',
              group_name: 'Group Alpha',
              enabled: true,
              nodes: [
                {
                  directory_source_id: 1,
                  department_external_id: 'dept-alpha',
                  department_display_path: 'Department Alpha',
                },
              ],
            },
          ],
        },
      },
    })
    const wrapper = await mountSettings()

    await selectChainGroup(wrapper, 'group-beta')
    expect(wrapper.text()).toContain('No configured department nodes.')
    await wrapper.get('[data-testid="quota-reset-save-chains"]').trigger('click')
    await flushPromises()

    expect(api.saveQuotaResetApprovalChains).toHaveBeenCalledTimes(1)
    expect(api.saveQuotaResetApprovalChains).toHaveBeenCalledWith([
      {
        provider_id: 1,
        group_id: 'group-alpha',
        group_name: 'Group Alpha',
        enabled: true,
        nodes: [
          {
            directory_source_id: 1,
            department_external_id: 'dept-alpha',
            department_display_path: 'Department Alpha',
          },
        ],
      },
      {
        provider_id: 1,
        group_id: 'group-beta',
        group_name: 'Group Beta',
        enabled: true,
        nodes: [],
      },
    ])
  })

  it('surfaces stale chain-reference details from the backend', async () => {
    const api = await import('@/api/quotaReset') as any
    api.saveQuotaResetApprovalChains.mockRejectedValueOnce({
      response: {
        data: {
          message: 'Group Alpha references stale Department Alpha without an enabled approver.',
        },
      },
    })
    const wrapper = await mountSettings()
    await selectChainGroup(wrapper, 'group-alpha')
    await wrapper.get('[data-testid="quota-reset-save-chains"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Group Alpha references stale Department Alpha without an enabled approver.')
  })

  it('selects an explicit notification channel and never displays the saved robot key', async () => {
    const credentials = [
      {
        id: 33,
        name: 'Generic webhook token',
        description: '',
        kind: 'secret_text',
        usage_count: 0,
        summary: { preview: 'syn****ken' },
        created_at: '',
        updated_at: '',
      },
    ]
    const api = await import('@/api/quotaReset') as any
    api.updateQuotaResetNotificationSettings.mockResolvedValueOnce({
      data: {
        data: {
          enabled: true,
          channel_type: 'generic_webhook',
          template_version: 1,
          url_configured: true,
          url_preview: 'https://hooks.example.com/.../redacted',
          auth_type: 'bearer_token',
          credential_id: 33,
        },
      },
    })
    const wrapper = await mountSettings(credentials)

    expect(wrapper.get('[data-testid="quota-reset-notification-channel"]').element).toHaveProperty('value', 'wecom_group_robot')
    expect(wrapper.text()).toContain('synthetic...redacted')
    expect(wrapper.html()).not.toContain('synthetic-saved-robot-key')
    expect((wrapper.get('[data-testid="quota-reset-notification-url"]').element as HTMLInputElement).value).toBe('')

    await wrapper.get('[data-testid="quota-reset-notification-channel"]').setValue('generic_webhook')
    await wrapper.get('[data-testid="quota-reset-notification-url"]').setValue('https://hooks.example.com/quota-reset')
    await wrapper.get('[data-testid="quota-reset-notification-auth"]').setValue('bearer_token')
    await wrapper.get('[data-testid="quota-reset-notification-credential"]').setValue('33')
    await wrapper.get('[data-testid="quota-reset-save-notification"]').trigger('click')
    await flushPromises()

    expect(api.updateQuotaResetNotificationSettings).toHaveBeenCalledWith({
      enabled: true,
      channel_type: 'generic_webhook',
      auth_type: 'bearer_token',
      credential_id: 33,
      url: 'https://hooks.example.com/quota-reset',
    })
    expect(wrapper.text()).toContain('Generic webhook token')
    expect(wrapper.text()).toContain('syn****ken')
  })

  it('shows a preset WeCom preview instead of a raw template editor', async () => {
    const wrapper = await mountSettings()
    const preview = wrapper.get('[data-testid="quota-reset-notification-preview"]')

    expect(preview.text()).toContain('Requester: Alice')
    expect(preview.text()).toContain('Team: Department Alpha / Platform')
    expect(preview.text()).toContain('Reason: Complete a time-sensitive build investigation.')
    expect(preview.text()).toContain('Current node: 2/3 · Department Beta')
    expect(preview.text()).toContain('Progress: 1/3')
    expect(preview.text()).toContain('@Bob')
    expect(wrapper.find('[data-testid="quota-reset-template-editor"]').exists()).toBe(false)
    expect(wrapper.find('textarea').exists()).toBe(false)
  })

  it('shows a warning when a WeCom test is delivered without an at-mention', async () => {
    const api = await import('@/api/quotaReset') as any
    api.testQuotaResetNotificationSettings.mockResolvedValueOnce({
      data: {
        data: {
          delivered: true,
          recipient_count: 0,
          missing_recipient_count: 1,
          warning: 'wecom_recipient_unavailable',
        },
      },
    })
    const wrapper = await mountSettings()

    await wrapper.get('[data-testid="quota-reset-test-notification"]').trigger('click')
    await flushPromises()

    const feedback = wrapper.get('[data-testid="quota-reset-notification-feedback"]')
    expect(feedback.classes()).toContain('bg-amber-50')
    expect(feedback.text()).toContain('Delivered without an @ mention')
  })

  it('preserves an existing URL when the admin does not replace it', async () => {
    const api = await import('@/api/quotaReset') as any
    api.getQuotaResetNotificationSettings.mockResolvedValueOnce({
      data: {
        data: {
          enabled: true,
          channel_type: 'generic_webhook',
          template_version: 1,
          url_configured: true,
          url_preview: 'https://hooks.example.com/.../redacted',
          auth_type: 'none',
          credential_id: null,
        },
      },
    })
    const wrapper = await mountSettings()

    expect((wrapper.get('[data-testid="quota-reset-notification-url"]').element as HTMLInputElement).value).toBe('')
    await wrapper.get('[data-testid="quota-reset-save-notification"]').trigger('click')
    await flushPromises()

    expect(api.updateQuotaResetNotificationSettings).toHaveBeenCalledWith({
      enabled: true,
      channel_type: 'generic_webhook',
      auth_type: 'none',
      credential_id: null,
    })
    expect(api.updateQuotaResetNotificationSettings.mock.calls[0][0]).not.toHaveProperty('url')
  })

  it('requires a credential before saving generic bearer authentication', async () => {
    const api = await import('@/api/quotaReset') as any
    const wrapper = await mountSettings()

    await wrapper.get('[data-testid="quota-reset-notification-channel"]').setValue('generic_webhook')
    await wrapper.get('[data-testid="quota-reset-notification-auth"]').setValue('bearer_token')
    await wrapper.get('[data-testid="quota-reset-save-notification"]').trigger('click')
    await flushPromises()

    expect(api.updateQuotaResetNotificationSettings).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Select a credential for bearer authentication.')
  })

  it('shows backend notification test failure details', async () => {
    const api = await import('@/api/quotaReset') as any
    api.testQuotaResetNotificationSettings.mockRejectedValueOnce({
      response: {
        data: {
          message: 'webhook returned errcode 40008: invalid message type',
        },
      },
    })
    const wrapper = await mountSettings()

    await wrapper.get('[data-testid="quota-reset-test-notification"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('webhook returned errcode 40008: invalid message type')
  })
})
