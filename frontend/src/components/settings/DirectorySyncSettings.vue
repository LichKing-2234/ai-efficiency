<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import {
  createDirectorySource,
  getDirectoryRun,
  listDirectoryRuns,
  previewDirectorySource,
  startDirectoryRun,
  updateDirectorySource,
  validateDirectorySource,
} from '@/api/directory'
import { useToast } from '@/composables/useToast'
import { useI18n, type MessageKey } from '@/i18n'
import { useSettingsResourcesStore } from '@/stores/settingsResources'
import { useWorkItemsStore } from '@/stores/workItems'
import type { DirectorySource, DirectorySourceRequest, DirectorySyncRun, DirectoryValidationIssue } from '@/types'

const { t } = useI18n()
const { showToast } = useToast()
const workItems = useWorkItemsStore()
const settingsResources = useSettingsResourcesStore()
const {
  directorySources: sources,
  directorySourcesLoading: loading,
  directorySourcesError,
} = storeToRefs(settingsResources)

const selectedSourceId = ref<number | null>(null)
const saving = ref(false)
const message = ref('')
const error = ref('')
const validationIssues = ref<DirectoryValidationIssue[]>([])
const aiPromptContext = ref('')
const runWarningSummaries = ref<Array<{ code: string; count: number; labelKey: MessageKey; helpKey: MessageKey }>>([])
const activeRun = ref<DirectorySyncRun | null>(null)
const activeRunAction = ref<'preview' | 'apply' | null>(null)
let displayRunPollTimer: number | undefined
let runRecoveryRequest = 0
let unmounted = false
const trackedApplyRunIDs = new Set<number>()
const applyPollTimers = new Map<number, number>()
const applyPollRequests = new Set<number>()
const form = ref<DirectorySourceRequest>({
  name: '',
  description: '',
  scope: 'full_company',
  enabled: false,
  dsl: '',
  schedule_enabled: false,
  schedule_interval: 'daily',
  schedule_timezone: 'UTC',
})

const selectedSource = computed(() => sources.value.find((source) => source.id === selectedSourceId.value) || null)
const currentCredentialRef = computed(() => {
  const match = form.value.dsl.match(/^\s*credential_ref:\s*["']?([^"'\s#]+)["']?\s*$/m)
  return match?.[1] || 'directory_api_key'
})

const templates: Array<{ nameKey: MessageKey; dsl: string }> = [
  {
    nameKey: 'directorySync.templateDepartmentsMembers',
    dsl: `version: 1
scope: full_company
auth:
  type: header
  header: X-Directory-API-Key
  credential_ref: directory_api_key
limits:
  timeout_seconds: 30
  max_response_bytes: 1048576
  max_items: 50000
steps:
  - id: departments
    request:
      method: GET
      url: https://directory.example.com/api/v1/departments
    extract:
      items: $.data.departments
    map:
      department:
        external_id: $.id
        parent_external_id: $.parent_id
        name: $.name
        path: $.path
        metadata:
          representative_external_ids: $.leader_ids
  - id: members
    foreach: departments.items
    request:
      method: GET
      url: https://directory.example.com/api/v1/users
      query:
        department_id: "{{ item.external_id }}"
    extract:
      items: $.data.users
    map:
      member:
        external_id: $.id
        email: $.email
        display_name: $.name
        department_external_id: "{{ source.external_id }}"
        status: $.status
        metadata:
          leader_department_ids: $.leader_department_ids
`,
  },
  {
    nameKey: 'directorySync.templateSingleMembers',
    dsl: `version: 1
scope: full_company
auth:
  type: header
  header: X-Directory-API-Key
  credential_ref: directory_api_key
limits:
  timeout_seconds: 30
  max_response_bytes: 1048576
  max_items: 50000
steps:
  - id: members
    request:
      method: GET
      url: https://directory.example.com/api/v1/members
    extract:
      items: $.data.members
    map:
      member:
        external_id: $.id
        email: $.email
        display_name: $.name
        department_external_id: $.department_id
        status: $.status
`,
  },
  {
    nameKey: 'directorySync.templatePagedMembers',
    dsl: `version: 1
scope: full_company
auth:
  type: header
  header: X-Directory-API-Key
  credential_ref: directory_api_key
limits:
  timeout_seconds: 30
  max_response_bytes: 1048576
  max_items: 50000
steps:
  - id: members
    request:
      method: GET
      url: https://directory.example.com/api/v1/members?page=1
    extract:
      items: $.data.items
    map:
      member:
        external_id: $.id
        email: $.email
        display_name: $.name
        department_external_id: $.department_id
        status: $.status
`,
  },
]

