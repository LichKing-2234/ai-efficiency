import client from './client'
import type { ApiResponse } from '@/types'
import type {
  ActivityV2Overview,
  ActivityV2Page,
  ActivityV2PageQuery,
  ActivityV2PullRequestRow,
  ActivityV2Query,
  ActivityV2RepositoryRow,
  ActivityV2TeamMemberAvailability,
  ActivityV2TeamMemberAvailabilityQuery,
} from '@/types/activity'

export function getActivityV2Overview(params: ActivityV2Query) {
  return client.get<ApiResponse<ActivityV2Overview>>('/activity/v2/overview', { params })
}

export function listActivityV2Repositories(params: ActivityV2PageQuery) {
  return client.get<ApiResponse<ActivityV2Page<ActivityV2RepositoryRow>>>('/activity/v2/repositories', { params })
}

export function listActivityV2PullRequests(params: ActivityV2PageQuery) {
  return client.get<ApiResponse<ActivityV2Page<ActivityV2PullRequestRow>>>('/activity/v2/pull-requests', { params })
}

export function getActivityV2TeamMemberAvailability(teamID: string, query: ActivityV2TeamMemberAvailabilityQuery) {
  const { user_ids: userIDs, ...params } = query
  return client.get<ApiResponse<ActivityV2TeamMemberAvailability>>(
    `/activity/v2/teams/${encodeURIComponent(teamID)}/member-availability`,
    { params: { ...params, user_ids: userIDs.length > 0 ? userIDs.join(',') : undefined } },
  )
}
