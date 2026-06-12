import { createFileRoute } from '@tanstack/react-router'
import { createServerFn } from '@tanstack/react-start'
import { getAuthOptionsTarget } from '@/lib/api/server'
import type { AuthOptions } from '@/lib/api/types'
import { LoginPage } from '@/features/auth/login-page'

const getLoginBootstrap = createServerFn({ method: 'GET' })
  .handler(async () => {
    let authOptions: AuthOptions | null = null

    try {
      const response = await fetch(getAuthOptionsTarget(new Request('http://localhost/login')), {
        method: 'GET',
        headers: { Accept: 'application/json' },
        redirect: 'manual'
      })
      if (response.ok) {
        const payload = await response.json() as { data?: AuthOptions }
        authOptions = payload.data ?? null
      }
    } catch {
      authOptions = null
    }

    return { localHandoffHref: '/oauth2/local' as string | null, authOptions }
  })

export const Route = createFileRoute('/login')({
  loader: () => getLoginBootstrap(),
  component: LoginRouteComponent
})

function LoginRouteComponent() {
  const data = Route.useLoaderData()
  return <LoginPage initialOptions={data.authOptions} localHandoffHref={data.localHandoffHref} />
}
