import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it } from 'vitest'
import UserUsageSubjectSelector from '@/components/user/usage/UserUsageSubjectSelector.vue'
import { setLocale } from '@/i18n'

describe('UserUsageSubjectSelector', () => {
  beforeEach(() => {
    setLocale('en-US')
  })

  it('renders My Usage and member subjects', async () => {
    const wrapper = mount(UserUsageSubjectSelector, {
      attachTo: document.body,
      props: {
        modelValue: 'self:100',
        subjects: [
          { subject_type: 'self', user_id: 100, display_name: 'Me', email: 'alice@example.com', selectable: true },
          {
            subject_type: 'member',
            user_id: 101,
            display_name: 'Alice',
            email: 'alice@example.com',
            department_display_path: 'Department Alpha',
            selectable: true,
          },
        ],
      },
    })

    await wrapper.get('[data-testid="usage-subject-selector"]').trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('My Usage')
    expect(document.body.textContent).toContain('Alice')
    expect(wrapper.get('[data-testid="usage-subject-selector"]').classes()).toContain('el-select')
    wrapper.unmount()
  })

  it('localizes the self option in Chinese', async () => {
    setLocale('zh-CN')
    const wrapper = mount(UserUsageSubjectSelector, {
      attachTo: document.body,
      props: {
        modelValue: 'self:100',
        subjects: [
          { subject_type: 'self', user_id: 100, display_name: 'Me', email: 'alice@example.com', selectable: true },
        ],
      },
    })

    await wrapper.get('[data-testid="usage-subject-selector"]').trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('我的用量')
    expect(document.body.textContent).not.toContain('My Usage')
    wrapper.unmount()
  })

  it('keeps unavailable directory members visible but disabled', async () => {
    const wrapper = mount(UserUsageSubjectSelector, {
      attachTo: document.body,
      props: {
        modelValue: 'self:100',
        subjects: [
          { subject_type: 'self', user_id: 100, display_name: 'Me', email: 'alice@example.com', selectable: true },
          {
            subject_type: 'member',
            user_id: 0,
            directory_member_external_id: 'member-bob',
            display_name: 'Bob',
            email: 'bob@example.org',
            department_display_path: 'Department Alpha',
            selectable: false,
          },
          {
            subject_type: 'member',
            user_id: 0,
            directory_member_external_id: 'member-carol',
            display_name: 'Carol',
            email: 'carol@example.net',
            department_display_path: 'Department Alpha',
            selectable: false,
          },
        ],
      },
    })

    await wrapper.get('[data-testid="usage-subject-selector"]').trigger('click')
    await flushPromises()
    const options = Array.from(document.body.querySelectorAll('[role="option"]'))
    expect(options.map((option) => option.textContent?.trim())).toEqual(['My Usage', 'Bob', 'Carol'])
    expect(options[1].getAttribute('aria-disabled')).toBe('true')
    expect(options[2].getAttribute('aria-disabled')).toBe('true')
    wrapper.unmount()
  })
})
