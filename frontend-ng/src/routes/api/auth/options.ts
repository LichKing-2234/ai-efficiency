import { createFileRoute } from '@tanstack/react-router'
import { authOptionsFromBackend } from '@/lib/api/server'

export const Route = createFileRoute('/api/auth/options')({
  server: {
    handlers: {
      GET: async ({ request }) => authOptionsFromBackend(request)
    }
  }
})
