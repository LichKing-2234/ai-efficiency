<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import AppLayout from '@/components/AppLayout.vue'
import ReportingReadinessGuide from '@/components/activity/ReportingReadinessGuide.vue'
import { createGroupCredential, getUserProviderModels, getUserProviders, regenerateGroupCredential, testUserProvider } from '@/api/user'
import {
  useUserOnboardingWorkflow,
  type UserOnboardingMessageKey,
} from '@/composables/useUserOnboardingWorkflow'
import { useAuthStore } from '@/stores/auth'
import { useI18n, type MessageKey } from '@/i18n'
import { authSourceLabel, userRoleLabel } from '@/utils/displayLabels'
import type {
  UserProviderModel,
  UserProviderProtocol,
} from '@/types'
import {
  buildCCSwitchProviderImportLink,
  buildDiscoverCommand,
  buildManualConfigSnippets,
  resolveCCSwitchAppsForGroup,
  isAgentAccessGroup,
  resolveDiscoverToolForPlatform,
} from '@/utils/userSetupReview'
import type { ManualConfigSnippet } from '@/utils/userSetupReview'

const auth = useAuthStore()
const { t } = useI18n()
const onboardingFlowElement = ref<HTMLElement | null>(null)
const horizontalSteps = ref(false)
const HORIZONTAL_STEPS_MIN_WIDTH = 700
let onboardingFlowObserver: ResizeObserver | null = null

const copiedCommandKey = ref('')
const manualConfigConfirmKey = ref('')
type SecretAction = 'reveal' | 'copy' | 'regenerate'
const secretConfirmAction = ref<SecretAction | null>(null)

const workflowMessageKeys: Record<UserOnboardingMessageKey, MessageKey> = {
  loadFailed: 'user.loadFailed',
  createKeyFailed: 'user.createKeyFailed',
  regenerateKeyFailed: 'user.regenerateKeyFailed',
  createKeyBeforeModelList: 'user.createKeyBeforeModelList',
  noModelsAvailable: 'user.noModelsAvailable',
  modelLoadFailed: 'user.modelLoadFailed',
  createKeyBeforeTesting: 'user.createKeyBeforeTesting',
  modelRequired: 'user.modelRequired',
  requestFailed: 'user.requestFailed',
}

function workflowMessage(key: UserOnboardingMessageKey) {
  return t(workflowMessageKeys[key])
}

function workflowErrorMessage(requestError: unknown, fallbackKey: UserOnboardingMessageKey) {
  const request = requestError as { response?: { data?: { message?: string } }; message?: string }
  return request.response?.data?.message || request.message || workflowMessage(fallbackKey)
}

const onboardingWorkflow = useUserOnboardingWorkflow({
  loadProviders: async () => (await getUserProviders()).data.data ?? null,
  createCredential: async (providerID, groupID) => (await createGroupCredential(providerID, groupID)).data.data ?? null,
  regenerateCredential: async (providerID, groupID) => (await regenerateGroupCredential(providerID, groupID)).data.data ?? null,
  loadModels: async (providerID, groupID, platform) => (
    await getUserProviderModels(providerID, groupID, platform)
  ).data.data ?? null,
  testConnection: async (providerID, request) => (await testUserProvider(providerID, request)).data.data ?? null,
  message: workflowMessage,
  errorMessage: workflowErrorMessage,
})

const {
  loading,
  error,
  providers,
  selectedMessage,
  selectedProviderID: selectedProviderId,
  selectedGroupID: selectedGroupId,
  selectedProvider,
  selectedGroup,
  selectedKeyValue,
  keyRevealed: isSecretRevealed,
  visibleStep: onboardingVisibleStep,
  reachableStep: onboardingReachableStep,
  onboardingState,
  protocol: providerTestProtocol,
  credentialLoading: credentialMutationLoading,
  models: providerModelOptions,
  model: providerTestModel,
  modelsLoading: providerModelsLoading,
  modelsMessage: providerModelsMessage,
  connectionLoading: providerTestLoading,
  connectionResult: providerTestResult,
  canTestConnection: canTestProvider,
  selectedConfigMethod,
  load: loadOnboardingWorkflow,
  selectProvider: selectWorkflowProvider,
  selectGroup: selectWorkflowGroup,
  goToStep: selectOnboardingStep,
  continueFromAccess: continueOnboardingFromAccess,
  setModel: setProviderTestModel,
  setProtocol: setProviderTestProtocol,
  setConfigMethod,
  setKeyRevealed,
  createCredential: createWorkflowCredential,
  regenerateCredential: regenerateWorkflowCredential,
  testConnection: testWorkflowConnection,
  dispose: disposeOnboardingWorkflow,
} = onboardingWorkflow

