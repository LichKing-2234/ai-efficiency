import { beforeEach, describe, expect, expectTypeOf, it, vi } from 'vitest'

vi.mock('@/api/client', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
  },
}))

import client from '@/api/client'
import {
  adminApproveQuotaResetRequest,
  adminRejectQuotaResetRequest,
  approveQuotaResetRequest,
  createQuotaResetRequest,
  getQuotaResetApprovalChainOptions,
  getQuotaResetApprovalChains,
  getQuotaResetApproverConfigs,
  getQuotaResetNotificationSettings,
  getQuotaResetOptions,
  listAdminQuotaResetRequests,
  listQuotaResetApproverCandidates,
  listQuotaResetApprovals,
  rejectQuotaResetRequest,
  saveQuotaResetApprovalChains,
  saveQuotaResetApproverConfigs,
  testQuotaResetNotificationSettings,
  updateQuotaResetNotificationSettings,
} from '@/api/quotaReset'
import type {
  QuotaResetApproveInput,
  QuotaResetApproverCandidateParams,
  QuotaResetRejectInput,
} from '@/api/quotaReset'
import type {
  ApiResponse,
  QuotaResetApprovalChainListResponse,
  QuotaResetApprovalChainOptionsResponse,
  QuotaResetApproverCandidateListResponse,
  QuotaResetNotificationSettings,
  QuotaResetNotificationSettingsInput,
  QuotaResetNotificationTestResult,
  QuotaResetRequestSummary,
  QuotaResetWorkflow,
} from '@/types'

