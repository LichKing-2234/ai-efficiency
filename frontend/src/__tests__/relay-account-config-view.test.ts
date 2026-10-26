import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { ElDialog } from 'element-plus'
import AdminRelayAccountsView from '@/views/admin/AdminRelayAccountsView.vue'

vi.mock('@/api/adminUsers', () => ({
  listAdminUserSubscriptionOptions: vi.fn(),
}))

vi.mock('@/api/relayPlanning', () => ({
  applyRelayAccountConfiguration: vi.fn(),
  listRelayAccountConfigurations: vi.fn(),
}))

const accounts = [
  { id: 11, name: 'Account Alpha', platform: 'openai', type: 'oauth', status: 'active', schedulable: true, group_relationships: [{ group_id: 42, priority: 0 }] },
  { id: 12, name: 'Account Beta', platform: 'openai', type: 'apikey', status: 'active', schedulable: false, group_relationships: [{ group_id: 43, priority: 0 }] },
  { id: 13, name: 'Account Gamma', platform: 'openai', type: 'oauth', status: 'disabled', schedulable: true, group_relationships: [{ group_id: 42, priority: 1 }] },
  { id: 14, name: 'Account Delta', platform: 'openai', type: 'apikey', status: 'active', schedulable: true, group_relationships: [] },
]

const baselinePage = {
  baseline_account_id: 0,
  items: accounts.map((account) => ({ account, configuration: { model_mapping: { 'gpt-4': 'gpt-4' } } })),
  total: accounts.length,
}

const diffPage = {
  baseline_account_id: 11,
  baseline_configuration: { model_mapping: { 'gpt-4': 'gpt-4' }, extra: { privacy_mode: true } },
  items: [
    {
      account: accounts[0],
      configuration: { model_mapping: { 'gpt-4': 'gpt-4' }, extra: { privacy_mode: true } },
      diff: { matches: true },
    },
    {
      account: accounts[1],
      configuration: {},
      diff: {
        matches: false,
        missing_keys: ['model_mapping'],
        differing_keys: ['extra.privacy_mode'],
        additional_keys: ['extra.legacy_mode'],
      },
    },
    {
      account: accounts[2],
      configuration: { model_mapping: { 'gpt-4': 'gpt-4' }, extra: { privacy_mode: true } },
      diff: { matches: true },
    },
    {
      account: accounts[3],
      configuration: { model_mapping: { 'gpt-4': 'gpt-4' } },
      diff: { matches: false, differing_keys: ['extra.privacy_mode'] },
    },
  ],
  total: accounts.length,
}

async function mountView() {
  const adminUsers = await import('@/api/adminUsers') as any
  const relayPlanning = await import('@/api/relayPlanning') as any
  adminUsers.listAdminUserSubscriptionOptions.mockResolvedValue({
    data: {
      data: {
        providers: [{
          id: 7,
          name: 'relay',
          display_name: 'Relay',
          groups: [
            { group_id: '42', group_name: 'Group Alpha', platform: 'openai' },
            { group_id: '43', group_name: 'Group Beta', platform: 'anthropic' },
          ],
        }],
      },
    },
  })
  relayPlanning.listRelayAccountConfigurations.mockImplementation((params: any) => Promise.resolve({
    data: { data: structuredClone(params?.baseline_account_id ? diffPage : baselinePage) },
  }))
  relayPlanning.applyRelayAccountConfiguration.mockResolvedValue({
    data: {
      data: {
        baseline_account_id: 11,
        results: [
          { account_id: 12, applied: true, diff: { matches: true } },
          { account_id: 14, applied: false, error: 'synthetic read-back failure', diff: { matches: false, missing_keys: ['model_mapping'] } },
        ],
      },
    },
  })

  const wrapper = mount(AdminRelayAccountsView, {
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
      },
    },
  })
  await flushPromises()
  return { wrapper, relayPlanning }
}

async function choosePlatformAndBaseline(wrapper: ReturnType<typeof mount>) {
  await wrapper.get('[data-testid="platform-select"]').setValue('openai')
  await flushPromises()
  await wrapper.get('[data-testid="set-baseline-11"]').trigger('click')
  await flushPromises()
}

