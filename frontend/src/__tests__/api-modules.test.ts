import { describe, it, expect, vi, beforeEach } from 'vitest'

// Mock the client module before importing API modules
vi.mock('@/api/client', () => {
  return {
    default: {
      get: vi.fn(),
      post: vi.fn(),
      put: vi.fn(),
      delete: vi.fn(),
    },
  }
})

import client from '@/api/client'
import { listProviders, createProvider, updateProvider, deleteProvider } from '@/api/scmProvider'
import { listRelayProviders, createRelayProvider, updateRelayProvider, deleteRelayProvider } from '@/api/relayProvider'
import { listPRs, getPR, syncPRs, getPRSyncJob, getLatestPRSyncJob, settlePR, refreshPRUsage } from '@/api/pr'
import { getDashboard } from '@/api/efficiency'
import { getSystemVersion, checkSystemUpdate } from '@/api/system'
import { getUserProviders, createGroupCredential, regenerateGroupCredential, getUserProviderModels, testUserProvider } from '@/api/user'
import {
  disableAdminUserAccess,
  startAdminUserSubscriptionJob,
  getAdminUserSubscriptionJob,
  getLatestAdminUserSubscriptionJob,
  listAdminUserDepartments,
} from '@/api/adminUsers'

const mockClient = client as unknown as {
  get: ReturnType<typeof vi.fn>
  post: ReturnType<typeof vi.fn>
  put: ReturnType<typeof vi.fn>
  delete: ReturnType<typeof vi.fn>
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('scmProvider API', () => {
  it('listProviders calls GET /scm-providers with pagination', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { items: [], total: 0 } } })
    await listProviders(2, 10)
    expect(mockClient.get).toHaveBeenCalledWith('/scm-providers', { params: { page: 2, page_size: 10 } })
  })

  it('listProviders uses default pagination', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { items: [], total: 0 } } })
    await listProviders()
    expect(mockClient.get).toHaveBeenCalledWith('/scm-providers', { params: { page: 1, page_size: 20 } })
  })

  it('createProvider calls POST /scm-providers', async () => {
    const payload = { name: 'GitHub', type: 'github', base_url: 'https://api.github.com' }
    mockClient.post.mockResolvedValue({ data: { data: { id: 1, ...payload } } })
    await createProvider(payload)
    expect(mockClient.post).toHaveBeenCalledWith('/scm-providers', payload)
  })

  it('updateProvider calls PUT /scm-providers/:id', async () => {
    const payload = { name: 'Updated' }
    mockClient.put.mockResolvedValue({ data: { data: { id: 3, name: 'Updated' } } })
    await updateProvider(3, payload)
    expect(mockClient.put).toHaveBeenCalledWith('/scm-providers/3', payload)
  })

  it('deleteProvider calls DELETE /scm-providers/:id', async () => {
    mockClient.delete.mockResolvedValue({ data: { data: null } })
    await deleteProvider(7)
    expect(mockClient.delete).toHaveBeenCalledWith('/scm-providers/7')
  })
})

describe('repo API', () => {
  it('listRepos calls GET /repos with scoped pagination params', async () => {
    const { listRepos } = await import('@/api/repo')
    mockClient.get.mockResolvedValue({ data: { data: { items: [], total: 0 } } })

    await listRepos({
      page: 3,
      pageSize: 15,
      scmProviderId: 7,
      scope: 'org',
      bindingState: 'bound',
    })

    expect(mockClient.get).toHaveBeenCalledWith('/repos', {
      params: {
        page: 3,
        page_size: 15,
        scm_provider_id: 7,
        scope: 'org',
        binding_state: 'bound',
      },
    })
  })

  it('getRepoInventory calls GET /repos/inventory', async () => {
    const { getRepoInventory } = await import('@/api/repo')
    mockClient.get.mockResolvedValue({ data: { data: [] } })

    await getRepoInventory()

    expect(mockClient.get).toHaveBeenCalledWith('/repos/inventory')
  })

  it('repairFailedWebhooks calls POST /repos/repair-webhooks', async () => {
    const { repairFailedWebhooks } = await import('@/api/repo')
    mockClient.post.mockResolvedValue({
      data: {
        data: {
          summary: { scanned: 0, repaired: 0, already_registered: 0, failed: 0 },
          items: [],
        },
      },
    })

    await repairFailedWebhooks({ force: true })

    expect(mockClient.post).toHaveBeenCalledWith('/repos/repair-webhooks', { force: true })
  })

  it('repairWebhook calls POST /repos/:id/repair-webhook', async () => {
    const { repairWebhook } = await import('@/api/repo')
    mockClient.post.mockResolvedValue({
      data: { data: { repo_config_id: 5, webhook_status: 'registered' } },
    })

    await repairWebhook(5, { force: false })

    expect(mockClient.post).toHaveBeenCalledWith('/repos/5/repair-webhook', { force: false })
  })
})

