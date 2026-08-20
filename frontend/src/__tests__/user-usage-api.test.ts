import { describe, it, expect, vi, beforeEach } from 'vitest'

vi.mock('@/api/client', () => ({
  default: {
    get: vi.fn(),
  },
}))

import client from '@/api/client'
import { getUserUsageDashboard, getUserUsageGroupPoolUsage, getUserUsageGroupQuotas } from '@/api/userUsage'

const mockClient = client as unknown as {
  get: ReturnType<typeof vi.fn>
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('user usage API', () => {
  it('calls the usage-only dashboard endpoint with params and an AbortSignal', async () => {
		mockClient.get.mockResolvedValue({ data: { data: { configured: false, trend: [], models: [] } } })
		const controller = new AbortController()
		await getUserUsageDashboard({
      start_date: '2026-06-01',
      end_date: '2026-06-06',
      granularity: 'day',
      timezone: 'Asia/Shanghai',
		}, controller.signal)
		expect(mockClient.get).toHaveBeenCalledWith('/user/usage/dashboard', {
			params: {
        start_date: '2026-06-01',
        end_date: '2026-06-06',
        granularity: 'day',
				timezone: 'Asia/Shanghai',
				include_group_quotas: false,
			},
			signal: controller.signal,
		})
	})

	it('calls the fresh quota endpoint with params and an AbortSignal', async () => {
		mockClient.get.mockResolvedValue({ data: { data: { group_quotas: { status: 'empty', groups: [] } } } })
		const controller = new AbortController()
		await getUserUsageGroupQuotas({
			start_date: '2026-06-01',
			end_date: '2026-06-06',
			granularity: 'day',
			timezone: 'Asia/Shanghai',
		}, controller.signal)
		expect(mockClient.get).toHaveBeenCalledWith('/user/usage/group-quotas', {
			params: {
				start_date: '2026-06-01',
				end_date: '2026-06-06',
				granularity: 'day',
				timezone: 'Asia/Shanghai',
			},
			signal: controller.signal,
		})
	})

	it('calls the OAuth pool usage endpoint with params and an AbortSignal', async () => {
		mockClient.get.mockResolvedValue({ data: { data: { group_pool_usage: { status: 'empty', groups: [] } } } })
		const controller = new AbortController()
		await getUserUsageGroupPoolUsage({
			start_date: '2026-06-01',
			end_date: '2026-06-06',
			granularity: 'day',
			timezone: 'Asia/Shanghai',
		}, controller.signal)
		expect(mockClient.get).toHaveBeenCalledWith('/user/usage/group-pool-usage', {
			params: {
				start_date: '2026-06-01',
				end_date: '2026-06-06',
				granularity: 'day',
				timezone: 'Asia/Shanghai',
			},
			signal: controller.signal,
		})
	})
})
