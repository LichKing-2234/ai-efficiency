import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useSettingsResourcesStore } from '@/stores/settingsResources'
import type { DirectorySource } from '@/types'

vi.mock('@/api/credential', () => ({
  listCredentials: vi.fn(),
}))

vi.mock('@/api/directory', () => ({
  listDirectorySources: vi.fn(),
}))

const credential = {
  id: 12,
  name: 'Provider Token',
  description: '',
  kind: 'secret_text',
  usage_count: 0,
  summary: {},
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}

const directorySource: DirectorySource = {
  id: 7,
  name: 'Directory Alpha',
  description: '',
  scope: 'full_company',
  enabled: true,
  dsl: 'version: 1',
  schedule_enabled: false,
  schedule_interval: 'daily',
  schedule_timezone: 'UTC',
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((onResolve, onReject) => {
    resolve = onResolve
    reject = onReject
  })
  return { promise, resolve, reject }
}

describe('settings resources store', () => {
  beforeEach(async () => {
    setActivePinia(createPinia())
    vi.useRealTimers()
    const { listCredentials } = await import('@/api/credential')
    const { listDirectorySources } = await import('@/api/directory')
    vi.mocked(listCredentials).mockReset().mockResolvedValue({ data: { data: [credential] } } as any)
    vi.mocked(listDirectorySources).mockReset().mockResolvedValue({ data: { data: { items: [directorySource] } } } as any)
  })

  it('deduplicates concurrent credential and directory source loads', async () => {
    const { listCredentials } = await import('@/api/credential')
    const { listDirectorySources } = await import('@/api/directory')
    const credentialRequest = deferred<any>()
    const directoryRequest = deferred<any>()
    vi.mocked(listCredentials).mockReturnValueOnce(credentialRequest.promise)
    vi.mocked(listDirectorySources).mockReturnValueOnce(directoryRequest.promise)
    const store = useSettingsResourcesStore()

    const credentialsA = store.loadCredentials()
    const credentialsB = store.loadCredentials()
    const sourcesA = store.loadDirectorySources()
    const sourcesB = store.loadDirectorySources()

    expect(listCredentials).toHaveBeenCalledTimes(1)
    expect(listDirectorySources).toHaveBeenCalledTimes(1)

    credentialRequest.resolve({ data: { data: [credential] } })
    directoryRequest.resolve({ data: { data: { items: [directorySource] } } })
    await Promise.all([credentialsA, credentialsB, sourcesA, sourcesB])
    expect(store.credentials).toEqual([credential])
    expect(store.directorySources).toEqual([directorySource])
  })

  it('reuses fresh values for five minutes and reloads after expiry', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-07-16T00:00:00Z'))
    const { listCredentials } = await import('@/api/credential')
    const { listDirectorySources } = await import('@/api/directory')
    const store = useSettingsResourcesStore()

    await store.loadCredentials()
    await store.loadDirectorySources()
    vi.advanceTimersByTime(5 * 60_000 - 1)
    await store.loadCredentials()
    await store.loadDirectorySources()
    expect(listCredentials).toHaveBeenCalledTimes(1)
    expect(listDirectorySources).toHaveBeenCalledTimes(1)

    vi.advanceTimersByTime(1)
    await store.loadCredentials()
    await store.loadDirectorySources()
    expect(listCredentials).toHaveBeenCalledTimes(2)
    expect(listDirectorySources).toHaveBeenCalledTimes(2)
  })

  it('retries errors and supports forced refresh after a mutation', async () => {
    const { listCredentials } = await import('@/api/credential')
    vi.mocked(listCredentials)
      .mockRejectedValueOnce(new Error('temporary'))
      .mockResolvedValue({ data: { data: [credential] } } as any)
    const store = useSettingsResourcesStore()

    await store.loadCredentials()
    expect(store.credentialsError).toBe('temporary')
    await store.loadCredentials()
    expect(store.credentials).toEqual([credential])
    expect(listCredentials).toHaveBeenCalledTimes(2)

    await store.loadCredentials()
    expect(listCredentials).toHaveBeenCalledTimes(2)
    await store.loadCredentials({ force: true })
    expect(listCredentials).toHaveBeenCalledTimes(3)
  })

  it('queues one forced refresh behind an older in-flight request', async () => {
    const { listCredentials } = await import('@/api/credential')
    const oldRequest = deferred<any>()
    const refreshedCredential = { ...credential, name: 'Provider Token Updated' }
    vi.mocked(listCredentials)
      .mockReturnValueOnce(oldRequest.promise)
      .mockResolvedValueOnce({ data: { data: [refreshedCredential] } } as any)
    const store = useSettingsResourcesStore()

    const initial = store.loadCredentials()
    const forced = store.loadCredentials({ force: true })
    expect(listCredentials).toHaveBeenCalledTimes(1)

    oldRequest.resolve({ data: { data: [credential] } })
    await initial
    await forced

    expect(listCredentials).toHaveBeenCalledTimes(2)
    expect(store.credentials).toEqual([refreshedCredential])
  })

  it('clones replacement and API arrays at the store boundary', async () => {
    const store = useSettingsResourcesStore()
    await store.loadCredentials()
    const replacements = [directorySource]
    store.replaceDirectorySources(replacements)
    replacements.splice(0)

    expect(store.directorySources).toEqual([directorySource])
    const response = (await import('@/api/credential')).listCredentials as any
    const returned = (await response.mock.results[0].value).data.data
    returned.splice(0)
    expect(store.credentials).toEqual([credential])
  })
})
