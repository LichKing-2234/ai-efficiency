import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { ElDialog, ElMessage, ElMessageBox } from 'element-plus'
import RelayPlanningView from '@/views/admin/RelayPlanningView.vue'

vi.mock('@/api/adminUsers', () => ({
  listAdminUserDepartmentOptions: vi.fn(),
  listAdminUserSubscriptionOptions: vi.fn(),
}))

vi.mock('@/api/relayPlanning', () => ({
	adoptCurrentRelayAccounts: vi.fn(),
	executeRelayPlan: vi.fn(),
  executeRelayMappingRenewal: vi.fn(),
  executeRelayReplan: vi.fn(),
  listRelayGroupMappings: vi.fn(),
  previewRelayMappingRenewal: vi.fn(),
  previewRelayPlan: vi.fn(),
  previewRelayReplan: vi.fn(),
	rebindRelayGroupMapping: vi.fn(),
	saveRelayDesiredAccounts: vi.fn(),
	searchRelayPlanningAccounts: vi.fn(),
	searchRelayPlanningUsers: vi.fn(),
}))

const plan = {
  provider_id: 7,
  department_id: 'dept-alpha',
  department_name: 'SDK Framework',
  platform: 'openai',
  template_group_id: 42,
  template_group_name: 'Group Alpha',
  source_group_id: 42,
  source_group_name: 'Group Alpha',
  weekly_cost_target: 2500,
  recommended_group_count: 1,
  group_count: 1,
  candidates: [{
    user_id: 1,
    relay_user_id: 101,
    username: 'alice',
    email: 'alice@example.com',
    range_cost: 1200,
    range_tokens: 100,
    global_token_rank: 1,
    migratable_key_count: 1,
    source_member: true,
    can_add: true,
    selected: true,
    eligible: true,
  }],
	assignments: [{
    index: 0,
    total_cost: 1200,
    user_ids: [1],
    target_group_name: 'SDK Framework-openai-01',
		desired_accounts: [{ account_id: 11, priority: 1 }],
		accounts: [{ id: 11, name: 'Account Alpha', platform: 'openai', type: 'oauth', status: 'active', schedulable: true, priority: 1 }],
	}],
	target_summaries: [{
		index: 0,
		target_group_id: 101,
		target_group_name: 'SDK Framework-openai-01',
		accounts: [
			{ account_id: 11, action: 'add', new_priority: 1 },
			{ account_id: 12, action: 'remove', old_priority: 1 },
			{ account_id: 13, action: 'reorder', old_priority: 2, new_priority: 1 },
		],
		members: [{ user_id: 1, relay_user_id: 101, action: 'move', from_group_id: 42, to_group_id: 101 }],
		subscriptions: [
			{ user_id: 1, relay_user_id: 101, action: 'add', group_id: 101 },
			{ user_id: 1, relay_user_id: 101, action: 'remove', group_id: 42 },
		],
		api_keys: [{ user_id: 1, relay_user_id: 101, action: 'move', count: 1, from_group_id: 42, to_group_id: 101 }],
	}],
	relationship_fingerprint: 'v1:preview-fingerprint',
	accounts_reviewed: true,
	generated_at: '2026-08-19T00:00:00Z',
}

const renewalPreview = {
	mapping_id: 9,
	provider_id: 7,
	platform: 'openai',
	renewal_days: 365,
	members: [
		{ user_id: 1, relay_user_id: 101, username: 'alice', email: 'alice@example.com', expected_target_group_id: 201, expected_target_group_name: 'Group Active', status: 'active', current_expiry: '2026-09-01T00:00:00Z', planned_action: 'extend', resulting_expiry: '2027-09-01T00:00:00Z', drift: [{ group_id: 999, group_name: 'Group Drift', status: 'active', expires_at: '2026-09-01T00:00:00Z' }] },
		{ user_id: 2, relay_user_id: 102, username: 'bob', email: 'bob@example.org', expected_target_group_id: 202, expected_target_group_name: 'Group Expired', status: 'expired', current_expiry: '2026-08-01T00:00:00Z', planned_action: 'renew', resulting_expiry: '2027-08-24T00:00:00Z' },
		{ user_id: 3, relay_user_id: 103, username: 'carol', email: 'carol@example.net', expected_target_group_id: 203, expected_target_group_name: 'Group Missing', status: 'missing', planned_action: 'create', resulting_expiry: '2027-08-24T00:00:00Z' },
		{ user_id: 4, relay_user_id: 104, username: 'dana', email: 'dana@example.edu', expected_target_group_id: 204, expected_target_group_name: 'Group Suspended', status: 'suspended', current_expiry: '2026-10-01T00:00:00Z', planned_action: 'skip', resulting_expiry: '2026-10-01T00:00:00Z' },
	],
	generated_at: '2026-08-24T00:00:00Z',
	relationship_fingerprint: 'v2:renewal-preview-fingerprint',
}

const renewalMapping = {
	id: 9,
	provider_id: 7,
	department_id: 'dept-alpha',
	department_name: 'SDK Framework',
	platform: 'openai',
	template_group_id: 42,
	template_group_name: 'Group Alpha',
	source_group_id: 0,
	source_group_name: '',
	group_ids: [201, 202, 203, 204],
	status: 'active',
	weekly_cost_target: 2500,
	member_assignments: { '1': 201, '2': 202, '3': 203, '4': 204 },
	account_management_initialized: false,
	desired_accounts: {},
	account_pools: [],
	updated_at: '2026-08-24T00:00:00Z',
}

async function mountView(initialMappings: any[] = [], wide = false) {
	const mediaQuery = {
		matches: wide,
		media: '(min-width: 1280px)',
		addEventListener: vi.fn(),
		removeEventListener: vi.fn(),
		addListener: vi.fn(),
		removeListener: vi.fn(),
	}
	const matchMedia = vi.fn(() => mediaQuery)
	Object.defineProperty(window, 'matchMedia', { configurable: true, value: matchMedia })
  const adminUsers = await import('@/api/adminUsers') as any
  const relayPlanning = await import('@/api/relayPlanning') as any
  adminUsers.listAdminUserDepartmentOptions.mockResolvedValue({
    data: { data: { items: [{ external_id: 'dept-alpha', name: 'SDK Framework', display_path: 'Engineering / SDK Framework' }] } },
  })
  adminUsers.listAdminUserSubscriptionOptions.mockResolvedValue({
    data: { data: { providers: [{ id: 7, name: 'relay', display_name: 'Relay', groups: [{ group_id: '42', group_name: 'Group Alpha', platform: 'openai' }] }] } },
  })
	relayPlanning.listRelayGroupMappings.mockResolvedValue({ data: { data: { items: initialMappings } } })
	relayPlanning.previewRelayMappingRenewal.mockImplementation((_id: number, data: { renewal_days: number }) => Promise.resolve({ data: { data: structuredClone({ ...renewalPreview, renewal_days: data.renewal_days }) } }))
  relayPlanning.previewRelayPlan.mockResolvedValue({ data: { data: structuredClone(plan) } })
	relayPlanning.searchRelayPlanningUsers.mockResolvedValue({
    data: { data: { items: [], total: 0, page: 1, page_size: 20 } },
	})
	relayPlanning.searchRelayPlanningAccounts.mockResolvedValue({ data: { data: { items: [], total: 0, page: 1, page_size: 20 } } })

  const wrapper = mount(RelayPlanningView, {
    global: {
      stubs: {
        teleport: true,
        AppLayout: { template: '<div><slot /></div>' },
        ElFormItem: { props: ['label'], template: '<label>{{ label }}<slot /></label>' },
        ElSelect: {
          props: ['modelValue'],
          emits: ['update:modelValue', 'change'],
          template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value); $emit(\'change\', $event.target.value)"><slot /></select>',
        },
        ElOption: { props: ['label', 'value'], template: '<option :value="value">{{ label }}</option>' },
        ElInputNumber: {
          props: ['modelValue'],
		  emits: ['update:modelValue', 'change'],
		  template: '<input type="number" :value="modelValue" @input="$emit(\'update:modelValue\', Number($event.target.value))" @change="$emit(\'change\', Number($event.target.value))">',
        },
				ElPagination: {
					props: ['currentPage', 'pageSize', 'total'],
					emits: ['current-change'],
					template: '<button v-bind="$attrs" @click="$emit(\'current-change\', currentPage + 1)"></button>',
				},
      },
    },
  })
  await flushPromises()
  return { wrapper, relayPlanning, matchMedia }
}

async function selectPlanningDepartment(wrapper: ReturnType<typeof mount>, departmentID = 'dept-alpha') {
  const picker = wrapper.get('[data-testid="department-select"]')
  await picker.get('[data-testid="admin-department-picker-trigger"]').trigger('click')
  await flushPromises()
  await picker.get(`[data-testid="admin-department-picker-option-${departmentID}"]`).trigger('click')
  await flushPromises()
}

