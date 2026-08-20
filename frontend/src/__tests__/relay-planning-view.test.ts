import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { ElDialog } from 'element-plus'
import RelayPlanningView from '@/views/admin/RelayPlanningView.vue'

vi.mock('@/api/adminUsers', () => ({
  listAdminUserDepartmentOptions: vi.fn(),
  listAdminUserSubscriptionOptions: vi.fn(),
}))

vi.mock('@/api/relayPlanning', () => ({
	adoptCurrentRelayAccounts: vi.fn(),
	executeRelayPlan: vi.fn(),
  executeRelayReplan: vi.fn(),
  listRelayGroupMappings: vi.fn(),
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
    target_group_name: 'Group Alpha (Copy)',
	}],
	target_summaries: [{
		index: 0,
		target_group_id: 101,
		target_group_name: 'Group Alpha (Copy)',
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
	generated_at: '2026-08-19T00:00:00Z',
}

async function mountView(initialMappings: any[] = []) {
  const adminUsers = await import('@/api/adminUsers') as any
  const relayPlanning = await import('@/api/relayPlanning') as any
  adminUsers.listAdminUserDepartmentOptions.mockResolvedValue({
    data: { data: { items: [{ external_id: 'dept-alpha', name: 'SDK Framework', display_path: 'Engineering / SDK Framework' }] } },
  })
  adminUsers.listAdminUserSubscriptionOptions.mockResolvedValue({
    data: { data: { providers: [{ id: 7, name: 'relay', display_name: 'Relay', groups: [{ group_id: '42', group_name: 'Group Alpha', platform: 'openai' }] }] } },
  })
	relayPlanning.listRelayGroupMappings.mockResolvedValue({ data: { data: { items: initialMappings } } })
  relayPlanning.previewRelayPlan.mockResolvedValue({ data: { data: plan } })
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
          emits: ['update:modelValue'],
          template: '<input type="number" :value="modelValue" @input="$emit(\'update:modelValue\', Number($event.target.value))">',
        },
      },
    },
  })
  await flushPromises()
  return { wrapper, relayPlanning }
}

