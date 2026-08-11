import client from './client'
import type { ApiResponse } from '@/types'
import type {
  ActivityBucketDetail,
  ActivityDetailParams,
  ActivityMemberResponse,
  ActivityMembersResponse,
  ActivityPageParams,
  ActivityRepositoryParams,
  ActivityRepositoryResponse,
  ActivityScopeResponse,
  ActivityTeamResponse,
  ActivityWindowParams,
  ActivityV2Overview,
  ActivityV2Page,
  ActivityV2PageQuery,
  ActivityV2PullRequestRow,
  ActivityV2Query,
  ActivityV2RepositoryRow,
} from '@/types/activity'

export function getActivityScope() {
  return client.get<ApiResponse<ActivityScopeResponse>>('/activity/scope')
}

export function getActivitySummary(params?: ActivityDetailParams) {
  return client.get<ApiResponse<ActivityMemberResponse>>('/activity/summary', { params })
}

export function listActivityMembers(params?: ActivityPageParams) {
  return client.get<ApiResponse<ActivityMembersResponse>>('/activity/members', { params })
}

export function getActivityMember(userID: number, params?: ActivityDetailParams) {
  return client.get<ApiResponse<ActivityMemberResponse>>(`/activity/members/${userID}`, { params })
}

export function getActivityTeam(teamID: string, params?: ActivityPageParams) {
  return client.get<ApiResponse<ActivityTeamResponse>>(`/activity/teams/${encodeURIComponent(teamID)}`, { params })
}

export function getActivityRepository(repoID: number, params?: ActivityRepositoryParams) {
  return client.get<ApiResponse<ActivityRepositoryResponse>>(`/activity/repos/${repoID}`, { params })
}

export function getActivityBucket(bucketID: string) {
  return client.get<ApiResponse<ActivityBucketDetail>>(`/activity/buckets/${encodeURIComponent(bucketID)}`)
}

export function getActivityV2Overview(params: ActivityV2Query) {
  return client.get<ApiResponse<ActivityV2Overview>>('/activity/v2/overview', { params })
}

export function listActivityV2Repositories(params: ActivityV2PageQuery) {
  return client.get<ApiResponse<ActivityV2Page<ActivityV2RepositoryRow>>>('/activity/v2/repositories', { params })
}

export function listActivityV2PullRequests(params: ActivityV2PageQuery) {
  return client.get<ApiResponse<ActivityV2Page<ActivityV2PullRequestRow>>>('/activity/v2/pull-requests', { params })
}

function page<T>(value: { items?: T[] | null; next_cursor?: string } | null | undefined) {
  return { items: value?.items ?? [], next_cursor: value?.next_cursor }
}

export function normalizeMemberActivity(value: ActivityMemberResponse): ActivityMemberResponse {
  return {
    ...value,
    prs: page(value.prs),
    commits: page(value.commits),
    buckets: page(value.buckets),
  }
}

export function normalizeScope(value: ActivityScopeResponse): ActivityScopeResponse {
  return { ...value, teams: value.teams ?? [] }
}

export function normalizeMembers(value: ActivityMembersResponse): ActivityMembersResponse {
  return { ...value, members: page(value.members) }
}

export function normalizeTeam(value: ActivityTeamResponse): ActivityTeamResponse {
  return { ...value, members: page(value.members) }
}

export function normalizeRepository(value: ActivityRepositoryResponse): ActivityRepositoryResponse {
  return { ...value, members: page(value.members), prs: page(value.prs), commits: page(value.commits) }
}

export type { ActivityWindowParams }