onMounted(loadSources)
onUnmounted(() => {
  unmounted = true
  runRecoveryRequest++
  stopDisplayRunPolling()
  stopApplyRunPolling()
  applyPollRequests.clear()
  trackedApplyRunIDs.clear()
})

async function loadSources(options: { force?: boolean } = {}) {
  await settingsResources.loadDirectorySources(options)
  if (directorySourcesError.value) {
    error.value = directorySourcesError.value || t('directorySync.loadSourcesFailed')
    return
  }
  if (sources.value.length > 0) {
    const current = sources.value.find((source) => source.id === selectedSourceId.value) ?? sources.value[0]
    selectSource(current)
  } else {
    applyTemplate(templates[0].dsl)
  }
}

function selectSource(source: DirectorySource) {
  clearFeedback()
  selectedSourceId.value = source.id
  form.value = {
    name: source.name,
    description: source.description || '',
    scope: 'full_company',
    enabled: source.enabled,
    dsl: source.dsl || templates[0].dsl,
    schedule_enabled: source.schedule_enabled,
    schedule_interval: source.schedule_interval || 'daily',
    schedule_timezone: source.schedule_timezone || 'UTC',
  }
  void startRecoverLatestRun(source.id).catch(() => {
    // Recovery is best-effort; normal source loading feedback stays separate.
  })
}

function applyTemplate(dsl: string) {
  clearFeedback()
  form.value.dsl = dsl
  if (!form.value.name) form.value.name = t('directorySync.exampleName')
  if (!form.value.description) form.value.description = t('directorySync.exampleDescription')
}

function clearFeedback() {
  runRecoveryRequest++
  stopDisplayRunPolling()
  message.value = ''
  error.value = ''
  validationIssues.value = []
  runWarningSummaries.value = []
  activeRun.value = null
  activeRunAction.value = null
}

function apiErrorMessage(e: any, fallback: string) {
  return e?.response?.data?.message || e?.message || fallback
}

function runStats(run: DirectorySyncRun | undefined) {
  return {
    departments: run?.department_count ?? 0,
    members: run?.member_count ?? 0,
    warnings: run?.warning_count ?? 0,
  }
}

const warningCopy: Record<string, { labelKey: MessageKey; helpKey: MessageKey }> = {
  duplicate_member_email: {
    labelKey: 'directorySync.warningDuplicateEmail',
    helpKey: 'directorySync.warningDuplicateEmailHelp',
  },
  invalid_member_email: {
    labelKey: 'directorySync.warningInvalidEmail',
    helpKey: 'directorySync.warningInvalidEmailHelp',
  },
  unknown: {
    labelKey: 'directorySync.warningOther',
    helpKey: 'directorySync.warningOtherHelp',
  },
}

function summarizeWarnings(run: DirectorySyncRun | undefined) {
  const counts = new Map<string, number>()
  for (const warning of run?.warnings ?? []) {
    const code = warningCopy[warning.code] ? warning.code : 'unknown'
    counts.set(code, (counts.get(code) ?? 0) + 1)
  }

  const knownCount = Array.from(counts.values()).reduce((sum, count) => sum + count, 0)
  const totalCount = run?.warning_count ?? knownCount
  const unlistedCount = totalCount - knownCount
  if (unlistedCount > 0) {
    counts.set('unknown', (counts.get('unknown') ?? 0) + unlistedCount)
  }

  return Array.from(counts.entries())
    .filter(([, count]) => count > 0)
    .map(([code, count]) => ({
      code,
      count,
      ...warningCopy[code],
    }))
}

function warningRecordUnit(count: number) {
  return t(count === 1 ? 'directorySync.warningRecordSingular' : 'directorySync.warningRecordPlural')
}

function isTerminalRun(run: DirectorySyncRun | undefined) {
  return run?.status === 'completed' || run?.status === 'completed_with_warnings' || run?.status === 'failed'
}

