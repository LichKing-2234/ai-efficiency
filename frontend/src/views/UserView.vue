<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import AppLayout from '@/components/AppLayout.vue'
import { createGroupCredential, getUserProviderModels, getUserProviders, regenerateGroupCredential, testUserProvider } from '@/api/user'
import { useAuthStore } from '@/stores/auth'
import { useI18n } from '@/i18n'
import type {
  UserProviderTestResult,
  UserProviderModel,
  UserProviderSummary,
} from '@/types'
import {
  buildCCSwitchProviderImportLink,
  buildDeviceLoginCommand,
  buildDiscoverCommand,
  buildDoctorCommand,
  buildPreferredGithubConnectivityCommand,
  buildHooksGlobalCommand,
  buildHooksStatusUploadsCommand,
  buildInstallCommand,
  buildLoginCommand,
  buildManualConfigSnippets,
  buildPreferredInstallCommand,
  buildRepoInitCommand,
  resolveCCSwitchAppForPlatform,
  buildSyncCommand,
  buildWindowsInstallCommand,
  detectInstallPlatform,
} from '@/utils/userSetupReview'
import type { ManualConfigSnippet } from '@/utils/userSetupReview'

const auth = useAuthStore()
const { t } = useI18n()

const loading = ref(true)
const error = ref('')
const providers = ref<UserProviderSummary[]>([])
const selectedProviderId = ref<number | null>(null)
const selectedGroupId = ref<string | null>(null)
const selectedMessage = ref('')
const sessionSecrets = reactive<Record<string, string>>({})
const revealedSecretKeys = reactive<Record<string, boolean>>({})
const providerTestModel = ref('')
const providerModelOptions = ref<UserProviderModel[]>([])
const providerModelsLoading = ref(false)
const providerModelsMessage = ref('')
const providerModelsRequestId = ref(0)
const providerTestPrompt = ref('Hi')
const providerTestLoading = ref(false)
const providerTestResult = ref<UserProviderTestResult | null>(null)
const copiedCommandKey = ref('')
const credentialMutationLoading = ref(false)
const selectedConfigMethod = ref<'manual' | 'automatic' | 'ccswitch' | null>(null)
const manualConfigConfirmKey = ref('')
type SecretAction = 'reveal' | 'copy' | 'regenerate'
const secretConfirmAction = ref<SecretAction | null>(null)

const currentOrigin = computed(() => window.location.origin)
const selectedProvider = computed(() => providers.value.find((provider) => provider.id === selectedProviderId.value) ?? null)
const selectedGroup = computed(() => selectedProvider.value?.groups.find((group) => group.group_id === selectedGroupId.value) ?? null)
const installPlatform = computed(() => detectInstallPlatform())
const shellInstallCommand = computed(() => buildInstallCommand(currentOrigin.value))
const windowsInstallCommand = computed(() => buildWindowsInstallCommand(currentOrigin.value))
const installCommand = computed(() => buildPreferredInstallCommand(currentOrigin.value, installPlatform.value))
const repoChangeDirCommand = computed(() => 'cd /path/to/repo')
const alternateInstallLabel = computed(() => installPlatform.value === 'windows' ? 'macOS / Linux' : 'Windows PowerShell')
const alternateInstallCommand = computed(() => installPlatform.value === 'windows' ? shellInstallCommand.value : windowsInstallCommand.value)
const alternateInstallCopyKey = computed(() => installPlatform.value === 'windows' ? 'install-macos' : 'install-windows')
const githubConnectivityCommand = computed(() => buildPreferredGithubConnectivityCommand(installPlatform.value))
const loginCommand = computed(() => buildLoginCommand(currentOrigin.value))
const deviceLoginCommand = computed(() => buildDeviceLoginCommand(currentOrigin.value))
const discoverCommand = computed(() => selectedProvider.value ? buildDiscoverCommand(currentOrigin.value, selectedProvider.value.name) : '')
const hooksGlobalCommand = computed(() => buildHooksGlobalCommand())
const repoInitCommand = computed(() => buildRepoInitCommand())
const doctorCommand = computed(() => buildDoctorCommand())
const syncCommand = computed(() => buildSyncCommand())
const hooksStatusUploadsCommand = computed(() => buildHooksStatusUploadsCommand())
const readyAccessGroupCount = computed(() =>
  providers.value.reduce(
    (count, provider) => count + provider.groups.filter((group) => group.credential.state === 'existing_hidden').length,
    0
  )
)
const totalAccessGroupCount = computed(() =>
  providers.value.reduce((count, provider) => count + provider.groups.length, 0)
)
const onboardingState = computed(() => {
  if (!selectedGroup.value) return 'no_group_selected' as const
  if (!selectedKeyValue.value) return 'group_selected_without_key' as const
  if (providerTestResult.value?.success) return 'test_success' as const
  if (providerTestResult.value && !providerTestResult.value.success) return 'test_failed' as const
  return 'key_ready_without_test' as const
})
const primaryOnboardingActionLabel = computed(() => {
  if (onboardingState.value === 'group_selected_without_key') return t('user.createMyApiKey')
  if (onboardingState.value === 'key_ready_without_test' || onboardingState.value === 'test_failed') {
    return t('user.runConnectionTest')
  }
  return ''
})
const showConfigurationMethods = computed(() => !!selectedKeyValue.value)
const ccSwitchImports = computed(() => {
  if (!selectedProvider.value || !selectedGroup.value || !selectedKeyValue.value) return []
  const app = resolveCCSwitchAppForPlatform(selectedGroup.value.platform)
  if (!app) return []
  const selectedModel = providerTestModel.value.trim()
  return [{
    key: app,
    label: app === 'codex' ? t('user.importToCodex') : app === 'claude' ? t('user.importToClaude') : t('user.importToGemini'),
    href: buildCCSwitchProviderImportLink({
      app,
      name: `${selectedProvider.value.display_name} / ${selectedGroup.value.group_name}`,
      endpoint: selectedProvider.value.base_url,
      apiKey: selectedKeyValue.value,
      model: selectedModel || (app === 'codex' ? 'gpt-5.4' : undefined),
    }),
  }]
})
const automaticMachineCommands = computed(() => [
  { key: 'auto-install', label: t('user.installCli'), value: installCommand.value },
  { key: 'auto-connectivity', label: t('user.githubConnectivityTitle'), value: githubConnectivityCommand.value },
  { key: 'auto-login', label: t('user.setupStepLoginTitle'), value: loginCommand.value },
  { key: 'auto-discover', label: t('user.setupStepConfigureTitle'), value: discoverCommand.value || t('user.selectProviderCommand') },
  { key: 'auto-hooks', label: t('user.setupStepHooksTitle'), value: hooksGlobalCommand.value },
])
const automaticRepoCommands = computed(() => [
  { key: 'auto-repo-cd', label: t('user.repoStep1'), value: repoChangeDirCommand.value },
  { key: 'auto-init', label: t('user.repoStep2'), value: repoInitCommand.value },
  { key: 'auto-doctor', label: t('user.repoStep3'), value: doctorCommand.value },
])