describe('relayProvider API', () => {
  it('listRelayProviders calls GET /admin/providers', async () => {
    mockClient.get.mockResolvedValue({ data: { data: [] } })
    await listRelayProviders()
    expect(mockClient.get).toHaveBeenCalledWith('/admin/providers')
  })

  it('createRelayProvider calls POST /admin/providers', async () => {
    const payload = {
      name: 'relay-main',
      display_name: 'Relay Main',
      base_url: 'https://relay.example.com',
      admin_api_key: 'admin-key',
      is_primary: true,
      enabled: true,
    }
    mockClient.post.mockResolvedValue({ data: { data: { id: 1, ...payload } } })
    await createRelayProvider(payload)
    expect(mockClient.post).toHaveBeenCalledWith('/admin/providers', payload)
  })

  it('updateRelayProvider calls PUT /admin/providers/:id', async () => {
    const payload = { display_name: 'Relay Secondary', enabled: false }
    mockClient.put.mockResolvedValue({ data: { data: { id: 3, ...payload } } })
    await updateRelayProvider(3, payload)
    expect(mockClient.put).toHaveBeenCalledWith('/admin/providers/3', payload)
  })

  it('deleteRelayProvider calls DELETE /admin/providers/:id', async () => {
    mockClient.delete.mockResolvedValue({ data: { data: null } })
    await deleteRelayProvider(7)
    expect(mockClient.delete).toHaveBeenCalledWith('/admin/providers/7')
  })

})

describe('pr API', () => {
  it('listPRs calls GET /repos/:id/prs with params', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { items: [], total: 0 } } })
    await listPRs(5, { status: 'merged', limit: 10, offset: 0, months: 3 })
    expect(mockClient.get).toHaveBeenCalledWith('/repos/5/prs', {
      params: { status: 'merged', limit: 10, offset: 0, months: 3 },
    })
  })

  it('listPRs works without optional params', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { items: [], total: 0 } } })
    await listPRs(5)
    expect(mockClient.get).toHaveBeenCalledWith('/repos/5/prs', { params: undefined })
  })

  it('getPR calls GET /prs/:id', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { id: 42, title: 'Fix bug' } } })
    await getPR(42)
    expect(mockClient.get).toHaveBeenCalledWith('/prs/42')
  })

  it('syncPRs starts a PR sync job', async () => {
    mockClient.post.mockResolvedValue({ data: { data: { job_id: 44, status: 'queued', phase: 'queued' } } })
    await syncPRs(5)
    expect(mockClient.post).toHaveBeenCalledWith('/repos/5/sync-prs')
  })

  it('getPRSyncJob calls GET /pr-sync-jobs/:id', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { id: 44, status: 'running', phase: 'fetching_prs' } } })
    await getPRSyncJob(44)
    expect(mockClient.get).toHaveBeenCalledWith('/pr-sync-jobs/44')
  })

  it('getLatestPRSyncJob fetches the latest repo PR sync job', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { id: 44, status: 'running', phase: 'fetching_prs' } } })
    await getLatestPRSyncJob(5)
    expect(mockClient.get).toHaveBeenCalledWith('/repos/5/pr-sync-job/latest')
  })

  it('settlePR calls POST /prs/:id/settle', async () => {
    mockClient.post.mockResolvedValue({ data: { data: { attribution_status: 'clear' } } })
    await settlePR(88)
    expect(mockClient.post).toHaveBeenCalledWith('/prs/88/settle')
  })

  it('refreshPRUsage calls POST /prs/:id/refresh-usage', async () => {
    mockClient.post.mockResolvedValue({ data: { data: { id: 88, usage_commit_count: 2 } } })
    await refreshPRUsage(88)
    expect(mockClient.post).toHaveBeenCalledWith('/prs/88/refresh-usage')
  })
})

