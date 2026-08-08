import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import SelectedSubjectSubscriptionRows from '@/components/user/usage/SelectedSubjectSubscriptionRows.vue'
import { setLocale } from '@/i18n'

const editableRow = {
  group_id: '42',
  group_name: 'Group Alpha',
  platform: 'openai',
  subscription_status: 'active',
  inherited_default_multiplier: 1,
  system_default_multiplier: 1,
  user_multiplier: null,
  effective_multiplier: 1,
  multiplier_source: 'group' as const,
  daily_display_used_usd: 0,
  weekly_display_used_usd: 0,
  monthly_display_used_usd: 80,
  daily_usage_usd: 0,
  weekly_usage_usd: 0,
  monthly_usage_usd: 80,
  monthly_effective_allowance_usd: 500,
  usage_value_basis: 'raw_actual_cost',
  quota_window_basis: 'sub2api_enforcement_window',
  editable: true,
}

const zeroMultiplierRow = {
  ...editableRow,
  group_id: '43',
  group_name: 'Group Zero',
  inherited_default_multiplier: 0,
  system_default_multiplier: 0,
  user_multiplier: 0,
  effective_multiplier: 0,
  monthly_effective_allowance_usd: null,
  monthly_effective_allowance_unlimited: true,
}

const unavailableMultiplierRow = {
  ...editableRow,
  group_id: '44',
  group_name: 'Group Beta',
  inherited_default_multiplier: 3.5,
  user_multiplier: null,
  effective_multiplier: null,
  multiplier_source: 'unknown' as const,
  multiplier_metadata_status: 'unavailable' as const,
  multiplier_metadata_message: 'upstream-internal-multiplier-error',
  editable: true,
}

async function mountRows(options: Parameters<typeof mount>[1]) {
  const wrapper = mount(SelectedSubjectSubscriptionRows, options)
  await flushPromises()
  return wrapper
}

