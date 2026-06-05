import { createFileRoute } from '@tanstack/react-router'
import { logoutResponse } from '@/lib/api/server'

export const Route = createFileRoute('/api/auth/logout')({
  server: {
    handlers: {
      POST: async ({ request }) => logoutResponse(request)
    }
  }
})