function isActiveRun(run: DirectorySyncRun | undefined) {
  return run?.status === 'queued' || run?.status === 'running'
}

function actionForRun(run: DirectorySyncRun | undefined): 'preview' | 'apply' | null {
  if (run?.mode === 'preview') return 'preview'
  if (run?.mode === 'apply') return 'apply'
  return null
}

function stopDisplayRunPolling() {
  if (displayRunPollTimer !== undefined) {
    window.clearTimeout(displayRunPollTimer)
    displayRunPollTimer = undefined
  }
}

function stopApplyRunPolling(runID?: number) {
  if (runID !== undefined) {
    const timer = applyPollTimers.get(runID)
    if (timer !== undefined) window.clearTimeout(timer)
    applyPollTimers.delete(runID)
    return
  }
  for (const timer of applyPollTimers.values()) {
    window.clearTimeout(timer)
  }
  applyPollTimers.clear()
}

function scheduleDisplayRunPolling(runID: number, sourceID?: number | null) {
  stopDisplayRunPolling()
  if (unmounted) return
  displayRunPollTimer = window.setTimeout(() => {
    displayRunPollTimer = undefined
    void pollDisplayedPreviewUntilDone(runID, sourceID)
  }, 1500)
}

function scheduleApplyRunPolling(runID: number) {
  if (unmounted || applyPollTimers.has(runID)) return
  const timer = window.setTimeout(() => {
    applyPollTimers.delete(runID)
    void pollObservedApplyUntilDone(runID)
  }, 1500)
  applyPollTimers.set(runID, timer)
}

function phaseLabel(phase?: string) {
  const labels: Record<string, MessageKey> = {
    validating: 'directorySync.phaseValidating',
    executing: 'directorySync.phaseExecuting',
    normalizing: 'directorySync.phaseNormalizing',
    applying: 'directorySync.phaseApplying',
    completed: 'directorySync.phaseCompleted',
    failed: 'directorySync.phaseFailed',
  }
  return t(phase ? labels[phase] ?? 'directorySync.phaseValidating' : 'directorySync.phaseValidating')
}

function showRunResult(run: DirectorySyncRun | undefined, action: 'preview' | 'apply') {
  const status = run?.status
  if (status === 'completed_with_warnings') {
    runWarningSummaries.value = summarizeWarnings(run)
    message.value = t(action === 'preview' ? 'directorySync.previewCompletedWithWarnings' : 'directorySync.applyCompletedWithWarnings', runStats(run))
    return
  }
  if (status === 'completed') {
    message.value = t(action === 'preview' ? 'directorySync.previewCompleted' : 'directorySync.applyCompleted', runStats(run))
    return
  }
  if (status === 'failed') {
    message.value = t(action === 'preview' ? 'directorySync.previewRunFailed' : 'directorySync.applyRunFailed')
  } else {
    message.value = t(action === 'preview' ? 'directorySync.previewStarted' : 'directorySync.applyStarted')
  }
  if (run?.status === 'failed' && run.error_message) {
    error.value = run.error_message
  }
}

async function refreshWorkItemsForCompletedApply(run: DirectorySyncRun, action: 'preview' | 'apply') {
  if (action !== 'apply' || !isTerminalRun(run)) return
  const tracked = trackedApplyRunIDs.delete(run.id)
  if (!tracked || run.status === 'failed') return
  workItems.invalidateCounts()
  await workItems.loadCounts({ force: true })
}

async function applyRunProgress(run: DirectorySyncRun | undefined, action: 'preview' | 'apply') {
  if (!run) return
  activeRun.value = run
  activeRunAction.value = action
  if (isTerminalRun(run)) {
    stopDisplayRunPolling()
    showRunResult(run, action)
    return
  }
  message.value = t(action === 'preview' ? 'directorySync.previewStarted' : 'directorySync.applyStarted')
}

async function pollDisplayedPreviewUntilDone(runID: number, sourceID?: number | null) {
  try {
    const res = await getDirectoryRun(runID)
    if (unmounted) return
    const run = res.data.data
    if ((sourceID && selectedSourceId.value !== sourceID) || (run?.source_id && selectedSourceId.value !== run.source_id)) {
      return
    }
    await applyRunProgress(run, 'preview')
    if (run && !isTerminalRun(run)) {
      scheduleDisplayRunPolling(runID, sourceID ?? run.source_id)
    }
  } catch (e: any) {
    if (unmounted) return
    stopDisplayRunPolling()
    activeRun.value = null
    activeRunAction.value = null
    error.value = apiErrorMessage(e, t('directorySync.runProgressFailed'))
  }
}

