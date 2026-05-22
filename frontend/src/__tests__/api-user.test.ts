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
import { getUserProviders, createGroupCredential, regenerateGroupCredential } from '@/api/user'

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

  it('createGroupCredential posts to the provider-and-group endpoint', async () => {
    mockClient.post.mockResolvedValue({ data: { data: { api_key_id: 7, secret: 'sk-new' } } })
    await createGroupCredential(7, '42')
    expect(mockClient.post).toHaveBeenCalledWith('/user/providers/7/groups/42/credential')
  })

  it('regenerateGroupCredential posts to the provider-and-group regenerate endpoint', async () => {
    mockClient.post.mockResolvedValue({ data: { data: { api_key_id: 7, secret: 'sk-regen' } } })
    await regenerateGroupCredential(7, '42')
    expect(mockClient.post).toHaveBeenCalledWith('/user/providers/7/groups/42/credential/regenerate')
  })
})
