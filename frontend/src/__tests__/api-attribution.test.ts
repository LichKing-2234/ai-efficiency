import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/api/client', () => ({
  default: { get: vi.fn() },
}))

import client from '@/api/client'
import { getReportingReadiness } from '@/api/attribution'

describe('attribution API', () => {
  beforeEach(() => vi.clearAllMocks())

  it('loads the authenticated aggregate reporting status', async () => {
    vi.mocked(client.get).mockResolvedValue({ data: { data: { state: 'active', retryable: false } } } as any)
    await getReportingReadiness()
    expect(client.get).toHaveBeenCalledWith('/attribution/status')
  })
})