async function fillAndPreview(wrapper: ReturnType<typeof mount>) {
  await wrapper.get('[data-testid="department-select"]').setValue('dept-alpha')
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
  beforeEach(() => vi.resetAllMocks())

  it('uses the automatic group recommendation and shows expected relay names', async () => {
    const { wrapper, relayPlanning } = await mountView()

    expect(wrapper.text()).toContain('30-day cost target per group (USD)')
    expect(wrapper.find('[data-testid="replan-group-count"]').exists()).toBe(false)

    await fillAndPreview(wrapper)

    expect(relayPlanning.previewRelayPlan).toHaveBeenCalledWith(expect.not.objectContaining({ group_count: expect.anything() }))
    expect(wrapper.text()).toContain('Group Alpha (Copy)')
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

	it('sends the Preview fingerprint and replaces a stale confirmation with the refreshed plan', async () => {
		const messageWarning = vi.spyOn(ElMessage, 'warning').mockImplementation(() => undefined as any)
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
				data: {
					message: 'Relay relationships changed after Preview',
					details: { error_code: 'stale_relay_plan', refreshed_plan: refreshedPlan, differences: ['subscription changed'] },
				},
			},
		})

		await wrapper.get('[data-testid="confirm-execution"]').trigger('click')
		await flushPromises()

		expect(relayPlanning.executeRelayPlan).toHaveBeenCalledWith(expect.objectContaining({
			expected_relationship_fingerprint: 'v1:preview-fingerprint',
		}))
		expect(wrapper.findComponent(ElDialog).props('modelValue')).toBe(false)
		expect(wrapper.text()).toContain('Group Beta')
		expect(messageWarning).toHaveBeenCalledWith('Relay relationships changed. Review the refreshed plan and confirm again.')
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
      }], total: 1, page: 1, page_size: 20 } },
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

    await wrapper.get('[data-testid="department-select"]').setValue('dept-alpha')
    await wrapper.get('[data-testid="platform-select"]').setValue('openai')
    await flushPromises()
    await wrapper.get('[data-testid="template-group-select"]').setValue('42')
    await wrapper.get('[data-testid="cost-target-input"]').setValue(2500)
    await wrapper.get('[data-testid="preview-allocation"]').trigger('click')
    await flushPromises()

    expect(relayPlanning.previewRelayPlan).toHaveBeenNthCalledWith(1, expect.objectContaining({ source_group_id: 0 }))

    const search = wrapper.get('[data-testid="target-user-search-0"]')
    await search.setValue('bob')
    await flushPromises()
    expect(relayPlanning.searchRelayPlanningUsers).toHaveBeenCalledWith(expect.objectContaining({ provider_id: 7, platform: 'openai', q: 'bob' }))
    expect(wrapper.text()).toContain('Engineering / SDK Runtime')

    await wrapper.get('[data-testid="add-searched-user-0-2"]').trigger('click')
    await flushPromises()
    expect(relayPlanning.previewRelayPlan).toHaveBeenNthCalledWith(2, expect.objectContaining({
      selected_user_ids: [1, 2],
      member_sources: expect.objectContaining({ '2': 0 }),
      assignments: [expect.objectContaining({ index: 0, user_ids: [1, 2] })],
    }))
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
		relayPlanning.searchRelayPlanningAccounts.mockResolvedValue({ data: { data: { items: [{ id: 12, name: 'Account Beta', platform: 'openai', type: 'apikey', status: 'error', schedulable: false, group_relationships: [] }], total: 1, page: 1, page_size: 20 } } })
		relayPlanning.saveRelayDesiredAccounts.mockResolvedValue({ data: { data: mapping } })

		await wrapper.get('[data-testid="manage-accounts-9"]').trigger('click')
		await wrapper.get('[data-testid="account-search-9-101"]').setValue('Beta')
		await flushPromises()
		expect(relayPlanning.searchRelayPlanningAccounts).toHaveBeenCalledWith(expect.objectContaining({ provider_id: 7, platform: 'openai', q: 'Beta' }))

		await wrapper.get('[data-testid="add-account-9-101-12"]').trigger('click')
		await wrapper.get('[data-testid="move-account-up-9-101-12"]').trigger('click')
		await wrapper.get('[data-testid="save-desired-accounts-9"]').trigger('click')
		await flushPromises()

		expect(relayPlanning.saveRelayDesiredAccounts).toHaveBeenCalledWith(9, {
			'101': [{ account_id: 12, priority: 1 }, { account_id: 11, priority: 2 }],
		})
		expect(relayPlanning.executeRelayReplan).not.toHaveBeenCalled()
	})

	it('includes only explicitly removed managed members in the confirmation preview', async () => {
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
			member_assignments: { '1': 101 },
			member_sources: { '1': 42 },
			account_management_initialized: true,
			desired_accounts: { '101': [{ account_id: 11, priority: 1 }] },
			account_pools: [],
			updated_at: '2026-08-20T00:00:00Z',
		}
		const replan = structuredClone({ ...plan, mapping_id: 9, assignments: [{ ...plan.assignments[0], target_group_id: 101 }] })
		const { wrapper, relayPlanning } = await mountView([mapping])
		relayPlanning.previewRelayReplan.mockResolvedValue({ data: { data: replan } })

		await wrapper.get('[data-testid="replan-mapping-9"]').trigger('click')
		await flushPromises()
		await wrapper.get('[data-testid="remove-member-1"]').trigger('click')
		await wrapper.get('[data-testid="open-execution-confirmation"]').trigger('click')
		await flushPromises()

		expect(relayPlanning.previewRelayReplan).toHaveBeenLastCalledWith(9, expect.objectContaining({
			removed_user_ids: [1],
			assignments: [expect.objectContaining({ user_ids: [] })],
		}))
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
		await flushPromises()
		await wrapper.get('[data-testid="add-searched-user-0-2"]').trigger('click')
		await flushPromises()

		expect(relayPlanning.previewRelayReplan).toHaveBeenLastCalledWith(9, expect.objectContaining({
			member_actions: { '2': { mode: 'move_here', from_mapping_id: 8 } },
		}))
		expect(wrapper.text()).toContain('Move here')
		expect(wrapper.text()).toContain('Add additionally')
	})
})
