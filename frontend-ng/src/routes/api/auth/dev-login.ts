import { createFileRoute } from '@tanstack/react-router'
import { devLoginThroughBackend } from '@/lib/api/server'

export const Route = createFileRoute('/api/auth/dev-login')({
  server: {
    handlers: {
      POST: async ({ request }) => devLoginThroughBackend(request)
    }
  }
})
