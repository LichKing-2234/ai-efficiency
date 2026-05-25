import { describe, it, expect, vi, beforeEach } from 'vitest'

vi.mock('@/api/client', () => {
  return {
    default: {
      get: vi.fn(),
      post: vi.fn(),
      put: vi.fn(),
      delete: vi.fn(),
    },
  }
})

import client from '@/api/client'
import { getEventSummary, getEventDetail, listEvents, searchEventUsers } from '@/api/events'

const mockClient = client as unknown as {
  get: ReturnType<typeof vi.fn>
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('events API', () => {
  it('listEvents calls GET /events with query params', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { items: [], total: 0, page: 0, page_size: 20 } } })
    await listEvents({ tool: 'claude', limit: 20 })
    expect(mockClient.get).toHaveBeenCalledWith('/events', {
      params: { tool: 'claude', limit: 20 },
    })
  })

  it('getEventSummary calls GET /events/summary with query params', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { total_events: 1, bound_events: 1, unbound_events: 0, tool_counts: [] } } })
    await getEventSummary({ binding_status: 'bound' })
    expect(mockClient.get).toHaveBeenCalledWith('/events/summary', {
      params: { binding_status: 'bound' },
    })
  })

  it('getEventDetail calls GET /events/:id', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { id: 12 } } })
    await getEventDetail(12)
    expect(mockClient.get).toHaveBeenCalledWith('/events/12')
  })

  it('searchEventUsers calls GET /events/users with query params', async () => {
    mockClient.get.mockResolvedValue({ data: { data: [] } })
    await searchEventUsers({ q: 'alice@example.com', limit: 20 })
    expect(mockClient.get).toHaveBeenCalledWith('/events/users', {
      params: { q: 'alice@example.com', limit: 20 },
    })
  })
})
