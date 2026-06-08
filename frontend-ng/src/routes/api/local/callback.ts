import { createFileRoute } from '@tanstack/react-router'
import { json } from '@/lib/api/server'

export const Route = createFileRoute('/api/local/callback')({
  server: {
    handlers: {
      GET: async ({ request }) => {
        const url = new URL(request.url)
        const code = url.searchParams.get('code')
        if (!code) {
          return json({ code: 400, message: 'handoff code is required' }, 400)
        }
        return json({
          code: 501,
          message: 'local handoff redeem requires backend one-time code support before it can be enabled'
        }, 501)
      }
    }
  }
})
