import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AppSidebar from '@/components/AppSidebar.vue'
import { useAuthStore } from '@/stores/auth'
import { setLocale } from '@/i18n'

vi.mock('@/api/teamUsage', () => ({ getTeamUsageScope: vi.fn() }))
vi.mock('@/api/teamAccounts', () => ({ listTeamAccounts: vi.fn() }))
vi.mock('@/api/workItems', () => ({ getWorkItemCounts: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn(), currentRoute: { value: { path: '/usage' } } }) }))

describe('sidebar rep nav', () => {
  it('shows the organization account link for a representative', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const auth = useAuthStore()
    auth.user = { id: 1, username: 'sdkrep', email: 'sdkrep@example.com', role: 'user' } as any
    auth.token = 'token'
    const teamUsage = await import('@/api/teamUsage')
    vi.mocked(teamUsage.getTeamUsageScope).mockResolvedValue({ data: { code: 0, data: { is_representative: true, departments: [] } } } as any)
    const teamAccounts = await import('@/api/teamAccounts')
    vi.mocked(teamAccounts.listTeamAccounts).mockResolvedValue({ data: { code: 0, data: { items: [] } } } as any)
    await setLocale('zh-CN')
    const wrapper = mount(AppSidebar, { global: { plugins: [pinia], stubs: { RouterLink: { template: '<a><slot /></a>' } } } })
    await flushPromises()
    expect(wrapper.text()).toContain('组织账号')
  })
})