function credentialStatusLabel(state: string) {
  return state === 'existing_hidden' ? t('user.readyToUse') : t('user.needsSetup')
}

function credentialStatusHelp(state: string, hasKey?: boolean) {
  if (state !== 'existing_hidden') return t('user.missingKey')
  return hasKey ? t('user.readyWithKey') : t('user.readyNoKey')
}

function secretStateKey(providerId: number, groupId: string) {
  return `${providerId}:${groupId}`
}

const selectedSecretKey = computed(() => {
  if (!selectedProvider.value || !selectedGroup.value) return ''
  return secretStateKey(selectedProvider.value.id, selectedGroup.value.group_id)
})
const selectedSecret = computed(() => (selectedSecretKey.value ? sessionSecrets[selectedSecretKey.value] ?? '' : ''))
const selectedKeyValue = computed(() => selectedSecret.value || selectedGroup.value?.credential.key || '')
const canReveal = computed(() => !!selectedKeyValue.value)
const canTestProvider = computed(() => !!selectedKeyValue.value && !!providerTestModel.value.trim())
const isSecretRevealed = computed(() => !!selectedSecretKey.value && !!revealedSecretKeys[selectedSecretKey.value])
const displayedSecret = computed(() => {
  if (!selectedKeyValue.value) return ''
  return isSecretRevealed.value ? selectedKeyValue.value : maskApiKey(selectedKeyValue.value)
})
const manualConfigDisplayApiKey = computed(() => {
  if (!selectedKeyValue.value) return t('user.manualConfigMissingKeyPlaceholder')
  return isSecretRevealed.value ? selectedKeyValue.value : t('user.manualConfigHiddenKeyPlaceholder')
})
const manualConfigDisplaySnippets = computed(() => buildSelectedManualConfigSnippets(manualConfigDisplayApiKey.value))
const manualConfigCopySnippets = computed(() =>
  buildSelectedManualConfigSnippets(selectedKeyValue.value || t('user.manualConfigMissingKeyPlaceholder'))
)
const pendingManualConfigSnippet = computed(() =>
  manualConfigCopySnippets.value.find((snippet) => snippet.key === manualConfigConfirmKey.value) ?? null
)

function providerModelLabel(model: UserProviderModel) {
  const displayName = model.display_name?.trim()
  if (!displayName || displayName === model.id) return model.id
  return `${displayName} (${model.id})`
}

function selectDefaultGroup(provider: UserProviderSummary | null) {
  selectedGroupId.value = provider?.groups[0]?.group_id ?? null
}

function selectDefaultProvider(rows: UserProviderSummary[]) {
  const primary = rows.find((provider) => provider.is_primary)
  selectedProviderId.value = primary?.id ?? rows[0]?.id ?? null
  const provider = rows.find((item) => item.id === selectedProviderId.value) ?? null
  selectDefaultGroup(provider)
}

function resetPostKeyFlow() {
  providerTestResult.value = null
  selectedConfigMethod.value = null
}

function selectProvider(providerId: number) {
  secretConfirmAction.value = null
  manualConfigConfirmKey.value = ''
  selectedProviderId.value = providerId
  const provider = providers.value.find((item) => item.id === providerId) ?? null
  selectDefaultGroup(provider)
  resetPostKeyFlow()
}

function selectGroup(groupId: string) {
  secretConfirmAction.value = null
  manualConfigConfirmKey.value = ''
  selectedGroupId.value = groupId
  resetPostKeyFlow()
}

function resetProviderModels(message = '') {
  providerModelOptions.value = []
  providerModelsLoading.value = false
  providerModelsMessage.value = message
  providerTestModel.value = ''
}