const mockClient = client as unknown as {
  get: ReturnType<typeof vi.fn>
  post: ReturnType<typeof vi.fn>
  put: ReturnType<typeof vi.fn>
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('quota reset api', () => {
  it('exports exact workflow, chain, candidate, and decision contracts', () => {
    const workflow: QuotaResetWorkflow = {
      version: 2,
      current_node: null,
      nodes: [
        {
          id: 456,
          position: 1,
          node_type: 'configured_department',
          label: 'Platform approval',
          departments: [
            {
              external_id: 'department-alpha',
              display_path: 'Engineering / Platform',
              resolution: 'configured',
            },
          ],
          status: 'approved',
          admin_fallback_required: false,
          approvers: [
            {
              user_id: 7,
              display_name: 'Alice Example',
              email: 'alice@example.com',
              source: 'configured',
            },
          ],
          satisfied_by_decision_id: 9001,
        },
      ],
      decisions: [
        {
          id: 9001,
          node_id: 456,
          actor_user_id: 7,
          actor_display_name: 'Alice Example',
          decision: 'approve',
          comment: 'Approved for the release investigation.',
          admin_override: false,
          created_at: '2026-07-14T04:00:00Z',
        },
      ],
      can_approve: false,
      can_reject: false,
      can_cancel: false,
      can_retry: false,
    }
    const candidatePage: QuotaResetApproverCandidateListResponse = {
      items: [
        {
          user_id: 7,
          username: 'alice',
          email: 'alice@example.com',
          display_name: 'Alice Example',
          directory_member_external_id: 'member-alice',
          department_paths: ['Engineering / Platform'],
          wecom_mention_available: true,
        },
      ],
      page: 2,
      page_size: 20,
      total: 1,
    }
    const chains: QuotaResetApprovalChainListResponse = {
      items: [
        {
          id: 11,
          provider_id: 1,
          group_id: 'group-alpha',
          group_name: 'Group Alpha',
          enabled: true,
          nodes: [
            {
              directory_source_id: 2,
              department_external_id: 'department-alpha',
              department_display_path: 'Engineering / Platform',
            },
          ],
        },
      ],
    }
    const options: QuotaResetApprovalChainOptionsResponse = {
      groups: [
        {
          provider_id: 1,
          group_id: 'group-alpha',
          group_name: 'Group Alpha',
          platform: 'openai',
        },
      ],
      departments: [
        {
          directory_source_id: 2,
          department_external_id: 'department-alpha',
          department_display_path: 'Engineering / Platform',
          approver_count: 1,
        },
      ],
    }

    expect(workflow.nodes[0].satisfied_by_decision_id).toBe(9001)
    expect(candidatePage).toMatchObject({ page: 2, page_size: 20, total: 1 })
    expect(chains.items[0]).toMatchObject({ provider_id: 1, group_id: 'group-alpha' })
    expect(options.departments[0].approver_count).toBe(1)
    expectTypeOf<QuotaResetRequestSummary>().toMatchTypeOf<{
      requester_display_name: string
      requester_email: string
      requester_department_paths: string[]
      workflow?: QuotaResetWorkflow
    }>()
    expectTypeOf<QuotaResetApproverCandidateParams>().toEqualTypeOf<{
      source_id?: number
      q?: string
      page?: number
      page_size?: number
    }>()
    expectTypeOf<{ request_node_id: number; decision_reason: string }>().toMatchTypeOf<QuotaResetApproveInput>()
    expectTypeOf<{}>().toMatchTypeOf<QuotaResetApproveInput>()
    expectTypeOf<{ decision_reason: string }>().toMatchTypeOf<QuotaResetApproveInput>()
    expectTypeOf<{ request_node_id: number }>().not.toMatchTypeOf<QuotaResetApproveInput>()
    expectTypeOf<{ request_node_id: number; decision_reason: string }>().toMatchTypeOf<QuotaResetRejectInput>()
    expectTypeOf<{ decision_reason: string }>().toMatchTypeOf<QuotaResetRejectInput>()
    expectTypeOf<{ request_node_id: number }>().not.toMatchTypeOf<QuotaResetRejectInput>()
  })

  it('uses user request endpoints', () => {
    getQuotaResetOptions()
    createQuotaResetRequest({ group_id: '42', reason: 'Need reset' })
    listQuotaResetApprovals({ status: 'pending' })

    expect(mockClient.get).toHaveBeenCalledWith('/user/quota-reset/options')
    expect(mockClient.post).toHaveBeenCalledWith('/user/quota-reset/requests', {
      group_id: '42',
      reason: 'Need reset',
    })
    expect(mockClient.get).toHaveBeenCalledWith('/user/quota-reset/approvals', {
      params: { status: 'pending' },
    })
  })

  it('posts v2 and legacy user decision bodies', () => {
    approveQuotaResetRequest(123, {
      request_node_id: 456,
      decision_reason: 'Approved for the release investigation.',
    })
    approveQuotaResetRequest(124)
    rejectQuotaResetRequest(125, {
      request_node_id: 457,
      decision_reason: 'Rejected because the evidence is incomplete.',
    })
    rejectQuotaResetRequest(126, { decision_reason: 'Legacy rejection comment.' })

    expect(mockClient.post).toHaveBeenNthCalledWith(1, '/user/quota-reset/approvals/123/approve', {
      request_node_id: 456,
      decision_reason: 'Approved for the release investigation.',
    })
    expect(mockClient.post).toHaveBeenNthCalledWith(2, '/user/quota-reset/approvals/124/approve', {})
    expect(mockClient.post).toHaveBeenNthCalledWith(3, '/user/quota-reset/approvals/125/reject', {
      request_node_id: 457,
      decision_reason: 'Rejected because the evidence is incomplete.',
    })
    expect(mockClient.post).toHaveBeenNthCalledWith(4, '/user/quota-reset/approvals/126/reject', {
      decision_reason: 'Legacy rejection comment.',
    })
  })

  it('posts v2 and legacy admin decision bodies', () => {
    adminApproveQuotaResetRequest(201, {
      request_node_id: 501,
      decision_reason: 'Approved by the incident administrator.',
    })
    adminApproveQuotaResetRequest(202)
    adminRejectQuotaResetRequest(203, {
      request_node_id: 502,
      decision_reason: 'Rejected by the incident administrator.',
    })
    adminRejectQuotaResetRequest(204, { decision_reason: 'Legacy admin rejection comment.' })

    expect(mockClient.post).toHaveBeenNthCalledWith(1, '/admin/quota-reset/requests/201/approve', {
      request_node_id: 501,
      decision_reason: 'Approved by the incident administrator.',
    })
    expect(mockClient.post).toHaveBeenNthCalledWith(2, '/admin/quota-reset/requests/202/approve', {})
    expect(mockClient.post).toHaveBeenNthCalledWith(3, '/admin/quota-reset/requests/203/reject', {
      request_node_id: 502,
      decision_reason: 'Rejected by the incident administrator.',
    })
    expect(mockClient.post).toHaveBeenNthCalledWith(4, '/admin/quota-reset/requests/204/reject', {
      decision_reason: 'Legacy admin rejection comment.',
    })
  })

  it('uses paginated approver candidate search', () => {
    listQuotaResetApproverCandidates({ source_id: 1, q: 'alice', page: 2, page_size: 20 })

    expect(mockClient.get).toHaveBeenCalledWith('/admin/quota-reset/approver-candidates', {
      params: { source_id: 1, q: 'alice', page: 2, page_size: 20 },
    })
  })

  it('uses admin approval configuration endpoints', () => {
    listAdminQuotaResetRequests({ page: 2, page_size: 20 })
    getQuotaResetApproverConfigs()
    saveQuotaResetApproverConfigs([
      {
        department_external_id: 'department-alpha',
        department_display_path: 'Department Alpha',
        approver_user_id: 7,
        enabled: true,
      },
    ], 'replace_all')

    expect(mockClient.get).toHaveBeenCalledWith('/admin/quota-reset/requests', {
      params: { page: 2, page_size: 20 },
    })
    expect(mockClient.get).toHaveBeenCalledWith('/admin/quota-reset/approver-configs')
    expect(mockClient.put).toHaveBeenCalledWith('/admin/quota-reset/approver-configs', {
      items: [
        {
          department_external_id: 'department-alpha',
          department_display_path: 'Department Alpha',
          approver_user_id: 7,
          enabled: true,
        },
      ],
      mode: 'replace_all',
    })
  })

  it('lists and fully replaces approval chains, including an empty list', () => {
    getQuotaResetApprovalChains()
    getQuotaResetApprovalChainOptions()
    saveQuotaResetApprovalChains([])

    expect(mockClient.get).toHaveBeenCalledWith('/admin/quota-reset/approval-chains')
    expect(mockClient.get).toHaveBeenCalledWith('/admin/quota-reset/approval-chain-options')
    expect(mockClient.put).toHaveBeenCalledWith('/admin/quota-reset/approval-chains', { items: [] })

    expectTypeOf<Awaited<ReturnType<typeof getQuotaResetApprovalChains>>['data']>()
      .toEqualTypeOf<ApiResponse<QuotaResetApprovalChainListResponse>>()
    expectTypeOf<Awaited<ReturnType<typeof getQuotaResetApprovalChainOptions>>['data']>()
      .toEqualTypeOf<ApiResponse<QuotaResetApprovalChainOptionsResponse>>()
  })

  it('preserves omitted and empty notification URLs and returns the test result', () => {
    const omittedURL: QuotaResetNotificationSettingsInput = {
      enabled: true,
      channel_type: 'generic_webhook',
      auth_type: 'none',
      credential_id: null,
    }
    const emptyURL: QuotaResetNotificationSettingsInput = {
      enabled: false,
      channel_type: 'generic_webhook',
      url: '',
      auth_type: 'none',
      credential_id: null,
    }
    const settings: QuotaResetNotificationSettings = {
      enabled: true,
      channel_type: 'generic_webhook',
      template_version: 1,
      url_configured: true,
      url_preview: 'https://hooks.example.com/...',
      auth_type: 'none',
      credential_id: null,
      updated_at: '2026-07-14T04:00:00Z',
    }
    const testResult: QuotaResetNotificationTestResult = {
      delivered: false,
      recipient_count: 0,
      missing_recipient_count: 1,
      warning: 'wecom_recipient_unavailable',
    }

    getQuotaResetNotificationSettings()
    updateQuotaResetNotificationSettings(omittedURL)
    updateQuotaResetNotificationSettings(emptyURL)
    testQuotaResetNotificationSettings()

    expect(settings.url_configured).toBe(true)
    expect(testResult.warning).toBe('wecom_recipient_unavailable')
    expect(mockClient.get).toHaveBeenCalledWith('/admin/quota-reset/notification-settings')
    expect(mockClient.put).toHaveBeenNthCalledWith(1, '/admin/quota-reset/notification-settings', omittedURL)
    expect(mockClient.put).toHaveBeenNthCalledWith(2, '/admin/quota-reset/notification-settings', emptyURL)
    expect(mockClient.post).toHaveBeenCalledWith('/admin/quota-reset/notification-settings/test')

    type NotificationSettingsHasRawURL = 'url' extends keyof QuotaResetNotificationSettings ? true : false
    expectTypeOf<NotificationSettingsHasRawURL>().toEqualTypeOf<false>()
    expectTypeOf<Awaited<ReturnType<typeof testQuotaResetNotificationSettings>>['data']>()
      .toEqualTypeOf<ApiResponse<QuotaResetNotificationTestResult>>()
  })
})
