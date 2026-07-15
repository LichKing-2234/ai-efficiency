import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import DepartmentApproverSettings from '@/components/settings/DepartmentApproverSettings.vue'
import QuotaResetApprovalSettings from '@/components/settings/QuotaResetApprovalSettings.vue'
import QuotaResetNotificationSettings from '@/components/settings/QuotaResetNotificationSettings.vue'
import SubscriptionGroupApprovalChains from '@/components/settings/SubscriptionGroupApprovalChains.vue'
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
  getDirectoryRun: vi.fn(),
  listDirectoryRuns: vi.fn(),
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

const malformedApproverConfigResponses = [
  {
    name: 'a null row',
    data: { directory_source_id: 1, items: [null] },
  },
  {
    name: 'an empty row',
    data: { directory_source_id: 1, items: [{}] },
  },
  {
    name: 'an unsafe id',
    data: {
      directory_source_id: 1,
      items: [{ ...configuredAliceApprover, id: Number.MAX_SAFE_INTEGER + 1 }],
    },
  },
  {
    name: 'a non-positive row source id',
    data: {
      directory_source_id: 1,
      items: [{ ...configuredAliceApprover, directory_source_id: 0 }],
    },
  },
  {
    name: 'a non-string department id',
    data: {
      directory_source_id: 1,
      items: [{ ...configuredAliceApprover, department_external_id: 17 }],
    },
  },
  {
    name: 'a non-string department path',
    data: {
      directory_source_id: 1,
      items: [{ ...configuredAliceApprover, department_display_path: false }],
    },
  },
  {
    name: 'a non-positive approver user id',
    data: {
      directory_source_id: 1,
      items: [{ ...configuredAliceApprover, approver_user_id: 0 }],
    },
  },
  {
    name: 'a non-string approver username',
    data: {
      directory_source_id: 1,
      items: [{ ...configuredAliceApprover, approver_username: null }],
    },
  },
  {
    name: 'a non-string approver email',
    data: {
      directory_source_id: 1,
      items: [{ ...configuredAliceApprover, approver_email: [] }],
    },
  },
  {
    name: 'a non-boolean enabled value',
    data: {
      directory_source_id: 1,
      items: [{ ...configuredAliceApprover, enabled: 'true' }],
    },
  },
  {
    name: 'a non-string created timestamp',
    data: {
      directory_source_id: 1,
      items: [{ ...configuredAliceApprover, created_at: 1 }],
    },
  },
  {
    name: 'a non-string updated timestamp',
    data: {
      directory_source_id: 1,
      items: [{ ...configuredAliceApprover, updated_at: {} }],
    },
  },
  {
    name: 'a row source that differs from the response source',
    data: {
      directory_source_id: 2,
      items: [configuredAliceApprover],
    },
  },
  {
    name: 'items when the response source is null',
    data: {
      directory_source_id: null,
      items: [configuredAliceApprover],
    },
  },
]

function approverConfigResponse(items: any[] = [], directorySourceID: number | null = 1) {
  return {
    data: {
      data: {
        directory_source_id: directorySourceID,
        items,
      },
    },
  }
}

function pagedCandidate(userID: number) {
  return {
    ...aliceCandidate,
    user_id: userID,
    username: `candidate-${userID}`,
    email: `candidate-${userID}@example.com`,
    display_name: `Candidate ${userID}`,
    directory_member_external_id: `member-${userID}`,
  }
}

function directorySource(id: number, name: string, lastSuccessfulRunID: number) {
  return {
    id,
    name,
    description: '',
    scope: 'full_company',
    enabled: true,
    dsl: '',
    schedule_enabled: false,
    schedule_interval: 'daily',
    schedule_timezone: 'UTC',
    last_successful_run_id: lastSuccessfulRunID,
  }
}

