import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it } from 'vitest'
import UserUsageSubjectSelector from '@/components/user/usage/UserUsageSubjectSelector.vue'
import { setLocale } from '@/i18n'

describe('UserUsageSubjectSelector', () => {
  beforeEach(() => {
    setLocale('en-US')
  })

  it('renders My Usage and member subjects', () => {
    const wrapper = mount(UserUsageSubjectSelector, {
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

    expect(wrapper.text()).toContain('My Usage')
    expect(wrapper.text()).toContain('Alice')
  })

  it('localizes the self option in Chinese', () => {
    setLocale('zh-CN')
    const wrapper = mount(UserUsageSubjectSelector, {
      props: {
        modelValue: 'self:100',
        subjects: [
          { subject_type: 'self', user_id: 100, display_name: 'Me', email: 'alice@example.com', selectable: true },
        ],
      },
    })

    expect(wrapper.text()).toContain('我的用量')
    expect(wrapper.text()).not.toContain('My Usage')
  })

  it('uses directory member ids for unavailable member option values', () => {
    const wrapper = mount(UserUsageSubjectSelector, {
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

    const options = wrapper.findAll('option')
    expect(options.map((option) => option.attributes('value'))).toEqual([
      'self:100',
      'member:directory:member-bob',
      'member:directory:member-carol',
    ])
    expect(options[1].attributes('disabled')).toBeDefined()
    expect(options[2].attributes('disabled')).toBeDefined()
  })
})
