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

function department(id: string, name: string, parent: string | null = null) {
  return {
    external_id: id,
    parent_external_id: parent,
    name,
    display_path: parent ? `${parent} / ${name}` : name,
    depth: parent ? 1 : 0,
    child_count: 0,
    member_count: 1,
    matched_user_count: 1,
    subtree_member_count: 1,
    subtree_matched_user_count: 1,
    path: name,
  }
}

function user(id: number, username: string, email: string) {
  return {
    id,
    username,
    email,
    role: 'user',
    auth_source: 'ldap',
    relay_auth_password: '',
    department: { external_id: 'dept-sdk', name: 'SDK', display_path: '声网 / 研发团队 / SDK' },
  }
}

async function mountDepartmentPicker() {
  const api = await import('@/api/adminUsers')
  vi.mocked(api.listAdminUserDepartments).mockResolvedValue(response({
    items: [department('root', '声网'), department('sdk', 'SDK框架与效能组', 'root')],
  }) as any)
  const wrapper = mount(TeamAccountDepartmentPicker, {
    props: { modelValue: '' },
    attachTo: document.body,
  })
  await flushPromises()
  return { wrapper, api }
}

async function mountUserPicker() {
  const api = await import('@/api/adminUsers')
  vi.mocked(api.listAdminUsers).mockResolvedValue(response({
    items: [user(7, 'alice', 'alice@example.com'), user(8, 'bob', 'bob@example.com')],
    page: 1,
    page_size: 100,
    total: 2,
  }) as any)
  const wrapper = mount(TeamAccountUserPicker, {
    props: { modelValue: null },
    attachTo: document.body,
  })
  await flushPromises()
  return { wrapper, api }
}

describe('team account pickers', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    void setLocale('en-US')
  })

  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('loads the whole department directory once', async () => {
    const { api } = await mountDepartmentPicker()
    expect(api.listAdminUserDepartments).toHaveBeenCalledTimes(1)
  })

  it('switches to a flat highlighted list while searching and selects the first match on Enter', async () => {
    const { wrapper } = await mountDepartmentPicker()
    const input = wrapper.get('[data-testid="team-account-department-input"]')
    await input.trigger('focus')
    await input.setValue('SDK')
    await flushPromises()

    const list = document.body.querySelector('[data-testid="team-account-department-list"]')!
    expect(list.textContent).toContain('SDK框架与效能组')
    expect(list.textContent).not.toContain('Agora US')
    expect(list.querySelector('mark')?.textContent).toBe('SDK')

    await input.trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['sdk'])
    expect(wrapper.emitted('change')?.[0]).toEqual(['sdk'])
  })

  it('filters users by department and selects the first match on Enter', async () => {
    const { wrapper, api } = await mountUserPicker()
    const input = wrapper.get('[data-testid="team-account-user-input"]')
    await input.trigger('focus')
    await input.setValue('ali')
    await new Promise((resolve) => setTimeout(resolve, 300))
    await flushPromises()

    expect(api.listAdminUsers).toHaveBeenLastCalledWith({
      q: 'ali',
      department_id: undefined,
      page: 1,
      page_size: 100,
    })

    const list = document.body.querySelector('[data-testid="team-account-user-list"]')!
    expect(list.textContent).toContain('alice')

    await input.trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('select')?.[0]?.[0]).toMatchObject({ id: 7, username: 'alice' })
  })
})
