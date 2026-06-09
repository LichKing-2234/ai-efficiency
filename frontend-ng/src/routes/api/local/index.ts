import { createFileRoute } from '@tanstack/react-router'
import { json } from '@/lib/api/server'
import { readAppTokens } from '@/lib/auth/cookies'
import { buildLocalCallbackUrl, isAllowedLocalTarget } from '@/lib/auth/local-handoff'

export const Route = createFileRoute('/api/local/')({
  server: {
    handlers: {
      GET: async ({ request }) => {
        const url = new URL(request.url)
        const target = url.searchParams.get('target') || 'http://localhost:3000'
        if (!isAllowedLocalTarget(target)) {
          return json({ code: 400, message: 'local target must be localhost' }, 400)
        }
        const tokens = readAppTokens(request)
        if (!tokens?.accessToken && !tokens?.refreshToken) {
          return json({ code: 401, message: 'local handoff requires an active app session' }, 401)
        }
        return Response.redirect(buildLocalCallbackUrl(target, tokens.accessToken, tokens.refreshToken), 302)
      }
    }
  }
})
