import { beforeEach, describe, expect, it, vi } from 'vitest'

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
  approveQuotaResetRequest,
  createQuotaResetRequest,
  getQuotaResetApproverConfigs,
  getQuotaResetNotificationSettings,
  getQuotaResetOptions,
  listAdminQuotaResetRequests,
  listQuotaResetApproverCandidates,
  listQuotaResetApprovals,
  saveQuotaResetApproverConfigs,
  updateQuotaResetNotificationSettings,
} from '@/api/quotaReset'

const mockClient = client as unknown as {
  get: ReturnType<typeof vi.fn>
  post: ReturnType<typeof vi.fn>
  put: ReturnType<typeof vi.fn>
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('quota reset api', () => {
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

  it('uses admin approval and settings endpoints', () => {
    listAdminQuotaResetRequests({ page: 2, page_size: 20 })
    adminApproveQuotaResetRequest(99, { decision_reason: 'Approved' })
    approveQuotaResetRequest(98, { decision_reason: 'Looks valid' })
    getQuotaResetApproverConfigs()
    listQuotaResetApproverCandidates({ source_id: 1, department_external_id: 'department-alpha' })
    saveQuotaResetApproverConfigs([
      {
        department_external_id: 'department-alpha',
        department_display_path: 'Department Alpha',
        approver_user_id: 7,
        enabled: true,
      },
    ], 'replace_all')
    getQuotaResetNotificationSettings()
    updateQuotaResetNotificationSettings({
      enabled: true,
      channel: 'generic_webhook',
      url: 'https://hooks.example.com/ai-efficiency',
      auth_type: 'none',
    })

    expect(mockClient.get).toHaveBeenCalledWith('/admin/quota-reset/requests', {
      params: { page: 2, page_size: 20 },
    })
    expect(mockClient.post).toHaveBeenCalledWith('/admin/quota-reset/requests/99/approve', {
      decision_reason: 'Approved',
    })
    expect(mockClient.post).toHaveBeenCalledWith('/user/quota-reset/approvals/98/approve', {
      decision_reason: 'Looks valid',
    })
    expect(mockClient.get).toHaveBeenCalledWith('/admin/quota-reset/approver-configs')
    expect(mockClient.get).toHaveBeenCalledWith('/admin/quota-reset/approver-candidates', {
      params: { source_id: 1, department_external_id: 'department-alpha' },
    })
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
    expect(mockClient.get).toHaveBeenCalledWith('/admin/quota-reset/notification-settings')
    expect(mockClient.put).toHaveBeenCalledWith('/admin/quota-reset/notification-settings', {
      enabled: true,
      channel: 'generic_webhook',
      url: 'https://hooks.example.com/ai-efficiency',
      auth_type: 'none',
    })
  })
})
