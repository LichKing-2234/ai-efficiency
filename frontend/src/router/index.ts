import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { reloadOnceForChunkError } from '@/utils/deploymentRecovery'

function resolveSafeRedirect(raw: unknown, fallback = '/') {
  if (typeof raw !== 'string') {
    return fallback
  }
  if (!raw.startsWith('/') || raw.startsWith('//')) {
    return fallback
  }
  if (raw === '/login' || raw.startsWith('/login?') || raw.startsWith('/login#')) {
    return fallback
  }
  return raw
}

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'Login',
      component: () => import('@/views/LoginView.vue'),
      meta: { public: true },
    },
    {
      path: '/oauth/authorize',
      name: 'OAuthAuthorize',
      component: () => import('@/views/oauth/AuthorizePage.vue'),
      meta: { public: true },
    },
    {
      path: '/oauth/device',
      name: 'OAuthDevice',
      component: () => import('@/views/oauth/DevicePage.vue'),
      meta: { public: true },
    },
    {
      path: '/',
      name: 'Dashboard',
      component: () => import('@/views/DashboardView.vue'),
    },
    {
      path: '/repos',
      name: 'RepoList',
      component: () => import('@/views/repos/RepoListView.vue'),
    },
    {
      path: '/repos/:id',
      name: 'RepoDetail',
      component: () => import('@/views/repos/RepoDetailView.vue'),
      props: true,
    },
    {
      path: '/repos/:repoId/scans',
      name: 'ScanResult',
      component: () => import('@/views/analysis/ScanResultView.vue'),
    },
    {
      path: '/repos/:repoId/scans/:scanId',
      name: 'ScanResultDetail',
      component: () => import('@/views/analysis/ScanResultView.vue'),
    },
    {
      path: '/sessions',
      name: 'SessionList',
      component: () => import('@/views/sessions/SessionListView.vue'),
    },
    {
      path: '/sessions/:id',
      name: 'SessionDetail',
      component: () => import('@/views/sessions/SessionDetailView.vue'),
    },
    {
      path: '/settings',
      name: 'Settings',
      component: () => import('@/views/SettingsView.vue'),
      meta: { requireAdmin: true },
    },
  ],
})

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  // Hydrate user info after page refresh (pinia state is lost)
  if (auth.isAuthenticated && !auth.user) {
    await auth.fetchMe()
  }
  if (to.name === 'Login' && auth.isAuthenticated) {
    return { path: resolveSafeRedirect(to.query.redirect) }
  }
  if (!to.meta.public && !auth.isAuthenticated) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  if (to.meta.requireAdmin && !auth.isAdmin) {
    return { path: '/' }
  }
})

export function handleRouterError(error: unknown) {
  if (reloadOnceForChunkError(error)) {
    return
  }
  console.error('Router error:', error)
}

router.onError(handleRouterError)

export default router