async function pollObservedApplyUntilDone(runID: number) {
  if (unmounted || !trackedApplyRunIDs.has(runID) || applyPollRequests.has(runID)) return
  applyPollRequests.add(runID)
  try {
    const res = await getDirectoryRun(runID)
    if (unmounted || !trackedApplyRunIDs.has(runID)) return
    const run = res.data.data
    if (!run) {
      stopApplyRunPolling(runID)
      trackedApplyRunIDs.delete(runID)
      return
    }
    const isDisplayed = activeRunAction.value === 'apply'
      && activeRun.value?.id === runID
      && (!run.source_id || selectedSourceId.value === run.source_id)
    if (isDisplayed) {
      await applyRunProgress(run, 'apply')
    }
    if (isTerminalRun(run)) {
      stopApplyRunPolling(runID)
      await refreshWorkItemsForCompletedApply(run, 'apply')
      return
    }
    scheduleApplyRunPolling(runID)
  } catch (e: any) {
    if (unmounted) return
    stopApplyRunPolling(runID)
    trackedApplyRunIDs.delete(runID)
    if (activeRunAction.value === 'apply' && activeRun.value?.id === runID) {
      activeRun.value = null
      activeRunAction.value = null
      error.value = apiErrorMessage(e, t('directorySync.runProgressFailed'))
    }
  } finally {
    applyPollRequests.delete(runID)
  }
}

function startRecoverLatestRun(sourceID: number, expectedAction?: 'preview' | 'apply') {
  return recoverLatestRun(sourceID, expectedAction, ++runRecoveryRequest)
}

async function recoverLatestRun(sourceID: number, expectedAction?: 'preview' | 'apply', requestID = runRecoveryRequest) {
  const res = await listDirectoryRuns(sourceID)
  const runs = res.data.data?.items ?? []
  if (unmounted || selectedSourceId.value !== sourceID || requestID !== runRecoveryRequest) return false
  const candidates = runs.filter((candidate) => {
    const action = actionForRun(candidate)
    return Boolean(action && (!expectedAction || action === expectedAction))
  })
  const activeApplyRuns = candidates.filter((candidate) => candidate.mode === 'apply' && isActiveRun(candidate))
  for (const applyRun of activeApplyRuns) {
    trackedApplyRunIDs.add(applyRun.id)
    scheduleApplyRunPolling(applyRun.id)
  }
  const run = candidates.find(isActiveRun) ?? candidates[0]
  const action = actionForRun(run)
  if (!run || !action) return false
  await applyRunProgress(run, action)
  if (isActiveRun(run) && action === 'preview') {
    scheduleDisplayRunPolling(run.id, sourceID)
  }
  return true
}

async function saveSource() {
  saving.value = true
  clearFeedback()
  try {
    if (selectedSourceId.value) {
      await updateDirectorySource(selectedSourceId.value, form.value)
      workItems.invalidateCounts()
      await Promise.all([
        loadSources({ force: true }),
        workItems.loadCounts({ force: true }),
      ])
    } else {
      const res = await createDirectorySource(form.value)
      selectedSourceId.value = res.data.data?.id ?? null
      await loadSources({ force: true })
    }
    message.value = t('directorySync.saved')
  } catch (e: any) {
    error.value = apiErrorMessage(e, t('directorySync.saveFailed'))
  } finally {
    saving.value = false
  }
}

async function validateSource() {
  if (!selectedSourceId.value) return
  clearFeedback()
  try {
    const res = await validateDirectorySource(selectedSourceId.value)
    const data = res.data.data
    validationIssues.value = data?.issues ?? []
    message.value = data?.valid ? t('directorySync.validationPassed') : t('directorySync.validationIssues', { count: validationIssues.value.length })
  } catch (e: any) {
    error.value = apiErrorMessage(e, t('directorySync.validateFailed'))
  }
}