async function loadProviderModels() {
  const provider = selectedProvider.value
  const group = selectedGroup.value
  const requestId = providerModelsRequestId.value + 1
  providerModelsRequestId.value = requestId
  providerModelsMessage.value = ''

  if (!provider || !group) {
    resetProviderModels()
    return
  }
  if (!selectedKeyValue.value) {
    resetProviderModels(t('user.createKeyBeforeModelList'))
    return
  }

  providerModelsLoading.value = true
  try {
    const res = await getUserProviderModels(provider.id, group.group_id, group.platform)
    if (providerModelsRequestId.value !== requestId) return
    const data = res.data.data
    const models = data?.models ?? []
    providerModelOptions.value = models
    providerModelsMessage.value = data?.message ?? ''
    if (models.length > 0) {
      const current = providerTestModel.value.trim()
      if (!models.some((model) => model.id === current)) {
        providerTestModel.value = models[0].id
      }
    } else {
      providerTestModel.value = ''
      providerModelsMessage.value = providerModelsMessage.value || t('user.noModelsAvailable')
    }
  } catch (err: any) {
    if (providerModelsRequestId.value !== requestId) return
    resetProviderModels(err.response?.data?.message || err.message || t('user.modelLoadFailed'))
  } finally {
    if (providerModelsRequestId.value === requestId) {
      providerModelsLoading.value = false
    }
  }
}

async function loadProviders() {
  loading.value = true
  error.value = ''
  try {
    const res = await getUserProviders()
    const data = res.data.data
    providers.value = data?.providers ?? []
    selectedMessage.value = data?.message ?? ''
    selectDefaultProvider(providers.value)
  } catch (err: any) {
    error.value = err.response?.data?.message || t('user.loadFailed')
    providers.value = []
    selectedProviderId.value = null
    selectedGroupId.value = null
  } finally {
    loading.value = false
  }
}

function maskApiKey(key: string) {
  if (!key) return ''
  if (key.length <= 12) return `${key.slice(0, 4)}***`
  return `${key.slice(0, 6)}...${key.slice(-4)}`
}

function buildSelectedManualConfigSnippets(apiKey: string) {
  if (!selectedProvider.value || !selectedGroup.value) return []
  return buildManualConfigSnippets({
    providerName: selectedProvider.value.name,
    baseUrl: selectedProvider.value.base_url,
    platform: selectedGroup.value.platform,
    apiKey,
  })
}

function manualConfigSnippetTitle(snippet: ManualConfigSnippet) {
  switch (snippet.key) {
    case 'codex-config':
      return t('user.manualConfigCodexConfig')
    case 'codex-auth':
      return t('user.manualConfigCodexAuth')
    case 'claude-settings':
      return t('user.manualConfigClaudeSettings')
    case 'gemini-env':
      return t('user.manualConfigGeminiEnv')
    case 'gemini-reload':
      return t('user.manualConfigGeminiReload')
    case 'gemini-model':
      return t('user.manualConfigGeminiModel')
    default:
      return snippet.path
  }
}

function updateSelectedGroupCredential(apiKeyId: number, name: string, status: string, key: string) {
  if (!selectedProvider.value || !selectedGroup.value) return
  providers.value = providers.value.map((provider) => {
    if (provider.id !== selectedProvider.value?.id) {
      return provider
    }
    return {
      ...provider,
      groups: provider.groups.map((group) =>
        group.group_id === selectedGroup.value?.group_id
          ? {
              ...group,
              credential: {
                ...group.credential,
                state: 'existing_hidden',
                api_key_id: apiKeyId,
                key,
                name,
                status,
              },
            }
          : group
      ),
    }
  })
}

async function handleCreateKey() {
  if (!selectedProvider.value || !selectedGroup.value) return
  if (credentialMutationLoading.value) return
  credentialMutationLoading.value = true
  error.value = ''
  try {
    const res = await createGroupCredential(selectedProvider.value.id, selectedGroup.value.group_id)
    const data = res.data.data
    if (!data) return
    sessionSecrets[selectedSecretKey.value] = data.secret
    revealedSecretKeys[selectedSecretKey.value] = false
    updateSelectedGroupCredential(data.api_key_id, data.name, data.status, data.secret)
    resetPostKeyFlow()
  } catch (err: any) {
    error.value = err.response?.data?.message || err.message || t('user.createKeyFailed')
  } finally {
    credentialMutationLoading.value = false
  }
}

function requestSecretAction(action: SecretAction) {
  manualConfigConfirmKey.value = ''
  secretConfirmAction.value = action
}

function secretConfirmTitle(action: SecretAction) {
  if (action === 'reveal') return t('user.confirmRevealKey')
  if (action === 'copy') return t('user.confirmCopyKey')
  return t('user.confirmRegenerateKey')
}

async function confirmSecretAction() {
  const action = secretConfirmAction.value
  if (!action) return
  secretConfirmAction.value = null
  if (action === 'reveal') {
    revealSelectedKey()
  } else if (action === 'copy') {
    await copySelectedKey()
  } else {
    await handleRegenerateKey()
  }
}

async function handleRegenerateKey() {
  if (!selectedProvider.value || !selectedGroup.value) return
  if (credentialMutationLoading.value) return
  credentialMutationLoading.value = true
  error.value = ''
  try {
    const res = await regenerateGroupCredential(selectedProvider.value.id, selectedGroup.value.group_id)
    const data = res.data.data
    if (!data) return
    sessionSecrets[selectedSecretKey.value] = data.secret
    revealedSecretKeys[selectedSecretKey.value] = true
    updateSelectedGroupCredential(data.api_key_id, data.name, data.status, data.secret)
    resetPostKeyFlow()
  } catch (err: any) {
    error.value = err.response?.data?.message || err.message || t('user.regenerateKeyFailed')
  } finally {
    credentialMutationLoading.value = false
  }
}

function revealSelectedKey() {
  if (!selectedSecretKey.value) return
  revealedSecretKeys[selectedSecretKey.value] = true
}

function hideSelectedKey() {
  if (!selectedSecretKey.value) return
  revealedSecretKeys[selectedSecretKey.value] = false
}

async function copySelectedKey() {
  if (!selectedKeyValue.value) return
  await navigator.clipboard.writeText(selectedKeyValue.value)
}