const currentOrigin = computed(() => window.location.origin)
const reportingCapabilities = computed(() => auth.user?.reporting_capabilities)
const selectedIsAgentGroup = computed(() => isAgentAccessGroup(selectedGroup.value?.group_name))
const showAutomaticConfigMethod = computed(() => !selectedIsAgentGroup.value)
const ccSwitchMethodTitle = computed(() => selectedIsAgentGroup.value ? t('user.appImportMethodTitle') : t('user.ccSwitchConfigMethodTitle'))
const ccSwitchMethodHelp = computed(() => selectedIsAgentGroup.value ? t('user.appImportMethodHelp') : t('user.ccSwitchConfigMethodHelp'))
const ccSwitchMethodAudience = computed(() => selectedIsAgentGroup.value ? t('user.appImportMethodAudience') : t('user.ccSwitchConfigMethodAudience'))
const discoverCommand = computed(() => selectedProvider.value ? buildDiscoverCommand(currentOrigin.value, selectedProvider.value.name) : '')
const discoverTool = computed(() => resolveDiscoverToolForPlatform(selectedGroup.value?.platform ?? ''))
const discoverFallbackCommand = computed(() =>
  selectedProvider.value && discoverTool.value
    ? buildDiscoverCommand(currentOrigin.value, selectedProvider.value.name, discoverTool.value)
    : ''
)
const readyAccessGroupCount = computed(() =>
  providers.value.reduce(
    (count, provider) => count + provider.groups.filter((group) => group.credential.state === 'existing_hidden').length,
    0
  )
)
const totalAccessGroupCount = computed(() =>
  providers.value.reduce((count, provider) => count + provider.groups.length, 0)
)
const hasAnyAccessGroups = computed(() => providers.value.some((provider) => provider.groups.length > 0))
const selectedProviderHasGroups = computed(() => !!selectedProvider.value?.groups.length)
const primaryOnboardingActionLabel = computed(() => {
  if (onboardingState.value === 'group_selected_without_key') return t('user.createKeyAndContinue')
  if (selectedKeyValue.value) {
    return t('user.nextConnectionTest')
  }
  return ''
})
const connectionTestActionLabel = computed(() =>
  providerTestResult.value && !providerTestResult.value.success
    ? t('user.retryConnectionTest')
    : t('user.runConnectionTest')
)
const showConfigurationMethods = computed(() => !!selectedKeyValue.value)

function handlePrimaryOnboardingAction() {
  void continueOnboardingFromAccess()
}

const ccSwitchImports = computed(() => {
  const provider = selectedProvider.value
  const group = selectedGroup.value
  const apiKey = selectedKeyValue.value
  if (!provider || !group || !apiKey) return []
  const apps = resolveCCSwitchAppsForGroup(group.platform, group.group_name)
  const selectedModel = providerTestModel.value.trim()
  return apps.map((app) => ({
    key: app,
    label:
      app === 'codex' ? t('user.importToCodex')
        : app === 'claude' ? t('user.importToClaude')
          : app === 'gemini' ? t('user.importToGemini')
            : app === 'hermes' ? t('user.importToHermes')
              : t('user.importToOpenClaw'),
    href: buildCCSwitchProviderImportLink({
      app,
      name: `${provider.display_name} / ${group.group_name}`,
      endpoint: provider.base_url,
      apiKey,
      model: selectedModel || (app === 'codex' ? 'gpt-5.4' : undefined),
    }),
  }))
})
const automaticAccessCommands = computed(() => [
  {
    key: 'auto-discover',
    label: t('user.setupStepConfigureTitle'),
    value: discoverCommand.value || t('user.selectProviderCommand'),
    fallback: discoverFallbackCommand.value
      ? {
          detailsTestId: 'auto-discover-fallback',
          title: t('user.discoverToolFallback'),
          help: t('user.automaticConfigDiscoverToolHelp'),
          label: t('user.fallbackCommand'),
          value: discoverFallbackCommand.value,
          copyKey: 'discover-tool',
        }
      : undefined,
  },
])

function credentialStatusLabel(state: string) {
  return state === 'existing_hidden' ? t('user.readyToUse') : t('user.needsSetup')
}

function credentialStatusHelp(state: string, hasKey?: boolean) {
  if (state !== 'existing_hidden') return t('user.missingKey')
  return hasKey ? t('user.readyWithKey') : t('user.readyNoKey')
}

const canReveal = computed(() => !!selectedKeyValue.value)
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

const protocolNames: Record<UserProviderProtocol, string> = {
  responses: 'Responses',
  chat_completions: 'Chat Completions',
  messages: 'Messages',
  generate_content: 'GenerateContent',
  antigravity_generate_content: 'Antigravity GenerateContent',
}

