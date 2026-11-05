import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import TeamAccountDepartmentPicker from '@/components/team-accounts/TeamAccountDepartmentPicker.vue'
import TeamAccountUserPicker from '@/components/team-accounts/TeamAccountUserPicker.vue'
import { setLocale } from '@/i18n'

vi.mock('@/api/teamAccounts', () => ({
  listTeamAccountUserOptions: vi.fn(),
  listAdminTeamAccountUserOptions: vi.fn(),
}))

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

  it('shows matches only while typing and selects the first match on Enter', async () => {
    const { wrapper } = await mountDepartmentPicker()
    const input = wrapper.get('[data-testid="team-account-department-input"]')
    expect(document.body.querySelector('[data-testid="team-account-department-list"]')).toBeNull()
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

  it('searches users while typing and selects the first match on Enter', async () => {
    const { wrapper, api } = await mountUserPicker()
    const input = wrapper.get('[data-testid="team-account-user-input"]')
    await input.trigger('focus')
    await input.setValue('ali')
    await new Promise((resolve) => setTimeout(resolve, 250))
    await flushPromises()

    expect(api.listAdminUsers).toHaveBeenLastCalledWith({
      q: 'ali',
      department_id: undefined,
      page: 1,
      page_size: 50,
    })

    const list = document.body.querySelector('[data-testid="team-account-user-list"]')!
    expect(list.textContent).toContain('alice')

    await input.trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('select')?.[0]?.[0]).toMatchObject({ id: 7, username: 'alice' })
  })

  it('clears the department when the typed text is deleted', async () => {
    const api = await import('@/api/adminUsers')
    vi.mocked(api.listAdminUserDepartments).mockResolvedValue(response({
      items: [department('root', '声网'), department('sdk', 'SDK框架与效能组', 'root')],
    }) as any)
    const wrapper = mount(TeamAccountDepartmentPicker, {
      props: { modelValue: 'sdk' },
      attachTo: document.body,
    })
    await flushPromises()
    const input = wrapper.get('[data-testid="team-account-department-input"]')
    await input.trigger('focus')
    await input.setValue('')
    await flushPromises()

    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([''])
    expect(wrapper.emitted('change')?.[0]).toEqual([''])
  })

  it('clears the chosen user when the typed text is deleted', async () => {
    const api = await import('@/api/adminUsers')
    vi.mocked(api.listAdminUsers).mockResolvedValue(response({
      items: [user(7, 'alice', 'alice@example.com')],
      page: 1,
      page_size: 50,
      total: 1,
    }) as any)
    const wrapper = mount(TeamAccountUserPicker, {
      props: { modelValue: 7 },
      attachTo: document.body,
    })
    await flushPromises()
    const input = wrapper.get('[data-testid="team-account-user-input"]')
    await input.trigger('focus')
    await input.setValue('')
    await flushPromises()

    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([null])
    expect(wrapper.emitted('select')?.[0]).toEqual([null])
  })

  it('drops the displayed user when the parent clears the value', async () => {
    const api = await import('@/api/adminUsers')
    vi.mocked(api.listAdminUsers).mockResolvedValue(response({
      items: [user(7, 'alice', 'alice@example.com')],
      page: 1,
      page_size: 50,
      total: 1,
    }) as any)
    const wrapper = mount(TeamAccountUserPicker, {
      props: { modelValue: 7 },
      attachTo: document.body,
    })
    await flushPromises()
    expect((wrapper.get('[data-testid="team-account-user-input"]').element as HTMLInputElement).value).toBe('alice')

    await wrapper.setProps({ modelValue: null })
    await flushPromises()

    expect((wrapper.get('[data-testid="team-account-user-input"]').element as HTMLInputElement).value).toBe('')
  })

  it('searches people through the account endpoint for account members', async () => {
    const api = await import('@/api/teamAccounts')
    vi.mocked(api.listTeamAccountUserOptions).mockResolvedValue(response({
      items: [{ id: 9, username: 'member', email: 'member@example.com' }],
    }) as any)
    const wrapper = mount(TeamAccountUserPicker, {
      props: { modelValue: null, mode: 'account', accountId: 3 },
      attachTo: document.body,
    })
    await flushPromises()
    const input = wrapper.get('[data-testid="team-account-user-input"]')
    await input.trigger('focus')
    await input.setValue('mem')
    await new Promise((resolve) => setTimeout(resolve, 250))
    await flushPromises()

    expect(api.listTeamAccountUserOptions).toHaveBeenCalledWith(3, 'mem')
  })

  it('Escape restores the committed user instead of clearing it', async () => {
    const api = await import('@/api/adminUsers')
    vi.mocked(api.listAdminUsers).mockResolvedValue(response({
      items: [user(7, 'alice', 'alice@example.com')],
      page: 1,
      page_size: 50,
      total: 1,
    }) as any)
    const wrapper = mount(TeamAccountUserPicker, {
      props: { modelValue: 7 },
      attachTo: document.body,
    })
    await flushPromises()
    const input = wrapper.get('[data-testid="team-account-user-input"]')
    await input.trigger('focus')
    await input.setValue('ali')
    await flushPromises()
    await input.trigger('keydown', { key: 'Escape' })
    await flushPromises()
    await input.trigger('blur')
    await flushPromises()

    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect((input.element as HTMLInputElement).value).toBe('alice')
  })

  it('ignores a stale search response that arrives after a newer one', async () => {
    const api = await import('@/api/adminUsers')
    const resolvers: Record<string, (value: unknown) => void> = {}
    vi.mocked(api.listAdminUsers).mockImplementation((params: any) => new Promise((resolve) => {
      resolvers[params?.q ?? ''] = resolve
    }) as any)
    const wrapper = mount(TeamAccountUserPicker, {
      props: { modelValue: null },
      attachTo: document.body,
    })
    const input = wrapper.get('[data-testid="team-account-user-input"]')
    await input.trigger('focus')
    await input.setValue('a')
    await new Promise((resolve) => setTimeout(resolve, 230))
    await input.setValue('ab')
    await new Promise((resolve) => setTimeout(resolve, 230))

    resolvers['ab']?.({ data: { code: 0, data: { items: [user(9, 'ab-person', 'ab@example.com')], page: 1, page_size: 50, total: 1 } } })
    await flushPromises()
    resolvers['a']?.({ data: { code: 0, data: { items: [user(7, 'stale-person', 'stale@example.com')], page: 1, page_size: 50, total: 1 } } })
    await flushPromises()

    expect(document.body.textContent).toContain('ab-person')
    expect(document.body.textContent).not.toContain('stale-person')
  })

  it('searches the committed text after an IME composition ends', async () => {
    const api = await import('@/api/adminUsers')
    vi.mocked(api.listAdminUsers).mockResolvedValue(response({
      items: [user(7, '张三', 'zhang@example.com')],
      page: 1,
      page_size: 50,
      total: 1,
    }) as any)
    const wrapper = mount(TeamAccountUserPicker, {
      props: { modelValue: null },
      attachTo: document.body,
    })
    const input = wrapper.get('[data-testid="team-account-user-input"]')
    await input.trigger('focus')
    await input.trigger('compositionstart')
    ;(input.element as HTMLInputElement).value = '张三'
    await input.trigger('compositionend')
    await new Promise((resolve) => setTimeout(resolve, 200))
    await flushPromises()

    expect(api.listAdminUsers).toHaveBeenCalledWith(expect.objectContaining({ q: '张三' }))
  })
})
