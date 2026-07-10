import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import WorkItemsView from '@/views/WorkItemsView.vue'
import { useAuthStore } from '@/stores/auth'
import { setLocale } from '@/i18n'

vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  getMe: vi.fn(),
  devLogin: vi.fn(),
}))

vi.mock('@/api/workItems', () => ({
  getWorkItemCounts: vi.fn(),
}))

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/work-items', component: WorkItemsView },
      { path: '/usage/quota-reset', component: { template: '<div>Quota Reset</div>' } },
      { path: '/user', component: { template: '<div>User Setup</div>' } },
      { path: '/admin/directory/offboarding', component: { template: '<div>Offboarding</div>' } },
    ],
  })
}

async function mountWorkItemsView(role: 'user' | 'admin' = 'user') {
  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore(pinia)
  auth.user = { id: role === 'admin' ? 1 : 2, username: role, email: `${role}@example.com`, role, auth_source: 'ldap' }
  const router = createTestRouter()
  await router.push('/work-items')
  await router.isReady()
  const wrapper = mount(WorkItemsView, {
    global: { plugins: [pinia, router] },
  })
  await flushPromises()
  return wrapper
}

beforeEach(async () => {
  setLocale('en-US')
  vi.clearAllMocks()
  const api = await import('@/api/workItems') as any
  api.getWorkItemCounts.mockResolvedValue({
    data: {
      data: {
        quota_reset_approval_count: 2,
        quota_reset_admin_count: 5,
        ai_access_setup_count: 0,
        offboarding_count: 3,
        total_count: 8,
      },
    },
  })
})

describe('WorkItemsView', () => {
  it('shows regular approvers their pending quota approvals', async () => {
    const wrapper = await mountWorkItemsView('user')

    expect(wrapper.text()).toContain('Work Items')
    expect(wrapper.text()).toContain('Quota reset approvals')
    expect(wrapper.text()).toContain('2')
    expect(wrapper.find('a[href="/usage/quota-reset"]').exists()).toBe(true)
    expect(wrapper.find('a[href="/admin/directory/offboarding"]').exists()).toBe(false)
  })

  it('shows admins quota approval and offboarding work counts', async () => {
    const wrapper = await mountWorkItemsView('admin')

    expect(wrapper.text()).toContain('Quota reset approvals')
    expect(wrapper.text()).toContain('5')
    expect(wrapper.text()).toContain('Offboarding review')
    expect(wrapper.text()).toContain('3')
    expect(wrapper.find('a[href="/admin/directory/offboarding"]').exists()).toBe(true)
  })

  it('shows missing AI access setup as a regular work item', async () => {
    const api = await import('@/api/workItems') as any
    api.getWorkItemCounts.mockResolvedValue({
      data: {
        data: {
          quota_reset_approval_count: 0,
          quota_reset_admin_count: 0,
          ai_access_setup_count: 1,
          offboarding_count: 0,
          total_count: 1,
        },
      },
    })

    const wrapper = await mountWorkItemsView('user')

    expect(wrapper.text()).toContain('AI access setup')
    expect(wrapper.text()).toContain('1')
    expect(wrapper.find('a[href="/user"]').exists()).toBe(true)
    expect(wrapper.find('a[href="/usage/quota-reset"]').exists()).toBe(false)
  })
})
