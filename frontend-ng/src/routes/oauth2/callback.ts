import { createFileRoute } from '@tanstack/react-router'
import { oauth2CallbackResponse } from '@/lib/auth/local-handoff.server'

export const Route = createFileRoute('/oauth2/callback')({
  server: {
    handlers: {
      GET: async ({ request }) => oauth2CallbackResponse(request)
    }
  }
})
