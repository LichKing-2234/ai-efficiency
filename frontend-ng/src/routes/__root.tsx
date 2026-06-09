import { QueryClientProvider, useQuery } from '@tanstack/react-query'
import { createRootRoute, HeadContent, Outlet, Scripts, useLocation, useNavigate } from '@tanstack/react-router'
import { useEffect } from 'react'
import { Toaster } from 'sonner'
import { AppShell } from '@/components/layout/app-shell'
import { LoadingState } from '@/components/primitives/data-state'
import { queryClient } from '@/query-client'
import { ensureAuthenticatedUser } from '@/lib/auth/session'
import { I18nProvider, useI18n } from '@/lib/i18n/i18n'
import stylesUrl from '../styles.css?url'

export const Route = createRootRoute({
  head: () => ({
    meta: [
      { charSet: 'utf-8' },
      { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      { title: 'AI Efficiency' }
    ]
  }),
  component: RootComponent,
  shellComponent: RootDocument
})

function RootComponent() {
  return (
    <QueryClientProvider client={queryClient}>
      <I18nProvider>
        <AuthFrame>
          <Outlet />
        </AuthFrame>
        <Toaster />
      </I18nProvider>
    </QueryClientProvider>
  )
}

function AuthFrame({ children }: { children: React.ReactNode }) {
  const location = useLocation()
  const navigate = useNavigate()
  const { t } = useI18n()
  const isPublic = location.pathname === '/login' || location.pathname.startsWith('/oauth/') || location.pathname.startsWith('/oauth2/')
  const { data: user, isLoading, error, refetch } = useQuery({
    queryKey: ['auth', 'me'],
    queryFn: ensureAuthenticatedUser,
    enabled: !isPublic
  })

  useEffect(() => {
    if (!isPublic && error) {
      void navigate({ to: '/login', search: { redirect: location.href } as never })
    }
  }, [error, isPublic, location.href, navigate])

  if (isPublic) return <>{children}</>
  if (isLoading && !user) return <AppShell user={null}><LoadingState label={t('auth.loadingAccount')} /></AppShell>
  return <AppShell user={user ?? null}>{children}</AppShell>
}

function RootDocument({ children }: { children: React.ReactNode }) {
  return (
    <html lang='en' suppressHydrationWarning>
      <head>
        <link rel='stylesheet' href={stylesUrl} />
        <HeadContent />
      </head>
      <body>
        <div id='app-content'>{children}</div>
        <Scripts />
      </body>
    </html>
  )
}
