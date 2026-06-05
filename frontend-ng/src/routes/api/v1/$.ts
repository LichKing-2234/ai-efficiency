import { createFileRoute } from '@tanstack/react-router'
import { proxyApiRequest } from '@/lib/api/server'

export const Route = createFileRoute('/api/v1/$')({
  server: {
    handlers: {
      GET: async ({ request, params }) => proxyApiRequest(request, `/api/v1/${params._splat ?? ''}`),
      POST: async ({ request, params }) => proxyApiRequest(request, `/api/v1/${params._splat ?? ''}`),
      PUT: async ({ request, params }) => proxyApiRequest(request, `/api/v1/${params._splat ?? ''}`),
      PATCH: async ({ request, params }) => proxyApiRequest(request, `/api/v1/${params._splat ?? ''}`),
      DELETE: async ({ request, params }) => proxyApiRequest(request, `/api/v1/${params._splat ?? ''}`)
    }
  }
})
