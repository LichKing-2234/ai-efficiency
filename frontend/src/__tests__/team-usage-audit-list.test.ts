import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import TeamUsageAuditList from '@/components/user/usage/TeamUsageAuditList.vue'

describe('TeamUsageAuditList', () => {
  it('renders representative audit rows without redacted target details', () => {
    const wrapper = mount(TeamUsageAuditList, {
      props: {
        items: [
          {
            id: 1,
            actor_user_id: 100,
            group_id: '42',
            group_name: 'Group Alpha',
            action: 'set_rate_multiplier',
            status: 'rejected',
            changed: false,
            rejection_reason: 'out_of_scope',
            reason: 'Representative requested an exception',
            created_at: '2026-06-26T00:00:00Z',
            updated_at: '2026-06-26T00:00:00Z',
          },
        ],
      },
    })

    expect(wrapper.text()).toContain('Group Alpha')
    expect(wrapper.text()).toContain('out_of_scope')
    expect(wrapper.text()).toContain('Representative requested an exception')
    expect(wrapper.text()).not.toContain('alice@example.com')
  })
})
