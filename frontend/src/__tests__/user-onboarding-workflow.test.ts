import { describe, expect, it, vi } from 'vitest'
import { useUserOnboardingWorkflow } from '@/composables/useUserOnboardingWorkflow'
import type {
  GroupCredentialMutationResult,
  UserProviderModelsResponse,
  UserProvidersResponse,
  UserProviderSummary,
  UserProviderTestRequest,
  UserProviderTestResult,
} from '@/types'

function providerFixtures(): UserProviderSummary[] {
  return [
    {
      id: 1,
      name: 'empty',
      display_name: 'Empty',
      base_url: 'https://empty.example.com',
      default_model: 'gpt-5.4',
      is_primary: true,
      groups: [],
    },
    {
      id: 2,
      name: 'prod',
      display_name: 'Production',
      base_url: 'https://prod.example.com',
      default_model: 'gpt-5.4',
      is_primary: false,
      groups: [
        {
          group_id: '42',
          group_name: 'Group Alpha',
          platform: 'openai',
          supported_protocols: ['responses', 'chat_completions'],
          recommended_protocol: 'responses',
          credential: { state: 'existing_hidden', api_key_id: 42, key: 'sk-alpha' },
        },
        {
          group_id: '43',
          group_name: 'Group Beta',
          platform: 'anthropic',
          supported_protocols: ['messages', 'responses'],
          recommended_protocol: 'messages',
          credential: { state: 'existing_hidden', api_key_id: 43, key: 'sk-beta' },
        },
        {
          group_id: '44',
          group_name: 'Group Missing',
          platform: 'openai',
          supported_protocols: ['responses', 'chat_completions'],
          recommended_protocol: 'responses',
          credential: { state: 'missing' },
        },
      ],
    },
  ]
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

function workflowOptions(overrides: Partial<{
  loadProviders: () => Promise<UserProvidersResponse | null>
  createCredential: (providerID: number, groupID: string) => Promise<GroupCredentialMutationResult | null>
  regenerateCredential: (providerID: number, groupID: string) => Promise<GroupCredentialMutationResult | null>
  loadModels: (providerID: number, groupID: string, platform: string) => Promise<UserProviderModelsResponse | null>
  testConnection: (providerID: number, request: UserProviderTestRequest) => Promise<UserProviderTestResult | null>
}> = {}) {
  return {
    loadProviders: vi.fn(async () => ({ providers: providerFixtures(), message: '' })),
    createCredential: vi.fn(async () => null),
    regenerateCredential: vi.fn(async () => null),
    loadModels: vi.fn(async () => ({ models: [] })),
    testConnection: vi.fn(async () => null),
    message: (key: string) => key,
    errorMessage: (_error: unknown, key: string) => key,
    ...overrides,
  }
}

describe('useUserOnboardingWorkflow', () => {
  it('selects the first usable provider and advances only through explicit navigation', async () => {
    const workflow = useUserOnboardingWorkflow(workflowOptions())

    await workflow.load()

    expect(workflow.selectedProvider.value?.id).toBe(2)
    expect(workflow.selectedGroup.value?.group_id).toBe('42')
    expect(workflow.visibleStep.value).toBe(0)
    expect(workflow.reachableStep.value).toBe(2)
    expect(workflow.protocol.value).toBe('responses')

    workflow.goToStep(1)
    expect(workflow.visibleStep.value).toBe(1)

    workflow.selectGroup('43')
    expect(workflow.selectedGroup.value?.group_id).toBe('43')
    expect(workflow.visibleStep.value).toBe(0)
    expect(workflow.protocol.value).toBe('messages')
    workflow.dispose()
  })

  it('keeps an explicit Access Group selection when an older Refresh finishes', async () => {
    const refresh = deferred<UserProvidersResponse | null>()
    const loadProviders = vi.fn()
      .mockResolvedValueOnce({ providers: providerFixtures(), message: '' })
      .mockReturnValueOnce(refresh.promise)
    const workflow = useUserOnboardingWorkflow(workflowOptions({ loadProviders }))
    await workflow.load()

    const pendingRefresh = workflow.load()
    workflow.selectGroup('43')
    refresh.resolve({ providers: providerFixtures(), message: 'stale refresh' })
    await pendingRefresh

    expect(workflow.selectedGroup.value?.group_id).toBe('43')
    expect(workflow.selectedMessage.value).toBe('')
    expect(workflow.loading.value).toBe(false)
    workflow.dispose()
  })

  it('settles provider loading without waiting for the dependent model request', async () => {
    const pendingModels = deferred<UserProviderModelsResponse | null>()
    const workflow = useUserOnboardingWorkflow(workflowOptions({
      loadModels: () => pendingModels.promise,
    }))

    await workflow.load()

    expect(workflow.loading.value).toBe(false)
    expect(workflow.modelsLoading.value).toBe(true)
    pendingModels.resolve({ models: [{ id: 'gpt-later' }] })
    await Promise.resolve()
    workflow.dispose()
  })

  it('applies credential creation only to the currently selected Access Group', async () => {
    const staleCreation = deferred<GroupCredentialMutationResult | null>()
    const createCredential = vi.fn()
      .mockReturnValueOnce(staleCreation.promise)
      .mockResolvedValueOnce({ api_key_id: 44, name: 'alice', status: 'active', secret: 'sk-current' })
    const workflow = useUserOnboardingWorkflow(workflowOptions({ createCredential }))
    await workflow.load()
    workflow.selectGroup('44')

    const pendingCreation = workflow.createCredential()
    expect(workflow.credentialLoading.value).toBe(true)
    workflow.selectGroup('43')
    staleCreation.resolve({ api_key_id: 99, name: 'alice', status: 'active', secret: 'sk-stale' })
    await pendingCreation

    expect(workflow.selectedGroup.value?.group_id).toBe('43')
    expect(workflow.selectedKeyValue.value).toBe('sk-beta')
    expect(workflow.visibleStep.value).toBe(0)

    workflow.selectGroup('44')
    await workflow.createCredential()

    expect(createCredential).toHaveBeenLastCalledWith(2, '44')
    expect(workflow.selectedKeyValue.value).toBe('sk-current')
    expect(workflow.selectedGroup.value?.credential.state).toBe('existing_hidden')
    expect(workflow.visibleStep.value).toBe(1)
    workflow.dispose()
  })

  it('clears a credential error when the user selects another Access Group', async () => {
    const workflow = useUserOnboardingWorkflow(workflowOptions({
      createCredential: vi.fn(async () => { throw new Error('synthetic creation failure') }),
    }))
    await workflow.load()
    workflow.selectGroup('44')

    await workflow.createCredential()
    expect(workflow.error.value).toBe('createKeyFailed')

    workflow.selectGroup('43')
    expect(workflow.error.value).toBe('')
    workflow.dispose()
  })

  it('keeps model choices bound to the current Access Group', async () => {
    const staleModels = deferred<UserProviderModelsResponse | null>()
    const currentModels = deferred<UserProviderModelsResponse | null>()
    const loadModels = vi.fn()
      .mockResolvedValueOnce({ models: [{ id: 'gpt-initial' }] })
      .mockReturnValueOnce(staleModels.promise)
      .mockReturnValueOnce(currentModels.promise)
    const workflow = useUserOnboardingWorkflow(workflowOptions({ loadModels }))
    await workflow.load()

    workflow.selectGroup('43')
    expect(workflow.models.value).toEqual([])
    workflow.selectGroup('42')
    workflow.setModel('gpt-manual')
    currentModels.resolve({
      models: [
        { id: 'gpt-current', display_name: 'GPT Current' },
        { id: 'gpt-manual', display_name: 'GPT Manual' },
      ],
    })
    await Promise.resolve()
    staleModels.resolve({ models: [{ id: 'claude-stale', display_name: 'Claude Stale' }] })
    await Promise.resolve()

    expect(loadModels).toHaveBeenLastCalledWith(2, '42', 'openai')
    expect(workflow.models.value).toEqual([
      { id: 'gpt-current', display_name: 'GPT Current' },
      { id: 'gpt-manual', display_name: 'GPT Manual' },
    ])
    expect(workflow.model.value).toBe('gpt-manual')
    expect(workflow.modelsLoading.value).toBe(false)
    workflow.dispose()
  })

  it('keeps only the latest Connection Test for the current key, model, and protocol', async () => {
    const staleTest = deferred<UserProviderTestResult | null>()
    const currentTest = deferred<UserProviderTestResult | null>()
    const testConnection = vi.fn()
      .mockReturnValueOnce(staleTest.promise)
      .mockReturnValueOnce(currentTest.promise)
    const workflow = useUserOnboardingWorkflow(workflowOptions({
      loadModels: async () => ({ models: [{ id: 'gpt-5.4' }, { id: 'gpt-5.5' }] }),
      testConnection,
    }))
    await workflow.load()
    workflow.goToStep(1)

    const staleRequest = workflow.testConnection()
    workflow.setProtocol('chat_completions')
    expect(workflow.connectionResult.value).toBeNull()
    const currentRequest = workflow.testConnection()
    currentTest.resolve({ success: true, message: 'Current result', response: 'ok' })
    await currentRequest
    staleTest.resolve({ success: true, message: 'Stale result', response: 'stale' })
    await staleRequest

    expect(testConnection).toHaveBeenLastCalledWith(2, {
      platform: 'openai',
      group_id: '42',
      model: 'gpt-5.4',
      protocol: 'chat_completions',
    })
    expect(workflow.connectionResult.value).toEqual({ success: true, message: 'Current result', response: 'ok' })
    expect(workflow.visibleStep.value).toBe(1)

    workflow.setModel('gpt-5.5')
    expect(workflow.connectionResult.value).toBeNull()
    workflow.dispose()
  })

  it('regenerates the current credential without leaving the Connection Test step', async () => {
    const regeneration = deferred<GroupCredentialMutationResult | null>()
    const staleTest = deferred<UserProviderTestResult | null>()
    const regenerateCredential = vi.fn(() => regeneration.promise)
    const testConnection = vi.fn()
      .mockResolvedValueOnce({ success: true, message: 'Connected' })
      .mockReturnValueOnce(staleTest.promise)
    const workflow = useUserOnboardingWorkflow(workflowOptions({
      loadModels: async () => ({ models: [{ id: 'gpt-5.4' }, { id: 'gpt-5.5' }] }),
      testConnection,
      regenerateCredential,
    }))
    await workflow.load()
    workflow.goToStep(1)
    workflow.setModel('gpt-5.5')
    await workflow.testConnection()
    workflow.setConfigMethod('manual')
    workflow.setKeyRevealed(true)

    const pendingRegeneration = workflow.regenerateCredential()
    const oldKeyTest = workflow.testConnection()
    regeneration.resolve({ api_key_id: 88, name: 'alice', status: 'active', secret: 'sk-regenerated' })
    await pendingRegeneration
    staleTest.resolve({ success: true, message: 'Result from old key' })
    await oldKeyTest

    expect(regenerateCredential).toHaveBeenCalledWith(2, '42')
    expect(workflow.selectedKeyValue.value).toBe('sk-regenerated')
    expect(workflow.connectionResult.value).toBeNull()
    expect(workflow.visibleStep.value).toBe(1)
    expect(workflow.model.value).toBe('gpt-5.5')
    expect(workflow.selectedConfigMethod.value).toBe('manual')
    expect(workflow.keyRevealed.value).toBe(false)
    workflow.dispose()
  })
})
