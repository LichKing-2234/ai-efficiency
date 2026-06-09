import { createFileRoute } from '@tanstack/react-router'
import { localHandoffCallbackResponse } from '@/lib/auth/local-handoff.server'

export const Route = createFileRoute('/api/local/callback')({
  server: {
    handlers: {
      GET: async ({ request }) => localHandoffCallbackResponse(request)
    }
  }
})
