import { createRouter, createWebHistory, type LocationQueryRaw, type RouteLocationGeneric } from 'vue-router'
import { installAuthNavigationGuards } from '@/router/authGuard'
import { reloadOnceForChunkError } from '@/utils/chunkReload'

function activityRedirect(to: RouteLocationGeneric) {
  const query: LocationQueryRaw = {}
  for (const key of ['from', 'to', 'range', 'days']) {
    const value = to.query?.[key]
    if (typeof value === 'string') query[key] = value
  }
  return { path: '/activity', query }
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
      children: [
        { path: 'team', name: 'UsageTeam', component: () => import('@/views/TeamOverviewView.vue') },
        { path: 'quota-reset', name: 'UsageQuotaReset', component: () => import('@/views/QuotaResetView.vue') },
      ],
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
      path: '/repos',
      name: 'RepoList',
      component: () => import('@/views/repos/RepoListView.vue'),
      meta: { requireAdmin: true },
    },
    {
      path: '/activity',
      name: 'Activity',
      component: () => import('@/views/activity/ActivityView.vue'),
    },
    {
      path: '/activity/teams',
      name: 'ActivityTeams',
      component: () => import('@/views/activity/ActivityTeamsView.vue'),
    },
    {
      path: '/activity/teams/:team_id',
      name: 'ActivityTeam',
      component: () => import('@/views/activity/ActivityTeamView.vue'),
    },
    {
      path: '/activity/members/:user_id',
      name: 'ActivityMember',
      component: () => import('@/views/activity/ActivityView.vue'),
    },
    {
      path: '/attribution',
      name: 'Attribution',
      redirect: activityRedirect,
    },
    {
      path: '/events',
      name: 'Events',
      redirect: activityRedirect,
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
      path: '/admin/relay-planning',
      name: 'AdminRelayPlanning',
      component: () => import('@/views/admin/RelayPlanningView.vue'),
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
      meta: { requireAdmin: true },
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