async function previewSource() {
  if (!selectedSourceId.value) return
  clearFeedback()
  activeRunAction.value = 'preview'
  message.value = t('directorySync.previewStarted')
  try {
    const res = await previewDirectorySource(selectedSourceId.value)
    const run = res.data.data
    await applyRunProgress(run, 'preview')
    if (run && !isTerminalRun(run)) {
      await pollDisplayedPreviewUntilDone(run.id, selectedSourceId.value)
    }
  } catch (e: any) {
    if (e?.response?.status === 409) {
      try {
        if (await startRecoverLatestRun(selectedSourceId.value, 'preview')) return
      } catch {
        // Fall through to the original preview error.
      }
    }
    error.value = apiErrorMessage(e, t('directorySync.previewFailed'))
  }
}

async function runNow() {
  if (!selectedSourceId.value) return
  clearFeedback()
  activeRunAction.value = 'apply'
  message.value = t('directorySync.applyStarted')
  try {
    const res = await startDirectoryRun(selectedSourceId.value, { mode: 'apply' })
    if (unmounted) return
    const run = res.data.data
    if (run?.id) trackedApplyRunIDs.add(run.id)
    await applyRunProgress(run, 'apply')
    if (run?.id && isTerminalRun(run)) {
      await refreshWorkItemsForCompletedApply(run, 'apply')
    } else if (run?.id) {
      await pollObservedApplyUntilDone(run.id)
    }
  } catch (e: any) {
    if (e?.response?.status === 409) {
      try {
        if (await startRecoverLatestRun(selectedSourceId.value, 'apply')) return
      } catch {
        // Fall through to the original apply error.
      }
    }
    error.value = apiErrorMessage(e, t('directorySync.runFailed'))
  }
}

async function copyAIPrompt() {
  const context = aiPromptContext.value.trim()
  const contextLines = context
    ? [t('directorySync.aiPromptProvidedDocs'), context]
    : [
        t('directorySync.aiPromptNoDocs'),
        t('directorySync.aiPromptNoDocsOrder'),
        t('directorySync.aiPromptNoDocsDepartment'),
        t('directorySync.aiPromptNoDocsMember'),
        t('directorySync.aiPromptNoDocsPagination'),
        t('directorySync.aiPromptNoDocsAuth'),
        t('directorySync.aiPromptNoDocsSamples'),
      ]
  const prompt = [
    t('directorySync.aiPromptLine1'),
    t('directorySync.aiPromptLine2'),
    '',
    t('directorySync.aiPromptTargetContractTitle'),
    t('directorySync.aiPromptContractRules'),
    t('directorySync.aiPromptIndentationRule'),
    t('directorySync.aiPromptRootArrayRule'),
    `version: 1
scope: full_company
auth:
  type: header
  header: X-Directory-API-Key
  credential_ref: directory_api_key
limits:
  timeout_seconds: 30
  max_response_bytes: 1048576
  max_items: 50000
steps:
  - id: departments
    request:
      method: GET
      url: https://directory.example.com/api/v1/departments
    extract:
      items: $.data.departments
    map:
      department:
        external_id: $.id
        parent_external_id: $.parent_id
        name: $.name
        path: $.path
  - id: members
    foreach: departments.items
    request:
      method: GET
      url: https://directory.example.com/api/v1/users
      query:
        department_id: "{{ item.external_id }}"
    extract:
      items: $.data.users
    map:
      member:
        external_id: $.id
        email: $.email
        display_name: $.name
        department_external_id: "{{ source.external_id }}"
        status: $.status`,
    '',
    t('directorySync.aiPromptStructuresTitle'),
    '- department.external_id: required stable department id',
    '- department.parent_external_id: optional parent department id',
    '- department.name: required display name',
    '- department.path: optional full path',
    '- department.metadata.representative_external_ids: optional array of representative member ids, when the department response provides leader or owner ids',
    '- member.external_id: optional stable person id',
    '- member.email: required user email; this system matches local users only by normalized email',
    '- member.display_name: optional display name',
    '- member.department_external_id: optional department id; use $.department_id, $.departmentIds[0], or {{ source.external_id }} when the member endpoint returns only direct members for the requested department',
    '- member.department_external_ids: optional array of department ids; use this when one member row returns all department memberships, while department_external_id can remain the primary or first department',
    '- member.status: optional employment status',
    '- member.metadata.leader_department_ids: optional array of department ids where this member is the representative or leader',
    '- metadata mappings are explicit allowlists; include only non-sensitive ids or role flags needed by this system',
    '',
    t('directorySync.aiPromptEvidenceTitle'),
    t('directorySync.aiPromptEvidenceTools'),
    t('directorySync.aiPromptEvidenceSummary'),
    t('directorySync.aiPromptEvidenceNoRawRows'),
    t('directorySync.aiPromptEvidenceEmail'),
    t('directorySync.aiPromptEvidenceExternalID'),
    t('directorySync.aiPromptEvidencePagination'),
    t('directorySync.aiPromptEvidenceAskFirst'),
    '',
    ...contextLines,
    '',
    t('directorySync.aiPromptLine3'),
    t('directorySync.aiPromptLine4'),
    t('directorySync.aiPromptLine5'),
  ].join('\n')
  try {
    await navigator.clipboard.writeText(prompt)
    showToast({ message: t('directorySync.aiPromptCopied'), tone: 'success' })
  } catch {
    showToast({ message: t('directorySync.copyFailed'), tone: 'error' })
  }
}
</script>

