import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import UserUsageSubjectSelector from '@/components/user/usage/UserUsageSubjectSelector.vue'

describe('UserUsageSubjectSelector', () => {
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
})
