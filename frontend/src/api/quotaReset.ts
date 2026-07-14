import client from './client'
import type {
  ApiResponse,
  QuotaResetApprovalChainInput,
  QuotaResetApprovalChainListResponse,
  QuotaResetApprovalChainOptionsResponse,
  QuotaResetApproverCandidateListResponse,
  QuotaResetApproverConfigInput,
  QuotaResetApproverConfigListResponse,
  QuotaResetNotificationSettings,
  QuotaResetNotificationSettingsInput,
  QuotaResetNotificationTestResult,
  QuotaResetOptionsResponse,
  QuotaResetRequestListResponse,
  QuotaResetRequestSummary,
} from '@/types'

export interface QuotaResetListParams {
  page?: number
  page_size?: number
  status?: string
}

export type QuotaResetApproverConfigSaveMode = 'replace_departments' | 'replace_all'

export interface QuotaResetApproverCandidateParams {
  source_id?: number
  q?: string
  page?: number
  page_size?: number
}

export interface QuotaResetWorkflowDecisionInput {
  request_node_id: number
  decision_reason: string
}

export type QuotaResetApproveInput =
  | QuotaResetWorkflowDecisionInput
  | { request_node_id?: never; decision_reason?: string }

export type QuotaResetRejectInput =
  | QuotaResetWorkflowDecisionInput
  | { request_node_id?: never; decision_reason: string }

export function getQuotaResetOptions() {
  return client.get<ApiResponse<QuotaResetOptionsResponse>>('/user/quota-reset/options')
}

export function createQuotaResetRequest(data: { group_id: string; reason: string }) {
  return client.post<ApiResponse<QuotaResetRequestSummary>>('/user/quota-reset/requests', data)
}

export function listMyQuotaResetRequests(params?: QuotaResetListParams) {
  return client.get<ApiResponse<QuotaResetRequestListResponse>>('/user/quota-reset/requests', { params })
}

export function cancelQuotaResetRequest(id: number) {
  return client.post<ApiResponse<QuotaResetRequestSummary>>(`/user/quota-reset/requests/${id}/cancel`)
}

export function listQuotaResetApprovals(params?: QuotaResetListParams) {
  return client.get<ApiResponse<QuotaResetRequestListResponse>>('/user/quota-reset/approvals', { params })
}

export function approveQuotaResetRequest(id: number, data: QuotaResetApproveInput = {}) {
  return client.post<ApiResponse<QuotaResetRequestSummary>>(`/user/quota-reset/approvals/${id}/approve`, data)
}

export function rejectQuotaResetRequest(id: number, data: QuotaResetRejectInput) {
  return client.post<ApiResponse<QuotaResetRequestSummary>>(`/user/quota-reset/approvals/${id}/reject`, data)
}

export function retryQuotaResetRequest(id: number) {
  return client.post<ApiResponse<QuotaResetRequestSummary>>(`/user/quota-reset/approvals/${id}/retry-reset`)
}

export function listAdminQuotaResetRequests(params?: QuotaResetListParams) {
  return client.get<ApiResponse<QuotaResetRequestListResponse>>('/admin/quota-reset/requests', { params })
}

export function adminApproveQuotaResetRequest(id: number, data: QuotaResetApproveInput = {}) {
  return client.post<ApiResponse<QuotaResetRequestSummary>>(`/admin/quota-reset/requests/${id}/approve`, data)
}

export function adminRejectQuotaResetRequest(id: number, data: QuotaResetRejectInput) {
  return client.post<ApiResponse<QuotaResetRequestSummary>>(`/admin/quota-reset/requests/${id}/reject`, data)
}

export function adminRetryQuotaResetRequest(id: number) {
  return client.post<ApiResponse<QuotaResetRequestSummary>>(`/admin/quota-reset/requests/${id}/retry-reset`)
}

export function getQuotaResetApproverConfigs() {
  return client.get<ApiResponse<QuotaResetApproverConfigListResponse>>('/admin/quota-reset/approver-configs')
}

export function listQuotaResetApproverCandidates(params: QuotaResetApproverCandidateParams) {
  return client.get<ApiResponse<QuotaResetApproverCandidateListResponse>>('/admin/quota-reset/approver-candidates', { params })
}

export function saveQuotaResetApproverConfigs(
  items: QuotaResetApproverConfigInput[],
  mode: QuotaResetApproverConfigSaveMode = 'replace_departments',
) {
  return client.put<ApiResponse<QuotaResetApproverConfigListResponse>>('/admin/quota-reset/approver-configs', { items, mode })
}

export function getQuotaResetApprovalChains() {
  return client.get<ApiResponse<QuotaResetApprovalChainListResponse>>('/admin/quota-reset/approval-chains')
}

export function getQuotaResetApprovalChainOptions() {
  return client.get<ApiResponse<QuotaResetApprovalChainOptionsResponse>>('/admin/quota-reset/approval-chain-options')
}

export function saveQuotaResetApprovalChains(items: QuotaResetApprovalChainInput[]) {
  return client.put<ApiResponse<QuotaResetApprovalChainListResponse>>('/admin/quota-reset/approval-chains', { items })
}

export function getQuotaResetNotificationSettings() {
  return client.get<ApiResponse<QuotaResetNotificationSettings>>('/admin/quota-reset/notification-settings')
}

export function updateQuotaResetNotificationSettings(data: QuotaResetNotificationSettingsInput) {
  return client.put<ApiResponse<QuotaResetNotificationSettings>>('/admin/quota-reset/notification-settings', data)
}

export function testQuotaResetNotificationSettings() {
  return client.post<ApiResponse<QuotaResetNotificationTestResult>>('/admin/quota-reset/notification-settings/test')
}
