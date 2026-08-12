import { afterEach, describe, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { ElDialog } from 'element-plus'
import QuotaResetRequestModal from '@/components/quota-reset/QuotaResetRequestModal.vue'
import { setLocale } from '@/i18n'
import { cleanupTeleportedContent, withTeleportedContent } from './helpers/teleport'

const groups = [
  {
    group_id: '42',
    group_name: 'Group Alpha',
    platform: 'openai',
    daily_usage_usd: 10,
    weekly_usage_usd: 20,
    monthly_usage_usd: 30,
  },
  {
    group_id: '43',
    group_name: 'Group Beta',
    platform: 'anthropic',
    daily_usage_usd: 40,
    weekly_usage_usd: 50,
    monthly_usage_usd: 60,
  },
]

async function mountModal(locale: 'en-US' | 'zh-CN' = 'en-US') {
  setLocale(locale)
  const wrapper = withTeleportedContent(mount(QuotaResetRequestModal, {
    props: { open: true, groups, submitting: false },
  }))
  await flushPromises()
  return wrapper
}

function selectInput(wrapper: Awaited<ReturnType<typeof mountModal>>) {
  return wrapper.get('[data-testid="quota-reset-group-select"] input[role="combobox"]')
}

async function selectGroup(wrapper: Awaited<ReturnType<typeof mountModal>>, groupID: string) {
  await wrapper.get('[data-testid="quota-reset-group-select"] .el-select__wrapper').trigger('click')
  await flushPromises()
  await wrapper.get(`[data-testid="quota-reset-group-option-${groupID}"]`).trigger('click')
  await flushPromises()
}

afterEach(() => {
  cleanupTeleportedContent()
})

describe('QuotaResetRequestModal', () => {
  it('starts empty and highlights access-group selection as the required first action', async () => {
    const wrapper = await mountModal()
    const dialog = wrapper.findComponent(ElDialog)
    expect(dialog.props('appendToBody')).toBe(true)
    expect(dialog.props('alignCenter')).toBe(true)

    const field = wrapper.get('[data-testid="quota-reset-group-field"]')
    expect(field.classes()).toContain('border-cyan-300')
    expect(selectInput(wrapper).attributes('aria-label')).toBe('Access group')
    expect(wrapper.get('[data-testid="quota-reset-group-select"]').text()).toContain('Select an access group')
    expect(wrapper.find('[data-testid="quota-reset-current-usage"]').exists()).toBe(false)

    await wrapper.get('button[data-testid="quota-reset-submit"]').trigger('click')
    expect(wrapper.text()).toContain('Access group is required')
    expect(wrapper.emitted('submit')).toBeUndefined()
  })

  it('closes the option popover, shows selected usage, and submits the explicit group', async () => {
    const wrapper = await mountModal()
    await selectGroup(wrapper, '43')

    expect(selectInput(wrapper).attributes('aria-expanded')).toBe('false')
    expect(wrapper.get('[data-testid="quota-reset-current-usage"]').text()).toContain('40.00 / 50.00 / 60.00')
    await wrapper.get('textarea').setValue('Need reset for a build investigation')
    await wrapper.get('button[data-testid="quota-reset-submit"]').trigger('click')
    expect(wrapper.emitted('submit')?.[0]).toEqual([{ group_id: '43', reason: 'Need reset for a build investigation' }])
  })

  it('clears group, reason, validation, and usage whenever the modal reopens', async () => {
    const wrapper = await mountModal()
    await selectGroup(wrapper, '42')
    await wrapper.get('textarea').setValue('Temporary reason')
    await wrapper.get('textarea').setValue('')
    await wrapper.get('button[data-testid="quota-reset-submit"]').trigger('click')
    expect(wrapper.text()).toContain('Reason is required')

    await wrapper.setProps({ open: false })
    await wrapper.setProps({ open: true })
    await flushPromises()

    expect(selectInput(wrapper).element).toHaveProperty('value', '')
    expect(wrapper.get('textarea').element).toHaveProperty('value', '')
    expect(wrapper.text()).not.toContain('Reason is required')
    expect(wrapper.find('[data-testid="quota-reset-current-usage"]').exists()).toBe(false)
  })

  it('uses access-group terminology consistently in Chinese', async () => {
    const wrapper = await mountModal('zh-CN')
    expect(wrapper.text()).toContain('接入组')
    expect(wrapper.text()).not.toContain('订阅组')
  })
})