async function copyCommand(key: string, command: string) {
  if (!command || command === t('user.selectProviderCommand')) return
  await navigator.clipboard.writeText(command)
  copiedCommandKey.value = key
  window.setTimeout(() => {
    if (copiedCommandKey.value === key) copiedCommandKey.value = ''
  }, 1800)
}

function copyCommandLabel(key: string) {
  return copiedCommandKey.value === key ? t('user.copied') : t('user.copyCommand')
}

function manualConfigCopyLabel(snippet: ManualConfigSnippet) {
  return copiedCommandKey.value === `manual-config-${snippet.key}` ? t('user.copied') : t('user.copyConfigSnippet')
}

async function copyManualConfigSnippet(snippet: ManualConfigSnippet) {
  const copySnippet = manualConfigCopySnippets.value.find((item) => item.key === snippet.key)
  if (!copySnippet) return
  if (copySnippet.containsSecret && !!selectedKeyValue.value) {
    secretConfirmAction.value = null
    manualConfigConfirmKey.value = copySnippet.key
    return
  }
  await copyCommand(`manual-config-${copySnippet.key}`, copySnippet.body)
}

async function confirmManualConfigCopy() {
  const snippet = pendingManualConfigSnippet.value
  if (!snippet) return
  manualConfigConfirmKey.value = ''
  await copyCommand(`manual-config-${snippet.key}`, snippet.body)
}

async function handleTestProvider() {
  if (!selectedProvider.value || !selectedGroup.value) return
  if (!selectedKeyValue.value) {
    providerTestResult.value = { success: false, message: t('user.createKeyBeforeTesting') }
    return
  }
  const model = providerTestModel.value.trim()
  if (!model) {
    providerTestResult.value = { success: false, message: t('user.modelRequired') }
    return
  }
  providerTestLoading.value = true
  providerTestResult.value = null
  try {
    const res = await testUserProvider(selectedProvider.value.id, {
      platform: selectedGroup.value.platform,
      group_id: selectedGroup.value.group_id,
      model,
      prompt: providerTestPrompt.value.trim() || 'Hi',
    })
    providerTestResult.value = res.data.data ?? { success: false, message: t('user.requestFailed') }
  } catch (err: any) {
    providerTestResult.value = {
      success: false,
      message: err.response?.data?.message || err.message || t('user.requestFailed'),
    }
  } finally {
    providerTestLoading.value = false
  }
}

watch(
  () => [
    selectedProvider.value?.id,
    selectedGroup.value?.group_id,
    selectedGroup.value?.platform,
    selectedKeyValue.value,
  ],
  () => {
    void loadProviderModels()
  }
)

onMounted(loadProviders)
</script>