describe('efficiency API', () => {
  it('getDashboard calls GET /efficiency/dashboard', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { total_repos: 5 } } })
    await getDashboard()
    expect(mockClient.get).toHaveBeenCalledWith('/efficiency/dashboard')
  })
})

describe('system API', () => {
  it('calls system version endpoints', async () => {
    mockClient.get.mockResolvedValue({ data: { data: {} } })
    mockClient.post.mockResolvedValue({ data: { data: {} } })

    await getSystemVersion()
    expect(mockClient.get).toHaveBeenCalledWith('/system/version')

    await checkSystemUpdate()
    expect(mockClient.post).toHaveBeenCalledWith('/system/version/check')
  })
})

describe('user API aggregate smoke', () => {
  it('calls user setup endpoints', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { providers: [] } } })
    mockClient.post.mockResolvedValue({ data: { data: { api_key_id: 1, name: 'alice', status: 'active', secret: 'sk-test' } } })

    await getUserProviders()
    expect(mockClient.get).toHaveBeenCalledWith('/user/providers')

    await createGroupCredential(7, '42')
    expect(mockClient.post).toHaveBeenCalledWith('/user/providers/7/groups/42/credential')

    await regenerateGroupCredential(7, '42')
    expect(mockClient.post).toHaveBeenCalledWith('/user/providers/7/groups/42/credential/regenerate')

    await getUserProviderModels(7, '42', 'openai')
    expect(mockClient.get).toHaveBeenCalledWith('/user/providers/7/groups/42/models', { params: { platform: 'openai' } })

    await testUserProvider(7, { platform: 'openai', group_id: '42', model: 'gpt-5.4', prompt: 'Hi' })
    expect(mockClient.post).toHaveBeenCalledWith('/user/providers/7/test', { platform: 'openai', group_id: '42', model: 'gpt-5.4', prompt: 'Hi' })
  })
})

describe('admin users API', () => {
  it('disables admin user access with explicit email confirmation', async () => {
    mockClient.post.mockResolvedValue({ data: { data: { status: 'disabled', relay_user_id: 42 } } })

    await disableAdminUserAccess(7, { confirm_email: 'alice@example.com' })

    expect(mockClient.post).toHaveBeenCalledWith('/admin/users/7/disable-access', { confirm_email: 'alice@example.com' })
  })

  it('starts admin user subscription jobs without a timeout override', async () => {
    const payload = {
      scope: 'selected' as const,
      user_ids: [1, 2],
      operation: 'add' as const,
      provider_id: 7,
      group_id: '42',
      validity_days: 30,
    }
    mockClient.post.mockResolvedValue({ data: { data: { id: 12, status: 'queued', phase: 'queued' } } })

    await startAdminUserSubscriptionJob(payload)

    expect(mockClient.post).toHaveBeenCalledWith('/admin/users/subscription-jobs', payload)
  })

  it('gets admin user subscription job progress', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { id: 12, status: 'running', phase: 'processing' } } })

    await getAdminUserSubscriptionJob(12)

    expect(mockClient.get).toHaveBeenCalledWith('/admin/users/subscription-jobs/12')
  })

  it('gets the latest admin user subscription job', async () => {
    mockClient.get.mockResolvedValue({ data: { data: null } })

    await getLatestAdminUserSubscriptionJob()

    expect(mockClient.get).toHaveBeenCalledWith('/admin/users/subscription-jobs/latest')
  })

  it('lists admin user departments for the in-route department view', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { items: [] } } })

    await listAdminUserDepartments()

    expect(mockClient.get).toHaveBeenCalledWith('/admin/users/departments')
  })
})
