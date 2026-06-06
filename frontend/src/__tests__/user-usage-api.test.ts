import { describe, it, expect, vi, beforeEach } from 'vitest'

vi.mock('@/api/client', () => ({
  default: {
    get: vi.fn(),
  },
}))

import client from '@/api/client'
import { getUserUsageDashboard } from '@/api/userUsage'

const mockClient = client as unknown as {
  get: ReturnType<typeof vi.fn>
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('user usage API', () => {
  it('calls the dashboard snapshot endpoint with params', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { configured: false, trend: [], models: [] } } })
    await getUserUsageDashboard({
      start_date: '2026-06-01',
      end_date: '2026-06-06',
      granularity: 'day',
      timezone: 'Asia/Shanghai',
    })
    expect(mockClient.get).toHaveBeenCalledWith('/user/usage/dashboard', {
      params: {
        start_date: '2026-06-01',
        end_date: '2026-06-06',
        granularity: 'day',
        timezone: 'Asia/Shanghai',
      },
    })
  })
})
