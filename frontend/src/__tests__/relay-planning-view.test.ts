import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { ElDialog } from 'element-plus'
import RelayPlanningView from '@/views/admin/RelayPlanningView.vue'

vi.mock('@/api/adminUsers', () => ({
  listAdminUserDepartmentOptions: vi.fn(),
  listAdminUserSubscriptionOptions: vi.fn(),
}))

vi.mock('@/api/relayPlanning', () => ({
  executeRelayPlan: vi.fn(),
  executeRelayReplan: vi.fn(),
  listRelayGroupMappings: vi.fn(),
  previewRelayPlan: vi.fn(),
  previewRelayReplan: vi.fn(),
  rebindRelayGroupMapping: vi.fn(),
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
  generated_at: '2026-08-19T00:00:00Z',
}

async function mountView() {
  const adminUsers = await import('@/api/adminUsers') as any
  const relayPlanning = await import('@/api/relayPlanning') as any
  adminUsers.listAdminUserDepartmentOptions.mockResolvedValue({
    data: { data: { items: [{ external_id: 'dept-alpha', name: 'SDK Framework', display_path: 'Engineering / SDK Framework' }] } },
  })
  adminUsers.listAdminUserSubscriptionOptions.mockResolvedValue({
    data: { data: { providers: [{ id: 7, name: 'relay', display_name: 'Relay', groups: [{ group_id: '42', group_name: 'Group Alpha', platform: 'openai' }] }] } },
  })
  relayPlanning.listRelayGroupMappings.mockResolvedValue({ data: { data: { items: [] } } })
  relayPlanning.previewRelayPlan.mockResolvedValue({ data: { data: plan } })

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
    expect(relayPlanning.executeRelayPlan).not.toHaveBeenCalled()
  })
})
