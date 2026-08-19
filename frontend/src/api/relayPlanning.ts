import client from './client'
import type { ApiResponse } from '@/types'

export interface RelayPlanningCandidate {
  user_id: number
  relay_user_id: number
  username: string
  email: string
  range_cost: number
  range_tokens: number
  global_token_rank: number
  current_group_ids?: number[]
  migratable_key_count: number
  source_member: boolean
  can_add: boolean
  selected: boolean
  eligible: boolean
  warnings?: string[]
}

export interface RelayPlanningAssignment {
  index: number
  total_cost: number
  user_ids: number[]
  target_group_id?: number
  target_group_name?: string
}

export interface RelayPlanningPlan {
  provider_id: number
  department_id: string
  department_name: string
  platform: string
  template_group_id: number
  template_group_name: string
  source_group_id: number
  source_group_name: string
  weekly_cost_target: number
  recommended_group_count: number
  group_count: number
  candidates: RelayPlanningCandidate[]
  assignments: RelayPlanningAssignment[]
  warnings?: string[]
  generated_at: string
  mapping_id?: number
}

export interface RelayPlanningMapping {
  id: number
  provider_id: number
  department_id: string
  department_name: string
  platform: string
  template_group_id: number
  template_group_name: string
  source_group_id: number
  source_group_name: string
  group_ids: number[]
  status: string
  weekly_cost_target: number
  member_assignments?: Record<string, number>
  member_sources?: Record<string, number>
  warnings?: string[]
  updated_at: string
}

export interface RelayPlanningExecution {
  plan: RelayPlanningPlan
  groups: Array<{ index: number; id?: number; name?: string; status: string; error?: string }>
  members: Array<{ user_id: number; target_group_id?: number; subscription: string; source_removal: string; api_keys?: string[]; error?: string }>
  mapping?: RelayPlanningMapping
  warnings?: string[]
}

export interface RelayPlanningRequest {
  provider_id: number
  department_id: string
  platform: string
  template_group_id: number
  source_group_id: number
  weekly_cost_target: number
  group_count?: number
  selected_user_ids?: number[]
  existing_mapping_id?: number
  assignments?: RelayPlanningAssignment[]
}

export function previewRelayPlan(data: RelayPlanningRequest) {
  return client.post<ApiResponse<RelayPlanningPlan>>('/admin/relay-planning/preview', data)
}

export function executeRelayPlan(data: RelayPlanningRequest & { operation_key: string }) {
  return client.post<ApiResponse<RelayPlanningExecution>>('/admin/relay-planning/execute', data)
}

export function listRelayGroupMappings(providerId?: number) {
  return client.get<ApiResponse<{ items: RelayPlanningMapping[] }>>('/admin/relay-planning/mappings', {
    params: providerId ? { provider_id: providerId } : undefined,
  })
}

export function rebindRelayGroupMapping(id: number, data: { department_id?: string; template_group_id?: number; source_group_id?: number; group_ids: number[]; status?: string }) {
  return client.put<ApiResponse<RelayPlanningMapping>>(`/admin/relay-planning/mappings/${id}/rebind`, data)
}

export function previewRelayReplan(id: number, data: { selected_user_ids?: number[]; assignments?: RelayPlanningAssignment[] }) {
  return client.post<ApiResponse<RelayPlanningPlan>>(`/admin/relay-planning/mappings/${id}/replan`, data)
}

export function executeRelayReplan(id: number, data: RelayPlanningRequest & { operation_key: string }) {
  return client.post<ApiResponse<RelayPlanningExecution>>(`/admin/relay-planning/mappings/${id}/replan/execute`, data)
}
