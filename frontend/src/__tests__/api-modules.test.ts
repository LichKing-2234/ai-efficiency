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
import { listRelayProviders, createRelayProvider, updateRelayProvider, deleteRelayProvider, testRelayProvider } from '@/api/relayProvider'
import { listPRs, getPR, syncPRs, settlePR, refreshPRUsage } from '@/api/pr'
import { getDashboard } from '@/api/efficiency'
import { getUserProviders, createGroupCredential, regenerateGroupCredential } from '@/api/user'
import { getSystemVersion, checkSystemUpdate } from '@/api/system'

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

describe('relayProvider API', () => {
  it('listRelayProviders calls GET /admin/providers', async () => {
    mockClient.get.mockResolvedValue({ data: { data: [] } })
    await listRelayProviders()
    expect(mockClient.get).toHaveBeenCalledWith('/admin/providers')
  })

  it('createRelayProvider calls POST /admin/providers', async () => {
    const payload = {
      name: 'sub2api-main',
      display_name: 'Sub2API Main',
      base_url: 'https://sub2api.agoraio.cn',
      admin_url: 'https://sub2api.agoraio.cn',
      admin_api_key: 'admin-key',
      is_primary: true,
      enabled: true,
    }
    mockClient.post.mockResolvedValue({ data: { data: { id: 1, ...payload } } })
    await createRelayProvider(payload)
    expect(mockClient.post).toHaveBeenCalledWith('/admin/providers', payload)
  })

  it('updateRelayProvider calls PUT /admin/providers/:id', async () => {
    const payload = { display_name: 'Sub2API Secondary', enabled: false }
    mockClient.put.mockResolvedValue({ data: { data: { id: 3, ...payload } } })
    await updateRelayProvider(3, payload)
    expect(mockClient.put).toHaveBeenCalledWith('/admin/providers/3', payload)
  })

  it('deleteRelayProvider calls DELETE /admin/providers/:id', async () => {
    mockClient.delete.mockResolvedValue({ data: { data: null } })
    await deleteRelayProvider(7)
    expect(mockClient.delete).toHaveBeenCalledWith('/admin/providers/7')
  })

  it('testRelayProvider calls POST /admin/providers/:id/test', async () => {
    mockClient.post.mockResolvedValue({ data: { data: { success: true, message: 'OK' } } })
    await testRelayProvider(3, { platform: 'openai', model: 'gpt-5.4', prompt: 'Hi' })
    expect(mockClient.post).toHaveBeenCalledWith('/admin/providers/3/test', { platform: 'openai', model: 'gpt-5.4', prompt: 'Hi' })
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

  it('syncPRs calls POST /repos/:id/sync-prs', async () => {
    mockClient.post.mockResolvedValue({ data: { data: { created: 2, updated: 1, total: 3 } } })
    await syncPRs(5)
    expect(mockClient.post).toHaveBeenCalledWith('/repos/5/sync-prs')
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
  it('getSystemVersion calls GET /system/version', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { version: { version: 'v0.4.0' } } } })
    await getSystemVersion()
    expect(mockClient.get).toHaveBeenCalledWith('/system/version')
  })

  it('checkSystemUpdate calls POST /system/version/check', async () => {
    mockClient.post.mockResolvedValue({ data: { data: { update_available: true } } })
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
  })
})
