import client from './client'
import type { ApiResponse } from '@/types'

export interface RelayPlanningCandidate {
  user_id: number
  relay_user_id: number
  username: string
  email: string
  range_cost: number
  range_tokens: number
  usage_known: boolean
  global_token_rank: number
  current_group_ids?: number[]
  migratable_key_count: number
  source_member: boolean
  source_group_id?: number
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
  target_unavailable?: boolean
  target_group_name?: string
	current_target_group_name?: string
	suggested_target_group_name?: string
	rename_selected?: boolean
	desired_accounts: RelayPlanningAccountIntent[]
	accounts: RelayPlanningAccount[]
}

export interface RelayPlanningUnmanagedMember {
  relay_user_id: number
  username: string
  email: string
  target_group_ids: number[]
  range_cost: number
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
	template_accounts: RelayPlanningAccount[]
	unmanaged_members?: RelayPlanningUnmanagedMember[]
	target_summaries: RelayPlanningTargetSummary[]
	warnings?: string[]
	relationship_fingerprint: string
	accounts_reviewed: boolean
	generated_at: string
  mapping_id?: number
}

export interface RelayPlanningTargetSummary {
	index: number
	target_group_id?: number
	target_group_name: string
	rename?: { from_name: string; to_name: string }
	accounts: Array<{ account_id: number; action: 'add' | 'remove' | 'reorder'; old_priority?: number; new_priority?: number }>
	members: Array<{ user_id?: number; relay_user_id?: number; action: 'add' | 'move' | 'remove'; from_group_id?: number; to_group_id?: number }>
	subscriptions: Array<{ user_id?: number; relay_user_id: number; action: 'add' | 'remove'; group_id?: number }>
	api_keys: Array<{ user_id?: number; relay_user_id: number; action: 'move'; count: number; from_group_id?: number; to_group_id?: number }>
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
	account_management_initialized: boolean
	desired_accounts: Record<string, RelayPlanningAccountIntent[]>
	account_pools: RelayPlanningTargetAccountPool[]
  operation_state?: Record<string, Record<string, string>>
	baseline_revision?: number
	alignment?: 'aligned' | 'drifted' | 'operating'
	alignment_differences?: string[]
	active_operation?: RelayPlanningOperation
  department_suggestions?: Array<{ id: string; name: string }>
  warnings?: string[]
  updated_at: string
}

export interface RelayPlanningOperationStep {
	id: number
	step_key: string
	action: string
	relationship_type: string
	lifecycle: 'planned' | 'dispatched' | 'readback_verified' | 'failed' | 'blocked_external'
	reviewed_resource_ids: number[]
	resume_supported: boolean
	restore_supported: boolean
}

export interface RelayPlanningOperation {
	id: number
	lifecycle: 'applying' | 'interrupted' | 'resuming' | 'restoring' | 'applied' | 'restored' | 'blocked_external'
	supported_directions: Array<'resume' | 'restore'>
	affected_mapping_ids: number[]
	attempt_count: number
	steps: RelayPlanningOperationStep[]
	external_blocker?: { resource_type: string; resource_id: number; relationship: string }
}

export interface RelayPlanningRecoveryPreview {
	operation: RelayPlanningOperation
	direction: 'resume' | 'restore'
	baseline_revisions: Record<string, number>
	relationship_fingerprint: string
	resume_only: boolean
	external_blocker?: { resource_type: string; resource_id: number; relationship: string }
	observed_facts: Array<Record<string, unknown>>
}

export interface RelayPlanningRecoveryResult {
	operation_id: number
	direction: 'resume' | 'restore'
	lifecycle: 'applied' | 'restored'
	attempt_id: number
}

export type RelayPlanningContainment =
	| { mode: 'none' }
	| { mode: 'resume_exact'; operation_key?: string }
	| { mode: 'manual_intervention'; reason: 'incomplete_identity' }

function operationEntryNeedsRetry(entry: Record<string, string>): boolean {
	return Boolean(entry.error || entry.status === 'failed' || entry.subscription === 'failed' || entry.source_removal === 'failed' || entry.api_keys?.includes(':failed:'))
}

