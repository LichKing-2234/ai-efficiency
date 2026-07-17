import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, disposePinia, setActivePinia } from 'pinia'
import { defineComponent } from 'vue'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import LoginView from '@/views/LoginView.vue'
import { installAuthNavigationGuards } from '@/router/authGuard'
import { useAuthStore } from '@/stores/auth'
import {
  clearBrowserSession,
  readBrowserSession,
  replaceBrowserSession,
} from '@/auth/browserSession'
import {
  devLogin as apiDevLogin,
  getAuthOptions,
  getMe,
  login as apiLogin,
} from '@/api/auth'
import type { User } from '@/types'

vi.mock('@/api/auth', () => ({
  devLogin: vi.fn(),
  getAuthOptions: vi.fn(),
  getMe: vi.fn(),
  login: vi.fn(),
}))

type Deferred<T> = {
  promise: Promise<T>
  resolve: (value: T) => void
  reject: (reason?: unknown) => void
}

type LoaderName = 'login' | 'oauthAuthorize' | 'oauthDevice' | 'usage' | 'repos' | 'settings'

function deferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

const alice: User = {
  id: 1,
  username: 'alice',
  email: 'alice@example.com',
  role: 'admin',
  auth_source: 'sso',
}

const bob: User = {
  id: 2,
  username: 'bob',
  email: 'bob@example.org',
  role: 'user',
  auth_source: 'ldap',
}

function userResponse(user: User) {
  return { data: { data: user } }
}

function tokenResponse(accessToken: string, refreshToken: string | null) {
  return {
    data: {
      data: {
        token: accessToken,
        ...(refreshToken ? { refresh_token: refreshToken } : {}),
      },
    },
  }
}

function marker(name: string) {
  return defineComponent({
    name: `${name}RouteSkeleton`,
    template: `<div data-route-skeleton="${name}">${name}</div>`,
  })
}

const activeHarnesses: Array<{
  pinia: ReturnType<typeof createPinia>
  wrapper: { unmount: () => void }
  disposeGuards: () => void
}> = []

function createHarness(options: {
  loginComponent?: typeof LoginView
  loaderPromises?: Partial<Record<LoaderName, Promise<any>>>
} = {}) {
  const pinia = createPinia()
  setActivePinia(pinia)

  const makeLoader = (name: LoaderName) => vi.fn(() => (
    options.loaderPromises?.[name] ?? Promise.resolve(marker(name))
  ))
  const loaders = {
    login: makeLoader('login'),
    oauthAuthorize: makeLoader('oauthAuthorize'),
    oauthDevice: makeLoader('oauthDevice'),
    usage: makeLoader('usage'),
    repos: makeLoader('repos'),
    settings: makeLoader('settings'),
  }

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: '/login',
        name: 'Login',
        component: options.loginComponent ?? loaders.login,
        meta: { public: true },
      },
      {
        path: '/oauth/authorize',
        name: 'OAuthAuthorize',
        component: loaders.oauthAuthorize,
        meta: { public: true },
      },
      {
        path: '/oauth/device',
        name: 'OAuthDevice',
        component: loaders.oauthDevice,
        meta: { public: true, redirectOnAuthExpiry: true },
      },
      { path: '/', name: 'Dashboard', component: marker('dashboard') },
      { path: '/usage', name: 'Usage', component: loaders.usage },
      { path: '/repos', name: 'RepoList', component: loaders.repos },
      {
        path: '/settings',
        name: 'Settings',
        component: loaders.settings,
        meta: { requireAdmin: true },
      },
    ],
  })

  const disposeGuards = installAuthNavigationGuards(router)
  const wrapper = mount(defineComponent({
    components: { RouterView },
    template: '<RouterView />',
  }), {
    global: { plugins: [pinia, router] },
  })

  const harness = { pinia, router, loaders, wrapper, disposeGuards }
  activeHarnesses.push(harness)
  return harness
}

