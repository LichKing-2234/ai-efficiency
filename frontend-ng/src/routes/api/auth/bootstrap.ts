import { createFileRoute } from '@tanstack/react-router'
import { bootstrapFromGateway } from '@/lib/api/server'

export const Route = createFileRoute('/api/auth/bootstrap')({
  server: {
    handlers: {
      POST: async ({ request }) => bootstrapFromGateway(request)
    }
  }
})
