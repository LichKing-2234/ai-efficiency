import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, disposePinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { getMe } from '@/api/auth'
import {
  clearBrowserSession,
  readBrowserSession,
  replaceBrowserSession,
} from '@/auth/browserSession'
import router, { handleRouterError } from '@/router'
import { installAuthNavigationGuards } from '@/router/authGuard'
import { useAuthStore } from '@/stores/auth'
import type { User } from '@/types'

vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  getMe: vi.fn(),
  devLogin: vi.fn(),
}))

vi.mock('@/utils/chunkReload', () => ({
  reloadOnceForChunkError: vi.fn(),
}))

type ReloadOnceMock = ReturnType<typeof vi.fn> & ((error: unknown, options?: any) => boolean)

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

const activeHarnesses: Array<{
  pinia: ReturnType<typeof createPinia>
  disposeGuards: () => void
}> = []

function createGuardedRouter() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const adminLoader = vi.fn(() => Promise.resolve({ template: '<div>Admin</div>' }))
  const localRouter = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', name: 'Login', component: { template: '<div>Login</div>' }, meta: { public: true } },
      { path: '/oauth/device', name: 'OAuthDevice', component: { template: '<div>Device</div>' }, meta: { public: true, redirectOnAuthExpiry: true } },
      { path: '/', name: 'Dashboard', component: { template: '<div>Dashboard</div>' } },
      { path: '/repos', name: 'RepoList', component: { template: '<div>Repos</div>' } },
      { path: '/admin/users', name: 'AdminUsers', component: adminLoader, meta: { requireAdmin: true } },
    ],
  })
  const disposeGuards = installAuthNavigationGuards(localRouter)
  const harness = { pinia, router: localRouter, adminLoader, disposeGuards }
  activeHarnesses.push(harness)
  return harness
}

