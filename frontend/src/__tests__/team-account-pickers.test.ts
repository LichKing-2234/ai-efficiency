import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import TeamAccountDepartmentPicker from '@/components/team-accounts/TeamAccountDepartmentPicker.vue'
import TeamAccountUserPicker from '@/components/team-accounts/TeamAccountUserPicker.vue'
import { setLocale } from '@/i18n'

vi.mock('@/api/adminUsers', () => ({
  listAdminUserDepartments: vi.fn(),
  listAdminUsers: vi.fn(),
}))

function response<T>(data: T) {
  return { data: { code: 0, data } }
}

describe('team account pickers', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    void setLocale('en-US')
  })

  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('loads the complete department tree without pagination', async () => {
    const api = await import('@/api/adminUsers')
    vi.mocked(api.listAdminUserDepartments).mockResolvedValue(response({
      items: [
        { external_id: 'root', parent_external_id: null, name: 'Agora', display_path: 'Agora', depth: 0, child_count: 1, member_count: 1, matched_user_count: 1, subtree_member_count: 2, subtree_matched_user_count: 2, path: 'Agora' },
        { external_id: 'child', parent_external_id: 'root', name: 'Data Platform', display_path: 'Agora / Data Platform', depth: 1, child_count: 0, member_count: 1, matched_user_count: 1, subtree_member_count: 1, subtree_matched_user_count: 1, path: 'Agora/Data' },
      ],
    }) as any)

    const wrapper = mount(TeamAccountDepartmentPicker, { props: { modelValue: '' } })
    await flushPromises()

    expect(api.listAdminUserDepartments).toHaveBeenCalledTimes(1)
    expect(wrapper.find('.el-select').exists()).toBe(true)
  })

  it('filters users by the selected department', async () => {
    const api = await import('@/api/adminUsers')
    vi.mocked(api.listAdminUsers).mockResolvedValue(response({
      items: [],
      page: 1,
      page_size: 50,
      total: 0,
    }) as any)

    mount(TeamAccountUserPicker, { props: { modelValue: null, departmentId: 'dept-data' } })
    await flushPromises()

    expect(api.listAdminUsers).toHaveBeenCalledWith({
      q: undefined,
      department_id: 'dept-data',
      page: 1,
      page_size: 50,
    })
  })

  it('searches users through the admin directory instead of capping the list', async () => {
    const api = await import('@/api/adminUsers')
    vi.mocked(api.listAdminUsers).mockResolvedValue(response({
      items: [{ id: 7, username: 'alice', email: 'alice@example.com', role: 'user', auth_source: 'ldap', relay_auth_password: '' }],
      page: 1,
      page_size: 50,
      total: 1,
    }) as any)

    mount(TeamAccountUserPicker, { props: { modelValue: null } })
    await flushPromises()

    expect(api.listAdminUsers).toHaveBeenCalledWith({ q: undefined, department_id: undefined, page: 1, page_size: 50 })
  })
})
