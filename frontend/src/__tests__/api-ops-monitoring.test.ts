import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/api/client', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}))

import client from '@/api/client'
import {
  getOpsMonitoringGroupAccounts,
  getOpsMonitoringGroups,
  testOpsMonitoringProxy,
} from '@/api/opsMonitoring'

const mockClient = client as unknown as {
  get: ReturnType<typeof vi.fn>
  post: ReturnType<typeof vi.fn>
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('opsMonitoring API', () => {
  it('getOpsMonitoringGroups calls the frozen groups endpoint', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { groups: [] } } })

    await getOpsMonitoringGroups({ provider_id: 7, window: '1h' })

    expect(mockClient.get).toHaveBeenCalledWith('/admin/ops-monitoring/groups', {
      params: { provider_id: 7, window: '1h' },
    })
  })

  it('getOpsMonitoringGroups appends force only for an explicit upstream read', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { groups: [] } } })

    await getOpsMonitoringGroups({ provider_id: 7, window: '6h', force: true })
    expect(mockClient.get).toHaveBeenLastCalledWith('/admin/ops-monitoring/groups', {
      params: { provider_id: 7, window: '6h', force: true },
    })

    // Automatic reads keep using the snapshot cache: no force parameter at all.
    await getOpsMonitoringGroups({ provider_id: 7, window: '6h', force: false })
    expect(mockClient.get).toHaveBeenLastCalledWith('/admin/ops-monitoring/groups', {
      params: { provider_id: 7, window: '6h' },
    })
  })

  it('getOpsMonitoringGroupAccounts scopes the account list to the provider', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { accounts: [] } } })

    await getOpsMonitoringGroupAccounts(42, { provider_id: 7 })

    expect(mockClient.get).toHaveBeenCalledWith('/admin/ops-monitoring/groups/42/accounts', {
      params: { provider_id: 7 },
    })
  })

  it('testOpsMonitoringProxy posts the provider scope as a query parameter', async () => {
    mockClient.post.mockResolvedValue({ data: { data: { success: true } } })

    await testOpsMonitoringProxy(31, { provider_id: 7 })

    expect(mockClient.post).toHaveBeenCalledWith('/admin/ops-monitoring/proxies/31/test', undefined, {
      params: { provider_id: 7 },
    })
  })

  it('testOpsMonitoringProxy appends force only for an explicit re-probe', async () => {
    mockClient.post.mockResolvedValue({ data: { data: { success: true } } })

    await testOpsMonitoringProxy(31, { provider_id: 7, force: true })

    expect(mockClient.post).toHaveBeenLastCalledWith('/admin/ops-monitoring/proxies/31/test', undefined, {
      params: { provider_id: 7, force: true },
    })

    await testOpsMonitoringProxy(31, { provider_id: 7, force: false })

    expect(mockClient.post).toHaveBeenLastCalledWith('/admin/ops-monitoring/proxies/31/test', undefined, {
      params: { provider_id: 7 },
    })
  })
})
