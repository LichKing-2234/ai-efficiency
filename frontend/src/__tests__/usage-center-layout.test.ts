import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import UsageCenterLayout from '@/views/UsageCenterLayout.vue'
import { getTeamUsageScope } from '@/api/teamUsage'
import { setLocale } from '@/i18n'

vi.mock('@/api/teamUsage', () => ({ getTeamUsageScope: vi.fn() }))
vi.mock('@/components/AppLayout.vue', () => ({
  default: { template: '<main><slot /></main>' },
}))

function createHarness(initialPath = '/usage') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: '/usage',
        name: 'UsageCenter',
        component: UsageCenterLayout,
        children: [
          { path: '', name: 'Usage', component: { template: '<div data-testid="personal-content">Personal</div>' } },
          { path: 'team', name: 'UsageTeam', component: { template: '<div data-testid="team-content">Team</div>' } },
          { path: 'quota-reset', name: 'UsageQuotaReset', component: { template: '<div data-testid="reset-content">Reset</div>' } },
        ],
      },
      { path: '/usage/members/:user_id', name: 'UsageMember', component: { template: '<div data-testid="member-content">Member</div>' } },
    ],
  })
  return router.push(initialPath).then(() => router.isReady()).then(() => ({
    router,
    wrapper: mount({ template: '<RouterView />', components: { RouterView } }, { global: { plugins: [router] } }),
  }))
}

describe('UsageCenterLayout', () => {
  beforeEach(() => {
    setLocale('en-US')
    vi.clearAllMocks()
    vi.mocked(getTeamUsageScope).mockResolvedValue({ data: { data: { is_representative: true } } } as any)
  })

  it('keeps one banner and one scope discovery while switching child pages', async () => {
    const { router, wrapper } = await createHarness()
    await flushPromises()

    expect(wrapper.findAll('[data-testid="usage-center-banner"]')).toHaveLength(1)
    expect(wrapper.text()).toContain('AI Usage Center')
    expect(wrapper.text()).toContain('My Usage')
    expect(wrapper.text()).toContain('Team Overview')
    expect(wrapper.text()).toContain('Reset Requests')

    const banner = wrapper.get('[data-testid="usage-center-banner"]').element
    await router.push('/usage/team')
    await flushPromises()
    expect(wrapper.get('[data-testid="usage-center-banner"]').element).toBe(banner)
    await router.push('/usage/quota-reset')
    await flushPromises()

    expect(wrapper.findAll('[data-testid="usage-center-banner"]')).toHaveLength(1)
    expect(getTeamUsageScope).toHaveBeenCalledTimes(1)
  })

  it('fails closed for team navigation without representative scope', async () => {
    vi.mocked(getTeamUsageScope).mockResolvedValue({ data: { data: { is_representative: false } } } as any)
    const { wrapper } = await createHarness()
    await flushPromises()

    expect(wrapper.text()).toContain('My Usage')
    expect(wrapper.text()).toContain('Reset Requests')
    expect(wrapper.text()).not.toContain('Team Overview')
  })

  it('keeps member usage outside the shared banner and scope lifecycle', async () => {
    const { wrapper } = await createHarness('/usage/members/42')
    await flushPromises()

    expect(wrapper.find('[data-testid="member-content"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="usage-center-banner"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="usage-center-tabs"]').exists()).toBe(false)
    expect(getTeamUsageScope).not.toHaveBeenCalled()
  })
})
