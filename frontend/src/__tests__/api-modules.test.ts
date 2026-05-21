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
import { listPRs, getPR, syncPRs, settlePR, refreshPRUsage } from '@/api/pr'
import { getDashboard } from '@/api/efficiency'
import { getLLMConfig, updateLLMConfig, testLLMConnection } from '@/api/settings'
import { getDeploymentStatus, checkForUpdate, applyUpdate, rollbackUpdate, restartDeployment } from '@/api/deployment'
import { getUserProviders, createManagedKey, regenerateManagedKey } from '@/api/user'

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

describe('settings API', () => {
  it('getLLMConfig calls GET /settings/llm', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { model: 'gpt-4' } } })
    await getLLMConfig()
    expect(mockClient.get).toHaveBeenCalledWith('/settings/llm')
  })

  it('updateLLMConfig calls PUT /settings/llm with data', async () => {
    const config = { sub2api_url: 'http://localhost:3000', sub2api_api_key: 'sk-test', relay_admin_api_key: 'admin-test', model: 'gpt-4' }
    mockClient.put.mockResolvedValue({ data: { data: config } })
    await updateLLMConfig(config)
    expect(mockClient.put).toHaveBeenCalledWith('/settings/llm', config)
  })

  it('testLLMConnection calls POST /settings/llm/test', async () => {
    mockClient.post.mockResolvedValue({ data: { data: { success: true, message: 'OK' } } })
    await testLLMConnection({ prompt: 'Hi' })
    expect(mockClient.post).toHaveBeenCalledWith('/settings/llm/test', { prompt: 'Hi' })
  })
})

describe('deployment API', () => {
  it('calls deployment endpoints', async () => {
    mockClient.get.mockResolvedValue({ data: { data: {} } })
    mockClient.post.mockResolvedValue({ data: { data: {} } })

    await getDeploymentStatus()
    expect(mockClient.get).toHaveBeenCalledWith('/settings/deployment')

    await checkForUpdate()
    expect(mockClient.post).toHaveBeenCalledWith('/settings/deployment/update/check')

    await applyUpdate({ target_version: 'v0.5.0' })
    expect(mockClient.post).toHaveBeenCalledWith('/settings/deployment/update/apply', { target_version: 'v0.5.0' })

    await rollbackUpdate()
    expect(mockClient.post).toHaveBeenCalledWith('/settings/deployment/update/rollback')
  })

  it('calls deployment restart endpoint', async () => {
    mockClient.post.mockResolvedValue({ data: { data: { phase: 'restart_requested' } } })
    await restartDeployment()
    expect(mockClient.post).toHaveBeenCalledWith('/settings/deployment/restart')
  })
})

describe('user API aggregate smoke', () => {
  it('calls user setup endpoints', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { providers: [] } } })
    mockClient.post.mockResolvedValue({ data: { data: { api_key_id: 1, name: 'ae-cli-auto', status: 'active', secret: 'sk-test' } } })

    await getUserProviders()
    expect(mockClient.get).toHaveBeenCalledWith('/user/providers')

    await createManagedKey(7)
    expect(mockClient.post).toHaveBeenCalledWith('/user/providers/7/managed-key')

    await regenerateManagedKey(7)
    expect(mockClient.post).toHaveBeenCalledWith('/user/providers/7/managed-key/regenerate')
  })
})
