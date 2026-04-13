import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import router, { handleRouterError } from '@/router'
import SessionDetailView from '@/views/sessions/SessionDetailView.vue'

vi.mock('@/utils/deploymentRecovery', () => ({
  reloadOnceForChunkError: vi.fn(),
}))

type ReloadOnceMock = ReturnType<typeof vi.fn> & ((error: unknown, options?: any) => boolean)

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', name: 'Login', component: { template: '<div>Login</div>' }, meta: { public: true } },
      { path: '/', name: 'Dashboard', component: { template: '<div>Dashboard</div>' } },
      { path: '/repos', name: 'RepoList', component: { template: '<div>Repos</div>' } },
      { path: '/sessions', name: 'SessionList', component: { template: '<div>Sessions</div>' } },
      { path: '/sessions/:id', name: 'SessionDetail', component: { template: '<div>Session Detail</div>' } },
    ],
  })
}

describe('Router Guards', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
  })

  it('redirects to login when not authenticated', async () => {
    const localRouter = createTestRouter()
    const pinia = createPinia()

    localRouter.beforeEach((to) => {
      const auth = useAuthStore(pinia)
      if (!to.meta.public && !auth.isAuthenticated) {
        return { path: '/login', query: { redirect: to.fullPath } }
      }
    })

    await localRouter.push('/')
    await localRouter.isReady()

    expect(localRouter.currentRoute.value.path).toBe('/login')
    expect(localRouter.currentRoute.value.query.redirect).toBe('/')
  })

  it('allows access to login page without auth', async () => {
    const localRouter = createTestRouter()
    const pinia = createPinia()

    localRouter.beforeEach((to) => {
      const auth = useAuthStore(pinia)
      if (!to.meta.public && !auth.isAuthenticated) {
        return { path: '/login', query: { redirect: to.fullPath } }
      }
    })

    await localRouter.push('/login')
    await localRouter.isReady()

    expect(localRouter.currentRoute.value.path).toBe('/login')
  })

  it('allows access to protected routes when authenticated', async () => {
    localStorage.setItem('token', 'valid-token')
    const localRouter = createTestRouter()
    const pinia = createPinia()

    localRouter.beforeEach((to) => {
      const auth = useAuthStore(pinia)
      if (!to.meta.public && !auth.isAuthenticated) {
        return { path: '/login', query: { redirect: to.fullPath } }
      }
    })

    await localRouter.push('/repos')
    await localRouter.isReady()

    expect(localRouter.currentRoute.value.path).toBe('/repos')
  })

  it('redirects to repos with redirect query when not authenticated', async () => {
    const localRouter = createTestRouter()
    const pinia = createPinia()

    localRouter.beforeEach((to) => {
      const auth = useAuthStore(pinia)
      if (!to.meta.public && !auth.isAuthenticated) {
        return { path: '/login', query: { redirect: to.fullPath } }
      }
    })

    await localRouter.push('/repos')
    await localRouter.isReady()

    expect(localRouter.currentRoute.value.path).toBe('/login')
    expect(localRouter.currentRoute.value.query.redirect).toBe('/repos')
  })

  it('includes session detail route in the router', async () => {
    const sessionDetail = router.getRoutes().find((r) => r.name === 'SessionDetail')
    expect(sessionDetail?.path).toBe('/sessions/:id')
    const componentLoader = sessionDetail?.components?.default as undefined | (() => Promise<{ default: unknown }>)
    expect(componentLoader).toBeTypeOf('function')
    const mod = await componentLoader!()
    expect(mod.default).toBe(SessionDetailView)
  })
})

describe('Router error handling', () => {
  let reloadOnceForChunkErrorMock: ReloadOnceMock

  beforeEach(async () => {
    const recovery = await import('@/utils/deploymentRecovery')
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