async function fillAndPreview(wrapper: ReturnType<typeof mount>) {
  await selectPlanningDepartment(wrapper)
  await wrapper.get('[data-testid="platform-select"]').setValue('openai')
  await flushPromises()
  await wrapper.get('[data-testid="template-group-select"]').setValue('42')
  await wrapper.get('[data-testid="source-group-select"]').setValue('42')
  await wrapper.get('[data-testid="cost-target-input"]').setValue(2500)
  await flushPromises()
  await wrapper.get('[data-testid="preview-allocation"]').trigger('click')
  await flushPromises()
}

describe('RelayPlanningView', () => {
  beforeEach(() => {
		vi.restoreAllMocks()
		vi.resetAllMocks()
	})

	it('uses the shared wide-content breakpoint for mutually exclusive table layouts', async () => {
		const { wrapper, matchMedia } = await mountView([], true)
		await fillAndPreview(wrapper)

		expect(matchMedia).toHaveBeenCalledWith('(min-width: 1280px)')
		expect(wrapper.find('[data-testid="candidate-table-layout"]').exists()).toBe(true)
		expect(wrapper.find('[data-testid="candidate-card-layout"]').exists()).toBe(false)
	})

	it('previews managed subscription renewal from both responsive mapping layouts', async () => {
		const mapping = structuredClone(renewalMapping)
		const { wrapper, relayPlanning } = await mountView([mapping])

		expect(wrapper.find('[data-testid="mapping-card-layout"]').exists()).toBe(true)
		await wrapper.get('[data-testid="renew-mapping-9"]').trigger('click')
		await flushPromises()

		expect(relayPlanning.previewRelayMappingRenewal).toHaveBeenCalledWith(9, { renewal_days: 365 })
		const dialog = wrapper.findAllComponents(ElDialog).find((item) => item.props('modelValue') === true)
		expect(dialog?.props('modelValue')).toBe(true)
		expect(dialog?.props('appendToBody')).toBe(true)
		expect(dialog?.props('alignCenter')).toBe(true)
		expect(wrapper.get('[data-testid="renewal-selected-count"]').text()).toContain('4')
		for (const userID of [1, 2, 3, 4]) {
			expect((wrapper.get(`[data-testid="renewal-member-${userID}"]`).find('input').element as HTMLInputElement).checked).toBe(true)
		}
		expect(wrapper.text()).toContain('Group Active')
		expect(wrapper.text()).toContain('Extend')
		expect(wrapper.text()).toContain('Expired')
		expect(wrapper.text()).toContain('Renew')
		expect(wrapper.text()).toContain('Missing')
		expect(wrapper.text()).toContain('Create')
		expect(wrapper.text()).toContain('Suspended')
		expect(wrapper.text()).toContain('Skip')
		expect(wrapper.text()).toContain('Group Drift')
		expect(wrapper.get('[data-testid="renewal-current-expiry-1"]').text()).toContain('2026')
		expect(wrapper.get('[data-testid="renewal-resulting-expiry-1"]').text()).toContain('2027')

		await wrapper.get('[data-testid="renewal-member-2"]').find('input').setValue(false)
		expect(wrapper.get('[data-testid="renewal-selected-count"]').text()).toContain('3')
		const term = wrapper.get('[data-testid="renewal-days-input"]')
		await term.setValue(30)
		await term.trigger('change')
		await flushPromises()
		expect(relayPlanning.previewRelayMappingRenewal).toHaveBeenLastCalledWith(9, { renewal_days: 30 })
		expect((wrapper.get('[data-testid="renewal-member-2"]').find('input').element as HTMLInputElement).checked).toBe(false)
		expect(relayPlanning.executeRelayPlan).not.toHaveBeenCalled()
		expect(relayPlanning.executeRelayReplan).not.toHaveBeenCalled()

		wrapper.unmount()
		const wide = await mountView([mapping], true)
		expect(wide.wrapper.find('[data-testid="mapping-table-layout"]').exists()).toBe(true)
		expect(wide.wrapper.find('[data-testid="renew-mapping-9"]').exists()).toBe(true)
	})

	it('confirms renewal and retries only failed members with the same operation key', async () => {
		const { wrapper, relayPlanning } = await mountView([structuredClone(renewalMapping)])
		const afterExecution = structuredClone({ ...renewalPreview, relationship_fingerprint: 'v2:after-execution' })
		relayPlanning.executeRelayMappingRenewal
			.mockImplementationOnce((_id: number, request: any) => Promise.resolve({ data: { data: {
				mapping_id: 9,
				renewal_days: 365,
				operation_key: request.operation_key,
				members: [
					{ user_id: 1, relay_user_id: 101, target_group_id: 201, action: 'extend', status: 'succeeded' },
					{ user_id: 2, relay_user_id: 102, target_group_id: 202, action: 'renew', status: 'failed', error: 'synthetic timeout' },
					{ user_id: 3, relay_user_id: 103, target_group_id: 203, action: 'create', status: 'failed', error: 'synthetic failure' },
					{ user_id: 4, relay_user_id: 104, target_group_id: 204, action: 'skip', status: 'skipped' },
				],
				preview: afterExecution,
			} } }))
			.mockImplementationOnce((_id: number, request: any) => Promise.resolve({ data: { data: {
				mapping_id: 9,
				renewal_days: 365,
				operation_key: request.operation_key,
				members: [
					{ user_id: 2, relay_user_id: 102, target_group_id: 202, action: 'renew', status: 'succeeded' },
					{ user_id: 3, relay_user_id: 103, target_group_id: 203, action: 'create', status: 'succeeded' },
				],
				preview: { ...afterExecution, relationship_fingerprint: 'v2:after-retry' },
			} } }))
			.mockImplementationOnce((_id: number, request: any) => Promise.resolve({ data: { data: {
				mapping_id: 9,
				renewal_days: 365,
				operation_key: request.operation_key,
				members: [],
				preview: renewalPreview,
			} } }))

		await wrapper.get('[data-testid="renew-mapping-9"]').trigger('click')
		await flushPromises()
		await wrapper.get('[data-testid="confirm-renewal"]').trigger('click')
		await flushPromises()

		expect(relayPlanning.executeRelayMappingRenewal).toHaveBeenCalledTimes(1)
		const firstRequest = relayPlanning.executeRelayMappingRenewal.mock.calls[0][1]
		expect(firstRequest).toEqual({
			renewal_days: 365,
			members: renewalPreview.members.map((member) => ({ user_id: member.user_id, target_group_id: member.expected_target_group_id, planned_action: member.planned_action })),
			expected_relationship_fingerprint: 'v2:renewal-preview-fingerprint',
			operation_key: expect.any(String),
			retry: false,
		})
		expect(wrapper.get('[data-testid="renewal-result-1"]').text()).toContain('Succeeded')
		expect(wrapper.get('[data-testid="renewal-result-2"]').text()).toContain('Failed')
		expect(wrapper.get('[data-testid="renewal-result-4"]').text()).toContain('Skipped')

		await wrapper.get('[data-testid="retry-renewal-failures"]').trigger('click')
		await flushPromises()
		const retryRequest = relayPlanning.executeRelayMappingRenewal.mock.calls[1][1]
		expect(retryRequest).toEqual({
			renewal_days: 365,
			members: [
				{ user_id: 2, target_group_id: 202, planned_action: 'renew' },
				{ user_id: 3, target_group_id: 203, planned_action: 'create' },
			],
			expected_relationship_fingerprint: 'v2:after-execution',
			operation_key: firstRequest.operation_key,
			retry: true,
		})
		expect(wrapper.get('[data-testid="renewal-result-1"]').text()).toContain('Succeeded')
		expect(wrapper.get('[data-testid="renewal-result-2"]').text()).toContain('Succeeded')
		expect(wrapper.get('[data-testid="renewal-result-4"]').text()).toContain('Skipped')
		expect(wrapper.find('[data-testid="retry-renewal-failures"]').exists()).toBe(false)

		await wrapper.get('[data-testid="close-renewal"]').trigger('click')
		await wrapper.get('[data-testid="renew-mapping-9"]').trigger('click')
		await flushPromises()
		expect(wrapper.find('[data-testid="renewal-result-1"]').exists()).toBe(false)
		await wrapper.get('[data-testid="confirm-renewal"]').trigger('click')
		await flushPromises()
		expect(relayPlanning.executeRelayMappingRenewal.mock.calls[2][1].operation_key).not.toBe(firstRequest.operation_key)
	})

	it('refreshes stale renewal facts and requires another explicit confirmation', async () => {
		const warning = vi.spyOn(ElMessage, 'warning').mockImplementation(() => undefined as any)
		const { wrapper, relayPlanning } = await mountView([structuredClone(renewalMapping)])
		const refreshed = structuredClone({ ...renewalPreview, relationship_fingerprint: 'v2:refreshed-renewal', members: renewalPreview.members.map((member) => member.user_id === 1 ? { ...member, expected_target_group_name: 'Group Active Renamed' } : member) })
		relayPlanning.executeRelayMappingRenewal
			.mockRejectedValueOnce({ response: { status: 409, data: { details: { error_code: 'stale_relay_plan', refreshed_preview: refreshed } } } })
			.mockImplementationOnce((_id: number, request: any) => Promise.resolve({ data: { data: { mapping_id: 9, renewal_days: 365, operation_key: request.operation_key, members: [], preview: refreshed } } }))

		await wrapper.get('[data-testid="renew-mapping-9"]').trigger('click')
		await flushPromises()
		await wrapper.get('[data-testid="confirm-renewal"]').trigger('click')
		await flushPromises()

		expect(relayPlanning.executeRelayMappingRenewal).toHaveBeenCalledTimes(1)
		const operationKey = relayPlanning.executeRelayMappingRenewal.mock.calls[0][1].operation_key
		expect(wrapper.text()).toContain('Group Active Renamed')
		expect(warning).toHaveBeenCalledWith('Relay relationships changed. Review the refreshed renewal and confirm again.')
		expect(wrapper.get('[data-testid="renewal-review-alert"]').text()).toContain('Review the refreshed renewal')

		await wrapper.get('[data-testid="confirm-renewal"]').trigger('click')
		await flushPromises()
		expect(relayPlanning.executeRelayMappingRenewal.mock.calls[1][1]).toEqual(expect.objectContaining({ expected_relationship_fingerprint: 'v2:refreshed-renewal', operation_key: operationKey }))
		expect(wrapper.find('[data-testid="renewal-review-alert"]').exists()).toBe(false)
	})

	it('refetches authoritative facts before retry when the post-write preview was unavailable', async () => {
		const warning = vi.spyOn(ElMessage, 'warning').mockImplementation(() => undefined as any)
		const { wrapper, relayPlanning } = await mountView([structuredClone(renewalMapping)])
		relayPlanning.executeRelayMappingRenewal
			.mockImplementationOnce((_id: number, request: any) => Promise.resolve({ data: { data: {
				mapping_id: 9,
				renewal_days: 365,
				operation_key: request.operation_key,
				members: [
					{ user_id: 1, relay_user_id: 101, target_group_id: 201, action: 'extend', status: 'succeeded' },
					{ user_id: 2, relay_user_id: 102, target_group_id: 202, action: 'renew', status: 'failed', error: 'synthetic timeout' },
				],
				preview_error: 'synthetic refresh unavailable',
			} } }))
			.mockImplementationOnce((_id: number, request: any) => Promise.resolve({ data: { data: {
				mapping_id: 9,
				renewal_days: 365,
				operation_key: request.operation_key,
				members: [{ user_id: 2, relay_user_id: 102, target_group_id: 202, action: 'renew', status: 'succeeded' }],
				preview: { ...renewalPreview, relationship_fingerprint: 'v2:after-recovered-retry' },
			} } }))

		await wrapper.get('[data-testid="renew-mapping-9"]').trigger('click')
		await flushPromises()
		await wrapper.get('[data-testid="confirm-renewal"]').trigger('click')
		await flushPromises()
		const operationKey = relayPlanning.executeRelayMappingRenewal.mock.calls[0][1].operation_key
		expect(wrapper.text()).toContain('synthetic refresh unavailable')
		expect(wrapper.find('[data-testid="retry-renewal-failures"]').exists()).toBe(true)
		const recoveredPreview = { ...renewalPreview, relationship_fingerprint: 'v2:recovered-preview', members: renewalPreview.members.map((member) => member.user_id === 2 ? { ...member, status: 'active', planned_action: 'extend', expected_target_group_name: 'Group Expired Refreshed' } : member) }
		relayPlanning.previewRelayMappingRenewal.mockResolvedValueOnce({ data: { data: recoveredPreview } })

		await wrapper.get('[data-testid="retry-renewal-failures"]').trigger('click')
		await flushPromises()

		expect(relayPlanning.previewRelayMappingRenewal).toHaveBeenCalledTimes(2)
		expect(relayPlanning.executeRelayMappingRenewal).toHaveBeenCalledTimes(1)
		expect(wrapper.text()).toContain('Group Expired Refreshed')
		expect(warning).toHaveBeenCalledWith('Relay relationships changed. Review the refreshed renewal and confirm again.')
		expect(wrapper.get('[data-testid="renewal-review-alert"]').text()).toContain('Review the refreshed renewal')

		await wrapper.get('[data-testid="retry-renewal-failures"]').trigger('click')
		await flushPromises()

		expect(relayPlanning.executeRelayMappingRenewal.mock.calls[1][1]).toEqual({
			renewal_days: 365,
			members: [{ user_id: 2, target_group_id: 202, planned_action: 'renew' }],
			expected_relationship_fingerprint: 'v2:recovered-preview',
			operation_key: operationKey,
			retry: true,
		})
		expect(wrapper.get('[data-testid="renewal-result-1"]').text()).toContain('Succeeded')
		expect(wrapper.get('[data-testid="renewal-result-2"]').text()).toContain('Succeeded')
		expect(wrapper.text()).not.toContain('synthetic refresh unavailable')
		expect(wrapper.find('[data-testid="renewal-review-alert"]').exists()).toBe(false)
	})

	it('allows the department field to shrink inside the planning grid', async () => {
		const { wrapper } = await mountView()
		const picker = wrapper.get('[data-testid="department-select"]')

		expect(picker.element.parentElement?.classList.contains('min-w-0')).toBe(true)
	})

	it('localizes multi-Account and reused-Account mapping warnings', async () => {
		const { wrapper } = await mountView([{
			id: 9,
			provider_id: 7,
			department_id: 'dept-alpha',
			department_name: 'SDK Framework',
			platform: 'openai',
			template_group_id: 42,
			template_group_name: 'Group Alpha',
			source_group_id: 0,
			source_group_name: '',
			group_ids: [101, 102],
			status: 'active',
			weekly_cost_target: 2500,
			account_management_initialized: true,
			desired_accounts: {},
			account_pools: [],
			warnings: ['target group 101 has multiple Accounts', 'account 11 is reused across target groups 101, 102'],
			updated_at: '2026-08-20T00:00:00Z',
		}])

		expect(wrapper.text()).toContain('Target Group #101 has multiple Accounts')
		expect(wrapper.text()).toContain('Account #11 is reused across Target Groups #101, #102')
	})

	it('paginates managed mappings without rendering every mapping at once', async () => {
		const mappings = Array.from({ length: 11 }, (_, index) => ({
			id: index + 1,
			provider_id: 7,
			department_id: `dept-${index + 1}`,
			department_name: `Department ${index + 1}`,
			platform: 'openai',
			template_group_id: 42,
			template_group_name: 'Group Alpha',
			source_group_id: 42,
			source_group_name: 'Group Alpha',
			group_ids: [101 + index],
			status: 'active',
			weekly_cost_target: 2500,
			account_management_initialized: false,
			desired_accounts: {},
			account_pools: [],
			updated_at: '2026-08-24T00:00:00Z',
		}))
		const { wrapper } = await mountView(mappings)

		expect(wrapper.find('[data-testid="rebind-mapping-1"]').exists()).toBe(true)
		expect(wrapper.find('[data-testid="rebind-mapping-11"]').exists()).toBe(false)

		await wrapper.get('[data-testid="mapping-pagination"]').trigger('click')
		await flushPromises()

		expect(wrapper.find('[data-testid="rebind-mapping-1"]').exists()).toBe(false)
		expect(wrapper.find('[data-testid="rebind-mapping-11"]').exists()).toBe(true)
	})

	it('uses the automatic group recommendation and shows expected relay names', async () => {
    const { wrapper, relayPlanning } = await mountView()

    expect(wrapper.text()).toContain('30-day cost target per group (USD)')
    expect(wrapper.find('[data-testid="replan-group-count"]').exists()).toBe(false)

    await fillAndPreview(wrapper)

    expect(relayPlanning.previewRelayPlan).toHaveBeenCalledWith(expect.not.objectContaining({ group_count: expect.anything() }))
		expect(wrapper.text()).toContain('SDK Framework-openai-01')
	})

	it('searches and selects a department beyond the first 100 without mutating Relay before Preview', async () => {
		const { wrapper, relayPlanning } = await mountView()
		const adminUsers = await import('@/api/adminUsers') as any
		const lateDepartment = {
			external_id: 'dept-101',
			name: 'Department 101',
			display_path: 'Company / Department 101',
		}
		expect(adminUsers.listAdminUserDepartmentOptions).not.toHaveBeenCalled()
		adminUsers.listAdminUserDepartmentOptions.mockClear()
		adminUsers.listAdminUserDepartmentOptions.mockImplementation((params: { q?: string; page: number }) => {
			const items = params.q || params.page === 6
				? [lateDepartment]
				: Array.from({ length: 20 }, (_, index) => {
					const number = (params.page - 1) * 20 + index + 1
					return { external_id: `dept-${number}`, name: `Department ${number}`, display_path: `Company / Department ${number}` }
				})
			return Promise.resolve({ data: { data: { items, selected: null, total: params.q ? 1 : 101, page: params.page, page_size: 20 } } })
		})

		const picker = wrapper.get('[data-testid="department-select"]')
		expect(picker.get('[data-testid="admin-department-picker-trigger"]').text()).toContain('Select department')
		await picker.get('[data-testid="admin-department-picker-trigger"]').trigger('click')
		await flushPromises()
		expect(picker.find('[data-testid="admin-department-picker-all"]').exists()).toBe(false)

  for (let page = 2; page <= 6; page += 1) {
   await picker.get('[data-testid="admin-department-picker-pagination"]').trigger('click')
   await flushPromises()
  }
		expect(adminUsers.listAdminUserDepartmentOptions).toHaveBeenLastCalledWith({ page: 6, page_size: 20 })
		await picker.get('[data-testid="admin-department-picker-option-dept-101"]').trigger('click')
		await picker.get('[data-testid="admin-department-picker-trigger"]').trigger('click')
		await picker.get('[data-testid="admin-department-picker-search"]').setValue('Department 101')
		await vi.waitFor(() => expect(adminUsers.listAdminUserDepartmentOptions).toHaveBeenCalledWith({ q: 'Department 101', page: 1, page_size: 20 }))

		expect(relayPlanning.previewRelayPlan).not.toHaveBeenCalled()
		expect(relayPlanning.executeRelayPlan).not.toHaveBeenCalled()
		await picker.get('[data-testid="admin-department-picker-option-dept-101"]').trigger('click')
		await wrapper.get('[data-testid="platform-select"]').setValue('openai')
		await wrapper.get('[data-testid="template-group-select"]').setValue('42')
		await wrapper.get('[data-testid="cost-target-input"]').setValue(2500)
		await wrapper.get('[data-testid="preview-allocation"]').trigger('click')
		await flushPromises()

		expect(relayPlanning.previewRelayPlan).toHaveBeenCalledWith(expect.objectContaining({ department_id: 'dept-101' }))
	})

	it('restores and searches the selected department in Rebind without mutation before confirmation', async () => {
		const mapping = {
			id: 9,
			provider_id: 7,
			department_id: 'dept-101',
			department_name: 'Department 101',
			platform: 'openai',
			template_group_id: 42,
			template_group_name: 'Group Alpha',
			source_group_id: 42,
			source_group_name: 'Group Alpha',
			group_ids: [101],
			status: 'active',
			weekly_cost_target: 2500,
			account_management_initialized: false,
			desired_accounts: {},
			account_pools: [],
			updated_at: '2026-08-20T00:00:00Z',
		}
		const selected = { external_id: 'dept-101', name: 'Department 101', display_path: 'Company / Department 101' }
		const replacement = { external_id: 'dept-beta', name: 'Department Beta', display_path: 'Company / Department Beta' }
		const { wrapper, relayPlanning } = await mountView([mapping])
		const adminUsers = await import('@/api/adminUsers') as any
		adminUsers.listAdminUserDepartmentOptions.mockClear()
		adminUsers.listAdminUserDepartmentOptions.mockImplementation((params: { q?: string; selected_id?: string; page: number }) => Promise.resolve({
			data: { data: {
				items: params.q ? [replacement] : [],
				selected: params.selected_id ? selected : null,
				total: params.q ? 1 : 101,
				page: params.page,
				page_size: 20,
			} },
		}))
		relayPlanning.rebindRelayGroupMapping.mockResolvedValue({ data: { data: mapping } })

		await wrapper.get('[data-testid="rebind-mapping-9"]').trigger('click')
		await flushPromises()
		const picker = wrapper.get('[data-testid="rebind-department-select"]')
		expect(adminUsers.listAdminUserDepartmentOptions).toHaveBeenCalledWith({ selected_id: 'dept-101', page: 1, page_size: 20 })
		expect(picker.get('[data-testid="admin-department-picker-trigger"]').text()).toContain('Company / Department 101')

		await picker.get('[data-testid="admin-department-picker-trigger"]').trigger('click')
		await picker.get('[data-testid="admin-department-picker-search"]').setValue('Department Beta')
		await vi.waitFor(() => expect(adminUsers.listAdminUserDepartmentOptions).toHaveBeenCalledWith({ q: 'Department Beta', page: 1, page_size: 20 }))
		expect(relayPlanning.rebindRelayGroupMapping).not.toHaveBeenCalled()
		await picker.get('[data-testid="admin-department-picker-option-dept-beta"]').trigger('click')
		await wrapper.get('[data-testid="confirm-rebind"]').trigger('click')
		await flushPromises()

		expect(relayPlanning.rebindRelayGroupMapping).toHaveBeenCalledWith(9, expect.objectContaining({ department_id: 'dept-beta' }))
		expect(relayPlanning.previewRelayPlan).not.toHaveBeenCalled()
		expect(relayPlanning.executeRelayPlan).not.toHaveBeenCalled()
	})

	it('edits a proposed Target name before confirmation', async () => {
		const { wrapper, relayPlanning } = await mountView()
		await fillAndPreview(wrapper)

		await wrapper.get('[data-testid="target-name-0"]').setValue('Custom Target')
		await wrapper.get('[data-testid="open-execution-confirmation"]').trigger('click')
		await flushPromises()

		expect(relayPlanning.previewRelayPlan).toHaveBeenLastCalledWith(expect.objectContaining({
			assignments: [expect.objectContaining({ target_group_name: 'Custom Target' })],
		}))
	})

	it('blocks confirmation when a reviewed Target name is already occupied', async () => {
		const { wrapper } = await mountView()
		await fillAndPreview(wrapper)

		await wrapper.get('[data-testid="target-name-0"]').setValue('Group Alpha')
		expect(wrapper.text()).toContain('This Relay Group name is already in use')
		expect(wrapper.get('[data-testid="open-execution-confirmation"]').attributes('disabled')).toBeDefined()
	})

	it('applies existing Target rename suggestions explicitly and confirms a rename-only plan', async () => {
		const mapping = {
			id: 9,
			provider_id: 7,
			department_id: 'dept-alpha',
			department_name: 'SDK Framework',
			platform: 'openai',
			template_group_id: 42,
			template_group_name: 'Group Alpha',
			source_group_id: 0,
			source_group_name: '',
			group_ids: [101, 102],
			status: 'active',
			weekly_cost_target: 2500,
			account_management_initialized: false,
			desired_accounts: {},
			account_pools: [],
			updated_at: '2026-08-20T00:00:00Z',
		}
		const replan = structuredClone({
			...plan,
			mapping_id: 9,
			group_count: 2,
			candidates: [],
			assignments: [
				{ index: 0, total_cost: 0, user_ids: [], target_group_id: 101, target_group_name: 'Legacy A', current_target_group_name: 'Legacy A', suggested_target_group_name: 'SDK Framework-openai-01', rename_selected: false, desired_accounts: [], accounts: [] },
				{ index: 1, total_cost: 0, user_ids: [], target_group_id: 102, target_group_name: 'Legacy B', current_target_group_name: 'Legacy B', suggested_target_group_name: 'SDK Framework-openai-02', rename_selected: false, desired_accounts: [], accounts: [] },
			],
			target_summaries: [],
		})
		const reviewed = structuredClone({
			...replan,
			assignments: [
				{ ...replan.assignments[0], target_group_name: 'SDK Framework-openai-01', rename_selected: true },
				{ ...replan.assignments[1], target_group_name: 'Custom B', rename_selected: true },
			],
			target_summaries: [
				{ index: 0, target_group_id: 101, target_group_name: 'SDK Framework-openai-01', rename: { from_name: 'Legacy A', to_name: 'SDK Framework-openai-01' }, accounts: [], members: [], subscriptions: [], api_keys: [] },
				{ index: 1, target_group_id: 102, target_group_name: 'Custom B', rename: { from_name: 'Legacy B', to_name: 'Custom B' }, accounts: [], members: [], subscriptions: [], api_keys: [] },
			],
		})
		const { wrapper, relayPlanning } = await mountView([mapping])
		relayPlanning.previewRelayReplan.mockResolvedValueOnce({ data: { data: replan } }).mockResolvedValueOnce({ data: { data: reviewed } })
		relayPlanning.executeRelayReplan.mockResolvedValue({ data: { data: { plan: reviewed, groups: [
			{ index: 0, id: 101, name: 'SDK Framework-openai-01', current_name: 'Legacy A', status: 'succeeded', rename: 'succeeded' },
			{ index: 1, id: 102, name: 'Custom B', current_name: 'Legacy B', status: 'failed', rename: 'failed', error: 'synthetic rename failure' },
		], accounts: [], members: [] } } })

		await wrapper.get('[data-testid="replan-mapping-9"]').trigger('click')
		await flushPromises()
		expect((wrapper.get('[data-testid="rename-target-0"] input').element as HTMLInputElement).checked).toBe(false)
		await wrapper.get('[data-testid="apply-all-target-names"]').trigger('click')
		await wrapper.get('[data-testid="target-name-1"]').setValue('Custom B')
		await wrapper.get('[data-testid="open-execution-confirmation"]').trigger('click')
		await flushPromises()

		expect(relayPlanning.previewRelayReplan).toHaveBeenLastCalledWith(9, expect.objectContaining({
			selected_user_ids: [],
			assignments: [
				expect.objectContaining({ target_group_id: 101, target_group_name: 'SDK Framework-openai-01', rename_selected: true }),
				expect.objectContaining({ target_group_id: 102, target_group_name: 'Custom B', rename_selected: true }),
			],
		}))
		expect(wrapper.text()).toContain('Legacy A -> SDK Framework-openai-01')
		expect(wrapper.text()).toContain('Legacy B -> Custom B')
		await wrapper.get('[data-testid="confirm-execution"]').trigger('click')
		await flushPromises()
		expect(relayPlanning.executeRelayReplan).toHaveBeenCalledWith(9, expect.objectContaining({
			selected_user_ids: [],
			assignments: expect.arrayContaining([expect.objectContaining({ rename_selected: true })]),
		}))
		expect(wrapper.text()).toContain('Rename succeeded')
		expect(wrapper.text()).toContain('Rename needs retry')
		expect(wrapper.text()).toContain('synthetic rename failure')
	})

	it('keeps an unavailable saved Target reviewable without requiring a synthetic name', async () => {
		const mapping = {
			...structuredClone(renewalMapping),
			group_ids: [101],
			member_assignments: { '1': 101 },
		}
		const unavailableTargetPlan = structuredClone({
			...plan,
			mapping_id: 9,
			assignments: [{
				...plan.assignments[0],
				target_group_id: 101,
				target_group_name: '',
				current_target_group_name: '',
				suggested_target_group_name: 'SDK Framework-openai-01',
				rename_selected: false,
				target_unavailable: true,
			}],
			target_summaries: [],
			warnings: ['target group 101 is unavailable'],
		})
		const { wrapper, relayPlanning } = await mountView([mapping])
		relayPlanning.previewRelayReplan.mockResolvedValue({ data: { data: unavailableTargetPlan } })

		await wrapper.get('[data-testid="replan-mapping-9"]').trigger('click')
		await flushPromises()

		expect(wrapper.text()).not.toContain('Target name is required')
		expect(wrapper.get('[data-testid="open-execution-confirmation"]').attributes('disabled')).toBeUndefined()
		expect(wrapper.get('[data-testid="rename-target-0"] input').attributes('disabled')).toBeDefined()
		await wrapper.get('[data-testid="apply-all-target-names"]').trigger('click')
		expect((wrapper.get('[data-testid="rename-target-0"] input').element as HTMLInputElement).checked).toBe(false)

		await wrapper.get('[data-testid="open-execution-confirmation"]').trigger('click')
		await flushPromises()
		expect(relayPlanning.previewRelayReplan).toHaveBeenLastCalledWith(9, expect.objectContaining({
			assignments: [expect.objectContaining({ target_group_id: 101, target_group_name: '', rename_selected: false })],
		}))
		expect(wrapper.findAllComponents(ElDialog).some((dialog) => dialog.props('modelValue') === true)).toBe(true)
	})

	it('adds and removes suggested groups before confirmation', async () => {
    const { wrapper, relayPlanning } = await mountView()
    await fillAndPreview(wrapper)

    await wrapper.get('[data-testid="add-suggested-group"]').trigger('click')
    await wrapper.get('[data-testid="add-suggested-group"]').trigger('click')
    expect(wrapper.findAll('[data-testid^="suggested-group-"]')).toHaveLength(3)
    expect(wrapper.get('[data-testid="suggested-group-2"]').text()).toContain('Account Alpha')

    await wrapper.get('[data-testid="remove-suggested-group-0"]').trigger('click')
    expect(wrapper.findAll('[data-testid^="suggested-group-"]')).toHaveLength(2)
    const target = wrapper.get('[data-testid="candidate-target-1"]')
    expect((target.element as HTMLSelectElement).value).toBe('')
    await target.setValue('0')
    await wrapper.get('[data-testid="open-execution-confirmation"]').trigger('click')
    await flushPromises()

    expect(relayPlanning.previewRelayPlan).toHaveBeenLastCalledWith(expect.objectContaining({
      selected_user_ids: [1],
      assignments: [
        expect.objectContaining({ index: 0, user_ids: [1], desired_accounts: [{ account_id: 11, priority: 1 }] }),
        expect.objectContaining({ index: 1, user_ids: [], desired_accounts: [{ account_id: 11, priority: 1 }] }),
      ],
    }))
	})

	it('confirms a rendered managed-member removal with Source, Target, and API Key effects', async () => {
		const mapping = {
			...structuredClone(renewalMapping),
			template_group_id: 10,
			template_group_name: 'Template',
			source_group_id: 42,
			source_group_name: 'Group Alpha',
			group_ids: [101],
			member_assignments: { '1': 101 },
			member_sources: { '1': 42 },
			account_management_initialized: true,
			desired_accounts: { '101': [{ account_id: 11, priority: 1 }] },
		}
		const replan = structuredClone({
			...plan,
			mapping_id: 9,
			template_group_id: 10,
			template_group_name: 'Template',
			assignments: [{ ...plan.assignments[0], target_group_id: 101, user_ids: [1] }],
			target_summaries: [],
		})
		const removal = structuredClone({
			...replan,
			assignments: [{ ...replan.assignments[0], user_ids: [] }],
			target_summaries: [{
				index: 0,
				target_group_id: 101,
				target_group_name: 'SDK Framework-openai-01',
				accounts: [],
				members: [{ user_id: 1, relay_user_id: 101, action: 'remove', from_group_id: 101, to_group_id: 42 }],
				subscriptions: [
					{ user_id: 1, relay_user_id: 101, action: 'add', group_id: 42 },
					{ user_id: 1, relay_user_id: 101, action: 'remove', group_id: 101 },
				],
				api_keys: [{ user_id: 1, relay_user_id: 101, action: 'move', count: 1, from_group_id: 101, to_group_id: 42 }],
			}],
		})
		const { wrapper, relayPlanning } = await mountView([mapping])
		relayPlanning.previewRelayReplan
			.mockResolvedValueOnce({ data: { data: replan } })
			.mockResolvedValueOnce({ data: { data: removal } })
		relayPlanning.executeRelayReplan.mockResolvedValue({ data: { data: {
			plan: removal,
			groups: [],
			accounts: [],
			members: [{ user_id: 1, target_group_id: 101, subscription: 'failed', source_removal: 'failed', error: 'relationship readback failed' }],
			mapping: { ...mapping, status: 'needs_retry' },
		} } })

		await wrapper.get('[data-testid="replan-mapping-9"]').trigger('click')
		await flushPromises()
		await wrapper.get('[data-testid="remove-member-1"]').trigger('click')
		await wrapper.get('[data-testid="open-execution-confirmation"]').trigger('click')
		await flushPromises()

		expect(relayPlanning.previewRelayReplan).toHaveBeenLastCalledWith(9, expect.objectContaining({
			removed_user_ids: [1],
			assignments: [expect.objectContaining({ target_group_id: 101, user_ids: [] })],
		}))
		expect(wrapper.text()).toContain('Remove user #1 from Group #101')
		expect(wrapper.text()).toContain('Add Group #42 subscription for user #1')
		expect(wrapper.text()).toContain('Remove Group #101 subscription for user #1')
		expect(wrapper.text()).toContain('Move 1 API Key(s) from Group #101 to Group #42 for user #1')

		await wrapper.get('[data-testid="confirm-execution"]').trigger('click')
		await flushPromises()
		expect(relayPlanning.executeRelayReplan).toHaveBeenCalledWith(9, expect.objectContaining({
			removed_user_ids: [1],
			assignments: [expect.objectContaining({ target_group_id: 101, user_ids: [] })],
		}))
		expect(wrapper.text()).toContain('Needs retry')
		expect(wrapper.text()).toContain('relationship readback failed')
	})

	it('prioritizes needs-retry status over relationship warnings', async () => {
		const mapping = {
			...structuredClone(renewalMapping),
			status: 'needs_retry',
			warnings: ['unmanaged member 2 in target group 25'],
		}
		const { wrapper } = await mountView([mapping])

		expect(wrapper.text()).toContain('Needs retry')
		expect(wrapper.text()).toContain('User 2 is unmanaged in target Group #25')
	})

	it('requires a removal destination for a legacy managed member', async () => {
		const mapping = {
			...structuredClone(renewalMapping),
			template_group_id: 10,
			template_group_name: 'Template',
			source_group_id: 42,
			source_group_name: 'Group Alpha',
			group_ids: [101],
			member_assignments: { '1': 101 },
			member_sources: {},
			account_management_initialized: true,
			desired_accounts: { '101': [{ account_id: 11, priority: 1 }] },
		}
		const replan = structuredClone({
			...plan,
			mapping_id: 9,
			template_group_id: 10,
			template_group_name: 'Template',
			candidates: [{ ...plan.candidates[0], source_group_id: 0, source_member: false }],
			assignments: [{ ...plan.assignments[0], target_group_id: 101, user_ids: [1] }],
			target_summaries: [],
		})
		const removal = structuredClone({
			...replan,
			candidates: [{ ...replan.candidates[0], source_group_id: 42 }],
			assignments: [{ ...replan.assignments[0], user_ids: [] }],
			target_summaries: [{
				index: 0,
				target_group_id: 101,
				target_group_name: 'SDK Framework-openai-01',
				accounts: [],
				members: [{ user_id: 1, relay_user_id: 101, action: 'remove', from_group_id: 101, to_group_id: 42 }],
				subscriptions: [
					{ user_id: 1, relay_user_id: 101, action: 'add', group_id: 42 },
					{ user_id: 1, relay_user_id: 101, action: 'remove', group_id: 101 },
				],
				api_keys: [{ user_id: 1, relay_user_id: 101, action: 'move', count: 1, from_group_id: 101, to_group_id: 42 }],
			}],
		})
		const { wrapper, relayPlanning } = await mountView([mapping])
		relayPlanning.previewRelayReplan
			.mockResolvedValueOnce({ data: { data: replan } })
			.mockResolvedValueOnce({ data: { data: removal } })

		await wrapper.get('[data-testid="replan-mapping-9"]').trigger('click')
		await flushPromises()
		await wrapper.get('[data-testid="remove-member-1"]').trigger('click')

		expect(wrapper.get('[data-testid="open-execution-confirmation"]').attributes('disabled')).toBeDefined()
		expect(wrapper.text()).toContain('Choose Source or Target only')
		const source = wrapper.get('[data-testid="removed-member-source-1"]')
		expect((source.element as HTMLSelectElement).value).toBe('')
		await source.setValue('42')
		expect(wrapper.get('[data-testid="open-execution-confirmation"]').attributes('disabled')).toBeUndefined()

		await wrapper.get('[data-testid="open-execution-confirmation"]').trigger('click')
		await flushPromises()
		expect(relayPlanning.previewRelayReplan).toHaveBeenLastCalledWith(9, expect.objectContaining({
			removed_user_ids: [1],
			member_sources: { '1': 42 },
		}))
		expect(wrapper.text()).toContain('Add Group #42 subscription for user #1')
		expect(wrapper.text()).toContain('Remove Group #101 subscription for user #1')
		expect(wrapper.text()).toContain('Move 1 API Key(s) from Group #101 to Group #42 for user #1')
	})

	it('opens a centered in-page confirmation without executing', async () => {
    const { wrapper, relayPlanning } = await mountView()
    await fillAndPreview(wrapper)

    await wrapper.get('[data-testid="open-execution-confirmation"]').trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent(ElDialog)
    expect(dialog.exists()).toBe(true)
    expect(dialog.props('appendToBody')).toBe(true)
    expect(dialog.props('alignCenter')).toBe(true)
		expect(wrapper.text()).toContain('Confirm group plan')
		expect(wrapper.text()).toContain('Account changes')
		expect(wrapper.text()).toContain('Add Account #11 at priority 1')
		expect(wrapper.text()).toContain('Remove Account #12')
		expect(wrapper.text()).toContain('Change Account #13 priority from 2 to 1')
		expect(wrapper.text()).toContain('Move user #1 from Group #42 to Group #101')
		expect(wrapper.text()).toContain('Add Group #101 subscription for user #1')
		expect(wrapper.text()).toContain('Move 1 API Key(s) from Group #42 to Group #101 for user #1')
		expect(relayPlanning.executeRelayPlan).not.toHaveBeenCalled()
  })

	it('replaces a stale confirmation with the refreshed plan without replaying execution', async () => {
		const warning = vi.spyOn(ElMessage, 'warning').mockImplementation(() => undefined as any)
		const { wrapper, relayPlanning } = await mountView()
		await fillAndPreview(wrapper)
		await wrapper.get('[data-testid="open-execution-confirmation"]').trigger('click')
		await flushPromises()
		const refreshedPlan = structuredClone({
			...plan,
			relationship_fingerprint: 'v1:refreshed-fingerprint',
			assignments: [{ ...plan.assignments[0], target_group_name: 'Group Beta', user_ids: [] }],
		})
		relayPlanning.executeRelayPlan.mockRejectedValue({
			response: {
				status: 409,
				data: { details: { error_code: 'stale_relay_plan', refreshed_plan: refreshedPlan } },
			},
		})

		await wrapper.get('[data-testid="confirm-execution"]').trigger('click')
		await flushPromises()

		expect(relayPlanning.executeRelayPlan).toHaveBeenCalledTimes(1)
		expect(relayPlanning.executeRelayPlan).toHaveBeenCalledWith(expect.objectContaining({
			expected_relationship_fingerprint: 'v1:preview-fingerprint',
		}))
		expect(wrapper.findComponent(ElDialog).props('modelValue')).toBe(false)
		expect(wrapper.text()).toContain('Group Beta')
		expect(warning).toHaveBeenCalledWith('Relay relationships changed. Review the refreshed plan and confirm again.')
	})

	it('previews without a migration source and adds a searched user to one target', async () => {
    const { wrapper, relayPlanning } = await mountView()
	    relayPlanning.searchRelayPlanningUsers.mockResolvedValue({
      data: { data: { items: [{
        user_id: 2,
        relay_user_id: 102,
        username: 'bob',
        email: 'bob@example.org',
        department: { external_id: 'dept-beta', name: 'SDK Runtime', display_path: 'Engineering / SDK Runtime' },
        selectable: true,
	      }], total: 45, page: 1, page_size: 20 } },
    })
    relayPlanning.previewRelayPlan
      .mockResolvedValueOnce({ data: { data: structuredClone({ ...plan, source_group_id: 0, source_group_name: '' }) } })
      .mockResolvedValueOnce({ data: { data: structuredClone({
        ...plan,
        source_group_id: 0,
        source_group_name: '',
        candidates: [...plan.candidates, { ...plan.candidates[0], user_id: 2, relay_user_id: 102, username: 'bob', email: 'bob@example.org', source_member: false, source_group_id: 0 }],
        assignments: [{ ...plan.assignments[0], user_ids: [1, 2] }],
      }) } })

    await selectPlanningDepartment(wrapper)
    await wrapper.get('[data-testid="platform-select"]').setValue('openai')
    await flushPromises()
    await wrapper.get('[data-testid="template-group-select"]').setValue('42')
    await wrapper.get('[data-testid="cost-target-input"]').setValue(2500)
    await wrapper.get('[data-testid="preview-allocation"]').trigger('click')
    await flushPromises()

    expect(relayPlanning.previewRelayPlan).toHaveBeenNthCalledWith(1, expect.objectContaining({ source_group_id: 0 }))

    const search = wrapper.get('[data-testid="target-user-search-0"]')
    await search.setValue('bob')
	await vi.waitFor(() => expect(relayPlanning.searchRelayPlanningUsers).toHaveBeenCalledWith(expect.objectContaining({ provider_id: 7, platform: 'openai', q: 'bob' })))
	    expect(wrapper.text()).toContain('Engineering / SDK Runtime')
		await wrapper.get('[data-testid="target-user-pagination-0"]').trigger('click')
		await flushPromises()
		expect(relayPlanning.searchRelayPlanningUsers).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2, page_size: 20 }))

    await wrapper.get('[data-testid="add-searched-user-0-2"]').trigger('click')
    await flushPromises()
    expect(relayPlanning.previewRelayPlan).toHaveBeenNthCalledWith(2, expect.objectContaining({
      selected_user_ids: [1, 2],
      member_sources: expect.objectContaining({ '2': 0 }),
      assignments: [expect.objectContaining({ index: 0, user_ids: [1, 2] })],
    }))
	})

	it('keeps a stale user search response out of the rendered results', async () => {
		vi.useFakeTimers()
		try {
			const { wrapper, relayPlanning } = await mountView()
			await fillAndPreview(wrapper)
			let resolveAlice!: (value: unknown) => void
			let resolveBob!: (value: unknown) => void
			relayPlanning.searchRelayPlanningUsers.mockImplementation(({ q }: { q: string }) => new Promise((resolve) => {
				if (q === 'alice') resolveAlice = resolve
				else resolveBob = resolve
			}))

			const search = wrapper.get('[data-testid="target-user-search-0"]')
			await search.setValue('alice')
			await vi.advanceTimersByTimeAsync(300)
			await search.setValue('bob')
			await vi.advanceTimersByTimeAsync(300)

			resolveBob({ data: { data: { items: [{ user_id: 2, relay_user_id: 102, username: 'latest-bob', email: 'bob@example.org', selectable: true }], total: 1, page: 1, page_size: 20 } } })
			await flushPromises()
			expect(wrapper.text()).toContain('latest-bob')

			resolveAlice({ data: { data: { items: [{ user_id: 3, relay_user_id: 103, username: 'stale-alice', email: 'stale@example.net', selectable: true }], total: 1, page: 1, page_size: 20 } } })
			await flushPromises()
			expect(wrapper.text()).not.toContain('stale@example.net')
		} finally {
			vi.useRealTimers()
		}
	})

		it('edits Account assignments in the Preview target before confirmation', async () => {
		vi.useFakeTimers()
		try {
			const { wrapper, relayPlanning } = await mountView()
			relayPlanning.searchRelayPlanningAccounts.mockResolvedValue({ data: { data: { items: [{ id: 12, name: 'Account Beta', platform: 'openai', type: 'apikey', status: 'active', schedulable: true, group_relationships: [] }], total: 1, page: 1, page_size: 20 } } })
			await fillAndPreview(wrapper)

			expect(wrapper.text()).toContain('Account Alpha')
			const search = wrapper.get('[data-testid="target-account-search-0"]')
			await search.setValue('Beta')
			await vi.advanceTimersByTimeAsync(300)
			await flushPromises()
			await wrapper.get('[data-testid="add-target-account-0-12"]').trigger('click')
			await wrapper.get('[data-testid="remove-target-account-0-11"]').trigger('click')
			await wrapper.get('[data-testid="open-execution-confirmation"]').trigger('click')
			await flushPromises()

			expect(relayPlanning.previewRelayPlan).toHaveBeenLastCalledWith(expect.objectContaining({
				assignments: [expect.objectContaining({ desired_accounts: [{ account_id: 12, priority: 1 }] })],
			}))
			} finally {
				vi.useRealTimers()
			}
			})

	it('keeps Account results visible and retries when paging fails', async () => {
		vi.useFakeTimers()
		try {
			const stablePage = { data: { data: { items: [{ id: 12, name: 'Stable Account', platform: 'openai', type: 'apikey', status: 'active', schedulable: true, group_relationships: [] }], total: 45, page: 1, page_size: 20 } } }
			const { wrapper, relayPlanning } = await mountView()
			relayPlanning.searchRelayPlanningAccounts
				.mockResolvedValueOnce(stablePage)
				.mockRejectedValueOnce(new Error('synthetic Account page failure'))
				.mockResolvedValueOnce(stablePage)
			await fillAndPreview(wrapper)

			await wrapper.get('[data-testid="target-account-search-0"]').setValue('Account')
			await vi.advanceTimersByTimeAsync(300)
			await flushPromises()
			await wrapper.get('[data-testid="target-account-pagination-0"]').trigger('click')
			await flushPromises()

			expect(wrapper.text()).toContain('Stable Account')
			expect(wrapper.text()).toContain('synthetic Account page failure')
			const retry = wrapper.findAll('button').find((button) => button.text().includes('Retry'))
			expect(retry).toBeDefined()
			await retry!.trigger('click')
			await flushPromises()
			expect(relayPlanning.searchRelayPlanningAccounts).toHaveBeenCalledTimes(3)
			expect(wrapper.text()).not.toContain('synthetic Account page failure')
		} finally {
			vi.useRealTimers()
		}
	})

		it('shows current Account relationships and adopts them without applying Relay changes', async () => {
		const mapping = {
			id: 9,
			provider_id: 7,
			department_id: 'dept-alpha',
			department_name: 'SDK Framework',
			platform: 'openai',
			template_group_id: 42,
			template_group_name: 'Group Alpha',
			source_group_id: 0,
			source_group_name: '',
			group_ids: [101],
			status: 'active',
			weekly_cost_target: 2500,
			account_management_initialized: false,
			desired_accounts: {},
			account_pools: [{
				target_group_id: 101,
				current: [{ id: 11, name: 'Account Alpha', platform: 'openai', type: 'oauth', status: 'active', schedulable: true, priority: 1 }],
				desired: [],
				drift: false,
			}],
			updated_at: '2026-08-20T00:00:00Z',
		}
		const { wrapper, relayPlanning } = await mountView([mapping])
		relayPlanning.adoptCurrentRelayAccounts.mockResolvedValue({ data: { data: { ...mapping, account_management_initialized: true } } })

		await wrapper.get('[data-testid="manage-accounts-9"]').trigger('click')
		expect(wrapper.text()).toContain('Account Alpha')
		expect(wrapper.text()).toContain('Account relationships are not managed yet')

		await wrapper.get('[data-testid="adopt-current-accounts-9"]').trigger('click')
		await flushPromises()
		expect(relayPlanning.adoptCurrentRelayAccounts).toHaveBeenCalledWith(9)
		expect(relayPlanning.saveRelayDesiredAccounts).not.toHaveBeenCalled()
	})

	it('searches, adds, reorders, and saves desired Accounts without applying Relay changes', async () => {
		const mapping = {
			id: 9,
			provider_id: 7,
			department_id: 'dept-alpha',
			department_name: 'SDK Framework',
			platform: 'openai',
			template_group_id: 42,
			template_group_name: 'Group Alpha',
			source_group_id: 0,
			source_group_name: '',
			group_ids: [101],
			status: 'active',
			weekly_cost_target: 2500,
			account_management_initialized: true,
			desired_accounts: { '101': [{ account_id: 11, priority: 1 }] },
			account_pools: [{
				target_group_id: 101,
				current: [{ id: 11, name: 'Account Alpha', platform: 'openai', type: 'oauth', status: 'active', schedulable: true, priority: 1 }],
				desired: [{ account_id: 11, priority: 1 }],
				drift: false,
			}],
			updated_at: '2026-08-20T00:00:00Z',
		}
		const { wrapper, relayPlanning } = await mountView([mapping])
			relayPlanning.searchRelayPlanningAccounts.mockImplementation(({ page }: { page: number }) => Promise.resolve({ data: { data: {
				items: [{ id: page === 2 ? 32 : 12, name: page === 2 ? 'Account Page Two' : 'Account Beta', platform: 'openai', type: 'apikey', status: 'error', schedulable: false, group_relationships: [] }],
				total: 45,
				page,
				page_size: 20,
			} } }))
		relayPlanning.saveRelayDesiredAccounts.mockResolvedValue({ data: { data: mapping } })

			await wrapper.get('[data-testid="manage-accounts-9"]').trigger('click')
			await wrapper.get('[data-testid="account-search-9-101"]').setValue('Beta')
			await vi.waitFor(() => expect(relayPlanning.searchRelayPlanningAccounts).toHaveBeenCalledWith(expect.objectContaining({ provider_id: 7, platform: 'openai', q: 'Beta' })))
			await wrapper.get('[data-testid="account-pagination-9-101"]').trigger('click')
			await flushPromises()
			expect(relayPlanning.searchRelayPlanningAccounts).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2, page_size: 20 }))
			expect(wrapper.text()).toContain('Account Page Two')

			await wrapper.get('[data-testid="add-account-9-101-32"]').trigger('click')
			await wrapper.get('[data-testid="move-account-up-9-101-32"]').trigger('click')
		await wrapper.get('[data-testid="save-desired-accounts-9"]').trigger('click')
		await flushPromises()

			expect(relayPlanning.saveRelayDesiredAccounts).toHaveBeenCalledWith(9, {
				'101': [{ account_id: 32, priority: 1 }, { account_id: 11, priority: 2 }],
		})
		expect(relayPlanning.executeRelayReplan).not.toHaveBeenCalled()
	})

	it('defaults an existing managed user to Move Here and exposes Add Additionally', async () => {
		const mapping = {
			id: 9,
			provider_id: 7,
			department_id: 'dept-alpha',
			department_name: 'SDK Framework',
			platform: 'openai',
			template_group_id: 42,
			template_group_name: 'Group Alpha',
			source_group_id: 0,
			source_group_name: '',
			group_ids: [101],
			status: 'active',
			weekly_cost_target: 2500,
			member_assignments: { '1': 101 },
			account_management_initialized: true,
			desired_accounts: {},
			account_pools: [],
			updated_at: '2026-08-20T00:00:00Z',
		}
		const replan = structuredClone({ ...plan, mapping_id: 9, source_group_id: 0, source_group_name: '', assignments: [{ ...plan.assignments[0], target_group_id: 101 }] })
		const withBob = structuredClone({ ...replan, candidates: [...replan.candidates, { ...replan.candidates[0], user_id: 2, relay_user_id: 102, username: 'bob', email: 'bob@example.org', source_member: false, source_group_id: 0 }], assignments: [{ ...replan.assignments[0], user_ids: [1, 2] }] })
		const { wrapper, relayPlanning } = await mountView([mapping])
		relayPlanning.previewRelayReplan.mockResolvedValueOnce({ data: { data: replan } }).mockResolvedValueOnce({ data: { data: withBob } })
		relayPlanning.searchRelayPlanningUsers.mockResolvedValue({ data: { data: { items: [{
			user_id: 2,
			relay_user_id: 102,
			username: 'bob',
			email: 'bob@example.org',
			selectable: true,
			managed_assignments: [{ mapping_id: 8, department_id: 'dept-beta', department_name: 'SDK Runtime', target_group_id: 88 }],
		}], total: 1, page: 1, page_size: 20 } } })

		await wrapper.get('[data-testid="replan-mapping-9"]').trigger('click')
		await flushPromises()
		await wrapper.get('[data-testid="target-user-search-0"]').setValue('bob')
		await vi.waitFor(() => expect(relayPlanning.searchRelayPlanningUsers).toHaveBeenCalledWith(expect.objectContaining({ q: 'bob' })))
		await wrapper.get('[data-testid="add-searched-user-0-2"]').trigger('click')
		await flushPromises()

			expect(relayPlanning.previewRelayReplan).toHaveBeenLastCalledWith(9, expect.objectContaining({
				member_actions: { '2': { mode: 'move_here', from_mapping_id: 8 } },
			}))
			expect(wrapper.text()).toContain('Move here')
			expect(wrapper.text()).toContain('Add additionally')
			expect(wrapper.text()).not.toContain('multiple managed Account pools')
			const addAdditionally = wrapper.findAll('.el-radio-button').find((item) => item.text().includes('Add additionally'))
			expect(addAdditionally).toBeDefined()
				await addAdditionally!.get('input').setValue(true)
				await flushPromises()
				expect(wrapper.text()).toContain('This user will remain in multiple managed Account pools')
			})

	it('restores failed Replan actions in the rendered workflow', async () => {
		const mapping = {
			...structuredClone(renewalMapping),
			source_group_id: 0,
			source_group_name: '',
			group_ids: [101],
			status: 'needs_retry',
			member_assignments: { '2': 101 },
			operation_state: {
				'member:1': { action: 'remove', source_reviewed: 'true', source_group_id: '0', source_removal: 'failed', error: 'synthetic removal failure' },
				'member:2': { action: 'move_here', from_mapping_id: '8', source_removal: 'failed', error: 'synthetic move failure' },
			},
		}
		const replan = structuredClone({
			...plan,
			mapping_id: 9,
			source_group_id: 0,
			source_group_name: '',
			candidates: [{ ...plan.candidates[0], user_id: 2, relay_user_id: 102, username: 'bob', email: 'bob@example.org' }],
			assignments: [{ ...plan.assignments[0], target_group_id: 101, user_ids: [2] }],
		})
		const { wrapper, relayPlanning } = await mountView([mapping])
		relayPlanning.previewRelayReplan.mockResolvedValue({ data: { data: replan } })

		await wrapper.get('[data-testid="replan-mapping-9"]').trigger('click')
		await flushPromises()

		expect(relayPlanning.previewRelayReplan).toHaveBeenCalledWith(9, {
			removed_user_ids: [1],
			member_actions: { '2': { mode: 'move_here', from_mapping_id: 8 } },
		})
		expect(wrapper.text()).toContain('Move here')
		expect(wrapper.get('[data-testid="removed-member-source-1"]').attributes('disabled')).toBeDefined()
	})

	it('renders only the last confirmed Replan roster as selected', async () => {
		const mapping = {
			...structuredClone(renewalMapping),
			source_group_id: 42,
			source_group_name: 'Group Alpha',
			group_ids: [101],
			member_assignments: { '1': 101 },
			member_sources: { '1': 42 },
		}
		const replan = structuredClone({
			...plan,
			mapping_id: 9,
			candidates: [
				{ ...plan.candidates[0], selected: true },
				{ ...plan.candidates[0], user_id: 2, relay_user_id: 102, username: 'bob', email: 'bob@example.org', selected: false },
			],
			assignments: [{ ...plan.assignments[0], target_group_id: 101, user_ids: [1] }],
		})
		const { wrapper, relayPlanning } = await mountView([mapping])
		relayPlanning.previewRelayReplan.mockResolvedValue({ data: { data: replan } })

		await wrapper.get('[data-testid="replan-mapping-9"]').trigger('click')
		await flushPromises()

		expect(wrapper.get('[data-testid="suggested-group-0"]').text()).toContain('alice')
		expect(wrapper.get('[data-testid="suggested-group-0"]').text()).not.toContain('bob')
		expect((wrapper.get('[data-testid="candidate-target-2"]').element as HTMLSelectElement).value).toBe('')
	})

		it('opens one centered Rebind form and locks the final submission', async () => {
		const mapping = {
			id: 9,
			provider_id: 7,
			department_id: 'dept-alpha',
			department_name: 'SDK Framework',
			platform: 'openai',
			template_group_id: 42,
			template_group_name: 'Group Alpha',
			source_group_id: 42,
			source_group_name: 'Group Alpha',
			group_ids: [101],
			status: 'active',
			weekly_cost_target: 2500,
			account_management_initialized: false,
			desired_accounts: {},
			account_pools: [],
			updated_at: '2026-08-20T00:00:00Z',
		}
		let resolveRebind!: (value: unknown) => void
		const pending = new Promise((resolve) => { resolveRebind = resolve })
		const { wrapper, relayPlanning } = await mountView([mapping])
		const prompt = vi.spyOn(ElMessageBox, 'prompt').mockRejectedValue('cancel')
		relayPlanning.rebindRelayGroupMapping.mockReturnValue(pending)

		await wrapper.get('[data-testid="rebind-mapping-9"]').trigger('click')
		const dialog = wrapper.findAllComponents(ElDialog).find((item) => item.props('modelValue') === true)
		expect(prompt).not.toHaveBeenCalled()
		expect(dialog?.props('appendToBody')).toBe(true)
		expect(dialog?.props('alignCenter')).toBe(true)
		const departmentPicker = wrapper.get('[data-testid="rebind-department-select"]')
		expect(departmentPicker.element.parentElement?.classList.contains('min-w-0')).toBe(true)
		expect(wrapper.find('[data-testid="rebind-template-select"]').exists()).toBe(true)
		expect(wrapper.find('[data-testid="rebind-source-select"]').exists()).toBe(true)
		expect(wrapper.find('[data-testid="rebind-targets-select"]').exists()).toBe(true)
		expect(relayPlanning.rebindRelayGroupMapping).not.toHaveBeenCalled()

		await wrapper.get('[data-testid="confirm-rebind"]').trigger('click')
		expect(relayPlanning.rebindRelayGroupMapping).toHaveBeenCalledTimes(1)
		expect(relayPlanning.rebindRelayGroupMapping).toHaveBeenCalledWith(9, {
			department_id: 'dept-alpha',
			template_group_id: 42,
			source_group_id: 42,
			group_ids: [101],
		})
		await wrapper.get('[data-testid="confirm-rebind"]').trigger('click')
		expect(relayPlanning.rebindRelayGroupMapping).toHaveBeenCalledTimes(1)

		resolveRebind({ data: { data: mapping } })
		await flushPromises()
	})

})
