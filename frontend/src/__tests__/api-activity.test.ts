import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/api/client', () => ({ default: { get: vi.fn() } }))

import client from '@/api/client'
import {
  getActivityV2Overview,
  getActivityV2TeamMemberAvailability,
  listActivityV2PullRequests,
  listActivityV2Repositories,
} from '@/api/activity'

describe('Activity API', () => {
  beforeEach(() => vi.clearAllMocks())

  it('uses only formal-v2 Activity endpoints', async () => {
    vi.mocked(client.get).mockResolvedValue({ data: { data: {} } } as any)
    const query = { scope: 'team' as const, team_id: 'team alpha', from: '2026-08-01', to: '2026-08-31', timezone: 'UTC' }
    await getActivityV2Overview(query)
    await listActivityV2Repositories({ ...query, sort: 'tokens' })
    await listActivityV2PullRequests({ ...query, sort: 'name' })
    await getActivityV2TeamMemberAvailability('team alpha', { from: query.from, to: query.to, timezone: query.timezone, user_ids: [7, 8] })

    expect(client.get).toHaveBeenNthCalledWith(1, '/activity/v2/overview', { params: query })
    expect(client.get).toHaveBeenNthCalledWith(2, '/activity/v2/repositories', { params: { ...query, sort: 'tokens' } })
    expect(client.get).toHaveBeenNthCalledWith(3, '/activity/v2/pull-requests', { params: { ...query, sort: 'name' } })
    expect(client.get).toHaveBeenNthCalledWith(4, '/activity/v2/teams/team%20alpha/member-availability', {
      params: { from: '2026-08-01', to: '2026-08-31', timezone: 'UTC', user_ids: '7,8' },
    })
  })
})