export function relayPlanningContainment(mapping: RelayPlanningMapping): RelayPlanningContainment {
	if (mapping.status !== 'needs_retry') return { mode: 'none' }
	const state = mapping.operation_state ?? {}
	const operation = state.operation ?? {}
	const retryMembers = Object.entries(state).filter(([key, entry]) => key.startsWith('member:') && operationEntryNeedsRetry(entry))
	if (!operation.intent_hash || retryMembers.some(([, entry]) => !entry.step_identity)) {
		return { mode: 'manual_intervention', reason: 'incomplete_identity' }
	}
	return { mode: 'resume_exact', ...(operation.key ? { operation_key: operation.key } : {}) }
}

export interface RelayPlanningMappingRenewalPreview {
	mapping_id: number
	provider_id: number
	platform: string
	renewal_days: number
	members: RelayPlanningMappingRenewalMember[]
	generated_at: string
	relationship_fingerprint: string
}

export interface RelayPlanningMappingRenewalMember {
	user_id: number
	relay_user_id: number
	username: string
	email: string
	expected_target_group_id: number
	expected_target_group_name: string
	status: 'active' | 'expired' | 'missing' | 'suspended'
	current_expiry?: string
	planned_action: 'extend' | 'renew' | 'create' | 'skip'
	resulting_expiry?: string
	drift?: Array<{ group_id: number; group_name: string; status: string; expires_at?: string }>
}

export interface RelayPlanningMappingRenewalReviewedMember {
	user_id: number
	target_group_id: number
	planned_action: RelayPlanningMappingRenewalMember['planned_action']
}

export interface RelayPlanningMappingRenewalExecution {
	mapping_id: number
	renewal_days: number
	operation_key: string
	members: RelayPlanningMappingRenewalMemberResult[]
	preview?: RelayPlanningMappingRenewalPreview
	preview_error?: string
}

export interface RelayPlanningMappingRenewalMemberResult {
	user_id: number
	relay_user_id: number
	target_group_id: number
	action: RelayPlanningMappingRenewalMember['planned_action']
	status: 'succeeded' | 'skipped' | 'failed'
	error?: string
}

export interface RelayPlanningAccountIntent {
	account_id: number
	priority: number
}

export interface RelayPlanningAccount {
	id: number
	name: string
	platform: string
	type: string
	status: string
	schedulable: boolean
	priority?: number
	group_relationships?: Array<{ group_id: number; priority: number }>
}

export interface RelayPlanningTargetAccountPool {
	target_group_id: number
	current: RelayPlanningAccount[]
	desired: RelayPlanningAccountIntent[]
	drift: boolean
}

export interface RelayPlanningAccountSearchPage {
	items: RelayPlanningAccount[]
	total: number
	page: number
	page_size: number
}

export interface RelayPlanningExecution {
  plan: RelayPlanningPlan
  groups: Array<{ index: number; id?: number; name?: string; current_name?: string; status: string; rename?: string; error?: string }>
	accounts: Array<{ target_group_id: number; account_id?: number; desired_priority?: number; status: string; error?: string }>
  members: Array<{ user_id?: number; relay_user_id?: number; target_group_id?: number; subscription: string; source_removal: string; api_keys?: string[]; error?: string }>
  mappings?: Array<{ mapping_id: number; role: 'destination' | 'source'; status: string; error?: string }>
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
  member_sources?: Record<string, number>
  adopt_relay_user_ids?: number[]
	removed_user_ids?: number[]
	member_actions?: Record<string, RelayPlanningMemberAction>
}

export interface RelayPlanningMemberAction {
	mode: 'move_here' | 'add_additionally'
	from_mapping_id?: number
}

export interface RelayPlanningUserSearchItem {
  user_id: number
  relay_user_id?: number
  username: string
  email: string
  department?: { external_id: string; name: string; display_path: string }
  selectable: boolean
  disabled_reason?: string
	managed_assignments?: Array<{ mapping_id: number; department_id: string; department_name: string; target_group_id: number }>
}

export interface RelayPlanningUserSearchPage {
  items: RelayPlanningUserSearchItem[]
  total: number
  page: number
  page_size: number
}

