import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import { setLocale } from '@/i18n'

vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  getMe: vi.fn(),
  devLogin: vi.fn(),
}))

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div>Dashboard</div>' } },
      { path: '/events', component: { template: '<div>Events</div>' } },
      { path: '/user', component: { template: '<div>User</div>' } },
      { path: '/repos', component: { template: '<div>Repos</div>' } },
      { path: '/admin/users', component: { template: '<div>Admin Users</div>' } },
      { path: '/settings', component: { template: '<div>Settings</div>' } },
      { path: '/login', component: { template: '<div>Login</div>' } },
    ],
  })
}

describe('AppLayout', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    setLocale('en-US')
    vi.clearAllMocks()
  })

  it('keeps the desktop sidebar in a fixed viewport layout while main content scrolls', async () => {
    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(AppLayout, {
      slots: {
        default: '<div class="w-[2000px]">Wide content</div>',
      },
      global: { plugins: [createPinia(), router] },
    })

    const shell = wrapper.get('div')
    expect(shell.classes()).toContain('md:h-screen')
    expect(shell.classes()).toContain('md:overflow-hidden')

    const sidebar = wrapper.get('aside')
    expect(sidebar.classes()).toContain('h-screen')
    expect(sidebar.classes()).toContain('shrink-0')

    const main = wrapper.get('main')
    expect(main.classes()).toContain('min-w-0')
    expect(main.classes()).toContain('overflow-auto')
    expect(main.classes()).toContain('md:h-screen')
    expect(main.classes()).toContain('md:min-h-0')
  })
})