<template>
  <section class="space-y-4 rounded-lg border border-gray-200 bg-white p-5">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <h3 class="text-base font-semibold text-gray-900">{{ t('directorySync.title') }}</h3>
        <p class="text-sm text-gray-500">{{ t('directorySync.subtitle') }}</p>
      </div>
      <button data-testid="directory-copy-ai-prompt" type="button" class="rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 hover:bg-gray-50" @click="copyAIPrompt">
        {{ t('directorySync.copyAiPrompt') }}
      </button>
    </div>

    <div class="grid gap-4 lg:grid-cols-[220px_1fr]">
      <div class="space-y-2">
        <button
          v-for="source in sources"
          :key="source.id"
          type="button"
          class="block w-full rounded-md border px-3 py-2 text-left text-sm"
          :class="source.id === selectedSourceId ? 'border-indigo-500 bg-indigo-50 text-indigo-900' : 'border-gray-200 text-gray-700 hover:bg-gray-50'"
          @click="selectSource(source)"
        >
          <span class="block font-medium">{{ source.name }}</span>
          <span class="block text-xs text-gray-500">{{ source.enabled ? t('settings.enabled') : t('settings.disabled') }}</span>
        </button>
        <p v-if="!loading && sources.length === 0" class="text-sm text-gray-500">{{ t('directorySync.noSource') }}</p>
      </div>

      <div class="space-y-4">
        <div class="grid gap-3 md:grid-cols-2">
          <label class="text-sm font-medium text-gray-700">
            {{ t('settings.name') }}
            <input data-testid="directory-source-name" v-model="form.name" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
          </label>
          <label class="text-sm font-medium text-gray-700">
            {{ t('directorySync.schedule') }}
            <select v-model="form.schedule_interval" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm">
              <option value="hourly">{{ t('directorySync.hourly') }}</option>
              <option value="daily">{{ t('directorySync.daily') }}</option>
              <option value="weekly">{{ t('directorySync.weekly') }}</option>
            </select>
          </label>
        </div>

        <div class="flex flex-wrap items-center gap-4 text-sm text-gray-700">
          <label class="inline-flex items-center gap-2">
            <input v-model="form.enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-indigo-600" />
            {{ t('settings.enabled') }}
          </label>
          <label class="inline-flex items-center gap-2">
            <input v-model="form.schedule_enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-indigo-600" />
            {{ t('directorySync.scheduledApply') }}
          </label>
          <span class="text-gray-500">{{ t('directorySync.credentialRef', { ref: currentCredentialRef }) }}</span>
        </div>

        <div class="rounded-md border border-gray-200 p-3">
          <div class="mb-2 flex flex-wrap gap-2">
            <button v-for="template in templates" :key="template.nameKey" type="button" class="rounded-md border border-gray-300 px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-50" @click="applyTemplate(template.dsl)">
              {{ t(template.nameKey) }}
            </button>
          </div>
          <p class="mb-2 text-xs text-gray-500">{{ t('directorySync.templatePlaceholderHelp') }}</p>
          <textarea data-testid="directory-dsl" v-model="form.dsl" class="h-72 w-full rounded-md border border-gray-300 px-3 py-2 font-mono text-xs" />
        </div>

        <div class="rounded-md border border-gray-200 p-3">
          <label class="text-sm font-medium text-gray-700">
            {{ t('directorySync.aiContextLabel') }}
            <textarea
              data-testid="directory-ai-context"
              v-model="aiPromptContext"
              class="mt-2 h-28 w-full rounded-md border border-gray-300 px-3 py-2 text-xs"
              :placeholder="t('directorySync.aiContextPlaceholder')"
            />
          </label>
          <div class="mt-3 rounded-md bg-gray-50 p-3 text-xs text-gray-600">
            <p class="font-medium text-gray-700">{{ t('directorySync.noDocsChecklistTitle') }}</p>
            <ol class="mt-2 list-decimal space-y-1 pl-4">
              <li>{{ t('directorySync.noDocsDepartment') }}</li>
              <li>{{ t('directorySync.noDocsMember') }}</li>
              <li>{{ t('directorySync.noDocsPagination') }}</li>
              <li>{{ t('directorySync.noDocsAuth') }}</li>
              <li>{{ t('directorySync.noDocsSamples') }}</li>
            </ol>
          </div>
        </div>

        <div v-if="message" class="rounded-md bg-green-50 p-3 text-sm text-green-700">{{ message }}</div>
        <div v-if="error" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</div>
        <div v-if="activeRun && !isTerminalRun(activeRun)" class="rounded-md bg-blue-50 p-3 text-sm text-blue-900" aria-live="polite">
          <p class="font-medium">{{ phaseLabel(activeRun.phase) }}</p>
          <p class="mt-1 text-xs text-blue-800">
            {{ t('directorySync.runProgressCounts', { departments: activeRun.department_count ?? 0, members: activeRun.member_count ?? 0, warnings: activeRun.warning_count ?? 0 }) }}
          </p>
        </div>
        <div v-if="runWarningSummaries.length > 0" class="rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900">
          <p class="font-medium">{{ t('directorySync.warningDetailsTitle') }}</p>
          <ul class="mt-2 space-y-2">
            <li v-for="summary in runWarningSummaries" :key="summary.code">
              <p class="font-medium">{{ t('directorySync.warningDetailCount', { label: t(summary.labelKey), count: summary.count, unit: warningRecordUnit(summary.count) }) }}</p>
              <p class="text-xs leading-5 text-amber-800">{{ t(summary.helpKey) }}</p>
            </li>
          </ul>
        </div>
        <div v-if="validationIssues.length > 0" class="rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900">
          <p class="font-medium">{{ t('directorySync.validationIssueDetails') }}</p>
          <ul class="mt-2 space-y-1">
            <li v-for="issue in validationIssues" :key="`${issue.path}:${issue.message}`" class="font-mono text-xs">
              {{ issue.path }}: {{ issue.message }}
            </li>
          </ul>
        </div>

        <div class="flex flex-wrap justify-end gap-2">
          <button data-testid="directory-validate" type="button" :disabled="!selectedSourceId || Boolean(activeRun && !isTerminalRun(activeRun))" class="rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 disabled:opacity-50" @click="validateSource">
            {{ t('directorySync.validate') }}
          </button>
          <button data-testid="directory-preview" type="button" :disabled="!selectedSourceId || Boolean(activeRun && !isTerminalRun(activeRun))" class="rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 disabled:opacity-50" @click="previewSource">
            {{ activeRunAction === 'preview' && activeRun && !isTerminalRun(activeRun) ? t('directorySync.previewing') : t('directorySync.preview') }}
          </button>
          <button data-testid="directory-run-now" type="button" :disabled="!selectedSourceId || Boolean(activeRun && !isTerminalRun(activeRun))" class="rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 disabled:opacity-50" @click="runNow">
            {{ activeRunAction === 'apply' && activeRun && !isTerminalRun(activeRun) ? t('directorySync.running') : t('directorySync.runNow') }}
          </button>
          <button data-testid="directory-save" type="button" :disabled="saving" class="rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white disabled:opacity-50" @click="saveSource">
            {{ saving ? t('settings.saving') : t('settings.save') }}
          </button>
        </div>
      </div>
    </div>
  </section>
</template>
