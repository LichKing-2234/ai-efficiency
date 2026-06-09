import { createFileRoute } from '@tanstack/react-router'
import { localHandoffIssueResponse } from '@/lib/auth/local-handoff.server'

export const Route = createFileRoute('/api/local/')({
  server: {
    handlers: {
      GET: async ({ request }) => localHandoffIssueResponse(request)
    }
  }
})
