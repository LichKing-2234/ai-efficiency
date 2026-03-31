import { describe, it, expect, beforeEach } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import router from '@/router'

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
    const router = createTestRouter()
    const pinia = createPinia()

    router.beforeEach((to) => {
      const auth = useAuthStore(pinia)
      if (!to.meta.public && !auth.isAuthenticated) {
        return { path: '/login', query: { redirect: to.fullPath } }
      }
    })

    await router.push('/')
    await router.isReady()

    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.redirect).toBe('/')
  })

  it('allows access to login page without auth', async () => {
    const router = createTestRouter()
    const pinia = createPinia()

    router.beforeEach((to) => {
      const auth = useAuthStore(pinia)
      if (!to.meta.public && !auth.isAuthenticated) {
        return { path: '/login', query: { redirect: to.fullPath } }
      }
    })

    await router.push('/login')
    await router.isReady()

    expect(router.currentRoute.value.path).toBe('/login')
  })

  it('allows access to protected routes when authenticated', async () => {
    localStorage.setItem('token', 'valid-token')
    const router = createTestRouter()
    const pinia = createPinia()

    router.beforeEach((to) => {
      const auth = useAuthStore(pinia)
      if (!to.meta.public && !auth.isAuthenticated) {
        return { path: '/login', query: { redirect: to.fullPath } }
      }
    })

    await router.push('/repos')
    await router.isReady()

    expect(router.currentRoute.value.path).toBe('/repos')
  })

  it('redirects to repos with redirect query when not authenticated', async () => {
    const router = createTestRouter()
    const pinia = createPinia()

    router.beforeEach((to) => {
      const auth = useAuthStore(pinia)
      if (!to.meta.public && !auth.isAuthenticated) {
        return { path: '/login', query: { redirect: to.fullPath } }
      }
    })

    await router.push('/repos')
    await router.isReady()

    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.redirect).toBe('/repos')
  })

  it('includes session detail route in the router', async () => {
    const sessionDetail = router.getRoutes().find((r) => r.name === 'SessionDetail')
    expect(sessionDetail?.path).toBe('/sessions/:id')
  })
})