describe('AdminRelayAccountsView', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.resetAllMocks()
  })

  it('loads accounts for the chosen platform without a baseline first', async () => {
    const { wrapper, relayPlanning } = await mountView()

    expect(relayPlanning.listRelayAccountConfigurations).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="platform-select"]').setValue('openai')
    await flushPromises()

    expect(relayPlanning.listRelayAccountConfigurations).toHaveBeenCalledWith({ provider_id: 7, platform: 'openai' })
    expect(wrapper.find('[data-testid="deviation-match-11"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="deviation-diff-12"]').exists()).toBe(false)
  })

  it('reloads with the baseline and marks a matching account as not selectable', async () => {
    const { wrapper, relayPlanning } = await mountView()
    await choosePlatformAndBaseline(wrapper)

    expect(relayPlanning.listRelayAccountConfigurations).toHaveBeenLastCalledWith({
      provider_id: 7,
      platform: 'openai',
      baseline_account_id: 11,
    })
    expect(wrapper.get('[data-testid="deviation-match-11"]').text()).toContain('Matches baseline')
    expect((wrapper.get('[data-testid="select-target-11"] input').element as HTMLInputElement).disabled).toBe(true)
    expect(wrapper.get('[data-testid="deviation-match-13"]').text()).toContain('Matches baseline')
    expect((wrapper.get('[data-testid="select-target-13"] input').element as HTMLInputElement).disabled).toBe(true)
  })

  it('shows a deviating account with counts and viewable key names, and keeps it selectable', async () => {
    const { wrapper } = await mountView()
    await choosePlatformAndBaseline(wrapper)

    expect(wrapper.get('[data-testid="deviation-counts-12"]').text()).toBe('1 missing, 1 different, 1 extra')
    expect(wrapper.find('[data-testid="deviation-keys-12"]').exists()).toBe(false)

    await wrapper.get('[data-testid="toggle-keys-12"]').trigger('click')
    const keys = wrapper.get('[data-testid="deviation-keys-12"]')
    expect(keys.text()).toContain('model_mapping')
    expect(keys.text()).toContain('extra.privacy_mode')
    expect(keys.text()).toContain('extra.legacy_mode')
    expect(keys.text()).toContain('Missing keys')
    expect(keys.text()).toContain('Differing keys')
    expect(keys.text()).toContain('Additional keys')

    expect((wrapper.get('[data-testid="select-target-12"] input').element as HTMLInputElement).disabled).toBe(false)
  })

  it('hides accounts that match the baseline when only-deviating is toggled on', async () => {
    const { wrapper } = await mountView()
    await choosePlatformAndBaseline(wrapper)

    expect(wrapper.find('[data-testid="account-row-11"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="account-row-12"]').exists()).toBe(true)

    await wrapper.get('[data-testid="only-deviating-toggle"] input').setValue(true)
    await flushPromises()

    expect(wrapper.find('[data-testid="account-row-11"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="account-row-13"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="account-row-12"]').exists()).toBe(true)
  })

  it('filters the table by account type and narrows the deviation count with it', async () => {
    const { wrapper } = await mountView()
    await choosePlatformAndBaseline(wrapper)

    await wrapper.get('[data-testid="account-type-filter"]').setValue('apikey')
    await flushPromises()

    expect(wrapper.find('[data-testid="account-row-11"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="account-row-12"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="account-row-13"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="account-row-14"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="deviation-summary"]').text()).toContain('2 account(s) deviate from the baseline')
  })

  it('filters the table by group membership', async () => {
    const { wrapper } = await mountView()
    await choosePlatformAndBaseline(wrapper)

    await wrapper.get('[data-testid="group-filter"]').setValue('42')
    await flushPromises()

    expect(wrapper.find('[data-testid="account-row-11"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="account-row-12"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="account-row-13"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="account-row-14"]').exists()).toBe(false)
  })

  it('requires the confirmation dialog before calling the apply API', async () => {
    const { wrapper, relayPlanning } = await mountView()
    await choosePlatformAndBaseline(wrapper)

    await wrapper.get('[data-testid="select-target-12"] input').setValue(true)
    await flushPromises()
    await wrapper.get('[data-testid="open-apply-dialog"]').trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent(ElDialog)
    expect(dialog.props('modelValue')).toBe(true)
    expect(dialog.props('appendToBody')).toBe(true)
    expect(dialog.props('alignCenter')).toBe(true)
    expect(relayPlanning.applyRelayAccountConfiguration).not.toHaveBeenCalled()

    const dialogText = wrapper.get('[data-testid="apply-dialog"]').text()
    expect(dialogText).toContain('Account Alpha (#11)')
    expect(dialogText).toContain('1 target(s) selected')
    expect(dialogText).toContain('Credential material (tokens) is neither read nor written')
    expect(dialogText).toContain('synchronous and leaves no record')

    await wrapper.get('[data-testid="confirm-apply"]').trigger('click')
    await flushPromises()

    expect(relayPlanning.applyRelayAccountConfiguration).toHaveBeenCalledTimes(1)
    expect(relayPlanning.applyRelayAccountConfiguration).toHaveBeenCalledWith({
      provider_id: 7,
      platform: 'openai',
      baseline_account_id: 11,
      target_account_ids: [12],
    })
  })

  it('excludes a manually ignored key from the comparison and the copy', async () => {
    const { wrapper, relayPlanning } = await mountView()
    await choosePlatformAndBaseline(wrapper)

    const include = wrapper.get('[data-testid="include-key-model_mapping"] input')
    expect((include.element as HTMLInputElement).checked).toBe(true)
    await include.setValue(false)
    await flushPromises()

    expect(relayPlanning.listRelayAccountConfigurations).toHaveBeenLastCalledWith({
      provider_id: 7,
      platform: 'openai',
      baseline_account_id: 11,
      ignore_keys: 'model_mapping',
    })

    await wrapper.get('[data-testid="select-target-12"] input').setValue(true)
    await flushPromises()
    await wrapper.get('[data-testid="open-apply-dialog"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="confirm-ignored-keys"]').text()).toContain('1 setting(s) excluded')

    await wrapper.get('[data-testid="confirm-apply"]').trigger('click')
    await flushPromises()
    expect(relayPlanning.applyRelayAccountConfiguration).toHaveBeenCalledWith({
      provider_id: 7,
      platform: 'openai',
      baseline_account_id: 11,
      target_account_ids: [12],
      ignore_keys: ['model_mapping'],
    })
  })

  it('reports per-target apply results and refreshes the list', async () => {
    const { wrapper, relayPlanning } = await mountView()
    await choosePlatformAndBaseline(wrapper)
    await wrapper.get('[data-testid="select-target-12"] input').setValue(true)
    await wrapper.get('[data-testid="select-target-14"] input').setValue(true)
    await flushPromises()
    await wrapper.get('[data-testid="open-apply-dialog"]').trigger('click')
    await flushPromises()
    const callsBeforeApply = relayPlanning.listRelayAccountConfigurations.mock.calls.length

    await wrapper.get('[data-testid="confirm-apply"]').trigger('click')
    await flushPromises()

    expect(relayPlanning.applyRelayAccountConfiguration).toHaveBeenCalledWith(expect.objectContaining({ target_account_ids: [12, 14] }))
    expect(wrapper.get('[data-testid="apply-result-12"]').text()).toContain('Applied')
    const failed = wrapper.get('[data-testid="apply-result-14"]').text()
    expect(failed).toContain('Failed')
    expect(failed).toContain('synthetic read-back failure')
    expect(relayPlanning.listRelayAccountConfigurations.mock.calls.length).toBeGreaterThan(callsBeforeApply)
    expect(relayPlanning.listRelayAccountConfigurations).toHaveBeenLastCalledWith({
      provider_id: 7,
      platform: 'openai',
      baseline_account_id: 11,
    })
  })
})
