import type { RouteRecordName, Router } from 'vue-router'
import {
  onAuthExpiry,
  readBrowserSession,
  readLatestAuthExpiry,
} from '@/auth/browserSession'
import { useAuthStore } from '@/stores/auth'
import type { AuthExpiryEvent } from '@/auth/browserSession'
import type { User } from '@/types'

type PendingHydration = {
  navigationGeneration: number
  sessionGeneration: number
  fullPath: string
  routeName: RouteRecordName | null | undefined
  kind: 'login' | 'protected' | 'oauth-device'
  safeRedirect: string | null
  promise: Promise<User | null>
}

type NavigationAttempt = {
  navigationGeneration: number
  fullPath: string
  routeName: RouteRecordName | null | undefined
}

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

function sameRoute(
  route: { name?: RouteRecordName | null; fullPath: string },
  attempt: NavigationAttempt,
) {
  return route.name === attempt.routeName && route.fullPath === attempt.fullPath
}

export function installAuthNavigationGuards(router: Router): () => void {
  let navigationGeneration = 0
  let activeNavigation: NavigationAttempt | null = null
  let confirmedNavigation: NavigationAttempt | null = null
  let pendingHydration: PendingHydration | null = null
  let consumedAuthExpiryGeneration = -1
  let disposed = false

  const navigationIsPending = () => (
    activeNavigation !== null
    && activeNavigation.navigationGeneration !== confirmedNavigation?.navigationGeneration
  )

  const scheduleExpiryPolicy = (
    event: AuthExpiryEvent,
    expectedNavigation: NavigationAttempt | null = confirmedNavigation,
  ) => {
    if (
      !expectedNavigation
      || event.clearedGeneration <= consumedAuthExpiryGeneration
    ) {
      return
    }

    queueMicrotask(() => {
      if (disposed) {
        return
      }
      const latest = readLatestAuthExpiry()
      const session = readBrowserSession()
      if (
        !latest
        || latest.expiredGeneration !== event.expiredGeneration
        || latest.clearedGeneration !== event.clearedGeneration
        || event.clearedGeneration <= consumedAuthExpiryGeneration
        || session.generation !== event.clearedGeneration
        || session.accessToken
      ) {
        return
      }
      if (
        activeNavigation?.navigationGeneration !== expectedNavigation.navigationGeneration
        || confirmedNavigation?.navigationGeneration !== expectedNavigation.navigationGeneration
        || !sameRoute(router.currentRoute.value, expectedNavigation)
      ) {
        return
      }

      consumedAuthExpiryGeneration = event.clearedGeneration
      const route = router.currentRoute.value
      if (route.name === 'Login' || route.name === 'OAuthAuthorize') {
        return
      }
      if (!route.meta.redirectOnAuthExpiry && route.meta.public) {
        return
      }

      void router.replace({
        path: '/login',
        query: { redirect: resolveSafeRedirect(route.fullPath) },
      })
    })
  }

  const followHydration = (pending: PendingHydration, user: User | null) => {
    if (
      disposed
      || pendingHydration !== pending
      || activeNavigation?.navigationGeneration !== pending.navigationGeneration
      || confirmedNavigation?.navigationGeneration !== pending.navigationGeneration
      || !sameRoute(router.currentRoute.value, pending)
      || readBrowserSession().generation !== pending.sessionGeneration
    ) {
      return
    }

    if (pending.kind === 'login' && user) {
      pendingHydration = null
      void router.replace(pending.safeRedirect ?? '/')
    }
  }

  const startHydration = (
    attempt: NavigationAttempt,
    kind: PendingHydration['kind'],
    safeRedirect: string | null,
  ) => {
    const auth = useAuthStore()
    const sessionGeneration = readBrowserSession().generation
    const promise = auth.ensureUser()
    const pending: PendingHydration = {
      ...attempt,
      sessionGeneration,
      kind,
      safeRedirect,
      promise,
    }
    pendingHydration = pending
    void promise.then((user) => followHydration(pending, user))
  }

  const removeBeforeEach = router.beforeEach(async (to) => {
    const attempt: NavigationAttempt = {
      navigationGeneration: ++navigationGeneration,
      fullPath: to.fullPath,
      routeName: to.name,
    }
    activeNavigation = attempt

    const auth = useAuthStore()

    if (to.meta.requireAdmin) {
      if (!auth.isAuthenticated) {
        return { path: '/login', query: { redirect: to.fullPath } }
      }

      if (!auth.user) {
        const capturedNavigationGeneration = attempt.navigationGeneration
        const user = await auth.ensureUser()
        if (capturedNavigationGeneration !== navigationGeneration) {
          return undefined
        }
        if (!auth.isAuthenticated || !user) {
          return { path: '/login', query: { redirect: to.fullPath } }
        }
        if (user.role !== 'admin') {
          return { path: '/' }
        }
        return undefined
      }

      if (!auth.isAdmin) {
        return { path: '/' }
      }
      return undefined
    }

    if (to.name === 'Login') {
      const safeRedirect = resolveSafeRedirect(to.query.redirect)
      if (auth.isAuthenticated && auth.user) {
        return { path: safeRedirect }
      }
      if (auth.isAuthenticated) {
        startHydration(attempt, 'login', safeRedirect)
      }
      return undefined
    }

    if (!to.meta.public) {
      if (!auth.isAuthenticated) {
        return { path: '/login', query: { redirect: to.fullPath } }
      }
      if (!auth.user) {
        startHydration(attempt, 'protected', null)
      }
      return undefined
    }

    if (auth.isAuthenticated && !auth.user) {
      if (to.name === 'OAuthDevice') {
        startHydration(attempt, 'oauth-device', null)
      } else {
        void auth.ensureUser()
      }
    }
    return undefined
  })

  const removeAfterEach = router.afterEach((to, _from, failure) => {
    if (failure || !activeNavigation || !sameRoute(to, activeNavigation)) {
      return
    }

    confirmedNavigation = { ...activeNavigation }
    const latestExpiry = readLatestAuthExpiry()
    if (latestExpiry) {
      scheduleExpiryPolicy(latestExpiry, confirmedNavigation)
    }
    if (pendingHydration) {
      const pending = pendingHydration
      void pending.promise.then((user) => followHydration(pending, user))
    }
  })

  const removeExpiryListener = onAuthExpiry((event) => {
    if (navigationIsPending()) {
      return
    }
    scheduleExpiryPolicy(event)
  })

  return () => {
    disposed = true
    pendingHydration = null
    removeBeforeEach()
    removeAfterEach()
    removeExpiryListener()
  }
}
