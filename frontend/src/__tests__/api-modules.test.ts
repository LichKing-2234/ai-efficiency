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
import { listProviders, getProvider, createProvider, updateProvider, deleteProvider } from '@/api/scmProvider'
import { triggerScan, listScans, getLatestScan, triggerOptimize, optimizePreview, optimizeConfirm } from '@/api/analysis'
import { listPRs, getPR, syncPRs, settlePR } from '@/api/pr'
import { getDashboard, getRepoMetrics, getRepoTrend } from '@/api/efficiency'
import { sendChatMessage } from '@/api/chat'
import { getLLMConfig, updateLLMConfig, testLLMConnection } from '@/api/settings'
import { listSessions } from '@/api/session'
import { getDeploymentStatus, checkForUpdate, applyUpdate, rollbackUpdate, restartDeployment } from '@/api/deployment'

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

  it('getProvider calls GET /scm-providers/:id', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { id: 5 } } })
    await getProvider(5)
    expect(mockClient.get).toHaveBeenCalledWith('/scm-providers/5')
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

describe('analysis API', () => {
  it('triggerScan calls POST /repos/:id/scan with extended timeout', async () => {
    mockClient.post.mockResolvedValue({ data: { data: { id: 1, score: 80 } } })
    await triggerScan(10)
    expect(mockClient.post).toHaveBeenCalledWith('/repos/10/scan', null, { timeout: 120000 })
  })

  it('listScans calls GET /repos/:id/scans with limit', async () => {
    mockClient.get.mockResolvedValue({ data: { data: [] } })
    await listScans(10, 5)
    expect(mockClient.get).toHaveBeenCalledWith('/repos/10/scans', { params: { limit: 5 } })
  })

  it('listScans uses default limit of 20', async () => {
    mockClient.get.mockResolvedValue({ data: { data: [] } })
    await listScans(10)
    expect(mockClient.get).toHaveBeenCalledWith('/repos/10/scans', { params: { limit: 20 } })
  })

  it('getLatestScan calls GET /repos/:id/scans/latest', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { id: 1, score: 75 } } })
    await getLatestScan(10)
    expect(mockClient.get).toHaveBeenCalledWith('/repos/10/scans/latest')
  })

  it('triggerOptimize calls POST /repos/:id/optimize with extended timeout', async () => {
    mockClient.post.mockResolvedValue({ data: { data: { branch_name: 'ai/opt' } } })
    await triggerOptimize(10)
    expect(mockClient.post).toHaveBeenCalledWith('/repos/10/optimize', null, { timeout: 120000 })
  })

  it('optimizePreview calls POST /repos/:id/optimize/preview with extended timeout', async () => {
    mockClient.post.mockResolvedValue({ data: { data: { files: [], score: 90 } } })
    await optimizePreview(10)
    expect(mockClient.post).toHaveBeenCalledWith('/repos/10/optimize/preview', null, { timeout: 120000 })
  })

  it('optimizeConfirm calls POST /repos/:id/optimize/confirm with files and score', async () => {
    const files = { 'README.md': '# Hello' }
    mockClient.post.mockResolvedValue({ data: { data: { pr_url: 'http://pr' } } })
    await optimizeConfirm(10, files, 85)
    expect(mockClient.post).toHaveBeenCalledWith('/repos/10/optimize/confirm', { files, score: 85 }, { timeout: 120000 })
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
})

describe('efficiency API', () => {
  it('getDashboard calls GET /efficiency/dashboard', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { total_repos: 5 } } })
    await getDashboard()
    expect(mockClient.get).toHaveBeenCalledWith('/efficiency/dashboard')
  })

  it('getRepoMetrics calls GET with period param', async () => {
    mockClient.get.mockResolvedValue({ data: { data: [] } })
    await getRepoMetrics(3, 'weekly')
    expect(mockClient.get).toHaveBeenCalledWith('/efficiency/repos/3/metrics', { params: { period: 'weekly' } })
  })

  it('getRepoMetrics uses default period of daily', async () => {
    mockClient.get.mockResolvedValue({ data: { data: [] } })
    await getRepoMetrics(3)
    expect(mockClient.get).toHaveBeenCalledWith('/efficiency/repos/3/metrics', { params: { period: 'daily' } })
  })

  it('getRepoTrend calls GET with period and limit params', async () => {
    mockClient.get.mockResolvedValue({ data: { data: [] } })
    await getRepoTrend(3, 'monthly', 6)
    expect(mockClient.get).toHaveBeenCalledWith('/efficiency/repos/3/trend', { params: { period: 'monthly', limit: 6 } })
  })

  it('getRepoTrend uses default period weekly and limit 12', async () => {
    mockClient.get.mockResolvedValue({ data: { data: [] } })
    await getRepoTrend(3)
    expect(mockClient.get).toHaveBeenCalledWith('/efficiency/repos/3/trend', { params: { period: 'weekly', limit: 12 } })
  })
})

describe('chat API', () => {
  it('sendChatMessage calls POST /repos/:id/chat with message and history', async () => {
    const history = [{ role: 'user' as const, content: 'hello' }]
    mockClient.post.mockResolvedValue({ data: { data: { role: 'assistant', content: 'hi' } } })
    await sendChatMessage(8, 'how to improve?', history)
    expect(mockClient.post).toHaveBeenCalledWith('/repos/8/chat', {
      message: 'how to improve?',
      history,
      preview_files: undefined,
    }, { timeout: 120000 })
  })
})

describe('session API', () => {
  it('listSessions calls GET /sessions with typed filter params', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { items: [], total: 0 } } })

    await listSessions({
      page: 2,
      page_size: 10,
      status: 'active',
      repo_id: 7,
      repo_query: 'org/repo',
      branch: 'feat/filters',
      owner_scope: 'unowned',
    })

    expect(mockClient.get).toHaveBeenCalledWith('/sessions', {
      params: {
        page: 2,
        page_size: 10,
        status: 'active',
        repo_id: 7,
        repo_query: 'org/repo',
        branch: 'feat/filters',
        owner_scope: 'unowned',
      },
    })
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
    await testLLMConnection()
    expect(mockClient.post).toHaveBeenCalledWith('/settings/llm/test')
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
