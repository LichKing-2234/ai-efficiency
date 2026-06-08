import { createFileRoute } from '@tanstack/react-router'
import { loginThroughBackend } from '@/lib/api/server'

export const Route = createFileRoute('/api/auth/login')({
  server: {
    handlers: {
      POST: async ({ request }) => loginThroughBackend(request)
    }
  }
})