describe('Router Guards', () => {
  beforeEach(() => {
    localStorage.clear()
    clearBrowserSession()
    vi.clearAllMocks()
  })

  afterEach(() => {
    while (activeHarnesses.length) {
      const harness = activeHarnesses.pop()!
      harness.disposeGuards()
      disposePinia(harness.pinia)
    }
  })

  it('redirects to login with the protected full path when not authenticated', async () => {
    const harness = createGuardedRouter()

    await harness.router.push('/repos?tab=active')

    expect(harness.router.currentRoute.value.path).toBe('/login')
    expect(harness.router.currentRoute.value.query.redirect).toBe('/repos?tab=active')
  })

  it('allows access to login page without auth', async () => {
    const harness = createGuardedRouter()

    await harness.router.push('/login')

    expect(harness.router.currentRoute.value.path).toBe('/login')
    expect(getMe).not.toHaveBeenCalled()
  })

  it('allows access to protected routes when authenticated', async () => {
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    vi.mocked(getMe).mockResolvedValue({ data: { data: alice } } as any)
    const harness = createGuardedRouter()

    await harness.router.push('/repos')

    expect(harness.router.currentRoute.value.path).toBe('/repos')
    await vi.waitFor(() => expect(getMe).toHaveBeenCalledTimes(1))
  })

  it('does not include legacy session routes in the router', () => {
    const sessionList = router.getRoutes().find((route) => route.name === 'SessionList')
    const sessionDetail = router.getRoutes().find((route) => route.name === 'SessionDetail')
    expect(sessionList).toBeUndefined()
    expect(sessionDetail).toBeUndefined()
  })

  it('includes oauth device route with destination-owned expiry policy', () => {
    const oauthDevice = router.getRoutes().find((route) => route.name === 'OAuthDevice')
    expect(oauthDevice?.path).toBe('/oauth/device')
    expect(oauthDevice?.meta.public).toBe(true)
    expect(oauthDevice?.meta.redirectOnAuthExpiry).toBe(true)
  })


  it('redirects legacy event and attribution routes to canonical Activity while preserving query state', () => {
    const eventsRoute = router.getRoutes().find((route) => route.name === 'Events')
    const attributionRoute = router.getRoutes().find((route) => route.name === 'Attribution')
    expect(eventsRoute?.redirect).toBeTypeOf('function')
    expect(attributionRoute?.redirect).toBeTypeOf('function')
    expect((eventsRoute?.redirect as Function)({ query: { from: '2026-08-01' }, hash: '' })).toEqual({ path: '/activity', query: { from: '2026-08-01' } })
    expect((attributionRoute?.redirect as Function)({ query: { to: '2026-08-31', days: '7', unsafe: 'discard-me' }, hash: '' })).toEqual({
      path: '/activity',
      query: { to: '2026-08-31', days: '7' },
    })
  })

  it('includes personal, team, and member Activity routes', () => {
    expect(router.getRoutes().find((route) => route.name === 'Activity')?.path).toBe('/activity')
    expect(router.getRoutes().find((route) => route.name === 'ActivityTeams')?.path).toBe('/activity/teams')
    expect(router.getRoutes().find((route) => route.name === 'ActivityTeam')?.path).toBe('/activity/teams/:team_id')
    expect(router.getRoutes().find((route) => route.name === 'ActivityMember')?.path).toBe('/activity/members/:user_id')
  })

  it('includes user route in the router', () => {
    const userRoute = router.getRoutes().find((route) => route.name === 'User')
    expect(userRoute?.path).toBe('/user')
  })

  it('includes canonical AI Usage Center routes in the router', () => {
    const usageCenterRoute = router.getRoutes().find((route) => route.path === '/usage' && route.children.length > 0)
    const usageRoute = router.getRoutes().find((route) => route.name === 'Usage')
    const memberUsageRoute = router.getRoutes().find((route) => route.name === 'UsageMember')
    const teamUsageRoute = router.getRoutes().find((route) => route.name === 'UsageTeam')
    const quotaResetRoute = router.getRoutes().find((route) => route.name === 'UsageQuotaReset')
    expect(usageRoute?.path).toBe('/usage')
    expect(memberUsageRoute?.path).toBe('/usage/members/:user_id')
    expect(teamUsageRoute?.path).toBe('/usage/team')
    expect(quotaResetRoute?.path).toBe('/usage/quota-reset')
    expect(usageCenterRoute?.children.map((child) => child.name)).toEqual([
      'Usage',
      'UsageTeam',
      'UsageQuotaReset',
    ])
    expect(usageCenterRoute?.children.some((child) => child.name === 'UsageMember')).toBe(false)
  })

  it('includes Work Items route for authenticated users', () => {
    const route = router.getRoutes().find((candidate) => candidate.name === 'WorkItems')
    expect(route?.path).toBe('/work-items')
    expect(route?.meta.requireAdmin).toBeUndefined()
  })

  it('does not expose user usage as a separate page route', () => {
    const userUsageRoute = router.getRoutes().find((route) => route.name === 'UserUsage' || route.path === '/user/usage')
    expect(userUsageRoute).toBeUndefined()
  })

  it('includes admin users route requiring admin access', () => {
    const adminUsersRoute = router.getRoutes().find((route) => route.name === 'AdminUsers')
    expect(adminUsersRoute?.path).toBe('/admin/users')
    expect(adminUsersRoute?.meta.requireAdmin).toBe(true)
  })

  it('includes directory offboarding route requiring admin access', () => {
    const route = router.getRoutes().find((candidate) => candidate.name === 'DirectoryOffboarding')
    expect(route?.path).toBe('/admin/directory/offboarding')
    expect(route?.meta.requireAdmin).toBe(true)
  })

  it('redirects verified users away from login using a safe redirect target', async () => {
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    const harness = createGuardedRouter()
    const auth = useAuthStore(harness.pinia)
    auth.user = alice

    await harness.router.push('/login?redirect=/repos')

    expect(harness.router.currentRoute.value.path).toBe('/repos')
  })

  it('keeps invalid-token users on login after hydration fails with 401', async () => {
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    vi.mocked(getMe).mockRejectedValue({ response: { status: 401 } })
    const harness = createGuardedRouter()

    await harness.router.push('/login?case=expired')
    await vi.waitFor(() => expect(readBrowserSession().accessToken).toBeNull())

    expect(harness.router.currentRoute.value.path).toBe('/login')
    expect(harness.router.currentRoute.value.query.case).toBe('expired')
    expect(readBrowserSession().refreshToken).toBeNull()
  })

  it('redirects non-admin users away from admin routes without loading their chunk', async () => {
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    vi.mocked(getMe).mockResolvedValue({ data: { data: bob } } as any)
    const harness = createGuardedRouter()

    await harness.router.push('/admin/users')

    expect(harness.router.currentRoute.value.path).toBe('/')
    expect(harness.adminLoader).not.toHaveBeenCalled()
  })
})

describe('Router error handling', () => {
  let reloadOnceForChunkErrorMock: ReloadOnceMock

  beforeEach(async () => {
    const recovery = await import('@/utils/chunkReload')
    reloadOnceForChunkErrorMock = recovery.reloadOnceForChunkError as ReloadOnceMock
    reloadOnceForChunkErrorMock.mockReset()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('reloads once for chunk load failures and skips logging', () => {
    const chunkError = new Error('Loading chunk 12 failed')
    reloadOnceForChunkErrorMock.mockReturnValue(true)
    const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {})

    handleRouterError(chunkError)

    expect(reloadOnceForChunkErrorMock).toHaveBeenCalledWith(chunkError)
    expect(consoleSpy).not.toHaveBeenCalled()
  })

  it('logs non chunk errors when reload guard does not handle them', () => {
    const runtimeError = new Error('boom')
    reloadOnceForChunkErrorMock.mockReturnValue(false)
    const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {})

    handleRouterError(runtimeError)

    expect(reloadOnceForChunkErrorMock).toHaveBeenCalledWith(runtimeError)
    expect(consoleSpy).toHaveBeenCalledWith('Router error:', runtimeError)
  })
})
