import { createFileRoute } from '@tanstack/react-router'
import { proxyOAuthRequest } from '@/lib/api/server'

export const Route = createFileRoute('/oauth/authorize/approve')({
  server: {
    handlers: {
      POST: async ({ request }) => proxyOAuthRequest(request, '/oauth/authorize/approve')
    }
  }
})