function completedApplyRun(id: number, sourceID: number, completedAt: string) {
  return {
    id,
    source_id: sourceID,
    mode: 'apply',
    status: 'completed',
    completed_at: completedAt,
  }
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

const configuredAlphaChain = {
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
}

const malformedChainListResponses = [
  { name: 'a null response', data: null },
  { name: 'an array response', data: [] },
  { name: 'missing items', data: {} },
  { name: 'non-array items', data: { items: null } },
  { name: 'a null chain', data: { items: [null] } },
  { name: 'an array chain', data: { items: [[]] } },
  { name: 'an empty chain object', data: { items: [{}] } },
  { name: 'a non-positive chain id', data: { items: [{ ...configuredAlphaChain, id: 0 }] } },
  { name: 'a non-number chain id', data: { items: [{ ...configuredAlphaChain, id: '20' }] } },
  {
    name: 'an unsafe chain id',
    data: { items: [{ ...configuredAlphaChain, id: Number.MAX_SAFE_INTEGER + 1 }] },
  },
  { name: 'a non-positive provider id', data: { items: [{ ...configuredAlphaChain, provider_id: 0 }] } },
  {
    name: 'an unsafe provider id',
    data: { items: [{ ...configuredAlphaChain, provider_id: Number.MAX_SAFE_INTEGER + 1 }] },
  },
  { name: 'a non-string group id', data: { items: [{ ...configuredAlphaChain, group_id: 17 }] } },
  { name: 'an empty group id', data: { items: [{ ...configuredAlphaChain, group_id: '  ' }] } },
  { name: 'a non-string group name', data: { items: [{ ...configuredAlphaChain, group_name: null }] } },
  { name: 'a non-boolean enabled value', data: { items: [{ ...configuredAlphaChain, enabled: 'true' }] } },
  { name: 'non-array nodes', data: { items: [{ ...configuredAlphaChain, nodes: null }] } },
  { name: 'a null node', data: { items: [{ ...configuredAlphaChain, nodes: [null] }] } },
  { name: 'an array node', data: { items: [{ ...configuredAlphaChain, nodes: [[]] }] } },
  { name: 'an empty node object', data: { items: [{ ...configuredAlphaChain, nodes: [{}] }] } },
  {
    name: 'a non-positive node source id',
    data: { items: [{ ...configuredAlphaChain, nodes: [{ ...configuredAlphaChain.nodes[0], directory_source_id: 0 }] }] },
  },
  {
    name: 'a non-number node source id',
    data: { items: [{ ...configuredAlphaChain, nodes: [{ ...configuredAlphaChain.nodes[0], directory_source_id: '1' }] }] },
  },
  {
    name: 'an unsafe node source id',
    data: {
      items: [{
        ...configuredAlphaChain,
        nodes: [{ ...configuredAlphaChain.nodes[0], directory_source_id: Number.MAX_SAFE_INTEGER + 1 }],
      }],
    },
  },
  {
    name: 'a non-string department id',
    data: { items: [{ ...configuredAlphaChain, nodes: [{ ...configuredAlphaChain.nodes[0], department_external_id: 17 }] }] },
  },
  {
    name: 'an empty department id',
    data: { items: [{ ...configuredAlphaChain, nodes: [{ ...configuredAlphaChain.nodes[0], department_external_id: ' ' }] }] },
  },
  {
    name: 'a non-string department path',
    data: { items: [{ ...configuredAlphaChain, nodes: [{ ...configuredAlphaChain.nodes[0], department_display_path: false }] }] },
  },
]

const malformedChainOptionsResponses = [
  { name: 'a null response', data: null },
  { name: 'an array response', data: [] },
  { name: 'missing groups', data: { departments: [] } },
  { name: 'missing departments', data: { groups: [] } },
  { name: 'non-array groups', data: { groups: null, departments: [] } },
  { name: 'non-array departments', data: { groups: [], departments: null } },
  { name: 'a null group', data: { ...chainOptions, groups: [null] } },
  { name: 'an array group', data: { ...chainOptions, groups: [[]] } },
  { name: 'an empty group object', data: { ...chainOptions, groups: [{}] } },
  {
    name: 'a non-positive group provider id',
    data: { ...chainOptions, groups: [{ ...chainOptions.groups[0], provider_id: 0 }] },
  },
  {
    name: 'an unsafe group provider id',
    data: { ...chainOptions, groups: [{ ...chainOptions.groups[0], provider_id: Number.MAX_SAFE_INTEGER + 1 }] },
  },
  {
    name: 'a non-string option group id',
    data: { ...chainOptions, groups: [{ ...chainOptions.groups[0], group_id: 17 }] },
  },
  {
    name: 'an empty option group id',
    data: { ...chainOptions, groups: [{ ...chainOptions.groups[0], group_id: '' }] },
  },
  {
    name: 'a non-string option group name',
    data: { ...chainOptions, groups: [{ ...chainOptions.groups[0], group_name: false }] },
  },
  {
    name: 'a non-string platform',
    data: { ...chainOptions, groups: [{ ...chainOptions.groups[0], platform: null }] },
  },
  { name: 'a null department', data: { ...chainOptions, departments: [null] } },
  { name: 'an array department', data: { ...chainOptions, departments: [[]] } },
  { name: 'an empty department object', data: { ...chainOptions, departments: [{}] } },
  {
    name: 'a non-positive department source id',
    data: { ...chainOptions, departments: [{ ...chainOptions.departments[0], directory_source_id: 0 }] },
  },
  {
    name: 'an unsafe department source id',
    data: {
      ...chainOptions,
      departments: [{ ...chainOptions.departments[0], directory_source_id: Number.MAX_SAFE_INTEGER + 1 }],
    },
  },
  {
    name: 'a non-string option department id',
    data: { ...chainOptions, departments: [{ ...chainOptions.departments[0], department_external_id: 17 }] },
  },
  {
    name: 'an empty option department id',
    data: { ...chainOptions, departments: [{ ...chainOptions.departments[0], department_external_id: ' ' }] },
  },
  {
    name: 'a non-string option department path',
    data: { ...chainOptions, departments: [{ ...chainOptions.departments[0], department_display_path: null }] },
  },
  {
    name: 'a negative approver count',
    data: { ...chainOptions, departments: [{ ...chainOptions.departments[0], approver_count: -1 }] },
  },
  {
    name: 'a fractional approver count',
    data: { ...chainOptions, departments: [{ ...chainOptions.departments[0], approver_count: 1.5 }] },
  },
  {
    name: 'a non-number approver count',
    data: { ...chainOptions, departments: [{ ...chainOptions.departments[0], approver_count: '1' }] },
  },
  {
    name: 'an unsafe approver count',
    data: {
      ...chainOptions,
      departments: [{ ...chainOptions.departments[0], approver_count: Number.MAX_SAFE_INTEGER + 1 }],
    },
  },
]

function chainOptionSet(groupID: string, groupName: string, departmentID: string, departmentName: string) {
  return {
    groups: [{ provider_id: 1, group_id: groupID, group_name: groupName, platform: 'openai' }],
    departments: [{
      directory_source_id: 1,
      department_external_id: departmentID,
      department_display_path: departmentName,
      approver_count: 1,
    }],
  }
}

function savedChain(id: number, groupID: string, groupName: string, departmentID: string, departmentName: string) {
  return {
    id,
    provider_id: 1,
    group_id: groupID,
    group_name: groupName,
    enabled: true,
    nodes: [{
      directory_source_id: 1,
      department_external_id: departmentID,
      department_display_path: departmentName,
    }],
  }
}

function notificationSettings(overrides: Record<string, unknown> = {}) {
  return {
    enabled: true,
    channel_type: 'wecom_group_robot',
    template_version: 1,
    url_configured: true,
    url_preview: 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic...redacted',
    auth_type: 'none',
    credential_id: null,
    ...overrides,
  }
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

async function mountDepartmentApprovers() {
  const wrapper = mount(DepartmentApproverSettings)
  await flushPromises()
  return wrapper
}

async function mountChains(approverRevision = 0) {
  const wrapper = mount(SubscriptionGroupApprovalChains, { props: { approverRevision } })
  await flushPromises()
  return wrapper
}

async function mountNotification(credentials: any[] = []) {
  const wrapper = mount(QuotaResetNotificationSettings, { props: { credentials } })
  await flushPromises()
  return wrapper
}

async function forceClick(button: any) {
  ;(button.element as HTMLButtonElement).disabled = false
  await button.trigger('click')
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

async function selectChainGroup(wrapper: VueWrapper, groupID: string) {
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
  vi.resetAllMocks()

  const api = await import('@/api/quotaReset') as any
  api.getQuotaResetApproverConfigs.mockResolvedValue(approverConfigResponse())
  api.listQuotaResetApproverCandidates.mockResolvedValue({
    data: { data: { items: [aliceCandidate], page: 1, page_size: 20, total: 1 } },
  })
  api.saveQuotaResetApproverConfigs.mockResolvedValue(approverConfigResponse())
  api.getQuotaResetApprovalChains.mockResolvedValue({ data: { data: { items: [] } } })
  api.getQuotaResetApprovalChainOptions.mockResolvedValue({ data: { data: chainOptions } })
  api.saveQuotaResetApprovalChains.mockResolvedValue({ data: { data: { items: [] } } })
  api.getQuotaResetNotificationSettings.mockResolvedValue({
    data: {
      data: {
        ...notificationSettings(),
        url: 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic-saved-robot-key',
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
        items: [directorySource(1, 'Directory Alpha', 10)],
      },
    },
  })
  directory.listDirectoryRuns.mockResolvedValue({
    data: {
      data: {
        items: [completedApplyRun(10, 1, '2026-07-14T08:00:00Z')],
      },
    },
  })
  directory.getDirectoryRun.mockResolvedValue({
    data: { data: completedApplyRun(10, 1, '2026-07-14T08:00:00Z') },
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

  it('does not save approver defaults before the authoritative config load completes', async () => {
    const api = await import('@/api/quotaReset') as any
    const configRequest = deferred<any>()
    api.getQuotaResetApproverConfigs.mockReturnValueOnce(configRequest.promise)
    const wrapper = await mountDepartmentApprovers()
    const saveButton = wrapper.get('[data-testid="quota-reset-save-approvers"]')

    expect(saveButton.attributes()).toHaveProperty('disabled')
    await forceClick(saveButton)
    await flushPromises()
    expect(api.saveQuotaResetApproverConfigs).not.toHaveBeenCalled()

    configRequest.resolve(approverConfigResponse())
    await flushPromises()
    expect(wrapper.get('[data-testid="quota-reset-save-approvers"]').attributes()).not.toHaveProperty('disabled')
  })

  it('guards approver full replacement after an authoritative config load failure', async () => {
    const api = await import('@/api/quotaReset') as any
    api.getQuotaResetApproverConfigs.mockRejectedValueOnce({
      response: { data: { message: 'Synthetic approver config load failed.' } },
    })
    const wrapper = await mountDepartmentApprovers()
    const saveButton = wrapper.get('[data-testid="quota-reset-save-approvers"]')

    expect(wrapper.text()).toContain('Synthetic approver config load failed.')
    expect(saveButton.attributes()).toHaveProperty('disabled')
    await forceClick(saveButton)
    await flushPromises()
    expect(api.saveQuotaResetApproverConfigs).not.toHaveBeenCalled()
  })

  it('preserves approver rows across a failed refresh and re-enables after recovery', async () => {
    const api = await import('@/api/quotaReset') as any
    api.getQuotaResetApproverConfigs.mockResolvedValueOnce(approverConfigResponse([configuredAliceApprover]))
    const wrapper = await mountDepartmentApprovers()
    expect(wrapper.text()).toContain('Department Alpha')

    api.getQuotaResetApproverConfigs.mockRejectedValueOnce({
      response: { data: { message: 'Synthetic approver refresh failed.' } },
    })
    await wrapper.get('[data-testid="quota-reset-reload-approvers"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Department Alpha')
    expect(wrapper.text()).toContain('Synthetic approver refresh failed.')
    const disabledSave = wrapper.get('[data-testid="quota-reset-save-approvers"]')
    expect(disabledSave.attributes()).toHaveProperty('disabled')
    await forceClick(disabledSave)
    await flushPromises()
    expect(api.saveQuotaResetApproverConfigs).not.toHaveBeenCalled()

    api.getQuotaResetApproverConfigs.mockResolvedValueOnce(approverConfigResponse([configuredAliceApprover]))
    await wrapper.get('[data-testid="quota-reset-reload-approvers"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="quota-reset-save-approvers"]').attributes()).not.toHaveProperty('disabled')
  })

  it.each(malformedApproverConfigResponses)(
    'rejects malformed approver config GET responses with $name',
    async ({ data }) => {
      const api = await import('@/api/quotaReset') as any
      const directory = await import('@/api/directory') as any
      api.getQuotaResetApproverConfigs.mockResolvedValueOnce(
        approverConfigResponse([configuredAliceApprover], 1),
      )
      directory.listDirectorySources.mockResolvedValueOnce({
        data: {
          data: {
            items: [
              directorySource(1, 'Directory Alpha', 10),
              directorySource(2, 'Directory Beta', 20),
            ],
          },
        },
      })
      const wrapper = await mountDepartmentApprovers()
      expect(wrapper.find('[data-testid="quota-reset-config-row-7"]').exists()).toBe(true)

      api.getQuotaResetApproverConfigs.mockResolvedValueOnce({ data: { data } })
      await wrapper.get('[data-testid="quota-reset-reload-approvers"]').trigger('click')
      await flushPromises()

      expect(wrapper.text()).toContain('Failed to load department approvers')
      expect(wrapper.get('[data-testid="quota-reset-current-directory-source"]').text()).toBe('Directory Alpha')
      expect(wrapper.find('[data-testid="quota-reset-config-row-7"]').exists()).toBe(true)
      expect(wrapper.get('[data-testid="quota-reset-save-approvers"]').attributes()).toHaveProperty('disabled')
    },
  )

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

  it('uses the backend-selected current source without client-side run ranking', async () => {
    const directory = await import('@/api/directory') as any
    const api = await import('@/api/quotaReset') as any
    api.getQuotaResetApproverConfigs.mockResolvedValueOnce(approverConfigResponse([], 1))
    directory.listDirectorySources.mockResolvedValueOnce({
      data: {
        data: {
          items: [
            directorySource(1, 'Backend Directory', 10),
            directorySource(2, 'Newer-looking Directory', 20),
          ],
        },
      },
    })
    directory.getDirectoryRun.mockImplementation((runID: number) => Promise.resolve({
      data: {
        data: runID === 10
          ? completedApplyRun(10, 1, '2026-07-14T08:00:00Z')
          : completedApplyRun(20, 2, '2026-07-14T09:00:00Z'),
      },
    }))
    const wrapper = await mountSettings()

    await selectApproverDepartment(wrapper)
    await wrapper.get('[data-testid="quota-reset-approver-select"]').trigger('click')
    await flushPromises()
    api.listQuotaResetApproverCandidates.mockClear()
    await wrapper.get('[data-testid="quota-reset-approver-filter"]').setValue('alice')
    await flushPromises()

    expect(directory.getDirectoryRun).not.toHaveBeenCalled()
    expect(directory.listDirectoryRuns).not.toHaveBeenCalled()
    expect(directory.listDirectoryDepartments).toHaveBeenCalledWith({ source_id: 1, q: '' })
    expect(api.listQuotaResetApproverCandidates).toHaveBeenCalledWith({
      source_id: 1,
      q: 'alice',
      page: 1,
      page_size: 20,
    })
    expect(wrapper.get('[data-testid="quota-reset-current-directory-source"]').text()).toBe('Backend Directory')
  })

  it('keeps source selection disabled when the backend reports no current source', async () => {
    const api = await import('@/api/quotaReset') as any
    const directory = await import('@/api/directory') as any
    api.getQuotaResetApproverConfigs.mockResolvedValueOnce(approverConfigResponse([], null))
    directory.listDirectorySources.mockResolvedValueOnce({
      data: {
        data: {
          items: [directorySource(1, 'Directory Alpha', 10)],
        },
      },
    })
    const wrapper = await mountSettings()

    expect(directory.getDirectoryRun).not.toHaveBeenCalled()
    expect(directory.listDirectoryRuns).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="quota-reset-department-select"]').attributes()).toHaveProperty('disabled')
    expect(wrapper.text()).toContain('Current directory source is unavailable.')
  })

  it('uses the backend source even when the readable source listing does not contain it', async () => {
    const api = await import('@/api/quotaReset') as any
    const directory = await import('@/api/directory') as any
    api.getQuotaResetApproverConfigs.mockResolvedValueOnce(approverConfigResponse([], 99))
    directory.listDirectorySources.mockResolvedValueOnce({
      data: { data: { items: [directorySource(1, 'Directory Alpha', 10)] } },
    })

    const wrapper = await mountSettings()
    await selectApproverDepartment(wrapper)

    expect(directory.getDirectoryRun).not.toHaveBeenCalled()
    expect(directory.listDirectoryDepartments).toHaveBeenCalledWith({ source_id: 99, q: '' })
  })

  it('invalidates an in-flight source-A candidate request when configs switch to source B', async () => {
    const api = await import('@/api/quotaReset') as any
    const directory = await import('@/api/directory') as any
    const staleCandidates = deferred<any>()
    api.getQuotaResetApproverConfigs
      .mockResolvedValueOnce(approverConfigResponse([], 1))
      .mockResolvedValueOnce(approverConfigResponse([], 2))
    api.listQuotaResetApproverCandidates.mockReturnValueOnce(staleCandidates.promise)
    directory.listDirectorySources.mockResolvedValueOnce({
      data: {
        data: {
          items: [
            directorySource(1, 'Directory A', 10),
            directorySource(2, 'Directory B', 20),
          ],
        },
      },
    })
    directory.listDirectoryDepartments.mockImplementation(({ source_id }: { source_id: number }) => Promise.resolve({
      data: {
        data: {
          items: source_id === 1
            ? [departmentAlpha]
            : [{ ...departmentBeta, id: 22, source_id: 2 }],
        },
      },
    }))
    const wrapper = await mountDepartmentApprovers()
    await selectApproverDepartment(wrapper)
    await wrapper.get('[data-testid="quota-reset-approver-select"]').trigger('click')
    expect(wrapper.find('[data-testid="quota-reset-approver-loading"]').exists()).toBe(true)

    await wrapper.get('[data-testid="quota-reset-reload-approvers"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="quota-reset-department-select"]').text()).toContain('Select department')
    expect(wrapper.get('[data-testid="quota-reset-approver-select"]').attributes()).toHaveProperty('disabled')
    expect(wrapper.find('[data-testid="quota-reset-approver-filter"]').exists()).toBe(false)

    staleCandidates.resolve({
      data: {
        data: {
          items: [{ ...aliceCandidate, display_name: 'Stale Source A Candidate' }],
          page: 1,
          page_size: 20,
          total: 1,
        },
      },
    })
    await flushPromises()
    expect(wrapper.text()).not.toContain('Stale Source A Candidate')

    await wrapper.get('[data-testid="quota-reset-department-select"]').trigger('click')
    await flushPromises()
    expect(directory.listDirectoryDepartments).toHaveBeenLastCalledWith({ source_id: 2, q: '' })
    expect(wrapper.find('[data-testid="quota-reset-department-option-dept-alpha"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quota-reset-department-option-dept-beta"]').exists()).toBe(true)
  })

  it('invalidates an in-flight source-A department request when configs switch to source B', async () => {
    const api = await import('@/api/quotaReset') as any
    const directory = await import('@/api/directory') as any
    const staleDepartments = deferred<any>()
    api.getQuotaResetApproverConfigs
      .mockResolvedValueOnce(approverConfigResponse([], 1))
      .mockResolvedValueOnce(approverConfigResponse([], 2))
    directory.listDirectoryDepartments.mockImplementation(({ source_id }: { source_id: number }) => (
      source_id === 1
        ? staleDepartments.promise
        : Promise.resolve({
            data: { data: { items: [{ ...departmentBeta, id: 22, source_id: 2 }] } },
          })
    ))
    const wrapper = await mountDepartmentApprovers()
    await wrapper.get('[data-testid="quota-reset-department-select"]').trigger('click')
    expect(wrapper.find('[data-testid="quota-reset-department-filter"]').exists()).toBe(true)

    await wrapper.get('[data-testid="quota-reset-reload-approvers"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="quota-reset-department-filter"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="quota-reset-department-select"]').text()).toContain('Select department')

    staleDepartments.resolve({ data: { data: { items: [departmentAlpha] } } })
    await flushPromises()
    expect(wrapper.find('[data-testid="quota-reset-department-option-dept-alpha"]').exists()).toBe(false)

    await wrapper.get('[data-testid="quota-reset-department-select"]').trigger('click')
    await flushPromises()
    expect(directory.listDirectoryDepartments).toHaveBeenLastCalledWith({ source_id: 2, q: '' })
    expect(wrapper.find('[data-testid="quota-reset-department-option-dept-beta"]').exists()).toBe(true)
  })

  it('localizes WeCom mention coverage in Chinese', async () => {
    setLocale('zh-CN')
    const wrapper = await mountSettings()
    await selectApproverDepartment(wrapper, 'dept-alpha', '筛选部门')
    await wrapper.get('[data-testid="quota-reset-approver-select"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="quota-reset-approver-option-12"]').text()).toContain('企业微信可 @')
  })

  it('loads later candidate pages without duplicates and keeps later candidates selectable', async () => {
    const api = await import('@/api/quotaReset') as any
    const firstPage = Array.from({ length: 20 }, (_, index) => pagedCandidate(index + 1))
    const lastCandidate = pagedCandidate(21)
    api.listQuotaResetApproverCandidates.mockImplementation(({ page }: { page: number }) => Promise.resolve({
      data: {
        data: {
          items: page === 2 ? [firstPage[19], lastCandidate] : firstPage,
          page,
          page_size: 20,
          total: 22,
        },
      },
    }))
    const wrapper = await mountDepartmentApprovers()
    await selectApproverDepartment(wrapper)

    await wrapper.get('[data-testid="quota-reset-approver-select"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-testid^="quota-reset-approver-option-"]')).toHaveLength(20)
    const loadMore = wrapper.get('[data-testid="quota-reset-approver-load-more"]')
    expect(loadMore.text()).toBe('Load more approvers')
    expect(loadMore.attributes('aria-label')).toBe('Load more approvers')

    await loadMore.trigger('click')
    await flushPromises()

    expect(api.listQuotaResetApproverCandidates).toHaveBeenLastCalledWith({
      source_id: 1,
      q: '',
      page: 2,
      page_size: 20,
    })
    expect(wrapper.findAll('[data-testid^="quota-reset-approver-option-"]')).toHaveLength(21)
    expect(wrapper.findAll('[data-testid="quota-reset-approver-option-20"]')).toHaveLength(1)
    expect(wrapper.find('[data-testid="quota-reset-approver-load-more"]').exists()).toBe(false)

    await wrapper.get('[data-testid="quota-reset-approver-option-21"]').trigger('click')
    expect(wrapper.get('[data-testid="quota-reset-approver-select"]').text()).toContain('Candidate 21')
  })

  it('stops loading when the final raw page contains only a displayed duplicate', async () => {
    const api = await import('@/api/quotaReset') as any
    const firstPage = Array.from({ length: 20 }, (_, index) => pagedCandidate(index + 1))
    api.listQuotaResetApproverCandidates.mockImplementation(({ page }: { page: number }) => Promise.resolve({
      data: {
        data: {
          items: page === 2 ? [firstPage[19]] : firstPage,
          page,
          page_size: 20,
          total: 21,
        },
      },
    }))
    const wrapper = await mountDepartmentApprovers()
    await selectApproverDepartment(wrapper)
    await wrapper.get('[data-testid="quota-reset-approver-select"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="quota-reset-approver-load-more"]').trigger('click')
    await flushPromises()

    expect(wrapper.findAll('[data-testid^="quota-reset-approver-option-"]')).toHaveLength(20)
    expect(wrapper.findAll('[data-testid="quota-reset-approver-option-20"]')).toHaveLength(1)
    expect(wrapper.find('[data-testid="quota-reset-approver-load-more"]').exists()).toBe(false)
  })

  it('accepts an empty later page after total shrinks and marks results exhausted', async () => {
    const api = await import('@/api/quotaReset') as any
    const firstPage = Array.from({ length: 20 }, (_, index) => pagedCandidate(index + 1))
    api.listQuotaResetApproverCandidates.mockImplementation(({ page }: { page: number }) => Promise.resolve({
      data: {
        data: {
          items: page === 2 ? [] : firstPage,
          page,
          page_size: 20,
          total: page === 2 ? 10 : 21,
        },
      },
    }))
    const wrapper = await mountDepartmentApprovers()
    await selectApproverDepartment(wrapper)
    await wrapper.get('[data-testid="quota-reset-approver-select"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="quota-reset-approver-load-more"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).not.toContain('Failed to load approver candidates')
    expect(wrapper.findAll('[data-testid^="quota-reset-approver-option-"]')).toHaveLength(20)
    expect(wrapper.find('[data-testid="quota-reset-approver-load-more"]').exists()).toBe(false)
  })

  it.each([
    {
      name: 'items are not an array',
      data: { items: null, page: 1, page_size: 20, total: 0 },
    },
    {
      name: 'requested page does not match',
      data: { items: [aliceCandidate], page: 2, page_size: 20, total: 21 },
    },
    {
      name: 'page is non-positive',
      data: { items: [], page: 0, page_size: 20, total: 0 },
    },
    {
      name: 'page size is non-positive',
      data: { items: [], page: 1, page_size: 0, total: 0 },
    },
    {
      name: 'page size does not match the request',
      data: { items: [aliceCandidate], page: 1, page_size: 10, total: 1 },
    },
    {
      name: 'total is negative',
      data: { items: [], page: 1, page_size: 20, total: -1 },
    },
    {
      name: 'total is incoherent with returned items',
      data: { items: [aliceCandidate], page: 1, page_size: 20, total: 0 },
    },
  ])('rejects malformed candidate pagination metadata when $name', async ({ data }) => {
    const api = await import('@/api/quotaReset') as any
    api.listQuotaResetApproverCandidates.mockResolvedValueOnce({ data: { data } })
    const wrapper = await mountDepartmentApprovers()
    await selectApproverDepartment(wrapper)

    await wrapper.get('[data-testid="quota-reset-approver-select"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Failed to load approver candidates')
    expect(wrapper.findAll('[data-testid^="quota-reset-approver-option-"]')).toHaveLength(0)
    expect(wrapper.find('[data-testid="quota-reset-approver-load-more"]').exists()).toBe(false)
  })

  it('retains page 1 and retries page 2 after a later-page error', async () => {
    const api = await import('@/api/quotaReset') as any
    const firstPage = Array.from({ length: 20 }, (_, index) => pagedCandidate(index + 1))
    const lastCandidate = pagedCandidate(21)
    let pageTwoCalls = 0
    api.listQuotaResetApproverCandidates.mockImplementation(({ page }: { page: number }) => {
      if (page === 1) {
        return Promise.resolve({
          data: { data: { items: firstPage, page: 1, page_size: 20, total: 21 } },
        })
      }
      pageTwoCalls += 1
      if (pageTwoCalls === 1) {
        return Promise.reject({
          response: { data: { message: 'Synthetic page 2 failed.' } },
        })
      }
      return Promise.resolve({
        data: { data: { items: [lastCandidate], page: 2, page_size: 20, total: 21 } },
      })
    })
    const wrapper = await mountDepartmentApprovers()
    await selectApproverDepartment(wrapper)
    await wrapper.get('[data-testid="quota-reset-approver-select"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="quota-reset-approver-load-more"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Synthetic page 2 failed.')
    expect(wrapper.findAll('[data-testid^="quota-reset-approver-option-"]')).toHaveLength(20)
    expect(wrapper.get('[data-testid="quota-reset-approver-load-more"]').attributes()).not.toHaveProperty('disabled')

    await wrapper.get('[data-testid="quota-reset-approver-load-more"]').trigger('click')
    await flushPromises()
    expect(pageTwoCalls).toBe(2)
    expect(wrapper.text()).not.toContain('Synthetic page 2 failed.')
    expect(wrapper.findAll('[data-testid^="quota-reset-approver-option-"]')).toHaveLength(21)
    expect(wrapper.find('[data-testid="quota-reset-approver-load-more"]').exists()).toBe(false)
  })

  it('resets pagination for search and ignores a stale later-page response', async () => {
    const api = await import('@/api/quotaReset') as any
    const staleSecondPage = deferred<any>()
    const currentSearch = deferred<any>()
    const unfilteredCandidates = Array.from({ length: 20 }, (_, index) => pagedCandidate(index + 1))
    const staleCandidate = { ...pagedCandidate(21), display_name: 'Stale Page Candidate' }
    api.listQuotaResetApproverCandidates.mockImplementation(({ q, page }: { q: string, page: number }) => {
      if (q === 'alice') return currentSearch.promise
      if (page === 2) return staleSecondPage.promise
      return Promise.resolve({
        data: { data: { items: unfilteredCandidates, page: 1, page_size: 20, total: 21 } },
      })
    })
    const wrapper = await mountDepartmentApprovers()
    await selectApproverDepartment(wrapper)
    await wrapper.get('[data-testid="quota-reset-approver-select"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="quota-reset-approver-load-more"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-approver-filter"]').setValue('alice')
    expect(api.listQuotaResetApproverCandidates).toHaveBeenLastCalledWith({
      source_id: 1,
      q: 'alice',
      page: 1,
      page_size: 20,
    })

    currentSearch.resolve({
      data: { data: { items: [aliceCandidate], page: 1, page_size: 20, total: 1 } },
    })
    await flushPromises()
    staleSecondPage.resolve({
      data: { data: { items: [staleCandidate], page: 2, page_size: 20, total: 21 } },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Alice')
    expect(wrapper.text()).not.toContain('Candidate 1')
    expect(wrapper.text()).not.toContain('Stale Page Candidate')
    expect(wrapper.find('[data-testid="quota-reset-approver-load-more"]').exists()).toBe(false)
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

  it('shows the latest candidate request error from the backend', async () => {
    const api = await import('@/api/quotaReset') as any
    api.listQuotaResetApproverCandidates.mockRejectedValueOnce({
      response: {
        status: 503,
        data: {
          message: 'Current directory snapshot is unavailable.',
        },
      },
    })
    const wrapper = await mountSettings()
    await selectApproverDepartment(wrapper)

    await wrapper.get('[data-testid="quota-reset-approver-select"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Current directory snapshot is unavailable.')
    expect(wrapper.find('[data-testid="quota-reset-approver-loading"]').exists()).toBe(false)
  })

  it('ignores an older candidate rejection after a newer search succeeds', async () => {
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
    filtered.resolve({
      data: { data: { items: [aliceCandidate], page: 1, page_size: 20, total: 1 } },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('Alice')

    unfiltered.reject({
      response: {
        status: 503,
        data: { message: 'Stale source should not replace current results.' },
      },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Alice')
    expect(wrapper.text()).not.toContain('Stale source should not replace current results.')
    expect(wrapper.find('[data-testid="quota-reset-approver-loading"]').exists()).toBe(false)
  })

  it('shows readable backend names for existing approver rows without raw ids', async () => {
    const api = await import('@/api/quotaReset') as any
    api.getQuotaResetApproverConfigs.mockResolvedValueOnce({
      data: {
        data: {
          directory_source_id: 1,
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
    api.getQuotaResetApproverConfigs.mockResolvedValueOnce(approverConfigResponse([configuredAliceApprover]))
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

  it('prioritizes a destructive save conflict over an earlier candidate error', async () => {
    const api = await import('@/api/quotaReset') as any
    api.getQuotaResetApproverConfigs.mockResolvedValueOnce(approverConfigResponse([configuredAliceApprover]))
    api.listQuotaResetApproverCandidates.mockRejectedValueOnce({
      response: {
        status: 503,
        data: { message: 'Synthetic candidate source is unavailable.' },
      },
    })
    api.saveQuotaResetApproverConfigs.mockRejectedValueOnce({
      response: {
        status: 409,
        data: { message: 'Group Alpha still references Department Alpha.' },
      },
    })
    const wrapper = await mountSettings()
    await selectApproverDepartment(wrapper)
    await wrapper.get('[data-testid="quota-reset-approver-select"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Synthetic candidate source is unavailable.')

    await wrapper.get('[data-testid="quota-reset-config-remove-7"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-save-approvers"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Group Alpha still references Department Alpha.')
    expect(wrapper.text()).not.toContain('Synthetic candidate source is unavailable.')
  })

  it('invalidates an in-flight candidate request before destructive save feedback', async () => {
    const api = await import('@/api/quotaReset') as any
    const candidateRequest = deferred<any>()
    const saveRequest = deferred<any>()
    api.getQuotaResetApproverConfigs.mockResolvedValueOnce(approverConfigResponse([configuredAliceApprover]))
    api.listQuotaResetApproverCandidates.mockReturnValueOnce(candidateRequest.promise)
    api.saveQuotaResetApproverConfigs.mockReturnValueOnce(saveRequest.promise)
    const wrapper = await mountSettings()
    await selectApproverDepartment(wrapper)
    await wrapper.get('[data-testid="quota-reset-approver-select"]').trigger('click')
    expect(wrapper.find('[data-testid="quota-reset-approver-filter"]').exists()).toBe(true)

    await wrapper.get('[data-testid="quota-reset-config-remove-7"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-save-approvers"]').trigger('click')
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-testid="quota-reset-approver-filter"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quota-reset-approver-loading"]').exists()).toBe(false)

    saveRequest.reject({
      response: {
        status: 409,
        data: { message: 'Group Beta still references Department Alpha.' },
      },
    })
    await flushPromises()
    candidateRequest.reject({
      response: {
        status: 503,
        data: { message: 'Late candidate failure must remain stale.' },
      },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Group Beta still references Department Alpha.')
    expect(wrapper.text()).not.toContain('Late candidate failure must remain stale.')
  })

  it('clears an earlier candidate error when full replacement succeeds', async () => {
    const api = await import('@/api/quotaReset') as any
    api.getQuotaResetApproverConfigs.mockResolvedValueOnce(approverConfigResponse([configuredAliceApprover]))
    api.listQuotaResetApproverCandidates.mockRejectedValueOnce({
      response: {
        status: 503,
        data: { message: 'Synthetic candidate lookup failed.' },
      },
    })
    api.saveQuotaResetApproverConfigs.mockResolvedValueOnce(approverConfigResponse())
    const wrapper = await mountSettings()
    await selectApproverDepartment(wrapper)
    await wrapper.get('[data-testid="quota-reset-approver-select"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Synthetic candidate lookup failed.')

    await wrapper.get('[data-testid="quota-reset-config-remove-7"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-save-approvers"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Approver configs saved')
    expect(wrapper.text()).not.toContain('Synthetic candidate lookup failed.')
  })

  it('rejects a malformed save response and preserves prior authoritative state', async () => {
    const api = await import('@/api/quotaReset') as any
    const directory = await import('@/api/directory') as any
    api.getQuotaResetApproverConfigs.mockResolvedValueOnce(approverConfigResponse([configuredAliceApprover], 1))
    api.saveQuotaResetApproverConfigs.mockResolvedValueOnce({
      data: { data: { directory_source_id: 2, items: null } },
    })
    directory.listDirectorySources.mockResolvedValueOnce({
      data: {
        data: {
          items: [
            directorySource(1, 'Directory A', 10),
            directorySource(2, 'Directory B', 20),
          ],
        },
      },
    })
    const wrapper = await mountDepartmentApprovers()
    expect(wrapper.get('[data-testid="quota-reset-current-directory-source"]').text()).toBe('Directory A')
    expect(wrapper.find('[data-testid="quota-reset-config-row-7"]').exists()).toBe(true)

    await wrapper.get('[data-testid="quota-reset-config-remove-7"]').trigger('click')
    expect(wrapper.find('[data-testid="quota-reset-config-row-7"]').exists()).toBe(false)
    await wrapper.get('[data-testid="quota-reset-save-approvers"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Failed to save approver configs')
    expect(wrapper.text()).not.toContain('Approver configs saved')
    expect(wrapper.get('[data-testid="quota-reset-current-directory-source"]').text()).toBe('Directory A')
    expect(wrapper.find('[data-testid="quota-reset-config-row-7"]').exists()).toBe(true)
  })

  it.each(malformedApproverConfigResponses)(
    'rejects malformed approver config PUT responses with $name',
    async ({ data }) => {
      const api = await import('@/api/quotaReset') as any
      const directory = await import('@/api/directory') as any
      api.getQuotaResetApproverConfigs.mockResolvedValueOnce(
        approverConfigResponse([configuredAliceApprover], 1),
      )
      api.saveQuotaResetApproverConfigs.mockResolvedValueOnce({ data: { data } })
      directory.listDirectorySources.mockResolvedValueOnce({
        data: {
          data: {
            items: [
              directorySource(1, 'Directory Alpha', 10),
              directorySource(2, 'Directory Beta', 20),
            ],
          },
        },
      })
      const wrapper = await mountDepartmentApprovers()

      await wrapper.get('[data-testid="quota-reset-save-approvers"]').trigger('click')
      await flushPromises()

      expect(wrapper.text()).toContain('Failed to save approver configs')
      expect(wrapper.text()).not.toContain('Approver configs saved')
      expect(wrapper.get('[data-testid="quota-reset-current-directory-source"]').text()).toBe('Directory Alpha')
      expect(wrapper.find('[data-testid="quota-reset-config-row-7"]').exists()).toBe(true)
    },
  )

  it('uses the authoritative current source returned by a successful save', async () => {
    const api = await import('@/api/quotaReset') as any
    const directory = await import('@/api/directory') as any
    api.getQuotaResetApproverConfigs.mockResolvedValueOnce(approverConfigResponse([], 1))
    api.saveQuotaResetApproverConfigs.mockResolvedValueOnce(approverConfigResponse([], 2))
    directory.listDirectorySources.mockResolvedValueOnce({
      data: {
        data: {
          items: [
            directorySource(1, 'Directory A', 10),
            directorySource(2, 'Directory B', 20),
          ],
        },
      },
    })
    const wrapper = await mountDepartmentApprovers()
    await selectApproverDepartment(wrapper)

    await wrapper.get('[data-testid="quota-reset-save-approvers"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Approver configs saved')
    expect(wrapper.get('[data-testid="quota-reset-current-directory-source"]').text()).toBe('Directory B')
    expect(wrapper.get('[data-testid="quota-reset-department-select"]').text()).toContain('Select department')
  })

  it('locks existing approver row mutations while a full replacement save is pending', async () => {
    const api = await import('@/api/quotaReset') as any
    const saveRequest = deferred<any>()
    const authoritative = { ...configuredAliceApprover, enabled: false }
    api.getQuotaResetApproverConfigs.mockResolvedValueOnce(
      approverConfigResponse([configuredAliceApprover]),
    )
    api.saveQuotaResetApproverConfigs.mockReturnValueOnce(saveRequest.promise)
    const wrapper = await mountDepartmentApprovers()

    await wrapper.get('[data-testid="quota-reset-save-approvers"]').trigger('click')
    const enabledToggle = wrapper.get('[data-testid="quota-reset-config-enabled-7"]')
    const removeButton = wrapper.get('[data-testid="quota-reset-config-remove-7"]')
    expect(enabledToggle.attributes()).toHaveProperty('disabled')
    expect(removeButton.attributes()).toHaveProperty('disabled')

    ;(enabledToggle.element as HTMLInputElement).disabled = false
    await enabledToggle.setValue(false)
    await forceClick(removeButton)
    expect((enabledToggle.element as HTMLInputElement).checked).toBe(true)
    expect(wrapper.find('[data-testid="quota-reset-config-row-7"]').exists()).toBe(true)
    expect(api.saveQuotaResetApproverConfigs).toHaveBeenCalledWith([
      {
        department_external_id: 'dept-alpha',
        department_display_path: 'Department Alpha',
        approver_user_id: 12,
        enabled: true,
      },
    ], 'replace_all')

    saveRequest.resolve(approverConfigResponse([authoritative]))
    await flushPromises()
    expect((wrapper.get('[data-testid="quota-reset-config-enabled-7"]').element as HTMLInputElement).checked).toBe(false)
    expect(wrapper.find('[data-testid="quota-reset-config-row-7"]').exists()).toBe(true)
  })

  it('emits saved after full replacement and reloads chains through the parent', async () => {
    const api = await import('@/api/quotaReset') as any
    api.getQuotaResetApproverConfigs.mockResolvedValueOnce(approverConfigResponse([configuredAliceApprover]))
    api.saveQuotaResetApproverConfigs.mockResolvedValueOnce(approverConfigResponse())
    const wrapper = await mountSettings()

    await wrapper.get('[data-testid="quota-reset-config-remove-7"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-save-approvers"]').trigger('click')
    await flushPromises()
    expect(api.saveQuotaResetApproverConfigs).toHaveBeenCalledWith([], 'replace_all')
    expect(api.getQuotaResetApprovalChains).toHaveBeenCalledTimes(2)
  })

  it.each(malformedChainListResponses)(
    'rejects malformed approval-chain GET responses with $name',
    async ({ data }) => {
      const api = await import('@/api/quotaReset') as any
      api.getQuotaResetApprovalChains.mockResolvedValueOnce({ data: { data } })
      const wrapper = await mountChains()

      expect(wrapper.text()).toContain('Failed to load approval chains')
      const saveButton = wrapper.get('[data-testid="quota-reset-save-chains"]')
      expect(saveButton.attributes()).toHaveProperty('disabled')
      await forceClick(saveButton)
      await flushPromises()
      expect(api.saveQuotaResetApprovalChains).not.toHaveBeenCalled()
    },
  )

  it.each(malformedChainOptionsResponses)(
    'rejects malformed approval-chain option GET responses with $name',
    async ({ data }) => {
      const api = await import('@/api/quotaReset') as any
      api.getQuotaResetApprovalChainOptions.mockResolvedValueOnce({ data: { data } })
      const wrapper = await mountChains()

      expect(wrapper.text()).toContain('Failed to load approval chains')
      const saveButton = wrapper.get('[data-testid="quota-reset-save-chains"]')
      expect(saveButton.attributes()).toHaveProperty('disabled')
      await forceClick(saveButton)
      await flushPromises()
      expect(api.saveQuotaResetApprovalChains).not.toHaveBeenCalled()
    },
  )

  it.each(malformedChainListResponses)(
    'rejects malformed approval-chain PUT responses with $name',
    async ({ data }) => {
      const api = await import('@/api/quotaReset') as any
      api.getQuotaResetApprovalChains.mockResolvedValueOnce({
        data: { data: { items: [configuredAlphaChain] } },
      })
      api.saveQuotaResetApprovalChains.mockResolvedValueOnce({ data: { data } })
      const wrapper = await mountChains()
      await selectChainGroup(wrapper, 'group-alpha')
      await addChainDepartment(wrapper, 'dept-beta')

      await wrapper.get('[data-testid="quota-reset-save-chains"]').trigger('click')
      await flushPromises()

      expect(wrapper.text()).toContain('Failed to save approval chains')
      expect(wrapper.text()).not.toContain('Approval chains saved')
      expect(wrapper.text()).toContain('Department Alpha')
      expect(wrapper.text()).toContain('Department Beta')
      expect(wrapper.get('[data-testid="quota-reset-save-chains"]').attributes()).not.toHaveProperty('disabled')
    },
  )

  it('keeps edited chains retryable after a malformed PUT response', async () => {
    const api = await import('@/api/quotaReset') as any
    const corrected = {
      ...configuredAlphaChain,
      id: 44,
      nodes: [
        configuredAlphaChain.nodes[0],
        {
          directory_source_id: 1,
          department_external_id: 'dept-beta',
          department_display_path: 'Department Beta',
        },
      ],
    }
    api.getQuotaResetApprovalChains.mockResolvedValueOnce({
      data: { data: { items: [configuredAlphaChain] } },
    })
    api.saveQuotaResetApprovalChains
      .mockResolvedValueOnce({ data: { data: {} } })
      .mockResolvedValueOnce({ data: { data: { items: [corrected] } } })
    const wrapper = await mountChains()
    await selectChainGroup(wrapper, 'group-alpha')
    await addChainDepartment(wrapper, 'dept-beta')

    await wrapper.get('[data-testid="quota-reset-save-chains"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Failed to save approval chains')
    expect(wrapper.text()).toContain('Department Beta')

    await wrapper.get('[data-testid="quota-reset-save-chains"]').trigger('click')
    await flushPromises()
    expect(api.saveQuotaResetApprovalChains.mock.calls[1][0][0].nodes).toEqual([
      configuredAlphaChain.nodes[0],
      {
        directory_source_id: 1,
        department_external_id: 'dept-beta',
        department_display_path: 'Department Beta',
      },
    ])
    expect(wrapper.text()).toContain('Approval chains saved')
  })

  it('accepts a valid chain PUT roundtrip as the next authoritative payload', async () => {
    const api = await import('@/api/quotaReset') as any
    const authoritative = {
      ...configuredAlphaChain,
      id: 45,
      enabled: false,
      nodes: [
        {
          directory_source_id: 1,
          department_external_id: 'dept-beta',
          department_display_path: 'Department Beta',
        },
        configuredAlphaChain.nodes[0],
      ],
    }
    api.getQuotaResetApprovalChains.mockResolvedValueOnce({
      data: { data: { items: [configuredAlphaChain] } },
    })
    api.saveQuotaResetApprovalChains.mockResolvedValue({
      data: { data: { items: [authoritative] } },
    })
    const wrapper = await mountChains()
    await selectChainGroup(wrapper, 'group-alpha')
    await addChainDepartment(wrapper, 'dept-beta')

    await wrapper.get('[data-testid="quota-reset-save-chains"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Approval chains saved')
    expect((wrapper.get('input[type="checkbox"]').element as HTMLInputElement).checked).toBe(false)
    expect(wrapper.findAll('[data-testid^="quota-reset-chain-node-"]').map(node => node.attributes('data-testid'))).toEqual([
      'quota-reset-chain-node-dept-beta',
      'quota-reset-chain-node-dept-alpha',
    ])

    await wrapper.get('[data-testid="quota-reset-save-chains"]').trigger('click')
    await flushPromises()
    expect(api.saveQuotaResetApprovalChains.mock.calls[1][0]).toEqual([{
      provider_id: 1,
      group_id: 'group-alpha',
      group_name: 'Group Alpha',
      enabled: false,
      nodes: authoritative.nodes,
    }])
  })

  it('locks every chain domain mutator while a save is pending and accepts the valid response', async () => {
    const api = await import('@/api/quotaReset') as any
    const saveRequest = deferred<any>()
    const authoritative = {
      ...configuredAlphaChain,
      id: 46,
      enabled: false,
      nodes: [{
        directory_source_id: 1,
        department_external_id: 'dept-beta',
        department_display_path: 'Department Beta',
      }],
    }
    api.getQuotaResetApprovalChains.mockResolvedValueOnce({
      data: { data: { items: [configuredAlphaChain] } },
    })
    api.saveQuotaResetApprovalChains.mockReturnValueOnce(saveRequest.promise)
    const wrapper = await mountChains()
    await selectChainGroup(wrapper, 'group-alpha')
    await addChainDepartment(wrapper, 'dept-beta')

    await wrapper.get('[data-testid="quota-reset-save-chains"]').trigger('click')
    const groupSelect = wrapper.get('[data-testid="quota-reset-chain-group-select"]')
    const enabledToggle = wrapper.get('[data-testid="quota-reset-chain-enabled"]')
    const departmentSelect = wrapper.get('[data-testid="quota-reset-chain-department-select"]')
    const moveBetaUp = wrapper.get('[data-testid="quota-reset-chain-move-up-dept-beta"]')
    const removeAlpha = wrapper.get('[data-testid="quota-reset-chain-remove-dept-alpha"]')
    for (const control of [groupSelect, enabledToggle, departmentSelect, moveBetaUp, removeAlpha]) {
      expect(control.attributes()).toHaveProperty('disabled')
    }

    await forceClick(groupSelect)
    await forceClick(departmentSelect)
    ;(enabledToggle.element as HTMLInputElement).disabled = false
    await enabledToggle.setValue(false)
    await forceClick(moveBetaUp)
    await forceClick(removeAlpha)
    expect(wrapper.find('[data-testid="quota-reset-chain-group-filter"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quota-reset-chain-department-filter"]').exists()).toBe(false)
    expect((enabledToggle.element as HTMLInputElement).checked).toBe(true)
    expect(wrapper.findAll('[data-testid^="quota-reset-chain-node-"]').map(node => node.attributes('data-testid'))).toEqual([
      'quota-reset-chain-node-dept-alpha',
      'quota-reset-chain-node-dept-beta',
    ])
    expect(api.saveQuotaResetApprovalChains).toHaveBeenCalledWith([{
      provider_id: 1,
      group_id: 'group-alpha',
      group_name: 'Group Alpha',
      enabled: true,
      nodes: [
        configuredAlphaChain.nodes[0],
        {
          directory_source_id: 1,
          department_external_id: 'dept-beta',
          department_display_path: 'Department Beta',
        },
      ],
    }])

    saveRequest.resolve({ data: { data: { items: [authoritative] } } })
    await flushPromises()
    expect((wrapper.get('[data-testid="quota-reset-chain-enabled"]').element as HTMLInputElement).checked).toBe(false)
    expect(wrapper.findAll('[data-testid^="quota-reset-chain-node-"]').map(node => node.attributes('data-testid'))).toEqual([
      'quota-reset-chain-node-dept-beta',
    ])
  })

  it('preserves dirty chain edits across an approver revision and submits them next', async () => {
    const api = await import('@/api/quotaReset') as any
    const authoritative = {
      ...configuredAlphaChain,
      id: 47,
      enabled: false,
      nodes: [
        {
          directory_source_id: 1,
          department_external_id: 'dept-beta',
          department_display_path: 'Department Beta',
        },
        configuredAlphaChain.nodes[0],
      ],
    }
    api.getQuotaResetApprovalChains.mockResolvedValueOnce({
      data: { data: { items: [configuredAlphaChain] } },
    })
    api.saveQuotaResetApprovalChains.mockResolvedValueOnce({
      data: { data: { items: [authoritative] } },
    })
    const wrapper = await mountChains()
    await selectChainGroup(wrapper, 'group-alpha')
    await addChainDepartment(wrapper, 'dept-beta')
    await wrapper.get('[data-testid="quota-reset-chain-move-up-dept-beta"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-chain-enabled"]').setValue(false)

    api.getQuotaResetApprovalChainOptions.mockResolvedValueOnce({ data: { data: chainOptions } })
    api.getQuotaResetApprovalChains.mockResolvedValueOnce({
      data: { data: { items: [configuredAlphaChain] } },
    })
    await wrapper.setProps({ approverRevision: 1 })
    await flushPromises()

    expect((wrapper.get('[data-testid="quota-reset-chain-enabled"]').element as HTMLInputElement).checked).toBe(false)
    expect(wrapper.findAll('[data-testid^="quota-reset-chain-node-"]').map(node => node.attributes('data-testid'))).toEqual([
      'quota-reset-chain-node-dept-beta',
      'quota-reset-chain-node-dept-alpha',
    ])
    await wrapper.get('[data-testid="quota-reset-save-chains"]').trigger('click')
    await flushPromises()
    expect(api.saveQuotaResetApprovalChains).toHaveBeenCalledWith([{
      provider_id: 1,
      group_id: 'group-alpha',
      group_name: 'Group Alpha',
      enabled: false,
      nodes: authoritative.nodes,
    }])
  })

  it.each(['options', 'chains'] as const)(
    'guards chain full replacement when the initial %s load fails',
    async (failedRead) => {
      const api = await import('@/api/quotaReset') as any
      if (failedRead === 'options') {
        api.getQuotaResetApprovalChainOptions.mockRejectedValueOnce({
          response: { data: { message: 'Synthetic chain options load failed.' } },
        })
      } else {
        api.getQuotaResetApprovalChains.mockRejectedValueOnce({
          response: { data: { message: 'Synthetic saved chains load failed.' } },
        })
      }
      const wrapper = await mountChains()
      const saveButton = wrapper.get('[data-testid="quota-reset-save-chains"]')

      expect(saveButton.attributes()).toHaveProperty('disabled')
      await forceClick(saveButton)
      await flushPromises()
      expect(api.saveQuotaResetApprovalChains).not.toHaveBeenCalled()
    },
  )

  it('preserves chains after a failed revision refresh and re-enables after recovery', async () => {
    const api = await import('@/api/quotaReset') as any
    api.getQuotaResetApprovalChains.mockResolvedValueOnce({
      data: { data: { items: [configuredAlphaChain] } },
    })
    const wrapper = await mountChains()
    await selectChainGroup(wrapper, 'group-alpha')
    expect(wrapper.text()).toContain('Department Alpha')

    api.getQuotaResetApprovalChainOptions.mockResolvedValueOnce({ data: { data: chainOptions } })
    api.getQuotaResetApprovalChains.mockRejectedValueOnce({
      response: { data: { message: 'Synthetic chain revision refresh failed.' } },
    })
    await wrapper.setProps({ approverRevision: 1 })
    await flushPromises()

    expect(wrapper.text()).toContain('Department Alpha')
    expect(wrapper.text()).toContain('Synthetic chain revision refresh failed.')
    const disabledSave = wrapper.get('[data-testid="quota-reset-save-chains"]')
    expect(disabledSave.attributes()).toHaveProperty('disabled')
    await forceClick(disabledSave)
    await flushPromises()
    expect(api.saveQuotaResetApprovalChains).not.toHaveBeenCalled()

    api.getQuotaResetApprovalChainOptions.mockResolvedValueOnce({ data: { data: chainOptions } })
    api.getQuotaResetApprovalChains.mockResolvedValueOnce({
      data: { data: { items: [configuredAlphaChain] } },
    })
    await wrapper.setProps({ approverRevision: 2 })
    await flushPromises()
    expect(wrapper.get('[data-testid="quota-reset-save-chains"]').attributes()).not.toHaveProperty('disabled')
  })

  it('queues one approver-revision reload until an in-flight chain save settles', async () => {
    const api = await import('@/api/quotaReset') as any
    const saveRequest = deferred<any>()
    const staleOptions = deferred<any>()
    const staleChains = deferred<any>()
    const saved = savedChain(303, 'group-saved', 'Group Saved', 'dept-saved', 'Department Saved')
    const freshOptions = chainOptionSet('group-saved', 'Group Saved', 'dept-saved', 'Department Saved')
    api.getQuotaResetApprovalChains.mockResolvedValueOnce({
      data: { data: { items: [configuredAlphaChain] } },
    })
    api.saveQuotaResetApprovalChains
      .mockReturnValueOnce(saveRequest.promise)
      .mockResolvedValueOnce({ data: { data: { items: [saved] } } })
    const wrapper = await mountChains()

    api.getQuotaResetApprovalChainOptions.mockReturnValueOnce(staleOptions.promise)
    api.getQuotaResetApprovalChains.mockReturnValueOnce(staleChains.promise)
    await wrapper.get('[data-testid="quota-reset-save-chains"]').trigger('click')
    await wrapper.setProps({ approverRevision: 1 })
    await wrapper.vm.$nextTick()

    expect(api.getQuotaResetApprovalChainOptions).toHaveBeenCalledTimes(1)
    expect(api.getQuotaResetApprovalChains).toHaveBeenCalledTimes(1)

    api.getQuotaResetApprovalChainOptions.mockReset()
    api.getQuotaResetApprovalChainOptions.mockResolvedValue({ data: { data: freshOptions } })
    api.getQuotaResetApprovalChains.mockReset()
    api.getQuotaResetApprovalChains.mockResolvedValue({ data: { data: { items: [saved] } } })
    saveRequest.resolve({ data: { data: { items: [saved] } } })
    await flushPromises()

    expect(api.getQuotaResetApprovalChainOptions).toHaveBeenCalledTimes(1)
    expect(api.getQuotaResetApprovalChains).toHaveBeenCalledTimes(1)

    staleOptions.resolve({
      data: { data: chainOptionSet('group-old', 'Group Old', 'dept-old', 'Department Old') },
    })
    staleChains.resolve({
      data: { data: { items: [savedChain(101, 'group-old', 'Group Old', 'dept-old', 'Department Old')] } },
    })
    await flushPromises()

    await selectChainGroup(wrapper, 'group-saved')
    expect(wrapper.text()).toContain('Department Saved')
    expect(wrapper.text()).not.toContain('Department Old')
    await wrapper.get('[data-testid="quota-reset-save-chains"]').trigger('click')
    await flushPromises()
    expect(api.saveQuotaResetApprovalChains.mock.calls[1][0]).toEqual([{
      provider_id: 1,
      group_id: 'group-saved',
      group_name: 'Group Saved',
      enabled: true,
      nodes: [{
        directory_source_id: 1,
        department_external_id: 'dept-saved',
        department_display_path: 'Department Saved',
      }],
    }])
  })

  it.each([
    { name: 'successful follow-up GET', followUpFails: false },
    { name: 'failed follow-up GET', followUpFails: true },
  ])('preserves a failed chain save across a queued $name', async ({ followUpFails }) => {
    const api = await import('@/api/quotaReset') as any
    const saveRequest = deferred<any>()
    api.getQuotaResetApprovalChains.mockResolvedValueOnce({
      data: { data: { items: [configuredAlphaChain] } },
    })
    api.saveQuotaResetApprovalChains
      .mockReturnValueOnce(saveRequest.promise)
      .mockImplementationOnce(async (items: any[]) => ({ data: { data: { items } } }))
    const wrapper = await mountChains()
    await selectChainGroup(wrapper, 'group-alpha')
    await addChainDepartment(wrapper, 'dept-beta')
    expect(wrapper.text()).toContain('Department Beta')

    await wrapper.get('[data-testid="quota-reset-save-chains"]').trigger('click')
    await wrapper.setProps({ approverRevision: 1 })
    await wrapper.vm.$nextTick()
    expect(api.getQuotaResetApprovalChainOptions).toHaveBeenCalledTimes(1)
    expect(api.getQuotaResetApprovalChains).toHaveBeenCalledTimes(1)

    if (followUpFails) {
      api.getQuotaResetApprovalChainOptions.mockRejectedValueOnce({
        response: { data: { message: 'Synthetic queued chain reload failed.' } },
      })
      api.getQuotaResetApprovalChains.mockResolvedValueOnce({
        data: { data: { items: [configuredAlphaChain] } },
      })
    } else {
      api.getQuotaResetApprovalChainOptions.mockResolvedValueOnce({ data: { data: chainOptions } })
      api.getQuotaResetApprovalChains.mockResolvedValueOnce({
        data: { data: { items: [configuredAlphaChain] } },
      })
    }
    saveRequest.reject({
      response: { data: { message: 'Group Alpha still references a stale approver.' } },
    })
    await flushPromises()

    expect(api.getQuotaResetApprovalChainOptions).toHaveBeenCalledTimes(2)
    expect(api.getQuotaResetApprovalChains).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('Group Alpha still references a stale approver.')
    expect(wrapper.text()).toContain('Department Beta')
    expect(wrapper.text()).not.toContain('Synthetic queued chain reload failed.')
    expect(wrapper.get('[data-testid="quota-reset-save-chains"]').attributes()).not.toHaveProperty('disabled')

    await wrapper.get('[data-testid="quota-reset-save-chains"]').trigger('click')
    await flushPromises()
    expect(api.saveQuotaResetApprovalChains.mock.calls[1][0]).toEqual([{
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
        {
          directory_source_id: 1,
          department_external_id: 'dept-beta',
          department_display_path: 'Department Beta',
        },
      ],
    }])
  })

  it('ignores a stale successful chain load after a newer revision succeeds', async () => {
    const api = await import('@/api/quotaReset') as any
    const staleOptions = deferred<any>()
    const staleChains = deferred<any>()
    api.getQuotaResetApprovalChainOptions
      .mockReturnValueOnce(staleOptions.promise)
      .mockResolvedValueOnce({
        data: { data: chainOptionSet('group-revision-2', 'Group Revision 2', 'dept-revision-2', 'Department Revision 2') },
      })
    api.getQuotaResetApprovalChains
      .mockReturnValueOnce(staleChains.promise)
      .mockResolvedValueOnce({
        data: { data: { items: [savedChain(202, 'group-revision-2', 'Group Revision 2', 'dept-revision-2', 'Department Revision 2')] } },
      })
    const wrapper = mount(SubscriptionGroupApprovalChains, { props: { approverRevision: 1 } })
    await wrapper.vm.$nextTick()

    await wrapper.setProps({ approverRevision: 2 })
    await flushPromises()
    await selectChainGroup(wrapper, 'group-revision-2')
    expect(wrapper.text()).toContain('Department Revision 2')

    staleOptions.resolve({
      data: { data: chainOptionSet('group-revision-1', 'Group Revision 1', 'dept-revision-1', 'Department Revision 1') },
    })
    staleChains.resolve({
      data: { data: { items: [savedChain(101, 'group-revision-1', 'Group Revision 1', 'dept-revision-1', 'Department Revision 1')] } },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Department Revision 2')
    expect(wrapper.text()).not.toContain('Department Revision 1')
    expect(wrapper.find('[data-testid="quota-reset-chain-loading"]').exists()).toBe(false)
    api.saveQuotaResetApprovalChains.mockClear()
    await wrapper.get('[data-testid="quota-reset-save-chains"]').trigger('click')
    await flushPromises()
    expect(api.saveQuotaResetApprovalChains).toHaveBeenCalledWith([
      {
        provider_id: 1,
        group_id: 'group-revision-2',
        group_name: 'Group Revision 2',
        enabled: true,
        nodes: [{
          directory_source_id: 1,
          department_external_id: 'dept-revision-2',
          department_display_path: 'Department Revision 2',
        }],
      },
    ])
  })

  it('ignores a stale failed chain load after a newer revision succeeds', async () => {
    const api = await import('@/api/quotaReset') as any
    const staleOptions = deferred<any>()
    const staleChains = deferred<any>()
    api.getQuotaResetApprovalChainOptions
      .mockReturnValueOnce(staleOptions.promise)
      .mockResolvedValueOnce({
        data: { data: chainOptionSet('group-revision-2', 'Group Revision 2', 'dept-revision-2', 'Department Revision 2') },
      })
    api.getQuotaResetApprovalChains
      .mockReturnValueOnce(staleChains.promise)
      .mockResolvedValueOnce({
        data: { data: { items: [savedChain(202, 'group-revision-2', 'Group Revision 2', 'dept-revision-2', 'Department Revision 2')] } },
      })
    const wrapper = mount(SubscriptionGroupApprovalChains, { props: { approverRevision: 1 } })
    await wrapper.vm.$nextTick()

    await wrapper.setProps({ approverRevision: 2 })
    await flushPromises()
    await selectChainGroup(wrapper, 'group-revision-2')
    expect(wrapper.text()).toContain('Department Revision 2')

    staleChains.resolve({ data: { data: { items: [] } } })
    staleOptions.reject({
      response: { data: { message: 'Stale revision failure must be ignored.' } },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Department Revision 2')
    expect(wrapper.text()).not.toContain('Stale revision failure must be ignored.')
    expect(wrapper.find('[data-testid="quota-reset-chain-loading"]').exists()).toBe(false)
  })

  it('removes an entire stale chain before atomic replacement save', async () => {
    const api = await import('@/api/quotaReset') as any
    api.getQuotaResetApprovalChainOptions.mockResolvedValueOnce({
      data: { data: chainOptionSet('group-alpha', 'Group Alpha', 'dept-alpha', 'Department Alpha') },
    })
    api.getQuotaResetApprovalChains.mockResolvedValueOnce({
      data: {
        data: {
          items: [
            configuredAlphaChain,
            savedChain(30, 'group-stale', 'Group Stale', 'dept-stale', 'Department Stale'),
          ],
        },
      },
    })
    const wrapper = await mountChains()
    await selectChainGroup(wrapper, 'group-stale')
    expect(wrapper.text()).toContain('This subscription group is no longer current.')

    const removeChain = wrapper.get('[data-testid="quota-reset-remove-chain"]')
    expect(removeChain.attributes('aria-label')).toBe('Remove approval chain Group Stale')
    expect(removeChain.attributes('title')).toBe('Remove approval chain Group Stale')
    expect(removeChain.find('svg').exists()).toBe(true)
    await removeChain.trigger('click')
    expect(wrapper.text()).not.toContain('Group Stale')

    await wrapper.get('[data-testid="quota-reset-save-chains"]').trigger('click')
    await flushPromises()
    expect(api.saveQuotaResetApprovalChains).toHaveBeenCalledWith([
      {
        provider_id: 1,
        group_id: 'group-alpha',
        group_name: 'Group Alpha',
        enabled: true,
        nodes: configuredAlphaChain.nodes,
      },
    ])
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

  it('guards notification save and test until the authoritative settings load completes', async () => {
    const api = await import('@/api/quotaReset') as any
    const settingsRequest = deferred<any>()
    api.getQuotaResetNotificationSettings.mockReturnValueOnce(settingsRequest.promise)
    const wrapper = mount(QuotaResetNotificationSettings, { props: { credentials: [] } })
    await wrapper.vm.$nextTick()

    const saveButton = wrapper.get('[data-testid="quota-reset-save-notification"]')
    const testButton = wrapper.get('[data-testid="quota-reset-test-notification"]')
    expect(saveButton.attributes()).toHaveProperty('disabled')
    expect(testButton.attributes()).toHaveProperty('disabled')

    await forceClick(saveButton)
    await forceClick(testButton)
    await flushPromises()
    expect(api.updateQuotaResetNotificationSettings).not.toHaveBeenCalled()
    expect(api.testQuotaResetNotificationSettings).not.toHaveBeenCalled()

    settingsRequest.resolve({ data: { data: notificationSettings() } })
    await flushPromises()
    expect(wrapper.get('[data-testid="quota-reset-save-notification"]').attributes()).not.toHaveProperty('disabled')
    expect(wrapper.get('[data-testid="quota-reset-test-notification"]').attributes()).not.toHaveProperty('disabled')
  })

  it('keeps notification defaults non-actionable after the initial settings load fails', async () => {
    const api = await import('@/api/quotaReset') as any
    api.getQuotaResetNotificationSettings.mockRejectedValueOnce({
      response: { data: { message: 'Synthetic notification settings load failed.' } },
    })
    const wrapper = await mountNotification()

    expect(wrapper.text()).toContain('Synthetic notification settings load failed.')
    const saveButton = wrapper.get('[data-testid="quota-reset-save-notification"]')
    const testButton = wrapper.get('[data-testid="quota-reset-test-notification"]')
    expect(saveButton.attributes()).toHaveProperty('disabled')
    expect(testButton.attributes()).toHaveProperty('disabled')

    await forceClick(saveButton)
    await forceClick(testButton)
    await flushPromises()
    expect(api.updateQuotaResetNotificationSettings).not.toHaveBeenCalled()
    expect(api.testQuotaResetNotificationSettings).not.toHaveBeenCalled()
  })

  it('locks every notification control and action while a save is in flight', async () => {
    const api = await import('@/api/quotaReset') as any
    const saveRequest = deferred<any>()
    api.getQuotaResetNotificationSettings.mockResolvedValueOnce({
      data: {
        data: notificationSettings({
          channel_type: 'generic_webhook',
          auth_type: 'bearer_token',
          credential_id: 33,
        }),
      },
    })
    api.updateQuotaResetNotificationSettings.mockReturnValueOnce(saveRequest.promise)
    const wrapper = await mountNotification([{
      id: 33,
      name: 'Synthetic webhook token',
      description: '',
      kind: 'secret_text',
      usage_count: 0,
      summary: { preview: 'syn****ken' },
      created_at: '',
      updated_at: '',
    }])

    await wrapper.get('[data-testid="quota-reset-save-notification"]').trigger('click')
    await wrapper.vm.$nextTick()

    for (const testID of [
      'quota-reset-notification-enabled',
      'quota-reset-notification-channel',
      'quota-reset-notification-url',
      'quota-reset-notification-auth',
      'quota-reset-notification-credential',
      'quota-reset-reload-notification',
      'quota-reset-save-notification',
      'quota-reset-test-notification',
    ]) {
      expect(wrapper.get(`[data-testid="${testID}"]`).attributes()).toHaveProperty('disabled')
    }

    await forceClick(wrapper.get('[data-testid="quota-reset-save-notification"]'))
    await forceClick(wrapper.get('[data-testid="quota-reset-test-notification"]'))
    await forceClick(wrapper.get('[data-testid="quota-reset-reload-notification"]'))
    await flushPromises()
    expect(api.updateQuotaResetNotificationSettings).toHaveBeenCalledTimes(1)
    expect(api.testQuotaResetNotificationSettings).not.toHaveBeenCalled()
    expect(api.getQuotaResetNotificationSettings).toHaveBeenCalledTimes(1)

    saveRequest.resolve({
      data: {
        data: notificationSettings({
          channel_type: 'generic_webhook',
          auth_type: 'bearer_token',
          credential_id: 33,
        }),
      },
    })
    await flushPromises()
    expect(wrapper.get('[data-testid="quota-reset-save-notification"]').attributes()).not.toHaveProperty('disabled')
    expect(wrapper.get('[data-testid="quota-reset-test-notification"]').attributes()).not.toHaveProperty('disabled')
  })

  it('locks every notification control and action while a test is in flight', async () => {
    const api = await import('@/api/quotaReset') as any
    const testRequest = deferred<any>()
    api.getQuotaResetNotificationSettings.mockResolvedValueOnce({
      data: {
        data: notificationSettings({
          channel_type: 'generic_webhook',
          auth_type: 'bearer_token',
          credential_id: 33,
        }),
      },
    })
    api.testQuotaResetNotificationSettings.mockReturnValueOnce(testRequest.promise)
    const wrapper = await mountNotification([{
      id: 33,
      name: 'Synthetic webhook token',
      description: '',
      kind: 'secret_text',
      usage_count: 0,
      summary: { preview: 'syn****ken' },
      created_at: '',
      updated_at: '',
    }])

    await wrapper.get('[data-testid="quota-reset-test-notification"]').trigger('click')
    await wrapper.vm.$nextTick()

    for (const testID of [
      'quota-reset-notification-enabled',
      'quota-reset-notification-channel',
      'quota-reset-notification-url',
      'quota-reset-notification-auth',
      'quota-reset-notification-credential',
      'quota-reset-reload-notification',
      'quota-reset-save-notification',
      'quota-reset-test-notification',
    ]) {
      expect(wrapper.get(`[data-testid="${testID}"]`).attributes()).toHaveProperty('disabled')
    }

    await forceClick(wrapper.get('[data-testid="quota-reset-save-notification"]'))
    await forceClick(wrapper.get('[data-testid="quota-reset-test-notification"]'))
    await forceClick(wrapper.get('[data-testid="quota-reset-reload-notification"]'))
    await flushPromises()
    expect(api.testQuotaResetNotificationSettings).toHaveBeenCalledTimes(1)
    expect(api.updateQuotaResetNotificationSettings).not.toHaveBeenCalled()
    expect(api.getQuotaResetNotificationSettings).toHaveBeenCalledTimes(1)

    testRequest.resolve({
      data: {
        data: {
          delivered: true,
          recipient_count: 1,
          missing_recipient_count: 0,
        },
      },
    })
    await flushPromises()
    expect(wrapper.get('[data-testid="quota-reset-save-notification"]').attributes()).not.toHaveProperty('disabled')
    expect(wrapper.get('[data-testid="quota-reset-test-notification"]').attributes()).not.toHaveProperty('disabled')
  })

  it('preserves redacted notification state but disables actions after a failed reload', async () => {
    const api = await import('@/api/quotaReset') as any
    const wrapper = await mountNotification()
    expect(wrapper.text()).toContain('synthetic...redacted')
    expect(wrapper.html()).not.toContain('synthetic-saved-robot-key')

    api.getQuotaResetNotificationSettings.mockRejectedValueOnce({
      response: { data: { message: 'Synthetic notification refresh failed.' } },
    })
    await wrapper.get('[data-testid="quota-reset-reload-notification"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Synthetic notification refresh failed.')
    expect(wrapper.text()).toContain('synthetic...redacted')
    expect(wrapper.html()).not.toContain('synthetic-saved-robot-key')
    const saveButton = wrapper.get('[data-testid="quota-reset-save-notification"]')
    const testButton = wrapper.get('[data-testid="quota-reset-test-notification"]')
    expect(saveButton.attributes()).toHaveProperty('disabled')
    expect(testButton.attributes()).toHaveProperty('disabled')
    expect(wrapper.get('[data-testid="quota-reset-reload-notification"]').attributes()).not.toHaveProperty('disabled')

    await forceClick(saveButton)
    await forceClick(testButton)
    await flushPromises()
    expect(api.updateQuotaResetNotificationSettings).not.toHaveBeenCalled()
    expect(api.testQuotaResetNotificationSettings).not.toHaveBeenCalled()

    api.getQuotaResetNotificationSettings.mockResolvedValueOnce({
      data: { data: notificationSettings({ url_preview: 'https://hooks.example.com/.../recovered' }) },
    })
    await wrapper.get('[data-testid="quota-reset-reload-notification"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('https://hooks.example.com/.../recovered')
    expect(wrapper.get('[data-testid="quota-reset-save-notification"]').attributes()).not.toHaveProperty('disabled')
    expect(wrapper.get('[data-testid="quota-reset-test-notification"]').attributes()).not.toHaveProperty('disabled')
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

  it.each([
    {
      name: 'Enterprise WeChat to generic webhook',
      initial: notificationSettings(),
      nextChannel: 'generic_webhook',
    },
    {
      name: 'generic webhook to Enterprise WeChat',
      initial: notificationSettings({
        channel_type: 'generic_webhook',
        url_preview: 'https://hooks.example.com',
        auth_type: 'none',
      }),
      nextChannel: 'wecom_group_robot',
    },
  ])('requires a replacement URL locally when switching $name', async ({ initial, nextChannel }) => {
    const api = await import('@/api/quotaReset') as any
    api.getQuotaResetNotificationSettings.mockResolvedValueOnce({ data: { data: initial } })
    const wrapper = await mountNotification()

    await wrapper.get('[data-testid="quota-reset-notification-channel"]').setValue(nextChannel)
    await wrapper.get('[data-testid="quota-reset-save-notification"]').trigger('click')
    await flushPromises()

    expect(api.updateQuotaResetNotificationSettings).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="quota-reset-notification-feedback"]').text())
      .toContain('Replace the endpoint when changing notification channels.')
  })

  it('refreshes the authoritative notification channel after a valid switch response', async () => {
    const api = await import('@/api/quotaReset') as any
    const genericSettings = notificationSettings({
      channel_type: 'generic_webhook',
      url_preview: 'https://hooks.example.com',
      auth_type: 'none',
    })
    api.updateQuotaResetNotificationSettings
      .mockResolvedValueOnce({ data: { data: genericSettings } })
      .mockResolvedValueOnce({ data: { data: genericSettings } })
    const wrapper = await mountNotification()

    await wrapper.get('[data-testid="quota-reset-notification-channel"]').setValue('generic_webhook')
    await wrapper.get('[data-testid="quota-reset-notification-url"]').setValue('https://hooks.example.com/quota-reset')
    await wrapper.get('[data-testid="quota-reset-save-notification"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="quota-reset-save-notification"]').trigger('click')
    await flushPromises()

    expect(api.updateQuotaResetNotificationSettings).toHaveBeenCalledTimes(2)
    expect(api.updateQuotaResetNotificationSettings.mock.calls[1][0]).toEqual({
      enabled: true,
      channel_type: 'generic_webhook',
      auth_type: 'none',
      credential_id: null,
    })
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
    await wrapper.get('[data-testid="quota-reset-notification-url"]').setValue('https://hooks.example.com/quota-reset')
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
