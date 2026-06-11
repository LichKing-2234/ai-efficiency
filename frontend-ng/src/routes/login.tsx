import { createFileRoute } from '@tanstack/react-router'
import { LoginPage } from '@/features/auth/login-page'

export const Route = createFileRoute('/login')({
  loader: ({ location }) => {
    const backendOrigin = (
      process.env.AE_FRONTEND_BACKEND_URL ||
      process.env.VITE_BACKEND_URL ||
      null
    )?.trim()
    if (!backendOrigin) {
      return { localHandoffHref: null }
    }

    try {
      const handoffUrl = new URL('/oauth2/local', backendOrigin)
      const currentUrl = new URL(location.href)
      handoffUrl.searchParams.set('target', currentUrl.origin)
      return { localHandoffHref: handoffUrl.toString() }
    } catch {
      return { localHandoffHref: null }
    }
  },
  component: LoginRouteComponent
})

function LoginRouteComponent() {
  const data = Route.useLoaderData()
  return <LoginPage localHandoffHref={data.localHandoffHref} />
}