describe('production auth navigation hydration', () => {
  beforeEach(() => {
    localStorage.clear()
    clearBrowserSession()
    vi.clearAllMocks()
    vi.mocked(getAuthOptions).mockResolvedValue({
      data: { data: { ldap_enabled: true, dev_login_enabled: true } },
    } as any)
  })

  afterEach(() => {
    while (activeHarnesses.length) {
      const harness = activeHarnesses.pop()!
      harness.wrapper.unmount()
      harness.disposeGuards()
      disposePinia(harness.pinia)
    }
  })

  it('loads and renders an ordinary protected route before identity hydration resolves', async () => {
    const identity = deferred<ReturnType<typeof userResponse>>()
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    vi.mocked(getMe).mockReturnValue(identity.promise as any)
    const { router, loaders, wrapper } = createHarness()

    await router.push('/usage')
    await flushPromises()

    expect(getMe).toHaveBeenCalledTimes(1)
    expect(loaders.usage).toHaveBeenCalledTimes(1)
    expect(router.currentRoute.value.fullPath).toBe('/usage')
    expect(wrapper.find('[data-route-skeleton="usage"]').exists()).toBe(true)

    identity.resolve(userResponse(alice))
    await flushPromises()
  })

  it('renders Login before hydration and redirects to its safe target only after verified identity', async () => {
    const identity = deferred<ReturnType<typeof userResponse>>()
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    vi.mocked(getMe).mockReturnValue(identity.promise as any)
    const { router, loaders, wrapper } = createHarness()
    const replaceSpy = vi.spyOn(router, 'replace')

    await router.push('/login?redirect=/repos')
    await flushPromises()

    expect(loaders.login).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-route-skeleton="login"]').exists()).toBe(true)
    expect(router.currentRoute.value.name).toBe('Login')
    expect(getMe).toHaveBeenCalledTimes(1)

    identity.resolve(userResponse(alice))
    await vi.waitFor(() => expect(router.currentRoute.value.fullPath).toBe('/repos'))
    expect(replaceSpy).toHaveBeenCalledTimes(1)
    expect(getMe).toHaveBeenCalledTimes(1)
  })

  it('renders OAuth Authorize before hydration and keeps it visible after verified identity', async () => {
    const identity = deferred<ReturnType<typeof userResponse>>()
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    vi.mocked(getMe).mockReturnValue(identity.promise as any)
    const { router, loaders, wrapper } = createHarness()

    await router.push('/oauth/authorize?client_id=client-test')
    await flushPromises()

    expect(loaders.oauthAuthorize).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-route-skeleton="oauthAuthorize"]').exists()).toBe(true)
    expect(getMe).toHaveBeenCalledTimes(1)

    identity.resolve(userResponse(alice))
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('OAuthAuthorize')
  })

  it.each([
    ['/login', 'login'],
    ['/oauth/authorize', 'oauthAuthorize'],
    ['/oauth/device', 'oauthDevice'],
  ] as const)('renders public route %s without requesting current identity', async (path, loaderName) => {
    const { router, loaders, wrapper } = createHarness()

    await router.push(path)
    await flushPromises()

    expect(loaders[loaderName]).toHaveBeenCalledTimes(1)
    expect(wrapper.find(`[data-route-skeleton="${loaderName}"]`).exists()).toBe(true)
    expect(getMe).not.toHaveBeenCalled()
  })

  it('keeps an administrator loader closed until a verified admin resolves', async () => {
    const identity = deferred<ReturnType<typeof userResponse>>()
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    vi.mocked(getMe).mockReturnValue(identity.promise as any)
    const { router, loaders } = createHarness()

    const navigation = router.push('/settings')
    await vi.waitFor(() => expect(getMe).toHaveBeenCalledTimes(1))
    expect(loaders.settings).not.toHaveBeenCalled()

    identity.resolve(userResponse(alice))
    await navigation

    expect(router.currentRoute.value.fullPath).toBe('/settings')
    expect(loaders.settings).toHaveBeenCalledTimes(1)
  })

  it('keeps an administrator loader closed and redirects a current non-admin to root', async () => {
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    vi.mocked(getMe).mockResolvedValue(userResponse(bob) as any)
    const { router, loaders } = createHarness()

    await router.push('/settings')

    expect(loaders.settings).not.toHaveBeenCalled()
    expect(router.currentRoute.value.fullPath).toBe('/')
  })

  it('keeps an administrator loader closed and redirects current invalid identity to Login', async () => {
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    vi.mocked(getMe).mockRejectedValue({ response: { status: 401 } })
    const { router, loaders } = createHarness()

    await router.push('/settings')

    expect(loaders.settings).not.toHaveBeenCalled()
    expect(router.currentRoute.value.name).toBe('Login')
    expect(router.currentRoute.value.query.redirect).toBe('/settings')
  })

  it('does not let an old pending Login hydration redirect a newer OAuth navigation', async () => {
    const identity = deferred<ReturnType<typeof userResponse>>()
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    vi.mocked(getMe).mockReturnValue(identity.promise as any)
    const { router, loaders } = createHarness()
    const replaceSpy = vi.spyOn(router, 'replace')

    await router.push('/login?redirect=/repos')
    await router.push('/oauth/authorize')
    expect(loaders.oauthAuthorize).toHaveBeenCalledTimes(1)

    identity.resolve(userResponse(alice))
    await flushPromises()

    expect(router.currentRoute.value.name).toBe('OAuthAuthorize')
    expect(replaceSpy).not.toHaveBeenCalled()
  })

  it('lets only the newest Login A navigation follow shared hydration after Login A -> OAuth -> Login A', async () => {
    const identity = deferred<ReturnType<typeof userResponse>>()
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    vi.mocked(getMe).mockReturnValue(identity.promise as any)
    const { router } = createHarness()
    const replaceSpy = vi.spyOn(router, 'replace')

    await router.push('/login?redirect=/repos')
    await router.push('/oauth/authorize')
    await router.push('/login?redirect=/repos')
    expect(getMe).toHaveBeenCalledTimes(1)

    identity.resolve(userResponse(alice))
    await vi.waitFor(() => expect(router.currentRoute.value.fullPath).toBe('/repos'))

    expect(replaceSpy).toHaveBeenCalledTimes(1)
  })

  it('invalidates pending Login A follow-up when normal login installs different-token B without navigation', async () => {
    const identityA = deferred<ReturnType<typeof userResponse>>()
    const identityB = deferred<ReturnType<typeof userResponse>>()
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    vi.mocked(getMe)
      .mockReturnValueOnce(identityA.promise as any)
      .mockReturnValueOnce(identityB.promise as any)
    vi.mocked(apiLogin).mockResolvedValue(tokenResponse('token-b', 'refresh-b') as any)
    const { router, pinia, wrapper } = createHarness({ loginComponent: LoginView })
    const auth = useAuthStore(pinia)
    const replaceSpy = vi.spyOn(router, 'replace')
    const pushSpy = vi.spyOn(router, 'push')

    await router.push('/login?redirect=/repos')
    await flushPromises()
    const requestA = auth.ensureUser()
    replaceSpy.mockClear()
    pushSpy.mockClear()

    await wrapper.get('input#username').setValue('bob')
    await wrapper.get('input#password').setValue('test-password')
    await wrapper.get('form').trigger('submit')
    await vi.waitFor(() => expect(getMe).toHaveBeenCalledTimes(2))

    identityA.resolve(userResponse(alice))
    await expect(requestA).resolves.toBeNull()
    expect(replaceSpy).not.toHaveBeenCalled()

    identityB.resolve(userResponse(bob))
    await vi.waitFor(() => expect(router.currentRoute.value.fullPath).toBe('/repos'))
    expect(replaceSpy).not.toHaveBeenCalled()
    expect(pushSpy).toHaveBeenCalledTimes(1)
    expect(readBrowserSession().accessToken).toBe('token-b')
  })

  it('invalidates pending Login A follow-up when Dev Login replaces the same token without navigation', async () => {
    const identityA = deferred<ReturnType<typeof userResponse>>()
    const identityB = deferred<ReturnType<typeof userResponse>>()
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    const generationA = readBrowserSession().generation
    vi.mocked(getMe)
      .mockReturnValueOnce(identityA.promise as any)
      .mockReturnValueOnce(identityB.promise as any)
    vi.mocked(apiDevLogin).mockResolvedValue(tokenResponse('token-a', 'refresh-b') as any)
    const { router, pinia, wrapper } = createHarness({ loginComponent: LoginView })
    const auth = useAuthStore(pinia)
    const replaceSpy = vi.spyOn(router, 'replace')
    const pushSpy = vi.spyOn(router, 'push')

    await router.push('/login?redirect=/repos')
    await flushPromises()
    const requestA = auth.ensureUser()
    replaceSpy.mockClear()
    pushSpy.mockClear()

    const devButton = wrapper.findAll('button').find((button) => button.text().includes('Dev Login'))
    await devButton!.trigger('click')
    await vi.waitFor(() => expect(getMe).toHaveBeenCalledTimes(2))
    expect(readBrowserSession().generation).toBeGreaterThan(generationA)

    identityA.resolve(userResponse(alice))
    await expect(requestA).resolves.toBeNull()
    expect(replaceSpy).not.toHaveBeenCalled()

    identityB.resolve(userResponse(alice))
    await vi.waitFor(() => expect(router.currentRoute.value.fullPath).toBe('/'))
    expect(replaceSpy).not.toHaveBeenCalled()
    expect(pushSpy).toHaveBeenCalledTimes(1)
  })

  it('keeps OAuth current when superseded Admin hydration resolves non-admin', async () => {
    const identity = deferred<ReturnType<typeof userResponse>>()
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    vi.mocked(getMe).mockReturnValue(identity.promise as any)
    const { router, loaders } = createHarness()

    const adminNavigation = router.push('/settings')
    await vi.waitFor(() => expect(getMe).toHaveBeenCalledTimes(1))
    await router.push('/oauth/authorize')
    expect(loaders.oauthAuthorize).toHaveBeenCalledTimes(1)

    identity.resolve(userResponse(bob))
    await adminNavigation
    await flushPromises()

    expect(router.currentRoute.value.name).toBe('OAuthAuthorize')
    expect(loaders.settings).not.toHaveBeenCalled()
  })

  it('keeps OAuth current when superseded Admin hydration expires the current session', async () => {
    const identity = deferred<ReturnType<typeof userResponse>>()
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    vi.mocked(getMe).mockReturnValue(identity.promise as any)
    const { router, loaders } = createHarness()

    const adminNavigation = router.push('/settings')
    await vi.waitFor(() => expect(getMe).toHaveBeenCalledTimes(1))
    await router.push('/oauth/authorize')
    expect(loaders.oauthAuthorize).toHaveBeenCalledTimes(1)

    identity.reject({ response: { status: 401 } })
    await adminNavigation
    await flushPromises()

    expect(router.currentRoute.value.name).toBe('OAuthAuthorize')
    expect(loaders.settings).not.toHaveBeenCalled()
  })

  it('renders OAuth Device before invalid identity redirects it to Login', async () => {
    const identity = deferred<ReturnType<typeof userResponse>>()
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    vi.mocked(getMe).mockReturnValue(identity.promise as any)
    const { router, loaders, wrapper } = createHarness()

    await router.push('/oauth/device')
    await flushPromises()
    expect(loaders.oauthDevice).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-route-skeleton="oauthDevice"]').exists()).toBe(true)

    identity.reject({ response: { status: 401 } })
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('Login'))
    expect(router.currentRoute.value.query.redirect).toBe('/oauth/device')
  })
})
