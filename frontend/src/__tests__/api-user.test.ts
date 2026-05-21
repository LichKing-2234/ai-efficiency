import { describe, it, expect, vi, beforeEach } from 'vitest'

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
import { getUserProviders, createManagedKey, regenerateManagedKey } from '@/api/user'

const mockClient = client as unknown as {
  get: ReturnType<typeof vi.fn>
  post: ReturnType<typeof vi.fn>
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('user API', () => {
  it('getUserProviders calls GET /user/providers', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { providers: [] } } })
    await getUserProviders()
    expect(mockClient.get).toHaveBeenCalledWith('/user/providers')
  })

  it('createManagedKey posts to the provider-scoped endpoint', async () => {
    mockClient.post.mockResolvedValue({ data: { data: { api_key_id: 7, secret: 'sk-new' } } })
    await createManagedKey(7)
    expect(mockClient.post).toHaveBeenCalledWith('/user/providers/7/managed-key')
  })

  it('regenerateManagedKey posts to the regenerate endpoint', async () => {
    mockClient.post.mockResolvedValue({ data: { data: { api_key_id: 7, secret: 'sk-regen' } } })
    await regenerateManagedKey(7)
    expect(mockClient.post).toHaveBeenCalledWith('/user/providers/7/managed-key/regenerate')
  })
})
