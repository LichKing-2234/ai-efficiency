import { createFileRoute } from '@tanstack/react-router'
import { json } from '@/lib/api/server'
import { appendTokenCookies } from '@/lib/auth/cookies'

export const Route = createFileRoute('/api/local/callback')({
  server: {
    handlers: {
      GET: async ({ request }) => {
        const url = new URL(request.url)
        const accessToken = url.searchParams.get('access_token')
        const refreshToken = url.searchParams.get('refresh_token') || undefined
        if (!accessToken && !refreshToken) {
          return json({ code: 400, message: 'app token is required' }, 400)
        }
        const headers = new Headers({ Location: '/' })
        appendTokenCookies(headers, {
          accessToken: accessToken || undefined,
          refreshToken
        }, request)
        return new Response(null, { status: 302, headers })
      }
    }
  }
})
