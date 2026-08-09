import { describe, it, expect, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent } from 'vue'
import { createRouter, createMemoryHistory, RouterView } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import { setLocale } from '@/i18n'

function installMatchMedia(initial = false) {
  let matches = initial
  const listeners = new Set<(event: MediaQueryListEvent) => void>()
  const mediaQuery = {
    get matches() {
      return matches
    },
    media: '(min-width: 768px)',
    onchange: null,
    addEventListener: vi.fn((_type: string, listener: (event: MediaQueryListEvent) => void) => listeners.add(listener)),
    removeEventListener: vi.fn((_type: string, listener: (event: MediaQueryListEvent) => void) => listeners.delete(listener)),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  } as unknown as MediaQueryList

  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: vi.fn(() => mediaQuery),
  })

  return {
    change(next: boolean) {
      matches = next
      const event = { matches: next, media: mediaQuery.media } as MediaQueryListEvent
      for (const listener of listeners) listener(event)
    },
  }
}

vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  getMe: vi.fn(),
  devLogin: vi.fn(),
}))

vi.mock('@/api/workItems', () => ({
  getWorkItemCounts: vi.fn(),
}))

function countsResponse(total = 0) {
  return {
    data: {
      data: {
        quota_reset_approval_count: total,
        quota_reset_admin_count: 0,
        ai_access_setup_count: 0,
        offboarding_count: 0,
        total_count: total,
      },
    },
  }
}

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

function layoutView(name: string) {
  return defineComponent({
    name,
    components: { AppLayout },
    template: `<AppLayout><div>${name}</div></AppLayout>`,
  })
}

function createLayoutRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [1, 2, 3, 4, 5].map((id) => ({
      path: `/view-${id}`,
      component: layoutView(`View${id}`),
    })),
  })
}

describe('AppLayout', () => {
  beforeEach(() => {
    installMatchMedia(false)
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

  it('uses an Element Plus action for the mobile navigation entry', async () => {
    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(AppLayout, {
      global: { plugins: [createPinia(), router] },
    })

    expect(wrapper.get('[aria-controls="mobile-navigation"]').classes()).toContain('el-button')
  })

  it('removes the default gap from the mobile navigation drawer header', async () => {
    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(AppLayout, {
      global: { plugins: [createPinia(), router] },
    })

    const drawer = wrapper.findComponent({ name: 'ElDrawer' })
    expect(drawer.props('showClose')).toBe(false)
    expect(drawer.props('headerClass')).toBe('!m-0')
    expect(drawer.props('bodyClass')).toBe('!p-0')

    await wrapper.get('[aria-controls="mobile-navigation"]').trigger('click')
    await flushPromises()

    const close = wrapper.get('button[title="Close"]')
    const header = close.element.closest('header')
    expect(header).not.toBeNull()
    expect(header?.textContent).toContain('Menu')
  })

  it('closes an open mobile drawer when the layout crosses into desktop width', async () => {
    const media = installMatchMedia(false)
    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(AppLayout, {
      global: { plugins: [createPinia(), router] },
    })

    await wrapper.get('[aria-controls="mobile-navigation"]').trigger('click')
    await flushPromises()
    expect(wrapper.findComponent({ name: 'ElDrawer' }).props('modelValue')).toBe(true)

    media.change(true)
    await wrapper.vm.$nextTick()

    expect(wrapper.findComponent({ name: 'ElDrawer' }).props('modelValue')).toBe(false)
  })

  it('bounds count loads across five protected route layout identities', async () => {
    vi.useFakeTimers()
    const pinia = createPinia()
    const router = createLayoutRouter()
    const api = await import('@/api/workItems') as any
    api.getWorkItemCounts.mockResolvedValue(countsResponse(1))
    vi.setSystemTime(1_000)

    await router.push('/view-1')
    await router.isReady()
    const wrapper = mount(defineComponent({
      components: { RouterView },
      template: '<RouterView />',
    }), {
      global: { plugins: [pinia, router] },
    })

    try {
      await flushPromises()
      for (const path of ['/view-2', '/view-3', '/view-4', '/view-5']) {
        await router.push(path)
        await flushPromises()
      }

      vi.setSystemTime(20_999)
      await router.push('/view-1')
      await flushPromises()
      expect(api.getWorkItemCounts).toHaveBeenCalledTimes(1)

      vi.setSystemTime(21_000)
      await router.push('/view-2')
      await flushPromises()
      expect(api.getWorkItemCounts).toHaveBeenCalledTimes(2)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('bounds count loads across repeated mobile sidebar mounts', async () => {
    vi.useFakeTimers()
    const pinia = createPinia()
    const router = createTestRouter()
    const api = await import('@/api/workItems') as any
    api.getWorkItemCounts.mockResolvedValue(countsResponse(1))
    vi.setSystemTime(1_000)

    await router.push('/')
    await router.isReady()
    const wrapper = mount(AppLayout, {
      global: { plugins: [pinia, router] },
    })

    try {
      await flushPromises()
      const menuButton = wrapper.get('[aria-controls="mobile-navigation"]')
      for (let i = 0; i < 5; i += 1) {
        await menuButton.trigger('click')
        await flushPromises()
        await wrapper.get('button[title="Close"]').trigger('click')
        await flushPromises()
      }
      expect(api.getWorkItemCounts).toHaveBeenCalledTimes(1)

      vi.setSystemTime(21_000)
      await menuButton.trigger('click')
      await flushPromises()
      expect(api.getWorkItemCounts).toHaveBeenCalledTimes(2)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })
})
