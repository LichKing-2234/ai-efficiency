import { createRouter, createWebHistory } from 'vue-router'
import { installAuthNavigationGuards } from '@/router/authGuard'
import { reloadOnceForChunkError } from '@/utils/chunkReload'

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
      meta: { public: true, redirectOnAuthExpiry: true },
    },
    {
      path: '/',
      redirect: '/usage',
    },
    {
      path: '/usage',
      name: 'Usage',
      component: () => import('@/views/DashboardView.vue'),
    },
    {
      path: '/work-items',
      name: 'WorkItems',
      component: () => import('@/views/WorkItemsView.vue'),
    },
    {
      path: '/usage/members/:user_id',
      name: 'UsageMember',
      component: () => import('@/views/DashboardView.vue'),
    },
    {
      path: '/usage/team',
      name: 'UsageTeam',
      component: () => import('@/views/TeamOverviewView.vue'),
    },
    {
      path: '/usage/quota-reset',
      name: 'UsageQuotaReset',
      component: () => import('@/views/QuotaResetView.vue'),
    },
    {
      path: '/repos',
      name: 'RepoList',
      component: () => import('@/views/repos/RepoListView.vue'),
    },
    {
      path: '/events',
      name: 'Events',
      component: () => import('@/views/events/EventsView.vue'),
    },
    {
      path: '/user',
      name: 'User',
      component: () => import('@/views/UserView.vue'),
    },
    {
      path: '/admin/users',
      name: 'AdminUsers',
      component: () => import('@/views/admin/AdminUsersView.vue'),
      meta: { requireAdmin: true },
    },
    {
      path: '/admin/directory/offboarding',
      name: 'DirectoryOffboarding',
      component: () => import('@/views/admin/DirectoryOffboardingView.vue'),
      meta: { requireAdmin: true },
    },
    {
      path: '/repos/:id',
      name: 'RepoDetail',
      component: () => import('@/views/repos/RepoDetailView.vue'),
      props: true,
    },
    {
      path: '/settings',
      name: 'Settings',
      component: () => import('@/views/SettingsView.vue'),
      meta: { requireAdmin: true },
    },
  ],
})

installAuthNavigationGuards(router)

export function handleRouterError(error: unknown) {
  if (reloadOnceForChunkError(error)) {
    return
  }
  console.error('Router error:', error)
}

router.onError(handleRouterError)

export default router
