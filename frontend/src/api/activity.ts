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
