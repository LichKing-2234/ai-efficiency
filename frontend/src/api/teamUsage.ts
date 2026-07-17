import client from './client'
import type {
  ApiResponse,
  GetTeamUsageAuditResponse,
  ListTeamUsageSubjectsResponse,
  SelectedSubjectUsageSnapshot,
  TeamUsageAuditParams,
  TeamUsageMembersParams,
  TeamUsageMembersResponse,
  TeamUsageOverviewParams,
  TeamUsageSummaryResponse,
  TeamUsageTrendResponse,
  TeamOverviewResponse,
  TeamUsageScopeResponse,
  UpdateTeamUsageRateMultiplierRequest,
  UpdateTeamUsageRateMultiplierResponse,
  UserUsageDashboardParams,
} from '@/types'

export function getTeamUsageScope() {
  return client.get<ApiResponse<TeamUsageScopeResponse>>('/user/team-usage/scope')
}

export function listTeamUsageSubjects(params?: { q?: string; page?: number; page_size?: number }) {
  return client.get<ApiResponse<ListTeamUsageSubjectsResponse>>('/user/team-usage/subjects', { params })
}

export function getTeamUsageSubjectDashboard(userID: number, params: UserUsageDashboardParams) {
  return client.get<ApiResponse<SelectedSubjectUsageSnapshot>>(`/user/team-usage/subjects/${userID}/usage/dashboard`, { params })
}

export function getTeamUsageOverview(params?: TeamUsageOverviewParams) {
  return client.get<ApiResponse<TeamOverviewResponse>>('/user/team-usage/overview', {
    params,
    timeout: 45000,
  })
}

export function getTeamUsageSummary(params?: TeamUsageOverviewParams) {
  return client.get<ApiResponse<TeamUsageSummaryResponse>>('/user/team-usage/summary', { params })
}

export function getTeamUsageTrend(params?: TeamUsageOverviewParams) {
  return client.get<ApiResponse<TeamUsageTrendResponse>>('/user/team-usage/trend', {
    params,
    timeout: 45000,
  })
}

export function getTeamUsageMembers(params?: TeamUsageMembersParams) {
  return client.get<ApiResponse<TeamUsageMembersResponse>>('/user/team-usage/members', {
    params,
    timeout: 45000,
  })
}

export function updateTeamUsageRateMultiplier(
  userID: number,
  groupID: string,
  data: UpdateTeamUsageRateMultiplierRequest,
) {
  return client.put<ApiResponse<UpdateTeamUsageRateMultiplierResponse>>(
    `/user/team-usage/subjects/${userID}/groups/${groupID}/rate-multiplier`,
    data,
  )
}

export function getTeamUsageAudit(params?: TeamUsageAuditParams) {
  return client.get<ApiResponse<GetTeamUsageAuditResponse>>('/user/team-usage/audit', { params })
}