describe('SelectedSubjectSubscriptionRows', () => {
  beforeEach(() => {
    setLocale('en-US')
  })

  it('keeps Used / Quota in enforcement units when draft multiplier changes', async () => {
    const wrapper = await mountRows({
      props: {
        subjectUserId: 101,
        rows: [editableRow],
      },
    })

    expect(wrapper.get('[data-testid="edit-multiplier-42"]').classes()).toContain('el-button')
    await wrapper.get('[data-testid="edit-multiplier-42"]').trigger('click')
    await wrapper.get('[data-testid="multiplier-input"]').setValue('2')

    expect(wrapper.text()).toContain('$80.00 / $500.00')
    expect(wrapper.text()).toContain('Future requests will consume this quota at 2x speed.')
    expect(wrapper.text()).toContain("It does not change this member's quota limit or recalculate existing Used / Quota values.")
  })

  it('keeps the multiplier modal open and shows an error when update fails', async () => {
    const updateMultiplier = vi.fn().mockRejectedValue(new Error('network failed'))
    const wrapper = await mountRows({
      props: {
        subjectUserId: 101,
        rows: [editableRow],
        updateMultiplier,
      },
    })

    await wrapper.get('[data-testid="edit-multiplier-42"]').trigger('click')
    await wrapper.get('[data-testid="multiplier-input"]').setValue('2')
    const confirmButton = wrapper.findAll('button').find((button) => button.text() === 'Confirm')
    expect(confirmButton).toBeTruthy()
    await confirmButton!.trigger('click')
    await flushPromises()

    expect(updateMultiplier).toHaveBeenCalled()
    expect(wrapper.find('[data-testid="multiplier-input"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Unable to update rate multiplier')
  })

  it('allows setting a multiplier below the inherited default', async () => {
    const updateMultiplier = vi.fn().mockResolvedValue(undefined)
    const wrapper = await mountRows({
      props: {
        subjectUserId: 101,
        rows: [editableRow],
        updateMultiplier,
      },
    })

    await wrapper.get('[data-testid="edit-multiplier-42"]').trigger('click')
    await wrapper.get('[data-testid="multiplier-input"]').setValue('0.5')
    const confirmButton = wrapper.findAll('button').find((button) => button.text() === 'Confirm')
    expect(confirmButton).toBeTruthy()
    expect(confirmButton!.attributes('disabled')).toBeUndefined()
    await confirmButton!.trigger('click')
    await flushPromises()

    expect(updateMultiplier).toHaveBeenCalledWith({
      subjectUserId: 101,
      groupID: '42',
      payload: { mode: 'set', rate_multiplier: 0.5 },
    })
  })

  it('rejects multiplier values with more than two decimal places', async () => {
    const updateMultiplier = vi.fn()
    const wrapper = await mountRows({
      props: {
        subjectUserId: 101,
        rows: [editableRow],
        updateMultiplier,
      },
    })

    await wrapper.get('[data-testid="edit-multiplier-42"]').trigger('click')
    await wrapper.get('[data-testid="multiplier-input"]').setValue('0.123')

    expect(wrapper.text()).toContain('Too many decimals')
    expect(wrapper.findAll('button').find((button) => button.text() === 'Confirm')?.attributes('disabled')).toBeDefined()
    expect(updateMultiplier).not.toHaveBeenCalled()
  })

  it('renders infinity for unlimited quota while keeping historical used amount', async () => {
    const wrapper = await mountRows({
      props: {
        subjectUserId: 101,
        rows: [zeroMultiplierRow],
      },
    })

    expect(wrapper.text()).toContain('$80.00 / ∞')
  })

  it('renders a compact warning and preserves usage when multiplier metadata is unavailable', async () => {
    const wrapper = await mountRows({
      props: {
        subjectUserId: 101,
        rows: [unavailableMultiplierRow],
      },
    })

    const warning = wrapper.get('[data-testid="multiplier-metadata-warning-44"]')
    expect(warning.text()).toBe('Multiplier unavailable')
    expect(warning.attributes('role')).toBe('status')
    const multiplierCell = warning.element.closest('td')
    expect(multiplierCell).not.toBeNull()
    expect(multiplierCell?.textContent?.trim()).toBe('Multiplier unavailable')
    expect(wrapper.text()).not.toContain('3.5x')
    expect(wrapper.text()).not.toContain('nullx')
    expect(wrapper.text()).not.toContain('upstream-internal-multiplier-error')
    expect(wrapper.text()).toContain('$80.00 / $500.00')

    const editButton = wrapper.get('[data-testid="edit-multiplier-44"]')
    expect(editButton.attributes('disabled')).toBeDefined()
    await editButton.trigger('click')
    expect(wrapper.find('[data-testid="multiplier-input"]').exists()).toBe(false)
  })

  it('treats a missing multiplier metadata status as available for rolling upgrades', async () => {
    const wrapper = await mountRows({
      props: {
        subjectUserId: 101,
        rows: [editableRow],
      },
    })

    expect(wrapper.text()).toContain('1x')
    expect(wrapper.find('[data-testid="multiplier-metadata-warning-42"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="edit-multiplier-42"]').attributes('disabled')).toBeUndefined()
  })

  it('localizes unavailable multiplier metadata in Chinese', async () => {
    setLocale('zh-CN')
    const wrapper = await mountRows({
      props: {
        subjectUserId: 101,
        rows: [unavailableMultiplierRow],
      },
    })

    const warning = wrapper.get('[data-testid="multiplier-metadata-warning-44"]')
    expect(warning.text()).toBe('倍率暂不可用')
    const multiplierCell = warning.element.closest('td')
    expect(multiplierCell).not.toBeNull()
    expect(multiplierCell?.textContent?.trim()).toBe('倍率暂不可用')
    expect(wrapper.text()).not.toContain('Multiplier unavailable')
    expect(wrapper.text()).toContain('$80.00 / $500.00')
  })

  it('localizes subscription rows and multiplier modal in Chinese', async () => {
    setLocale('zh-CN')
    const wrapper = await mountRows({
      props: {
        subjectUserId: 101,
        rows: [editableRow],
      },
    })

    expect(wrapper.text()).toContain('订阅组')
    const headers = wrapper.findAll('th').map((header) => header.text())
    expect(headers).toEqual(['组', '状态', '倍率', '已使用 / 配额', '操作'])
    expect(wrapper.findAll('button').map((button) => button.text())).toContain('编辑')
    expect(wrapper.text()).not.toContain('Subscription groups')
    expect(headers).not.toContain('Group')
    expect(headers).not.toContain('Status')
    expect(headers).not.toContain('Multiplier')
    expect(headers).not.toContain('Action')
    expect(wrapper.text()).not.toContain('Edit')

    await wrapper.get('[data-testid="edit-multiplier-42"]').trigger('click')

    expect(wrapper.text()).toContain('倍率')
    expect(wrapper.text()).toContain('后续请求会按这个倍率消耗配额')
    expect(wrapper.text()).toContain('不会修改该组员的配额上限')
    expect(wrapper.text()).toContain('设置')
    expect(wrapper.text()).toContain('重置')
    expect(wrapper.text()).toContain('原因')
    expect(wrapper.text()).toContain('取消')
    expect(wrapper.text()).toContain('确认')
    expect(wrapper.text()).not.toContain('Rate multiplier')
    expect(wrapper.text()).not.toContain('Close')
    expect(wrapper.text()).not.toContain('Set')
    expect(wrapper.text()).not.toContain('Reset')
    expect(wrapper.text()).not.toContain('Reason')
    expect(wrapper.text()).not.toContain('Cancel')
    expect(wrapper.text()).not.toContain('Confirm')
  })
})
