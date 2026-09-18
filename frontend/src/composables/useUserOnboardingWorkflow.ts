import { computed, reactive, ref } from 'vue'
import { resolveCCSwitchAppsForGroup } from '@/utils/userSetupReview'
import type {
  GroupCredentialMutationResult,
  UserProviderModelsResponse,
  UserProviderProtocol,
  UserProviderSummary,
  UserProviderTestRequest,
  UserProviderTestResult,
  UserProvidersResponse,
} from '@/types'

export type UserOnboardingStep = 0 | 1 | 2
export type UserOnboardingConfigMethod = 'manual' | 'automatic' | 'ccswitch'
export type UserOnboardingMessageKey =
  | 'loadFailed'
  | 'createKeyFailed'
  | 'regenerateKeyFailed'
  | 'createKeyBeforeModelList'
  | 'noModelsAvailable'
  | 'modelLoadFailed'
  | 'createKeyBeforeTesting'
  | 'modelRequired'
  | 'requestFailed'

export interface UserOnboardingWorkflowOptions {
  loadProviders: () => Promise<UserProvidersResponse | null>
  createCredential: (providerID: number, groupID: string) => Promise<GroupCredentialMutationResult | null>
  regenerateCredential: (providerID: number, groupID: string) => Promise<GroupCredentialMutationResult | null>
  loadModels: (providerID: number, groupID: string, platform: string) => Promise<UserProviderModelsResponse | null>
  testConnection: (providerID: number, request: UserProviderTestRequest) => Promise<UserProviderTestResult | null>
  message: (key: UserOnboardingMessageKey) => string
  errorMessage: (error: unknown, fallbackKey: UserOnboardingMessageKey) => string
}