export function previewRelayPlan(data: RelayPlanningRequest) {
  return client.post<ApiResponse<RelayPlanningPlan>>('/admin/relay-planning/preview', data)
}

export function searchRelayPlanningUsers(params: { provider_id: number; platform: string; q?: string; page?: number; page_size?: number }) {
  return client.get<ApiResponse<RelayPlanningUserSearchPage>>('/admin/relay-planning/users', { params })
}

export function searchRelayPlanningAccounts(params: { provider_id: number; platform: string; q?: string; page?: number; page_size?: number }) {
	return client.get<ApiResponse<RelayPlanningAccountSearchPage>>('/admin/relay-planning/accounts', { params })
}

export function executeRelayPlan(data: RelayPlanningRequest & { operation_key: string; expected_relationship_fingerprint: string }) {
  return client.post<ApiResponse<RelayPlanningExecution>>('/admin/relay-planning/execute', data)
}

export function listRelayGroupMappings(providerId?: number) {
  return client.get<ApiResponse<{ items: RelayPlanningMapping[] }>>('/admin/relay-planning/mappings', {
    params: providerId ? { provider_id: providerId } : undefined,
  })
}

export function getRelayPlanningOperation(operationId: number) {
	return client.get<ApiResponse<RelayPlanningOperation>>(`/admin/relay-planning/operations/${operationId}`)
}

export function previewRelayPlanningRecovery(operationId: number, direction: 'resume' | 'restore') {
	return client.post<ApiResponse<RelayPlanningRecoveryPreview>>(`/admin/relay-planning/operations/${operationId}/recovery/preview`, { direction })
}

export function confirmRelayPlanningRecovery(operationId: number, data: { direction: 'resume' | 'restore'; expected_baseline_revisions: Record<string, number>; expected_relationship_fingerprint: string }) {
	return client.post<ApiResponse<RelayPlanningRecoveryResult>>(`/admin/relay-planning/operations/${operationId}/recovery/confirm`, data)
}

export function previewRelayMappingRenewal(id: number, data: { renewal_days: number }) {
	return client.post<ApiResponse<RelayPlanningMappingRenewalPreview>>(`/admin/relay-planning/mappings/${id}/renewal/preview`, data)
}

export function executeRelayMappingRenewal(id: number, data: { renewal_days: number; members: RelayPlanningMappingRenewalReviewedMember[]; expected_relationship_fingerprint: string; operation_key: string; retry: boolean }) {
	return client.post<ApiResponse<RelayPlanningMappingRenewalExecution>>(`/admin/relay-planning/mappings/${id}/renewal/execute`, data)
}

export function rebindRelayGroupMapping(id: number, data: { department_id?: string; template_group_id?: number; source_group_id?: number; group_ids: number[]; status?: string }) {
  return client.put<ApiResponse<RelayPlanningMapping>>(`/admin/relay-planning/mappings/${id}/rebind`, data)
}

export function adoptCurrentRelayAccounts(id: number) {
	return client.post<ApiResponse<RelayPlanningMapping>>(`/admin/relay-planning/mappings/${id}/accounts/adopt`)
}

export function saveRelayDesiredAccounts(id: number, desiredAccounts: Record<string, RelayPlanningAccountIntent[]>) {
	return client.put<ApiResponse<RelayPlanningMapping>>(`/admin/relay-planning/mappings/${id}/accounts`, { desired_accounts: desiredAccounts })
}

export function previewRelayReplan(id: number, data: { selected_user_ids?: number[]; assignments?: RelayPlanningAssignment[]; member_sources?: Record<string, number>; removed_user_ids?: number[]; member_actions?: Record<string, RelayPlanningMemberAction>; adopt_relay_user_ids?: number[] }) {
  return client.post<ApiResponse<RelayPlanningPlan>>(`/admin/relay-planning/mappings/${id}/replan`, data)
}

export function executeRelayReplan(id: number, data: RelayPlanningRequest & { operation_key: string; expected_relationship_fingerprint: string }) {
  return client.post<ApiResponse<RelayPlanningExecution>>(`/admin/relay-planning/mappings/${id}/replan/execute`, data)
}
