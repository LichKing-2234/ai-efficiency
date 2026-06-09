import { createFileRoute } from '@tanstack/react-router'
import { oauth2LocalHandoffResponse } from '@/lib/auth/local-handoff.server'

export const Route = createFileRoute('/oauth2/local')({
  server: {
    handlers: {
      GET: async ({ request }) => oauth2LocalHandoffResponse(request)
    }
  }
})