export function useUserOnboardingWorkflow(options: UserOnboardingWorkflowOptions) {
  const loading = ref(false)
  const error = ref('')
  const providers = ref<UserProviderSummary[]>([])
  const selectedMessage = ref('')
  const selectedProviderID = ref<number | null>(null)
  const selectedGroupID = ref<string | null>(null)
  const visibleStep = ref<UserOnboardingStep>(0)
  const protocol = ref<UserProviderTestRequest['protocol'] | ''>('')
  const sessionSecrets = reactive<Record<string, string>>({})
  const revealedSecretKeys = reactive<Record<string, boolean>>({})
  const selectedConfigMethod = ref<UserOnboardingConfigMethod | null>(null)
  const credentialLoading = ref(false)
  const models = ref<UserProviderModelsResponse['models']>([])
  const model = ref('')
  const modelsLoading = ref(false)
  const modelsMessage = ref('')
  const connectionLoading = ref(false)
  const connectionResult = ref<UserProviderTestResult | null>(null)
  let loadGeneration = 0
  let credentialGeneration = 0
  let modelsGeneration = 0
  let connectionGeneration = 0

  const selectedProvider = computed(() => (
    providers.value.find((provider) => provider.id === selectedProviderID.value) ?? null
  ))
  const selectedGroup = computed(() => (
    selectedProvider.value?.groups.find((group) => group.group_id === selectedGroupID.value) ?? null
  ))
  const selectedSecretKey = computed(() => (
    selectedProvider.value && selectedGroup.value
      ? `${selectedProvider.value.id}:${selectedGroup.value.group_id}`
      : ''
  ))
  const selectedKeyValue = computed(() => (
    (selectedSecretKey.value ? sessionSecrets[selectedSecretKey.value] : '')
    || selectedGroup.value?.credential.key
    || ''
  ))
  const keyRevealed = computed(() => Boolean(
    selectedSecretKey.value && revealedSecretKeys[selectedSecretKey.value],
  ))
  const reachableStep = computed<UserOnboardingStep>(() => selectedKeyValue.value ? 2 : 0)
  const canTestConnection = computed(() => Boolean(
    selectedKeyValue.value && model.value.trim() && protocol.value,
  ))
  const onboardingState = computed(() => {
    if (!selectedGroup.value) return 'no_group_selected' as const
    if (!selectedKeyValue.value) return 'group_selected_without_key' as const
    if (connectionResult.value?.success) return 'test_success' as const
    if (connectionResult.value) return 'test_failed' as const
    return 'key_ready_without_test' as const
  })

  function invalidateConnection() {
    connectionGeneration += 1
    connectionLoading.value = false
    connectionResult.value = null
  }

  function preferredProvider(rows: UserProviderSummary[]) {
    return rows.find((provider) => provider.is_primary && provider.groups.length > 0)
      ?? rows.find((provider) => provider.groups.length > 0)
      ?? rows.find((provider) => provider.is_primary)
      ?? rows[0]
      ?? null
  }

  function preferredConfigMethod(): UserOnboardingConfigMethod {
    const group = selectedGroup.value
    if (!group || !selectedKeyValue.value) return 'manual'
    return resolveCCSwitchAppsForGroup(group.platform, group.group_name).length > 0
      ? 'ccswitch'
      : 'manual'
  }

  function resetSelection(provider: UserProviderSummary | null, groupID?: string) {
    credentialGeneration += 1
    credentialLoading.value = false
    invalidateConnection()
    error.value = ''
    selectedProviderID.value = provider?.id ?? null
    selectedGroupID.value = groupID ?? provider?.groups[0]?.group_id ?? null
    protocol.value = selectedGroup.value?.recommended_protocol
      || selectedGroup.value?.supported_protocols?.[0]
      || ''
    modelsGeneration += 1
    models.value = []
    model.value = ''
    modelsLoading.value = false
    modelsMessage.value = selectedGroup.value && !selectedKeyValue.value
      ? options.message('createKeyBeforeModelList')
      : ''
    selectedConfigMethod.value = preferredConfigMethod()
    visibleStep.value = 0
  }

  async function loadModels() {
    const provider = selectedProvider.value
    const group = selectedGroup.value
    const generation = ++modelsGeneration
    models.value = []
    modelsLoading.value = false
    modelsMessage.value = ''
    if (!provider || !group) {
      model.value = ''
      return
    }
    if (!selectedKeyValue.value) {
      model.value = ''
      modelsMessage.value = options.message('createKeyBeforeModelList')
      return
    }
    modelsLoading.value = true
    try {
      const data = await options.loadModels(provider.id, group.group_id, group.platform)
      if (generation !== modelsGeneration) return
      models.value = data?.models ?? []
      const selectedModel = model.value.trim()
      model.value = models.value.some((item) => item.id === selectedModel)
        ? selectedModel
        : models.value[0]?.id ?? ''
      modelsMessage.value = data?.message ?? ''
      if (models.value.length === 0 && !modelsMessage.value) {
        modelsMessage.value = options.message('noModelsAvailable')
      }
    } catch (requestError) {
      if (generation !== modelsGeneration) return
      model.value = ''
      modelsMessage.value = options.errorMessage(requestError, 'modelLoadFailed')
    } finally {
      if (generation === modelsGeneration) modelsLoading.value = false
    }
  }

  async function load() {
    const generation = ++loadGeneration
    credentialGeneration += 1
    credentialLoading.value = false
    invalidateConnection()
    loading.value = true
    error.value = ''
    try {
      const data = await options.loadProviders()
      if (generation !== loadGeneration) return
      providers.value = data?.providers ?? []
      selectedMessage.value = data?.message ?? ''
      resetSelection(preferredProvider(providers.value))
      void loadModels()
    } catch (requestError) {
      if (generation !== loadGeneration) return
      providers.value = []
      selectedMessage.value = ''
      resetSelection(null)
      error.value = options.errorMessage(requestError, 'loadFailed')
    } finally {
      if (generation === loadGeneration) loading.value = false
    }
  }

  function selectProvider(providerID: number) {
    loadGeneration += 1
    loading.value = false
    resetSelection(providers.value.find((provider) => provider.id === providerID) ?? null)
    void loadModels()
  }

  function selectGroup(groupID: string) {
    loadGeneration += 1
    loading.value = false
    resetSelection(selectedProvider.value, groupID)
    void loadModels()
  }

  function goToStep(step: UserOnboardingStep) {
    if (step <= reachableStep.value) visibleStep.value = step
  }

  async function continueFromAccess() {
    if (selectedKeyValue.value) {
      visibleStep.value = 1
      return
    }
    await createCredential()
  }

  function setModel(value: string) {
    if (model.value === value) return
    model.value = value
    invalidateConnection()
  }

  function setProtocol(value: UserProviderProtocol | '') {
    if (protocol.value === value) return
    protocol.value = value
    invalidateConnection()
  }

  function setConfigMethod(method: UserOnboardingConfigMethod) {
    selectedConfigMethod.value = method
  }

  function setKeyRevealed(revealed: boolean) {
    if (!selectedSecretKey.value) return
    revealedSecretKeys[selectedSecretKey.value] = revealed
  }

  function applyCredential(
    providerID: number,
    groupID: string,
    credential: GroupCredentialMutationResult,
  ) {
    sessionSecrets[`${providerID}:${groupID}`] = credential.secret
    revealedSecretKeys[`${providerID}:${groupID}`] = false
    providers.value = providers.value.map((provider) => (
      provider.id !== providerID
        ? provider
        : {
            ...provider,
            groups: provider.groups.map((group) => (
              group.group_id !== groupID
                ? group
                : {
                    ...group,
                    credential: {
                      ...group.credential,
                      state: 'existing_hidden',
                      api_key_id: credential.api_key_id,
                      key: credential.secret,
                      name: credential.name,
                      status: credential.status,
                    },
                  }
            )),
          }
    ))
  }

  async function createCredential() {
    const provider = selectedProvider.value
    const group = selectedGroup.value
    if (!provider || !group || credentialLoading.value) return
    const generation = ++credentialGeneration
    credentialLoading.value = true
    error.value = ''
    try {
      const credential = await options.createCredential(provider.id, group.group_id)
      if (generation !== credentialGeneration || !credential) return
      invalidateConnection()
      applyCredential(provider.id, group.group_id, credential)
      selectedConfigMethod.value = preferredConfigMethod()
      visibleStep.value = 1
      void loadModels()
    } catch (requestError) {
      if (generation === credentialGeneration) {
        error.value = options.errorMessage(requestError, 'createKeyFailed')
      }
    } finally {
      if (generation === credentialGeneration) credentialLoading.value = false
    }
  }

  async function regenerateCredential() {
    const provider = selectedProvider.value
    const group = selectedGroup.value
    if (!provider || !group || credentialLoading.value) return
    const generation = ++credentialGeneration
    credentialLoading.value = true
    error.value = ''
    try {
      const credential = await options.regenerateCredential(provider.id, group.group_id)
      if (generation !== credentialGeneration || !credential) return
      invalidateConnection()
      applyCredential(provider.id, group.group_id, credential)
      void loadModels()
    } catch (requestError) {
      if (generation === credentialGeneration) {
        error.value = options.errorMessage(requestError, 'regenerateKeyFailed')
      }
    } finally {
      if (generation === credentialGeneration) credentialLoading.value = false
    }
  }

  async function testConnection() {
    const provider = selectedProvider.value
    const group = selectedGroup.value
    if (!provider || !group) return
    if (!selectedKeyValue.value) {
      connectionResult.value = { success: false, message: options.message('createKeyBeforeTesting') }
      return
    }
    const selectedModel = model.value.trim()
    if (!selectedModel) {
      connectionResult.value = { success: false, message: options.message('modelRequired') }
      return
    }
    const generation = ++connectionGeneration
    connectionLoading.value = true
    connectionResult.value = null
    try {
      const result = await options.testConnection(provider.id, {
        platform: group.platform,
        group_id: group.group_id,
        model: selectedModel,
        protocol: protocol.value || undefined,
      })
      if (generation !== connectionGeneration) return
      connectionResult.value = result ?? { success: false, message: options.message('requestFailed') }
    } catch (requestError) {
      if (generation !== connectionGeneration) return
      connectionResult.value = {
        success: false,
        message: options.errorMessage(requestError, 'requestFailed'),
      }
    } finally {
      if (generation === connectionGeneration) connectionLoading.value = false
    }
  }

  function dispose() {
    loadGeneration += 1
    credentialGeneration += 1
    modelsGeneration += 1
    connectionGeneration += 1
  }

  return {
    loading,
    error,
    providers,
    selectedMessage,
    selectedProviderID,
    selectedGroupID,
    selectedProvider,
    selectedGroup,
    selectedKeyValue,
    keyRevealed,
    visibleStep,
    reachableStep,
    onboardingState,
    protocol,
    credentialLoading,
    models,
    model,
    modelsLoading,
    modelsMessage,
    connectionLoading,
    connectionResult,
    canTestConnection,
    selectedConfigMethod,
    load,
    selectProvider,
    selectGroup,
    goToStep,
    continueFromAccess,
    setModel,
    setProtocol,
    setConfigMethod,
    setKeyRevealed,
    createCredential,
    regenerateCredential,
    testConnection,
    dispose,
  }
}
