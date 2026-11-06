import { beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import TeamAccountGroupShuttle from '@/components/team-accounts/TeamAccountGroupShuttle.vue'
import { setLocale } from '@/i18n'

const groups = [
  { id: 1, name: 'China', platform: 'composite' },
  { id: 2, name: 'Claude', platform: 'anthropic' },
  { id: 3, name: 'Agent', platform: 'composite' },
]

function mountShuttle(modelValue: number[]) {
  return mount(TeamAccountGroupShuttle, {
    props: {
      modelValue,
      groups,
      defaultGroupIds: [1],
      grantableGroupIds: [1, 2, 3],
    },
  })
}

describe('TeamAccountGroupShuttle', () => {
  beforeEach(() => {
    void setLocale('en-US')
  })

  it('lists grantable groups that are not selected on the left', () => {
    const wrapper = mountShuttle([1])
    expect(wrapper.find('[data-testid="group-selected-1"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="group-available-2"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="group-available-1"]').exists()).toBe(false)
  })

  it('moves an available group to the selected side when clicked', async () => {
    const wrapper = mountShuttle([1])
    await wrapper.get('[data-testid="group-available-2"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([[1, 2]])
  })

  it('moves an extra group back but keeps defaults locked', async () => {
    const wrapper = mountShuttle([1, 2])
    await wrapper.get('[data-testid="group-selected-2"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([[1]])

    await wrapper.get('[data-testid="group-selected-1"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toHaveLength(1)
  })
})
