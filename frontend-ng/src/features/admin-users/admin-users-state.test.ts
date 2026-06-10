import { describe, expect, test } from 'vitest'
import {
  buildAdminUsersParams,
  buildAdminUsersSearch,
  buildAdminUserTableMetrics,
  buildSubscriptionJobPayload,
  canSubmitSubscriptionJob,
  defaultSubscriptionTarget,
  parseAdminUsersSearch,
  isActiveSubscriptionJob,
  nextVisibleSelection,
  subscriptionJobMessage
} from './admin-users-state'
import type { AdminAssignableSubscriptionProvider, AdminSubscriptionJob, AdminUser } from '@/lib/api/types'

const users: AdminUser[] = [
  user(1, 'alice'),
  user(2, 'bob'),
  user(3, 'carol')
]

const providers: AdminAssignableSubscriptionProvider[] = [
  { id: 1, name: 'empty', display_name: 'Empty', groups: [] },
  {
    id: 2,
    name: 'relay',
    display_name: 'Relay',
    groups: [
      { group_id: '42', group_name: 'Group Alpha', platform: 'openai', subscription_type: 'pro' },
      { group_id: '43', group_name: 'Group Beta', platform: 'anthropic', subscription_type: 'pro' }
    ]
  }
]

describe('admin users params', () => {
  test('omits empty search and default pagination', () => {
    expect(buildAdminUsersParams({ q: '  ', page: 1, pageSize: 20 })).toEqual({ page: 1, page_size: 20 })
  })

  test('trims query and preserves page size', () => {
    expect(buildAdminUsersParams({ q: ' alice ', page: 2, pageSize: 50 })).toEqual({ q: 'alice', page: 2, page_size: 50 })
  })

  test('parses admin users filters from URL search with sane fallbacks', () => {
    expect(parseAdminUsersSearch({ q: ' alice ', page: '2', page_size: '50' })).toEqual({ q: 'alice', page: 2, pageSize: 50 })
    expect(parseAdminUsersSearch({ page: '-1', page_size: 'NaN' })).toEqual({ q: '', page: 1, pageSize: 20 })
  })

  test('serializes admin users filters into compact URL search', () => {
    expect(buildAdminUsersSearch({ q: '', page: 1, pageSize: 20 })).toEqual({})
    expect(buildAdminUsersSearch({ q: ' alice ', page: 3, pageSize: 50 })).toEqual({ q: 'alice', page: 3, page_size: 50 })
  })
})

describe('subscription defaults and gating', () => {
  test('chooses the first provider that has assignable groups', () => {
    expect(defaultSubscriptionTarget(providers)).toEqual({ providerId: 2, groupId: '42' })
  })

  test('requires selected users for selected scope', () => {
    expect(canSubmitSubscriptionJob({ providerId: 2, groupId: '42', scope: 'selected', operation: 'add', selectedUserIds: [], days: 30, confirmRemove: false, loading: false })).toBe(false)
    expect(canSubmitSubscriptionJob({ providerId: 2, groupId: '42', scope: 'selected', operation: 'add', selectedUserIds: [1], days: 30, confirmRemove: false, loading: false })).toBe(true)
  })

  test('requires explicit remove confirmation', () => {
    expect(canSubmitSubscriptionJob({ providerId: 2, groupId: '42', scope: 'all_mapped', operation: 'remove', selectedUserIds: [], days: 0, confirmRemove: false, loading: false })).toBe(false)
    expect(canSubmitSubscriptionJob({ providerId: 2, groupId: '42', scope: 'all_mapped', operation: 'remove', selectedUserIds: [], days: 0, confirmRemove: true, loading: false })).toBe(true)
  })
})

describe('subscription payload', () => {
  test('builds selected add payload with validity_days', () => {
    expect(buildSubscriptionJobPayload({
      scope: 'selected',
      operation: 'add',
      providerId: 2,
      groupId: '42',
      selectedUserIds: [3, 1],
      q: 'alice',
      days: 30
    })).toEqual({
      scope: 'selected',
      operation: 'add',
      provider_id: 2,
      group_id: '42',
      user_ids: [3, 1],
      validity_days: 30
    })
  })

  test('builds current-filter extend payload with days and trimmed filter', () => {
    expect(buildSubscriptionJobPayload({
      scope: 'current_filter',
      operation: 'extend',
      providerId: 2,
      groupId: '42',
      selectedUserIds: [],
      q: ' bob ',
      days: 7
    })).toMatchObject({
      scope: 'current_filter',
      operation: 'extend',
      filters: { q: 'bob' },
      days: 7
    })
  })

  test('builds remove payload without day fields', () => {
    expect(buildSubscriptionJobPayload({
      scope: 'all_mapped',
      operation: 'remove',
      providerId: 2,
      groupId: '42',
      selectedUserIds: [],
      q: '',
      days: 0
    })).not.toHaveProperty('days')
  })
})

describe('visible selection', () => {
  test('selects and clears visible rows without dropping hidden selections', () => {
    expect(nextVisibleSelection([9], users, true)).toEqual([9, 1, 2, 3])
    expect(nextVisibleSelection([9, 1, 2], users, false)).toEqual([9])
  })
})

describe('subscription job status', () => {
  test('classifies active jobs', () => {
    expect(isActiveSubscriptionJob(job({ status: 'queued' }))).toBe(true)
    expect(isActiveSubscriptionJob(job({ status: 'running' }))).toBe(true)
    expect(isActiveSubscriptionJob(job({ status: 'completed' }))).toBe(false)
  })

  test('summarizes completed jobs', () => {
    expect(subscriptionJobMessage(job({ status: 'completed', success_count: 2, skipped_count: 1, failed_count: 0 }))).toBe('Completed: 2 succeeded, 1 skipped, 0 failed')
  })
})

describe('admin user table metrics', () => {
  test('uses backend-provided usage fields when present and derives status from mapping otherwise', () => {
    expect(buildAdminUserTableMetrics({
      ...user(1, 'alice'),
      relay_user_id: 42,
      tokens_month: 4200,
      events_month: 12,
      status: 'active'
    })).toEqual({
      eventsMonth: 12,
      status: 'active',
      tokensMonth: 4200
    })

    expect(buildAdminUserTableMetrics({
      ...user(2, 'bob'),
      relay_user_id: null
    })).toEqual({
      eventsMonth: 0,
      status: 'invited',
      tokensMonth: 0
    })
  })
})

function user(id: number, username: string): AdminUser {
  return {
    id,
    username,
    email: `${username}@example.com`,
    role: 'user',
    auth_source: 'local',
    relay_user_id: id + 100,
    relay_auth_password: '',
    created_at: '2026-06-05T00:00:00Z',
    updated_at: '2026-06-05T00:00:00Z'
  }
}

function job(overrides: Partial<AdminSubscriptionJob>): AdminSubscriptionJob {
  return {
    id: 1,
    status: 'queued',
    phase: 'queued',
    scope: 'selected',
    operation: 'add',
    provider_id: 2,
    group_id: '42',
    total_count: 3,
    processed_count: 0,
    success_count: 0,
    skipped_count: 0,
    failed_count: 0,
    results: [],
    ...overrides
  }
}
