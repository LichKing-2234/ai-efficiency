import { createFileRoute } from '@tanstack/react-router'
import { json } from '@/lib/api/server'

function isAllowedLocalTarget(value: string | null) {
  if (!value) return false
  try {
    const url = new URL(value)
    return ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)
  } catch {
    return false
  }
}

export const Route = createFileRoute('/api/local/')({
  server: {
    handlers: {
      GET: async ({ request }) => {
        const url = new URL(request.url)
        const target = url.searchParams.get('target') || 'http://localhost:3000'
        if (!isAllowedLocalTarget(target)) {
          return json({ code: 400, message: 'local target must be localhost' }, 400)
        }
        return json({
          code: 501,
          message: 'local handoff requires backend one-time code support before it can be enabled',
          target
        }, 501)
      }
    }
  }
})