function providerProtocolLabel(protocol: UserProviderProtocol) {
  const kind = protocol === selectedGroup.value?.recommended_protocol
    ? t('user.protocolRecommended')
    : t('user.protocolCompatibility')
  return `${protocolNames[protocol]} - ${kind}`
}

function clearPendingViewActions() {
  secretConfirmAction.value = null
  manualConfigConfirmKey.value = ''
}

function selectProvider(providerID: number) {
  clearPendingViewActions()
  selectWorkflowProvider(providerID)
}

function selectGroup(groupID: string) {
  clearPendingViewActions()
  selectWorkflowGroup(groupID)
}

async function loadProviders() {
  clearPendingViewActions()
  await loadOnboardingWorkflow()
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
    groupName: selectedGroup.value.group_name,
    model: providerTestModel.value.trim(),
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
    case 'hermes-agent':
      return t('user.manualConfigHermesAgent')
    case 'openclaw-agent':
      return t('user.manualConfigOpenClaw')
    case 'custom-agent-env':
      return t('user.manualConfigCustomAgentEnv')
    case 'custom-agent-json':
      return t('user.manualConfigCustomAgentJson')
    default:
      return snippet.path
  }
}

async function handleCreateKey() {
  await createWorkflowCredential()
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
  await regenerateWorkflowCredential()
}

function revealSelectedKey() {
  setKeyRevealed(true)
}

function hideSelectedKey() {
  setKeyRevealed(false)
}

async function copySelectedKey() {
  if (!selectedKeyValue.value) return
  try {
    await navigator.clipboard.writeText(selectedKeyValue.value)
    ElMessage.success(t('user.copied'))
  } catch {
    ElMessage.error(t('user.copyFailed'))
  }
}

async function copyCommand(key: string, command: string) {
  if (!command || command === t('user.selectProviderCommand')) return
  try {
    await navigator.clipboard.writeText(command)
    ElMessage.success(t('user.copied'))
    copiedCommandKey.value = key
    window.setTimeout(() => {
      if (copiedCommandKey.value === key) copiedCommandKey.value = ''
    }, 1800)
  } catch {
    ElMessage.error(t('user.copyFailed'))
  }
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
  await testWorkflowConnection()
}

function syncOnboardingStepDirection(width?: number) {
  if (width === undefined) {
    const element = onboardingFlowElement.value
    if (!element) return
    const style = getComputedStyle(element)
    width = element.getBoundingClientRect().width
      - parseFloat(style.paddingLeft)
      - parseFloat(style.paddingRight)
      - parseFloat(style.borderLeftWidth)
      - parseFloat(style.borderRightWidth)
  }
  horizontalSteps.value = width >= HORIZONTAL_STEPS_MIN_WIDTH
}

onMounted(() => {
  void loadProviders()
  syncOnboardingStepDirection()

  if (typeof ResizeObserver === 'undefined' || !onboardingFlowElement.value) return
  onboardingFlowObserver = new ResizeObserver(([entry]) => {
    if (entry) syncOnboardingStepDirection(entry.contentRect.width)
  })
  onboardingFlowObserver.observe(onboardingFlowElement.value)
})

onBeforeUnmount(() => {
  onboardingFlowObserver?.disconnect()
  disposeOnboardingWorkflow()
})
</script>