<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h1 class="text-2xl font-bold text-gray-900">{{ t('user.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500">{{ t('user.subtitle') }}</p>
        </div>
        <button
          class="rounded-md border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50"
          @click="loadProviders"
        >
          {{ t('user.refresh') }}
        </button>
      </div>

      <div class="grid min-w-0 gap-6 lg:grid-cols-[320px_minmax(0,1fr)]">
        <div class="min-w-0 space-y-6">
          <section class="rounded-lg bg-white p-5 shadow">
            <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">{{ t('user.accountTitle') }}</h2>
            <dl class="mt-4 space-y-3 text-sm">
              <div class="flex flex-col gap-1 sm:flex-row sm:justify-between sm:gap-4"><dt class="text-gray-500">{{ t('user.username') }}</dt><dd class="break-all font-medium text-gray-900">{{ auth.user?.username ?? '—' }}</dd></div>
              <div class="flex flex-col gap-1 sm:flex-row sm:justify-between sm:gap-4"><dt class="text-gray-500">{{ t('user.email') }}</dt><dd class="break-all font-medium text-gray-900">{{ auth.user?.email ?? '—' }}</dd></div>
              <div class="flex flex-col gap-1 sm:flex-row sm:justify-between sm:gap-4"><dt class="text-gray-500">{{ t('user.role') }}</dt><dd class="break-all font-medium text-gray-900">{{ auth.user?.role ?? '—' }}</dd></div>
              <div class="flex flex-col gap-1 sm:flex-row sm:justify-between sm:gap-4"><dt class="text-gray-500">{{ t('user.authSource') }}</dt><dd class="break-all font-medium text-gray-900">{{ auth.user?.auth_source ?? '—' }}</dd></div>
            </dl>
          </section>

          <section class="rounded-lg bg-white p-5 shadow">
            <div class="flex items-center justify-between">
              <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">{{ t('user.aiAccessTitle') }}</h2>
              <span v-if="loading" class="text-xs text-gray-400">{{ t('user.loading') }}</span>
            </div>
            <div class="mt-3 rounded-md bg-slate-50 px-3 py-2 text-xs text-slate-600">
              <span class="font-medium text-slate-900">{{ readyAccessGroupCount }}</span>
              /
              <span>{{ totalAccessGroupCount }}</span>
              {{ t('user.readyToUse') }}
            </div>
            <p v-if="selectedMessage" class="mt-3 rounded-md bg-gray-50 p-3 text-sm text-gray-600">{{ selectedMessage }}</p>
            <p v-if="error" class="mt-3 rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
            <div class="mt-4 space-y-3">
              <button
                v-for="provider in providers"
                :key="provider.id"
                :data-testid="`provider-${provider.id}`"
                class="w-full rounded-lg border px-4 py-3 text-left transition"
                :class="provider.id === selectedProviderId ? 'border-gray-900 bg-gray-900 text-white' : 'border-gray-200 bg-white text-gray-900 hover:border-gray-400'"
                @click="selectProvider(provider.id)"
              >
                <div class="flex items-center justify-between gap-3">
                  <div>
                    <div class="font-medium">{{ provider.display_name }}</div>
                    <div class="text-xs" :class="provider.id === selectedProviderId ? 'text-gray-300' : 'text-gray-500'">{{ provider.name }}</div>
                  </div>
                  <span
                    v-if="provider.is_primary"
                    class="rounded-full px-2 py-1 text-[11px] font-semibold"
                    :class="provider.id === selectedProviderId ? 'bg-white/15 text-white' : 'bg-gray-100 text-gray-700'"
                  >
                    {{ t('user.primary') }}
                  </span>
                </div>
              </button>
            </div>
          </section>
        </div>

        <div class="min-w-0 space-y-6">
          <section data-testid="primary-onboarding-flow" class="rounded-lg bg-white p-5 shadow">
            <div class="border-b border-gray-100 pb-4">
              <h2 class="text-2xl font-bold text-gray-900">{{ t('user.manageMyApiKey') }}</h2>
              <p class="mt-2 text-sm text-gray-600">{{ t('user.primaryFlowHelp') }}</p>
            </div>

            <div class="mt-5 space-y-4">
              <section class="rounded-lg border border-gray-200 p-4">
                <div class="flex items-start justify-between gap-4">
                  <div>
                    <h3 class="text-base font-semibold text-gray-900">{{ t('user.accessTitle') }}</h3>
                    <p class="mt-1 text-sm text-gray-600">{{ t('user.accessGroupHelp') }}</p>
                    <p v-if="selectedProvider" class="mt-2 text-sm text-gray-500">{{ selectedProvider.base_url }}</p>
                  </div>
                  <button
                    v-if="onboardingState === 'group_selected_without_key'"
                    data-testid="primary-onboarding-action"
                    class="rounded-md bg-gray-900 px-3 py-2 text-sm font-medium text-white hover:bg-black disabled:opacity-50"
                    :disabled="credentialMutationLoading"
                    @click="handleCreateKey"
                  >
                    {{ credentialMutationLoading ? t('user.creatingKey') : primaryOnboardingActionLabel }}
                  </button>
                </div>

                <div v-if="selectedProvider" class="mt-4 space-y-4">
                  <div class="flex flex-wrap gap-2">
                    <button
                      v-for="group in selectedProvider.groups"
                      :key="group.group_id"
                      :data-testid="`group-${group.group_id}`"
                      class="rounded-full border px-3 py-2 text-sm transition"
                      :class="group.group_id === selectedGroupId ? 'border-gray-900 bg-gray-900 text-white' : 'border-gray-300 bg-white text-gray-800 hover:border-gray-500'"
                      @click="selectGroup(group.group_id)"
                    >
                      {{ group.group_name }}
                    </button>
                  </div>

                  <div v-if="selectedGroup" class="rounded-md bg-gray-50 p-4 text-sm text-gray-700">
                    <div class="font-medium text-gray-900">{{ credentialStatusLabel(selectedGroup.credential.state) }}</div>
                    <div class="mt-2">{{ t('user.group') }}: {{ selectedGroup.group_name }}</div>
                    <div class="mt-1">{{ t('user.platform') }}: {{ selectedGroup.platform }}</div>
                    <div class="mt-2">{{ credentialStatusHelp(selectedGroup.credential.state, !!selectedGroup.credential.key) }}</div>
                  </div>
                </div>
              </section>

              <section class="rounded-lg border border-gray-200 p-4">
                <div class="flex items-start justify-between gap-4">
                  <div>
                    <h3 class="text-base font-semibold text-gray-900">{{ t('user.apiKeyTitle') }}</h3>
                    <p class="mt-1 text-sm text-gray-600">{{ t('user.apiKeyStageHelp') }}</p>
                  </div>
                </div>

                <div v-if="selectedGroup" class="mt-4 space-y-4">
                  <div class="rounded-md border border-dashed border-gray-300 p-4">
                    <div class="text-xs uppercase tracking-wide text-gray-400">{{ t('user.apiKeyTitle') }}</div>
                    <div class="mt-2 break-all rounded-md bg-gray-950 px-3 py-2 font-mono text-sm text-green-300">
                      {{ displayedSecret || '••••••••••••••••' }}
                    </div>
                    <div class="mt-3 flex flex-wrap gap-2">
                      <button
                        v-if="selectedGroup.credential.state === 'missing'"
                        data-testid="create-key"
                        class="rounded-md bg-gray-900 px-3 py-2 text-sm font-medium text-white hover:bg-black disabled:opacity-50"
                        :disabled="credentialMutationLoading"
                        @click="handleCreateKey"
                      >
                        {{ credentialMutationLoading ? t('user.creatingKey') : t('user.createKey') }}
                      </button>
                      <button
                        v-if="selectedGroup.credential.state === 'existing_hidden'"
                        data-testid="regenerate-key"
                        class="rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm font-medium text-amber-900 hover:bg-amber-100 disabled:opacity-50"
                        :disabled="credentialMutationLoading"
                        @click="requestSecretAction('regenerate')"
                      >
                        {{ credentialMutationLoading ? t('user.regenerating') : t('user.regenerate') }}
                      </button>
                      <button
                        v-if="canReveal"
                        data-testid="reveal-key"
                        class="rounded-md border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50"
                        @click="isSecretRevealed ? hideSelectedKey() : requestSecretAction('reveal')"
                      >
                        {{ isSecretRevealed ? t('user.hide') : t('user.reveal') }}
                      </button>
                      <button
                        v-if="canReveal"
                        data-testid="copy-key"
                        class="rounded-md border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50"
                        @click="requestSecretAction('copy')"
                      >
                        {{ t('user.copy') }}
                      </button>
                    </div>
                    <div v-if="secretConfirmAction" class="mt-3 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900">
                      <div class="font-medium">{{ secretConfirmTitle(secretConfirmAction) }}</div>
                      <p class="mt-1 text-xs">{{ t('user.secretRiskText') }}</p>
                      <div class="mt-3 flex flex-wrap gap-2">
                        <button
                          data-testid="confirm-secret-action"
                          class="rounded-md bg-amber-700 px-3 py-2 text-xs font-medium text-white hover:bg-amber-800 disabled:opacity-50"
                          :disabled="credentialMutationLoading"
                          @click="confirmSecretAction"
                        >
                          {{ t('user.confirmAction') }}
                        </button>
                        <button
                          class="rounded-md border border-amber-300 bg-white px-3 py-2 text-xs font-medium text-amber-900 hover:bg-amber-100"
                          @click="secretConfirmAction = null"
                        >
                          {{ t('user.cancel') }}
                        </button>
                      </div>
                    </div>
                  </div>

                  <div class="rounded-md border border-gray-200 p-4">
                    <div class="font-medium text-gray-900">{{ t('user.testTitle') }}</div>
                    <p class="mt-1 text-xs text-gray-500">
                      {{ t('user.testHelp') }}
                    </p>
                    <div class="mt-3 grid gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
                      <div>
                        <label class="block text-xs font-medium uppercase tracking-wide text-gray-500">{{ t('user.platform') }}</label>
                        <input
                          :value="selectedGroup.platform"
                          disabled
                          class="mt-1 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm text-gray-500"
                        />
                      </div>
                      <div>
                        <label class="block text-xs font-medium uppercase tracking-wide text-gray-500">{{ t('user.model') }}</label>
                        <select
                          v-if="providerModelOptions.length > 0"
                          v-model="providerTestModel"
                          data-testid="user-provider-test-model"
                          class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
                        >
                          <option
                            v-for="model in providerModelOptions"
                            :key="model.id"
                            :value="model.id"
                          >
                            {{ providerModelLabel(model) }}
                          </option>
                        </select>
                        <input
                          v-else
                          v-model="providerTestModel"
                          data-testid="user-provider-test-model"
                          type="text"
                          :placeholder="providerModelsLoading ? t('user.loadingModels') : 'gpt-5.4'"
                          class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
                        />
                        <p v-if="providerModelsLoading" class="mt-1 text-xs text-gray-500">{{ t('user.loadingModels') }}</p>
                        <p v-else-if="providerModelsMessage" class="mt-1 text-xs text-gray-500">{{ providerModelsMessage }}</p>
                      </div>
                    </div>
                    <div class="mt-3">
                      <label class="block text-xs font-medium uppercase tracking-wide text-gray-500">{{ t('user.prompt') }}</label>
                      <input
                        v-model="providerTestPrompt"
                        data-testid="user-provider-test-prompt"
                        type="text"
                        placeholder="Hi"
                        class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
                      />
                    </div>
                    <div class="mt-3 flex flex-wrap items-center gap-3">
                      <button
                        data-testid="user-provider-test-run"
                        :disabled="providerTestLoading || !canTestProvider"
                        class="rounded-md bg-gray-900 px-3 py-2 text-sm font-medium text-white hover:bg-black disabled:opacity-50"
                        @click="handleTestProvider"
                      >
                        {{ providerTestLoading ? t('user.testing') : t('user.runTest') }}
                      </button>
                      <span v-if="providerTestResult" class="text-sm" :class="providerTestResult.success ? 'text-green-700' : 'text-red-700'">
                        {{ providerTestResult.message }}
                      </span>
                    </div>
                    <div v-if="providerTestResult?.response" class="mt-3 rounded-md bg-gray-50 p-3 text-sm text-gray-700">
                      {{ providerTestResult.response }}
                    </div>
                  </div>
                </div>
              </section>

              <section
                v-if="showConfigurationMethods"
                data-testid="configuration-methods"
                class="rounded-lg border border-gray-200 p-4"
              >
                <h3 class="text-base font-semibold text-gray-900">{{ t('user.configurationMethodsTitle') }}</h3>
                <p class="mt-1 text-sm text-gray-600">{{ t('user.configurationMethodsHelp') }}</p>

                <div class="mt-4 grid gap-3 md:grid-cols-3">
                  <button
                    data-testid="config-method-manual"
                    class="rounded-lg border px-4 py-3 text-left transition"
                    :class="selectedConfigMethod === 'manual' ? 'border-gray-900 bg-gray-50' : 'border-gray-200 hover:border-gray-400'"
                    @click="selectedConfigMethod = 'manual'"
                  >
                    <div class="font-medium text-gray-900">{{ t('user.manualConfigMethodTitle') }}</div>
                    <p class="mt-1 text-sm text-gray-600">{{ t('user.manualConfigMethodHelp') }}</p>
                    <p class="mt-3 text-xs text-gray-500">{{ t('user.manualConfigMethodAudience') }}</p>
                  </button>
                  <button
                    data-testid="config-method-automatic"
                    class="rounded-lg border px-4 py-3 text-left transition"
                    :class="selectedConfigMethod === 'automatic' ? 'border-gray-900 bg-gray-50' : 'border-gray-200 hover:border-gray-400'"
                    @click="selectedConfigMethod = 'automatic'"
                  >
                    <div class="font-medium text-gray-900">{{ t('user.automaticConfigMethodTitle') }}</div>
                    <p class="mt-1 text-sm text-gray-600">{{ t('user.automaticConfigMethodHelp') }}</p>
                    <p class="mt-3 text-xs text-gray-500">{{ t('user.automaticConfigMethodAudience') }}</p>
                  </button>
                  <button
                    v-if="ccSwitchImports.length > 0"
                    data-testid="config-method-ccswitch"
                    class="rounded-lg border px-4 py-3 text-left transition"
                    :class="selectedConfigMethod === 'ccswitch' ? 'border-gray-900 bg-gray-50' : 'border-gray-200 hover:border-gray-400'"
                    @click="selectedConfigMethod = 'ccswitch'"
                  >
                    <div class="font-medium text-gray-900">{{ t('user.ccSwitchConfigMethodTitle') }}</div>
                    <p class="mt-1 text-sm text-gray-600">{{ t('user.ccSwitchConfigMethodHelp') }}</p>
                    <p class="mt-3 text-xs text-gray-500">{{ t('user.ccSwitchConfigMethodAudience') }}</p>
                  </button>
                </div>

                <div v-if="selectedConfigMethod === 'manual'" class="mt-4 rounded-lg border border-gray-200 p-4">
                  <div class="font-medium text-gray-900">{{ t('user.manualConfigDetailsTitle') }}</div>
                  <dl class="mt-3 grid gap-2 sm:grid-cols-[120px_minmax(0,1fr)]">
                    <dt class="text-xs font-medium uppercase tracking-wide text-slate-500">{{ t('user.manualConfigProviderUrl') }}</dt>
                    <dd class="break-all font-mono text-xs text-slate-900">{{ selectedProvider?.base_url || '—' }}</dd>
                    <dt class="text-xs font-medium uppercase tracking-wide text-slate-500">{{ t('user.manualConfigPlatform') }}</dt>
                    <dd class="break-all font-mono text-xs text-slate-900">{{ selectedGroup?.platform || '—' }}</dd>
                    <dt class="text-xs font-medium uppercase tracking-wide text-slate-500">{{ t('user.manualConfigGroup') }}</dt>
                    <dd class="break-all text-xs text-slate-900">{{ selectedGroup?.group_name || '—' }}</dd>
                    <dt class="text-xs font-medium uppercase tracking-wide text-slate-500">{{ t('user.manualConfigApiKey') }}</dt>
                    <dd class="text-xs text-slate-900">{{ t('user.manualConfigApiKeyHelp') }}</dd>
                  </dl>
                  <div v-if="manualConfigDisplaySnippets.length > 0" class="mt-4 space-y-3">
                    <div
                      v-for="snippet in manualConfigDisplaySnippets"
                      :key="snippet.key"
                      class="rounded-md border border-slate-200 bg-white p-3"
                    >
                      <div class="flex flex-wrap items-start justify-between gap-3">
                        <div class="min-w-0">
                          <div class="font-medium text-slate-900">{{ manualConfigSnippetTitle(snippet) }}</div>
                          <div class="mt-1 break-all font-mono text-xs text-slate-500">{{ snippet.path }}</div>
                        </div>
                        <button
                          class="shrink-0 text-xs font-medium text-indigo-700 hover:text-indigo-900"
                          type="button"
                          :data-testid="`manual-config-copy-${snippet.key}`"
                          @click="copyManualConfigSnippet(snippet)"
                        >
                          {{ manualConfigCopyLabel(snippet) }}
                        </button>
                      </div>
                      <pre class="mt-3 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 text-xs text-green-300">{{ snippet.body }}</pre>
                    </div>
                    <div v-if="pendingManualConfigSnippet" class="rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900">
                      <div class="font-medium">{{ t('user.confirmCopyConfigSnippet') }}</div>
                      <p class="mt-1 text-xs">{{ t('user.secretRiskText') }}</p>
                      <div class="mt-3 flex flex-wrap gap-2">
                        <button
                          data-testid="confirm-manual-config-copy"
                          class="rounded-md bg-amber-700 px-3 py-2 text-xs font-medium text-white hover:bg-amber-800"
                          @click="confirmManualConfigCopy"
                        >
                          {{ t('user.confirmAction') }}
                        </button>
                        <button
                          class="rounded-md border border-amber-300 bg-white px-3 py-2 text-xs font-medium text-amber-900 hover:bg-amber-100"
                          @click="manualConfigConfirmKey = ''"
                        >
                          {{ t('user.cancel') }}
                        </button>
                      </div>
                    </div>
                  </div>
                  <p v-else class="mt-4 rounded-md border border-amber-200 bg-amber-50 p-3 text-xs text-amber-900">
                    {{ t('user.manualConfigUnsupportedPlatform') }}
                  </p>
                </div>

                <div v-if="selectedConfigMethod === 'automatic'" class="mt-4 rounded-lg border border-gray-200 p-4">
                  <div class="font-medium text-gray-900">{{ t('user.automaticConfigMethodTitle') }}</div>
                  <p class="mt-1 text-sm text-gray-600">{{ t('user.automaticConfigProviderHelp') }}</p>
                  <p class="mt-3 text-sm text-gray-600">{{ t('user.automaticConfigOverview') }}</p>

                  <div class="mt-4 rounded-lg border border-gray-200 p-4">
                    <div class="font-medium text-gray-900">{{ t('user.automaticConfigMachineTitle') }}</div>
                    <p class="mt-1 text-sm text-gray-600">{{ t('user.automaticConfigMachineHelp') }}</p>
                    <div class="mt-4 space-y-4 text-sm">
                      <div v-for="command in automaticMachineCommands" :key="command.key">
                        <div class="flex items-center justify-between gap-3">
                          <span class="text-xs font-medium uppercase tracking-wide text-gray-500">{{ command.label }}</span>
                          <button class="shrink-0 text-xs font-medium text-indigo-700 hover:text-indigo-900" type="button" @click="copyCommand(command.key, command.value)">
                            {{ copyCommandLabel(command.key) }}
                          </button>
                        </div>
                        <pre class="mt-2 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 text-xs text-green-300">{{ command.value }}</pre>
                      </div>
                    </div>
                  </div>

                  <div class="mt-4 rounded-lg border border-gray-200 p-4">
                    <div class="font-medium text-gray-900">{{ t('user.automaticConfigRepoTitle') }}</div>
                    <p class="mt-1 text-sm text-gray-600">{{ t('user.automaticConfigRepoHelp') }}</p>
                    <div class="mt-4 space-y-4 text-sm">
                      <div v-for="command in automaticRepoCommands" :key="command.key">
                        <div class="flex items-center justify-between gap-3">
                          <span class="text-xs font-medium uppercase tracking-wide text-gray-500">{{ command.label }}</span>
                          <button class="shrink-0 text-xs font-medium text-indigo-700 hover:text-indigo-900" type="button" @click="copyCommand(command.key, command.value)">
                            {{ copyCommandLabel(command.key) }}
                          </button>
                        </div>
                        <pre class="mt-2 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 text-xs text-green-300">{{ command.value }}</pre>
                      </div>
                    </div>
                  </div>

                  <details class="mt-4 rounded-lg border border-gray-200 p-4">
                    <summary class="cursor-pointer text-sm font-semibold text-gray-900">
                      {{ t('user.commandReference') }}
                    </summary>
                    <p class="mt-2 text-sm text-gray-500">{{ t('user.commandReferenceHelp') }}</p>

                    <div class="mt-4 space-y-4 text-sm">
                      <div class="rounded-md border border-gray-200 p-4">
                        <div class="font-medium text-gray-900">{{ t('user.alternateInstall') }}</div>
                        <div class="mt-3 text-xs font-medium uppercase tracking-wide text-gray-500">{{ alternateInstallLabel }}</div>
                        <div class="mt-2 flex justify-end">
                          <button class="text-xs font-medium text-indigo-700 hover:text-indigo-900" type="button" @click="copyCommand(alternateInstallCopyKey, alternateInstallCommand)">
                            {{ copyCommandLabel(alternateInstallCopyKey) }}
                          </button>
                        </div>
                        <pre class="mt-2 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 text-xs text-green-300">{{ alternateInstallCommand }}</pre>
                      </div>

                      <div class="rounded-md border border-gray-200 p-4">
                        <div class="font-medium text-gray-900">{{ t('user.deviceLoginFallback') }}</div>
                        <div class="mt-2 flex justify-end">
                          <button class="text-xs font-medium text-indigo-700 hover:text-indigo-900" type="button" @click="copyCommand('device-login', deviceLoginCommand)">
                            {{ copyCommandLabel('device-login') }}
                          </button>
                        </div>
                        <pre class="mt-2 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 text-xs text-green-300">{{ deviceLoginCommand }}</pre>
                      </div>

                      <div class="rounded-md border border-gray-200 p-4">
                        <div class="font-medium text-gray-900">{{ t('user.manualRecovery') }}</div>
                        <div class="mt-4 space-y-4">
                          <div>
                            <div class="text-sm font-medium text-gray-900">{{ t('user.manualSync') }}</div>
                            <div class="mt-2 flex justify-end">
                              <button class="text-xs font-medium text-indigo-700 hover:text-indigo-900" type="button" @click="copyCommand('manual-sync', syncCommand)">
                                {{ copyCommandLabel('manual-sync') }}
                              </button>
                            </div>
                            <pre class="mt-2 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 text-xs text-green-300">{{ syncCommand }}</pre>
                          </div>
                          <div>
                            <div class="text-sm font-medium text-gray-900">{{ t('user.hookStatus') }}</div>
                            <div class="mt-2 flex justify-end">
                              <button class="text-xs font-medium text-indigo-700 hover:text-indigo-900" type="button" @click="copyCommand('hook-status', hooksStatusUploadsCommand)">
                                {{ copyCommandLabel('hook-status') }}
                              </button>
                            </div>
                            <pre class="mt-2 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 text-xs text-green-300">{{ hooksStatusUploadsCommand }}</pre>
                          </div>
                        </div>
                      </div>
                    </div>
                  </details>
                </div>

                <div v-if="selectedConfigMethod === 'ccswitch'" class="mt-4 rounded-lg border border-gray-200 p-4">
                  <div class="font-medium text-gray-900">{{ t('user.ccSwitchConfigMethodTitle') }}</div>
                  <p class="mt-1 text-sm text-gray-600">{{ t('user.ccSwitchConfigMethodHelp') }}</p>
                  <div class="mt-3">
                    <a
                      :href="t('user.ccSwitchDownloadUrl')"
                      target="_blank"
                      rel="noreferrer"
                      class="text-sm font-medium text-indigo-700 hover:text-indigo-900"
                    >
                      {{ t('user.ccSwitchDownload') }}
                    </a>
                  </div>
                  <div class="mt-4 flex flex-wrap gap-2">
                    <a
                      v-for="item in ccSwitchImports"
                      :key="item.key"
                      :data-testid="`ccswitch-import-${item.key}`"
                      :href="item.href"
                      class="inline-flex rounded-md bg-blue-700 px-3 py-2 text-sm font-medium text-white hover:bg-blue-800"
                    >
                      {{ item.label }}
                    </a>
                  </div>
                  <p class="mt-3 text-xs text-gray-500">{{ t('user.ccSwitchFallback') }}</p>
                </div>
              </section>
            </div>
          </section>

        </div>
      </div>
    </div>
  </AppLayout>
</template>
