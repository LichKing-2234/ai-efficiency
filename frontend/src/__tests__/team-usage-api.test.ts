import { beforeEach, describe, expect, expectTypeOf, it, vi } from 'vitest'

vi.mock('@/api/client', () => ({
  default: {
    get: vi.fn(),
    put: vi.fn(),
  },
}))

import client from '@/api/client'
import {
  getTeamUsageAudit,
  getTeamUsageOverview,
  getTeamUsageScope,
  getTeamUsageSubjectDashboard,
  listTeamUsageSubjects,
  updateTeamUsageRateMultiplier,
} from '@/api/teamUsage'
import type { TeamUsageAuditParams, TeamUsageAuditRecord, TeamUsageOverviewParams } from '@/types'

const mockClient = client as unknown as {
  get: ReturnType<typeof vi.fn>
  put: ReturnType<typeof vi.fn>
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('team usage API', () => {
  it('exports explicit team usage request param contracts', () => {
    expectTypeOf<TeamUsageOverviewParams>().toMatchTypeOf<{
      start_date?: string
      end_date?: string
      granularity?: string
      timezone?: string
      page?: number
      page_size?: number
    }>()

    expectTypeOf<TeamUsageAuditParams>().toMatchTypeOf<{
      page?: number
      page_size?: number
      target_user_id?: number
    }>()

    expectTypeOf<TeamUsageAuditRecord['reason']>().toEqualTypeOf<string>()
  })

  it('fetches representative scope', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { is_representative: true, departments: [] } } })

    await getTeamUsageScope()

    expect(mockClient.get).toHaveBeenCalledWith('/user/team-usage/scope')
  })

  it('lists team usage subjects with params', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { page: 1, page_size: 20, total: 1, subjects: [] } } })

    await listTeamUsageSubjects({ q: 'Alice', page: 1, page_size: 20 })

    expect(mockClient.get).toHaveBeenCalledWith('/user/team-usage/subjects', {
      params: { q: 'Alice', page: 1, page_size: 20 },
    })
  })

  it('fetches a selected subject dashboard', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { configured: true } } })

    await getTeamUsageSubjectDashboard(101, {
      start_date: '2026-06-01',
      end_date: '2026-06-07',
      granularity: 'day',
      timezone: 'Asia/Shanghai',
    })

    expect(mockClient.get).toHaveBeenCalledWith('/user/team-usage/subjects/101/usage/dashboard', {
      params: {
        start_date: '2026-06-01',
        end_date: '2026-06-07',
        granularity: 'day',
        timezone: 'Asia/Shanghai',
      },
    })
  })

  it('fetches the team overview', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { configured: true } } })

    await getTeamUsageOverview({
      start_date: '2026-06-01',
      end_date: '2026-06-30',
      granularity: 'day',
      timezone: 'Asia/Shanghai',
      page: 1,
      page_size: 10,
    })

    expect(mockClient.get).toHaveBeenCalledWith('/user/team-usage/overview', {
      params: {
        start_date: '2026-06-01',
        end_date: '2026-06-30',
        granularity: 'day',
        timezone: 'Asia/Shanghai',
        page: 1,
        page_size: 10,
      },
      timeout: 45000,
    })
  })

  it('updates member group multiplier', async () => {
    mockClient.put.mockResolvedValue({ data: { data: { status: 'succeeded', changed: true } } })

    await updateTeamUsageRateMultiplier(101, '42', {
      mode: 'set',
      rate_multiplier: 2,
      reason: 'Reduce allowance for Group Alpha',
    })

    expect(mockClient.put).toHaveBeenCalledWith('/user/team-usage/subjects/101/groups/42/rate-multiplier', {
      mode: 'set',
      rate_multiplier: 2,
      reason: 'Reduce allowance for Group Alpha',
    })
  })

  it('fetches team usage audit entries with paging params', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { items: [], page: 1, page_size: 20, total: 0 } } })

    await getTeamUsageAudit({ page: 1, page_size: 20, target_user_id: 101 })

    expect(mockClient.get).toHaveBeenCalledWith('/user/team-usage/audit', {
      params: { page: 1, page_size: 20, target_user_id: 101 },
    })
  })
})
