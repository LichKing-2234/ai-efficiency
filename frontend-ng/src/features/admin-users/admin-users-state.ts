import type {
  AdminAssignableSubscriptionProvider,
  AdminManageSubscriptionsRequest,
  AdminSubscriptionJob,
  AdminSubscriptionManageOperation,
  AdminSubscriptionManageScope,
  AdminUser
} from '@/lib/api/types'

export interface AdminUsersFilterState {
  q: string
  page: number
  pageSize: number
}

export interface SubscriptionSubmitState {
  providerId: number | null
  groupId: string
  scope: AdminSubscriptionManageScope
  operation: AdminSubscriptionManageOperation
  selectedUserIds: number[]
  days: number
  confirmRemove: boolean
  loading: boolean
}

export interface SubscriptionPayloadState {
  scope: AdminSubscriptionManageScope
  operation: AdminSubscriptionManageOperation
  providerId: number
  groupId: string
  selectedUserIds: number[]
  q: string
  days: number
}

export function buildAdminUsersParams(filters: AdminUsersFilterState) {
  return {
    ...(filters.q.trim() ? { q: filters.q.trim() } : {}),
    page: Math.max(1, filters.page),
    page_size: filters.pageSize
  }
}

export function defaultSubscriptionTarget(providers: AdminAssignableSubscriptionProvider[]) {
  const provider = providers.find((item) => item.groups.length > 0) ?? providers[0] ?? null
  return {
    providerId: provider?.id ?? null,
    groupId: provider?.groups[0]?.group_id ?? ''
  }
}

export function canSubmitSubscriptionJob(state: SubscriptionSubmitState) {
  if (state.loading || !state.providerId || !state.groupId) return false
  if (state.scope === 'selected' && state.selectedUserIds.length === 0) return false
  if ((state.operation === 'add' || state.operation === 'extend') && state.days <= 0) return false
  if (state.operation === 'remove' && !state.confirmRemove) return false
  return true
}

export function buildSubscriptionJobPayload(state: SubscriptionPayloadState): AdminManageSubscriptionsRequest {
  const payload: AdminManageSubscriptionsRequest = {
    scope: state.scope,
    operation: state.operation,
    provider_id: state.providerId,
    group_id: state.groupId
  }
  if (state.scope === 'selected') {
    payload.user_ids = state.selectedUserIds
  } else if (state.scope === 'current_filter') {
    payload.filters = state.q.trim() ? { q: state.q.trim() } : {}
  }
  if (state.operation === 'add') {
    payload.validity_days = state.days
  } else if (state.operation === 'extend') {
    payload.days = state.days
  }
  return payload
}

export function nextVisibleSelection(current: number[], rows: AdminUser[], checked: boolean) {
  const next = new Set(current)
  for (const row of rows) {
    if (checked) {
      next.add(row.id)
    } else {
      next.delete(row.id)
    }
  }
  return Array.from(next)
}

export function isActiveSubscriptionJob(job: AdminSubscriptionJob | null | undefined) {
  return job?.status === 'queued' || job?.status === 'running'
}

export function subscriptionJobMessage(job: AdminSubscriptionJob) {
  if (job.status === 'queued') return `Queued: ${job.processed_count}/${job.total_count} processed`
  if (job.status === 'running') return `Running: ${job.processed_count}/${job.total_count} processed`
  if (job.status === 'completed') return `Completed: ${job.success_count} succeeded, ${job.skipped_count} skipped, ${job.failed_count} failed`
  if (job.status === 'abandoned') return 'Abandoned'
  return `Failed: ${job.last_error || 'Unknown error'}`
}
