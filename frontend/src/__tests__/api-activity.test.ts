import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/api/client', () => ({
  default: { get: vi.fn() },
}))

import client from '@/api/client'
import {
  getActivityBucket,
  getActivityMember,
  getActivityRepository,
  getActivityScope,
  getActivitySummary,
  getActivityTeam,
  listActivityMembers,
  normalizeMemberActivity,
} from '@/api/activity'

describe('Activity API', () => {
  beforeEach(() => vi.clearAllMocks())

  it('uses separate bounded Activity endpoints', async () => {
    vi.mocked(client.get).mockResolvedValue({ data: { data: {} } } as any)
    await getActivityScope()
    await getActivitySummary({ from: '2026-08-01T00:00:00Z', to: '2026-08-31T00:00:00Z' })
    await listActivityMembers({ limit: 25, cursor: 'member-cursor' })
    await getActivityMember(7, { pr_limit: 10, pr_cursor: 'pr-cursor' })
    await getActivityTeam('team alpha', { limit: 20 })
    await getActivityRepository(9, { commit_limit: 10 })
    await getActivityBucket('bucket/a')

    expect(client.get).toHaveBeenNthCalledWith(1, '/activity/scope')
    expect(client.get).toHaveBeenNthCalledWith(2, '/activity/summary', { params: { from: '2026-08-01T00:00:00Z', to: '2026-08-31T00:00:00Z' } })
    expect(client.get).toHaveBeenNthCalledWith(3, '/activity/members', { params: { limit: 25, cursor: 'member-cursor' } })
    expect(client.get).toHaveBeenNthCalledWith(4, '/activity/members/7', { params: { pr_limit: 10, pr_cursor: 'pr-cursor' } })
    expect(client.get).toHaveBeenNthCalledWith(5, '/activity/teams/team%20alpha', { params: { limit: 20 } })
    expect(client.get).toHaveBeenNthCalledWith(6, '/activity/repos/9', { params: { commit_limit: 10 } })
    expect(client.get).toHaveBeenNthCalledWith(7, '/activity/buckets/bucket%2Fa')
  })

  it('normalizes all member detail collections to arrays', () => {
    const normalized = normalizeMemberActivity({
      prs: { items: null },
      commits: { items: undefined },
      buckets: null,
    } as any)
    expect(normalized.prs.items).toEqual([])
    expect(normalized.commits.items).toEqual([])
    expect(normalized.buckets.items).toEqual([])
  })
})
