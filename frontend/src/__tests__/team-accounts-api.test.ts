import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/api/client', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
    delete: vi.fn(),
  },
}))

import client from '@/api/client'
import {
  addTeamAccountManager,
  createAdminTeamAccount,
  createTeamAccount,
  createTeamAccountKey,
  getAdminTeamAccount,
  getTeamAccount,
  getTeamAccountUsage,
  listAdminTeamAccounts,
  listTeamAccountEvents,
  listTeamAccounts,
  removeTeamAccountManager,
  revealTeamAccountCredentials,
  revokeTeamAccountKey,
  setAdminTeamAccountStatus,
  transferTeamAccount,
} from '@/api/teamAccounts'

const mockClient = client as unknown as {
  get: ReturnType<typeof vi.fn>
  post: ReturnType<typeof vi.fn>
  delete: ReturnType<typeof vi.fn>
}

function emptyResponse() {
  return { data: { code: 0, data: {} } }
}

beforeEach(() => {
  vi.clearAllMocks()
  mockClient.get.mockResolvedValue(emptyResponse())
  mockClient.post.mockResolvedValue(emptyResponse())
  mockClient.delete.mockResolvedValue(emptyResponse())
})

describe('team accounts API', () => {
  it('lists the current user department accounts', async () => {
    await listTeamAccounts()
    expect(mockClient.get).toHaveBeenCalledWith('/user/team-accounts')
  })

  it('creates a department account for the current user', async () => {
    const payload = { name: 'Data Platform', department_external_id: 'dept-data' }
    await createTeamAccount(payload)
    expect(mockClient.post).toHaveBeenCalledWith('/user/team-accounts', payload)
  })

  it('loads one account detail', async () => {
    await getTeamAccount(7)
    expect(mockClient.get).toHaveBeenCalledWith('/user/team-accounts/7')
  })

  it('reveals credentials through the credentials route', async () => {
    await revealTeamAccountCredentials(7)
    expect(mockClient.post).toHaveBeenCalledWith('/user/team-accounts/7/credentials')
  })

  it('creates and revokes a key', async () => {
    const payload = { relay_group_id: 9, name: 'CI pipeline' }
    await createTeamAccountKey(7, payload)
    await revokeTeamAccountKey(7, 11)
    expect(mockClient.post).toHaveBeenNthCalledWith(1, '/user/team-accounts/7/keys', payload)
    expect(mockClient.post).toHaveBeenNthCalledWith(2, '/user/team-accounts/7/keys/11/revoke')
  })

  it('adds and removes managers', async () => {
    await addTeamAccountManager(7, 3)
    await removeTeamAccountManager(7, 3)
    expect(mockClient.post).toHaveBeenCalledWith('/user/team-accounts/7/managers', { user_id: 3 })
    expect(mockClient.delete).toHaveBeenCalledWith('/user/team-accounts/7/managers/3')
  })

  it('transfers ownership with the new owner id', async () => {
    await transferTeamAccount(7, 4)
    expect(mockClient.post).toHaveBeenCalledWith('/user/team-accounts/7/transfer', { new_owner_user_id: 4 })
  })

  it('loads usage and events', async () => {
    await getTeamAccountUsage(7, { from: '2026-10-01', to: '2026-10-07' })
    await listTeamAccountEvents(7, 100)
    expect(mockClient.get).toHaveBeenNthCalledWith(1, '/user/team-accounts/7/usage', {
      params: { from: '2026-10-01', to: '2026-10-07' },
    })
    expect(mockClient.get).toHaveBeenNthCalledWith(2, '/user/team-accounts/7/events', {
      params: { limit: 100 },
    })
  })

  it('uses the admin collection and status routes', async () => {
    const payload = { name: 'Data Platform', department_external_id: 'dept-data', owner_user_id: 3 }
    await listAdminTeamAccounts()
    await createAdminTeamAccount(payload)
    await getAdminTeamAccount(7)
    await setAdminTeamAccountStatus(7, 'suspended')
    expect(mockClient.get).toHaveBeenNthCalledWith(1, '/admin/team-accounts')
    expect(mockClient.post).toHaveBeenNthCalledWith(1, '/admin/team-accounts', payload)
    expect(mockClient.get).toHaveBeenNthCalledWith(2, '/admin/team-accounts/7')
    expect(mockClient.post).toHaveBeenNthCalledWith(2, '/admin/team-accounts/7/status', { status: 'suspended' })
  })

})