<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h1 class="text-3xl font-semibold tracking-tight text-gray-900">{{ t('user.title') }}</h1>
          <p class="mt-2 max-w-3xl text-sm text-gray-500">{{ t('user.subtitle') }}</p>
        </div>
        <ElButton
          :loading="loading"
          @click="loadProviders"
        >
          {{ t('user.refresh') }}
        </ElButton>
      </div>

      <div data-testid="user-page-grid" class="grid min-w-0 gap-6 xl:grid-cols-[320px_minmax(0,1fr)]">
        <div data-testid="user-summary-column" class="order-2 min-w-0 space-y-6 xl:order-1">
          <section class="rounded-lg border border-gray-200 bg-white p-5">
            <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">{{ t('user.accountTitle') }}</h2>
            <dl class="mt-4 space-y-3 text-sm">
              <div class="flex flex-col gap-1 sm:flex-row sm:justify-between sm:gap-4"><dt class="text-gray-500">{{ t('user.username') }}</dt><dd class="break-all font-medium text-gray-900">{{ auth.user?.username ?? '—' }}</dd></div>
              <div class="flex flex-col gap-1 sm:flex-row sm:justify-between sm:gap-4"><dt class="text-gray-500">{{ t('user.email') }}</dt><dd class="break-all font-medium text-gray-900">{{ auth.user?.email ?? '—' }}</dd></div>
              <div class="flex flex-col gap-1 sm:flex-row sm:justify-between sm:gap-4"><dt class="text-gray-500">{{ t('user.role') }}</dt><dd class="break-all font-medium text-gray-900">{{ userRoleLabel(auth.user?.role, t) }}</dd></div>
              <div class="flex flex-col gap-1 sm:flex-row sm:justify-between sm:gap-4"><dt class="text-gray-500">{{ t('user.authSource') }}</dt><dd class="break-all font-medium text-gray-900">{{ authSourceLabel(auth.user?.auth_source, t) }}</dd></div>
            </dl>
          </section>

          <section class="rounded-lg border border-gray-200 bg-white p-5">
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
            <ElAlert v-if="selectedMessage" class="mt-3" type="info" :closable="false" :title="selectedMessage" />
            <ElAlert v-if="error" class="mt-3" type="error" :closable="false" :title="error" />
            <ElRadioGroup
              :model-value="selectedProviderId ?? undefined"
              class="mt-4 flex w-full flex-col gap-3"
            >
              <ElRadio
                v-for="provider in providers"
                :key="provider.id"
                :data-testid="`provider-${provider.id}`"
                border
                class="!mx-0 !h-auto w-full !items-start !p-4"
                :value="provider.id"
                @click="selectProvider(provider.id)"
              >
                <div class="flex min-w-0 items-start gap-3 whitespace-normal">
                  <div class="min-w-0 flex-1">
                    <div class="break-words font-medium">{{ provider.display_name }}</div>
                    <div class="break-words text-xs text-gray-500">{{ provider.name }}</div>
                  </div>
                  <ElTag
                    v-if="provider.is_primary"
                    :data-testid="`provider-primary-tag-${provider.id}`"
                    class="shrink-0"
                    type="info"
                    size="small"
                  >
                    {{ t('user.primary') }}
                  </ElTag>
                </div>
              </ElRadio>
            </ElRadioGroup>
          </section>
        </div>

        <div data-testid="user-onboarding-column" class="order-1 min-w-0 space-y-6 xl:order-2">
          <section ref="onboardingFlowElement" data-testid="primary-onboarding-flow" class="rounded-lg border border-gray-200 bg-white p-5 shadow-sm">
            <div class="border-b border-gray-100 pb-4">
              <h2 class="text-lg font-semibold text-gray-900">{{ t('user.setupFlowTitle') }}</h2>
              <p class="mt-2 text-sm text-gray-600">{{ t('user.primaryFlowHelp') }}</p>
            </div>

            <div class="mt-4">
              <ElSteps
                data-testid="onboarding-steps"
                :data-direction="horizontalSteps ? 'horizontal' : 'vertical'"
                :active="onboardingVisibleStep"
                :simple="horizontalSteps"
                :direction="horizontalSteps ? 'horizontal' : 'vertical'"
                :class="horizontalSteps ? 'w-full' : 'h-44'"
              >
                <ElStep data-testid="onboarding-step-trigger-0" class="cursor-pointer" @click="selectOnboardingStep(0)">
                  <template #title>
                    <ElButton
                      data-testid="onboarding-step-button-0"
                      class="!h-auto min-h-11 !whitespace-normal !p-0 text-left"
                      link
                    >
                      {{ t('user.accessTitle') }}
                    </ElButton>
                  </template>
                </ElStep>
                <ElStep
                  data-testid="onboarding-step-trigger-1"
                  :class="onboardingReachableStep >= 1 ? 'cursor-pointer' : 'cursor-not-allowed'"
                  @click="selectOnboardingStep(1)"
                >
                  <template #title>
                    <ElButton
                      data-testid="onboarding-step-button-1"
                      class="!h-auto min-h-11 !whitespace-normal !p-0 text-left"
                      link
                      :disabled="onboardingReachableStep < 1"
                    >
                      {{ t('user.apiKeyStepTitle') }}
                    </ElButton>
                  </template>
                </ElStep>
                <ElStep
                  data-testid="onboarding-step-trigger-2"
                  :class="onboardingReachableStep >= 2 ? 'cursor-pointer' : 'cursor-not-allowed'"
                  @click="selectOnboardingStep(2)"
                >
                  <template #title>
                    <ElButton
                      data-testid="onboarding-step-button-2"
                      class="!h-auto min-h-11 !whitespace-normal !p-0 text-left"
                      link
                      :disabled="onboardingReachableStep < 2"
                    >
                      {{ t('user.configurationMethodsTitle') }}
                    </ElButton>
                  </template>
                </ElStep>
              </ElSteps>
            </div>

            <div class="mt-5">
              <section v-if="onboardingVisibleStep === 0" data-testid="onboarding-step-0" class="py-1">
                <div data-testid="onboarding-step-header" class="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
                  <div data-testid="onboarding-step-copy" class="min-w-0">
                    <h3 class="text-base font-semibold text-gray-900">{{ t('user.accessTitle') }}</h3>
                    <p class="mt-1 text-sm text-gray-600">{{ t('user.accessGroupHelp') }}</p>
                    <p v-if="selectedProvider" class="mt-2 text-sm text-gray-500">{{ selectedProvider.base_url }}</p>
                  </div>
                  <ElButton
                    v-if="selectedGroup"
                    data-testid="primary-onboarding-action"
                    class="w-full xl:w-auto xl:shrink-0"
                    type="primary"
                    :loading="credentialMutationLoading"
                    :disabled="credentialMutationLoading"
                    @click="handlePrimaryOnboardingAction"
                  >
                    {{ credentialMutationLoading ? t('user.creatingKey') : primaryOnboardingActionLabel }}
                  </ElButton>
                </div>

                <div v-if="selectedProvider" class="mt-4 space-y-4">
                  <ElRadioGroup
                    :model-value="selectedGroupId ?? undefined"
                    class="flex max-w-full flex-wrap gap-2"
                    fill="var(--el-color-primary-light-9)"
                    text-color="var(--el-color-primary)"
                  >
                    <ElRadioButton
                      v-for="group in selectedProvider.groups"
                      :key="group.group_id"
                      :data-testid="`group-${group.group_id}`"
                      :data-selected="selectedGroupId === group.group_id"
                      :value="group.group_id"
                      @click="selectGroup(group.group_id)"
                    >
                      <span class="inline-flex items-center gap-2">
                        <span
                          :data-testid="`group-indicator-${group.group_id}`"
                          :data-selected="selectedGroupId === group.group_id"
                          class="flex h-5 w-5 shrink-0 items-center justify-center rounded-full border"
                          aria-hidden="true"
                        >
                          <span v-if="selectedGroupId === group.group_id" class="h-2.5 w-2.5 rounded-full bg-gray-900" />
                        </span>
                        {{ group.group_name }}
                      </span>
                    </ElRadioButton>
                  </ElRadioGroup>

                  <div v-if="selectedGroup" class="rounded-md bg-gray-50 p-4 text-sm text-gray-700">
                    <div class="font-medium text-gray-900">{{ credentialStatusLabel(selectedGroup.credential.state) }}</div>
                    <div class="mt-2">{{ t('user.group') }}: {{ selectedGroup.group_name }}</div>
                    <div class="mt-1">{{ t('user.platform') }}: {{ selectedGroup.platform }}</div>
                    <div class="mt-2">{{ credentialStatusHelp(selectedGroup.credential.state, !!selectedGroup.credential.key) }}</div>
                  </div>
                  <div v-else class="rounded-md border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900">
                    <div class="font-medium">{{ t('user.noAccessGroupTitle') }}</div>
                    <p class="mt-2">{{ hasAnyAccessGroups ? t('user.noAccessGroupOnProviderHelp') : t('user.noAccessGroupHelp') }}</p>
                  </div>
                </div>
              </section>

              <section v-else-if="onboardingVisibleStep === 1" data-testid="onboarding-step-1" class="py-1">
                <div class="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
                  <div class="min-w-0">
                    <h3 class="text-base font-semibold text-gray-900">{{ t('user.apiKeyStepTitle') }}</h3>
                    <p class="mt-1 text-sm text-gray-600">{{ t('user.apiKeyStageHelp') }}</p>
                  </div>
                  <div v-if="selectedKeyValue" class="flex w-full flex-col gap-2 xl:w-auto xl:shrink-0 xl:flex-row">
                    <ElButton
                      data-testid="user-provider-test-run"
                      class="!ml-0 w-full xl:w-auto"
                      :type="providerTestResult?.success ? undefined : 'primary'"
                      :loading="providerTestLoading"
                      :disabled="providerTestLoading || !canTestProvider"
                      @click="handleTestProvider"
                    >
                      {{ providerTestLoading ? t('user.testing') : connectionTestActionLabel }}
                    </ElButton>
                    <ElButton
                      v-if="providerTestResult?.success"
                      data-testid="onboarding-next-configuration"
                      class="!ml-0 w-full xl:w-auto"
                      type="primary"
                      @click="selectOnboardingStep(2)"
                    >
                      {{ t('user.nextConfigureTools') }}
                    </ElButton>
                  </div>
                </div>

                <div v-if="selectedGroup" class="mt-4 space-y-4">
                  <div class="rounded-md border border-dashed border-gray-300 p-4">
                    <div class="text-xs uppercase tracking-wide text-gray-400">{{ t('user.apiKeyTitle') }}</div>
                    <div class="mt-2 break-all rounded-md bg-gray-950 px-3 py-2 font-mono text-sm text-green-300">
                      {{ displayedSecret || '••••••••••••••••' }}
                    </div>
                    <div class="mt-3 flex flex-wrap gap-2">
                      <ElButton
                        v-if="selectedGroup.credential.state === 'missing'"
                        data-testid="create-key"
                        type="primary"
                        :loading="credentialMutationLoading"
                        :disabled="credentialMutationLoading"
                        @click="handleCreateKey"
                      >
                        {{ credentialMutationLoading ? t('user.creatingKey') : t('user.createKey') }}
                      </ElButton>
                      <ElButton
                        v-if="selectedGroup.credential.state === 'existing_hidden'"
                        data-testid="regenerate-key"
                        type="warning"
                        plain
                        :loading="credentialMutationLoading"
                        :disabled="credentialMutationLoading"
                        @click="requestSecretAction('regenerate')"
                      >
                        {{ credentialMutationLoading ? t('user.regenerating') : t('user.regenerate') }}
                      </ElButton>
                      <ElButton
                        v-if="canReveal"
                        data-testid="reveal-key"
                        @click="isSecretRevealed ? hideSelectedKey() : requestSecretAction('reveal')"
                      >
                        {{ isSecretRevealed ? t('user.hide') : t('user.reveal') }}
                      </ElButton>
                      <ElButton
                        v-if="canReveal"
                        data-testid="copy-key"
                        @click="requestSecretAction('copy')"
                      >
                        {{ t('user.copy') }}
                      </ElButton>
                    </div>
                    <div v-if="secretConfirmAction" class="mt-3">
                      <ElAlert
                        type="warning"
                        :closable="false"
                        show-icon
                        :title="secretConfirmTitle(secretConfirmAction)"
                        :description="t('user.secretRiskText')"
                      />
                      <div class="mt-3 flex flex-wrap gap-2">
                        <ElButton
                          data-testid="confirm-secret-action"
                          type="warning"
                          :disabled="credentialMutationLoading"
                          @click="confirmSecretAction"
                        >
                          {{ t('user.confirmAction') }}
                        </ElButton>
                        <ElButton
                          @click="secretConfirmAction = null"
                        >
                          {{ t('user.cancel') }}
                        </ElButton>
                      </div>
                    </div>
                  </div>

                  <div class="rounded-md border border-gray-200 p-4">
                    <div class="font-medium text-gray-900">{{ t('user.testTitle') }}</div>
                    <p class="mt-1 text-xs text-gray-500">
                      {{ t('user.testHelp') }}
                    </p>
                    <div class="mt-3 grid gap-3 sm:grid-cols-3">
                      <div>
                        <label class="block text-xs font-medium uppercase tracking-wide text-gray-500">{{ t('user.platform') }}</label>
                        <ElInput
                          :value="selectedGroup.platform"
                          disabled
                          class="mt-1 w-full"
                        />
                      </div>
                      <div>
                        <label class="block text-xs font-medium uppercase tracking-wide text-gray-500">{{ t('user.protocol') }}</label>
                        <ElSelect
                          :model-value="providerTestProtocol"
                          data-testid="user-provider-test-protocol"
                          :teleported="false"
                          :aria-label="t('user.protocol')"
                          class="mt-1 w-full"
                          @update:model-value="(value) => setProviderTestProtocol(value as UserProviderProtocol)"
                        >
                          <ElOption
                            v-for="protocol in selectedGroup.supported_protocols || []"
                            :key="protocol"
                            :label="providerProtocolLabel(protocol)"
                            :value="protocol"
                          />
                        </ElSelect>
                      </div>
                      <div>
                        <label class="block text-xs font-medium uppercase tracking-wide text-gray-500">{{ t('user.model') }}</label>
                        <ElSelect
                          v-if="providerModelOptions.length > 0"
                          :model-value="providerTestModel"
                          data-testid="user-provider-test-model"
                          :teleported="false"
                          :aria-label="t('user.model')"
                          class="mt-1 w-full"
                          @update:model-value="(value) => setProviderTestModel(String(value || ''))"
                        >
                          <ElOption
                            v-for="model in providerModelOptions"
                            :key="model.id"
                            :label="providerModelLabel(model)"
                            :value="model.id"
                          />
                        </ElSelect>
                        <ElInput
                          v-else
                          :model-value="providerTestModel"
                          data-testid="user-provider-test-model"
                          type="text"
                          :placeholder="providerModelsLoading ? t('user.loadingModels') : 'gpt-5.4'"
                          class="mt-1 w-full"
                          @update:model-value="(value) => setProviderTestModel(String(value || ''))"
                        />
                        <p v-if="providerModelsLoading" class="mt-1 text-xs text-gray-500">{{ t('user.loadingModels') }}</p>
                        <p v-else-if="providerModelsMessage" class="mt-1 text-xs text-gray-500">{{ providerModelsMessage }}</p>
                      </div>
                    </div>
                    <div class="mt-3 flex flex-wrap items-center gap-3">
                      <ElAlert
                        v-if="providerTestResult"
                        class="min-w-0 max-w-full"
                        :type="providerTestResult.success ? 'success' : 'error'"
                        :closable="false"
                      >
                        <template #title>
                          <div
                            data-testid="user-provider-test-message"
                            :class="providerTestResult.success ? '' : 'max-h-64 overflow-y-auto break-all whitespace-pre-wrap'"
                          >
                            {{ providerTestResult.message }}
                          </div>
                        </template>
                      </ElAlert>
                    </div>
                    <div v-if="providerTestResult?.response" class="mt-3 rounded-md bg-gray-50 p-3 text-sm text-gray-700">
                      {{ providerTestResult.response }}
                    </div>
                  </div>
                </div>
                <div v-else class="mt-4 rounded-md border border-gray-200 bg-gray-50 p-4 text-sm text-gray-600">
                  {{ hasAnyAccessGroups && !selectedProviderHasGroups ? t('user.noAccessGroupOnProviderApiKeyHelp') : t('user.noAccessGroupApiKeyHelp') }}
                </div>
              </section>

              <section
                v-else-if="onboardingVisibleStep === 2 && showConfigurationMethods"
                data-testid="configuration-methods"
                class="py-1"
              >
                <h3 class="text-base font-semibold text-gray-900">{{ t('user.configurationMethodsTitle') }}</h3>
                <p class="mt-1 text-sm text-gray-600">{{ t('user.configurationMethodsHelp') }}</p>

                <ElRadioGroup
                  :model-value="selectedConfigMethod ?? undefined"
                  class="mt-4 !grid w-full !items-stretch gap-3 lg:grid-cols-3"
                >
                  <ElRadio
                    data-testid="config-method-manual"
                    border
                    class="!mx-0 !h-full w-full !items-start !p-4"
                    value="manual"
                    @click="setConfigMethod('manual')"
                  >
                    <div class="whitespace-normal">
                      <div class="font-medium text-gray-900">{{ t('user.manualConfigMethodTitle') }}</div>
                      <p class="mt-1 text-sm text-gray-600">{{ t('user.manualConfigMethodHelp') }}</p>
                      <p class="mt-3 text-xs text-gray-500">{{ t('user.manualConfigMethodAudience') }}</p>
                    </div>
                  </ElRadio>
                  <ElRadio
                    v-if="showAutomaticConfigMethod"
                    data-testid="config-method-automatic"
                    border
                    class="!mx-0 !h-full w-full !items-start !p-4"
                    value="automatic"
                    @click="setConfigMethod('automatic')"
                  >
                    <div class="whitespace-normal">
                      <div class="font-medium text-gray-900">{{ t('user.automaticConfigMethodTitle') }}</div>
                      <p class="mt-1 text-sm text-gray-600">{{ t('user.automaticConfigMethodHelp') }}</p>
                      <p class="mt-3 text-xs text-gray-500">{{ t('user.automaticConfigMethodAudience') }}</p>
                    </div>
                  </ElRadio>
                  <ElRadio
                    v-if="ccSwitchImports.length > 0"
                    data-testid="config-method-ccswitch"
                    border
                    class="!mx-0 !h-full w-full !items-start !p-4"
                    value="ccswitch"
                    @click="setConfigMethod('ccswitch')"
                  >
                    <div class="whitespace-normal">
                      <div class="flex flex-wrap items-center gap-2">
                        <div class="font-medium text-gray-900">{{ ccSwitchMethodTitle }}</div>
                        <ElTag
                          data-testid="config-method-recommended"
                          class="shrink-0"
                          type="success"
                          effect="light"
                          size="small"
                        >
                          {{ t('user.recommended') }}
                        </ElTag>
                      </div>
                      <p class="mt-1 text-sm text-gray-600">{{ ccSwitchMethodHelp }}</p>
                      <p class="mt-3 text-xs text-gray-500">{{ ccSwitchMethodAudience }}</p>
                    </div>
                  </ElRadio>
                </ElRadioGroup>

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
                        <ElButton
                          class="shrink-0"
                          link
                          type="primary"
                          :data-testid="`manual-config-copy-${snippet.key}`"
                          @click="copyManualConfigSnippet(snippet)"
                        >
                          {{ manualConfigCopyLabel(snippet) }}
                        </ElButton>
                      </div>
                      <pre class="mt-3 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 text-xs text-green-300">{{ snippet.body }}</pre>
                    </div>
                    <div v-if="pendingManualConfigSnippet">
                      <ElAlert
                        type="warning"
                        :closable="false"
                        show-icon
                        :title="t('user.confirmCopyConfigSnippet')"
                        :description="t('user.secretRiskText')"
                      />
                      <div class="mt-3 flex flex-wrap gap-2">
                        <ElButton
                          data-testid="confirm-manual-config-copy"
                          type="warning"
                          @click="confirmManualConfigCopy"
                        >
                          {{ t('user.confirmAction') }}
                        </ElButton>
                        <ElButton
                          @click="manualConfigConfirmKey = ''"
                        >
                          {{ t('user.cancel') }}
                        </ElButton>
                      </div>
                    </div>
                  </div>
                  <p v-else class="mt-4 rounded-md border border-amber-200 bg-amber-50 p-3 text-xs text-amber-900">
                    {{ t('user.manualConfigUnsupportedPlatform') }}
                  </p>
                </div>

                <div v-if="selectedConfigMethod === 'automatic'" class="mt-4 rounded-lg border border-gray-200 p-4">
                  <p class="text-sm leading-5 text-gray-600">{{ t('user.automaticConfigProviderHelp') }}</p>
                  <p class="mt-2 text-sm leading-5 text-gray-600">{{ t('user.automaticConfigOverview') }}</p>

                  <section class="mt-4">
                    <div class="text-base font-semibold leading-6 text-gray-900">{{ t('user.setupStepConfigureTitle') }}</div>
                    <div class="mt-4 space-y-3 text-sm">
                      <div v-for="command in automaticAccessCommands" :key="command.key" class="rounded-md border border-gray-200 p-3 shadow-sm">
                        <div class="flex items-center justify-between gap-3">
                          <div class="text-[11px] font-semibold text-gray-500">{{ command.label }}</div>
                          <ElButton link type="primary" @click="copyCommand(command.key, command.value)">
                            {{ copyCommandLabel(command.key) }}
                          </ElButton>
                        </div>
                        <pre class="mt-1.5 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 text-[13px] leading-5 text-green-300">{{ command.value }}</pre>
                        <ElCollapse
                          v-if="command.fallback"
                          :data-testid="command.fallback.detailsTestId"
                          class="mt-3 rounded-md border border-gray-200 bg-gray-50 px-3"
                        >
                          <ElCollapseItem name="fallback">
                            <template #title>
                              <span class="text-sm font-medium leading-6 text-gray-700">{{ command.fallback.title }}</span>
                            </template>
                            <p class="text-xs leading-5 text-gray-500">{{ command.fallback.help }}</p>
                            <div class="mt-2 flex items-center justify-between gap-3">
                              <div class="text-[10px] font-semibold uppercase tracking-wide text-gray-500">{{ command.fallback.label }}</div>
                              <ElButton link type="primary" @click="copyCommand(command.fallback.copyKey, command.fallback.value)">
                                {{ copyCommandLabel(command.fallback.copyKey) }}
                              </ElButton>
                            </div>
                            <pre class="mt-1.5 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 text-[13px] leading-5 text-green-300">{{ command.fallback.value }}</pre>
                          </ElCollapseItem>
                        </ElCollapse>
                      </div>
                    </div>
                  </section>
                </div>

                <div v-if="selectedConfigMethod === 'ccswitch'" class="mt-4 rounded-lg border border-gray-200 p-4">
                  <div class="font-medium text-gray-900">{{ ccSwitchMethodTitle }}</div>
                  <p class="mt-1 text-sm text-gray-600">{{ ccSwitchMethodHelp }}</p>
                  <div class="mt-3">
                    <ElLink
                      :href="t('user.ccSwitchDownloadUrl')"
                      target="_blank"
                      type="primary"
                    >
                      {{ t('user.ccSwitchDownload') }}
                    </ElLink>
                  </div>
                  <div class="mt-4 flex flex-wrap gap-2">
                    <ElButton
                      v-for="item in ccSwitchImports"
                      :key="item.key"
                      :data-testid="`ccswitch-import-${item.key}`"
                      tag="a"
                      :href="item.href"
                      type="primary"
                    >
                      {{ item.label }}
                    </ElButton>
                  </div>
                  <p
                    v-if="selectedIsAgentGroup"
                    class="mt-3 rounded-md border border-amber-200 bg-amber-50 p-3 text-xs text-amber-900"
                  >
                    {{ t('user.agentImportHermesVersionWarning') }}
                  </p>
                  <p
                    v-if="selectedIsAgentGroup"
                    data-testid="agent-import-v1-notice"
                    class="mt-3 rounded-md border border-amber-200 bg-amber-50 p-3 text-xs text-amber-900"
                  >
                    {{ t('user.agentImportV1EndpointNotice') }}
                  </p>
                  <p class="mt-3 text-xs text-gray-500">{{ t('user.ccSwitchFallback') }}</p>
                </div>
              </section>
            </div>
          </section>

        </div>
      </div>

      <ReportingReadinessGuide
        v-if="reportingCapabilities?.setup_available"
        variant="full"
        :readiness-available="reportingCapabilities.readiness_available"
      />
    </div>
  </AppLayout>
</template>
