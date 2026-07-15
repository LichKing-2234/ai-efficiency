<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import {
  createDirectorySource,
  getDirectoryRun,
  listDirectoryRuns,
  listDirectorySources,
  previewDirectorySource,
  startDirectoryRun,
  updateDirectorySource,
  validateDirectorySource,
} from '@/api/directory'
import { useToast } from '@/composables/useToast'
import { useI18n, type MessageKey } from '@/i18n'
import type {
  Credential,
  DirectoryRunSummary,
  DirectorySource,
  DirectorySourceRequest,
  DirectorySyncRun,
  DirectoryValidationIssue,
} from '@/types'

defineProps<{
  credentials: Credential[]
}>()

const { locale, t } = useI18n()
const { showToast } = useToast()

const sources = ref<DirectorySource[]>([])
const selectedSourceId = ref<number | null>(null)
const loading = ref(false)
const saving = ref(false)
const message = ref('')
const error = ref('')
const validationIssues = ref<DirectoryValidationIssue[]>([])
const aiPromptContext = ref('')
const runWarningSummaries = ref<Array<{ code: string; count: number; labelKey: MessageKey; helpKey: MessageKey }>>([])
const activeRun = ref<DirectoryRunSummary | DirectorySyncRun | null>(null)
const activeRunAction = ref<'preview' | 'apply' | null>(null)
const runSummaries = ref<DirectoryRunSummary[]>([])
const runTotal = ref(0)
const runPage = ref(0)
const runPageSize = ref(20)
const runOffset = ref(0)
const pendingRunOffset = ref<number | null>(null)
const runHistoryLoading = ref(false)
const runHistoryError = ref('')
const latestActiveRun = ref<DirectoryRunSummary | null>(null)
const selectedRunId = ref<number | null>(null)
const selectedRunSummary = ref<DirectoryRunSummary | null>(null)
const selectedRunDetail = ref<DirectorySyncRun | null>(null)
const selectedRunLoading = ref(false)
const selectedRunError = ref('')
const RUN_PAGE_SIZE = 20
let runPollTimer: number | undefined
let pageRequestGeneration = 0
let pendingRunPageActionGeneration: number | null = null
let detailRequestGeneration = 0
let actionRequestGeneration = 0
let pollGeneration = 0
let activePollRunId: number | null = null
let pollInFlightGeneration: number | null = null
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
const runPageCount = computed(() => Math.max(1, Math.ceil(runTotal.value / Math.max(runPageSize.value, 1))))
const canLoadPreviousRuns = computed(() => !runHistoryLoading.value && runOffset.value > 0)
const canLoadNextRuns = computed(() => !runHistoryLoading.value && runOffset.value + runPageSize.value < runTotal.value)
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
  pageRequestGeneration++
  detailRequestGeneration++
  actionRequestGeneration++
  invalidateRunPolling()
})

async function loadSources() {
  loading.value = true
  try {
    const res = await listDirectorySources()
    sources.value = res.data.data?.items ?? []
    if (sources.value.length > 0) {
      selectSource(sources.value[0])
    } else {
      applyTemplate(templates[0].dsl)
    }
  } catch (e: any) {
    error.value = e?.message || t('directorySync.loadSourcesFailed')
  } finally {
    loading.value = false
  }
}

function selectSource(source: DirectorySource) {
  actionRequestGeneration++
  clearFeedback()
  resetRunView()
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
  void loadRunPage(source.id, 0)
}

function applyTemplate(dsl: string) {
  clearFeedback()
  form.value.dsl = dsl
  if (!form.value.name) form.value.name = t('directorySync.exampleName')
  if (!form.value.description) form.value.description = t('directorySync.exampleDescription')
}

function clearFeedback() {
  invalidateRunPolling()
  message.value = ''
  error.value = ''
  validationIssues.value = []
  runWarningSummaries.value = []
  activeRun.value = null
  activeRunAction.value = null
  latestActiveRun.value = null
}

function resetRunView() {
  pageRequestGeneration++
  detailRequestGeneration++
  runSummaries.value = []
  runTotal.value = 0
  runPage.value = 0
  runPageSize.value = RUN_PAGE_SIZE
  runOffset.value = 0
  pendingRunOffset.value = null
  pendingRunPageActionGeneration = null
  runHistoryLoading.value = false
  runHistoryError.value = ''
  selectedRunId.value = null
  selectedRunSummary.value = null
  selectedRunDetail.value = null
  selectedRunLoading.value = false
  selectedRunError.value = ''
}

function apiErrorMessage(e: any, fallback: string) {
  return e?.response?.data?.message || e?.message || fallback
}

function actionContextMatches(generation: number, sourceID: number) {
  return generation === actionRequestGeneration && selectedSourceId.value === sourceID
}

function beginActionRequest() {
  const generation = ++actionRequestGeneration
  pageRequestGeneration++
  pendingRunOffset.value = null
  pendingRunPageActionGeneration = null
  runHistoryLoading.value = false
  return generation
}

interface RunPageRecoveryContext {
  action: 'preview' | 'apply'
  generation: number
}

function pageRequestContextMatches(generation: number, sourceID: number, offset: number, recovery?: RunPageRecoveryContext) {
  return generation === pageRequestGeneration
    && selectedSourceId.value === sourceID
    && pendingRunOffset.value === offset
    && pendingRunPageActionGeneration === (recovery?.generation ?? null)
    && (!recovery || actionContextMatches(recovery.generation, sourceID))
}

type RunDisplay = DirectoryRunSummary | DirectorySyncRun

function runStats(run: RunDisplay | undefined) {
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

function summarizeWarnings(run: RunDisplay | undefined) {
  const counts = new Map<string, number>()
  for (const warning of (run as DirectorySyncRun | undefined)?.warnings ?? []) {
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

function isTerminalRun(run: RunDisplay | null | undefined) {
  return run?.status === 'completed' || run?.status === 'completed_with_warnings' || run?.status === 'failed'
}

function isActiveRun(run: RunDisplay | null | undefined) {
  return run?.status === 'queued' || run?.status === 'running'
}

function actionForRun(run: RunDisplay | null | undefined): 'preview' | 'apply' | null {
  if (run?.mode === 'preview') return 'preview'
  if (run?.mode === 'apply') return 'apply'
  return null
}

function invalidateRunPolling() {
  pollGeneration++
  if (runPollTimer) {
    window.clearTimeout(runPollTimer)
    runPollTimer = undefined
  }
  activePollRunId = null
  pollInFlightGeneration = null
}

function pollContextMatches(generation: number, sourceID: number, runID: number) {
  return generation === pollGeneration
    && selectedSourceId.value === sourceID
    && activePollRunId === runID
}

function scheduleRunPolling(runID: number, action: 'preview' | 'apply', sourceID: number, generation: number) {
  if (!pollContextMatches(generation, sourceID, runID) || runPollTimer || pollInFlightGeneration === generation) return
  runPollTimer = window.setTimeout(() => {
    runPollTimer = undefined
    void pollRunUntilDone(runID, action, sourceID, generation)
  }, 1500)
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

function showRunResult(run: RunDisplay | undefined, action: 'preview' | 'apply') {
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
  const detail = run as DirectorySyncRun | undefined
  if (detail?.status === 'failed' && detail.error_message) {
    error.value = detail.error_message
  }
}

function applyRunProgress(run: RunDisplay | undefined, action: 'preview' | 'apply') {
  if (!run) return
  activeRun.value = run
  activeRunAction.value = action
  if (isTerminalRun(run)) {
    showRunResult(run, action)
    return
  }
  message.value = t(action === 'preview' ? 'directorySync.previewStarted' : 'directorySync.applyStarted')
}

function summaryFromRun(run: RunDisplay, sourceID: number): DirectoryRunSummary {
  return {
    id: run.id,
    source_id: run.source_id || sourceID,
    mode: run.mode,
    trigger: run.trigger || 'manual',
    status: run.status,
    phase: run.phase || (run.status === 'failed' ? 'failed' : isTerminalRun(run) ? 'completed' : 'validating'),
    started_at: run.started_at ?? null,
    completed_at: run.completed_at ?? null,
    http_request_count: run.http_request_count ?? 0,
    department_count: run.department_count ?? 0,
    member_count: run.member_count ?? 0,
    invalid_member_count: run.invalid_member_count ?? 0,
    warning_count: run.warning_count ?? 0,
  }
}

function modeLabel(mode: DirectoryRunSummary['mode']) {
  const labels: Record<DirectoryRunSummary['mode'], MessageKey> = {
    validate: 'directorySync.runModeValidate',
    preview: 'directorySync.runModePreview',
    apply: 'directorySync.runModeApply',
  }
  return t(labels[mode])
}

function statusLabel(status: DirectoryRunSummary['status']) {
  const labels: Record<DirectoryRunSummary['status'], MessageKey> = {
    queued: 'directorySync.runStatusQueued',
    running: 'directorySync.runStatusRunning',
    completed: 'directorySync.runStatusCompleted',
    completed_with_warnings: 'directorySync.runStatusCompletedWithWarnings',
    failed: 'directorySync.runStatusFailed',
  }
  return t(labels[status])
}

function runStartedLabel(run: DirectoryRunSummary) {
  if (!run.started_at) return t('directorySync.runQueuedAt')
  return new Date(run.started_at).toLocaleString(locale.value)
}

function formatDiagnostic(value: Record<string, unknown> | undefined) {
  return JSON.stringify(value ?? {}, null, 2)
}

function adoptLatestActiveRun(run: DirectoryRunSummary, sourceID: number) {
  const action = actionForRun(run)
  if (!action) return false
  latestActiveRun.value = run
  applyRunProgress(run, action)
  if (activePollRunId !== run.id) {
    invalidateRunPolling()
    activePollRunId = run.id
  }
  scheduleRunPolling(run.id, action, sourceID, pollGeneration)
  return true
}

function applyPageRecovery(items: DirectoryRunSummary[], latestActive: DirectoryRunSummary | null, sourceID: number, offset: number, expectedAction?: 'preview' | 'apply') {
  if (latestActive) {
    const action = actionForRun(latestActive)
    const recovered = adoptLatestActiveRun(latestActive, sourceID)
    return recovered && (!expectedAction || action === expectedAction)
  }

  latestActiveRun.value = null
  if (activePollRunId !== null) invalidateRunPolling()
  if (offset !== 0) return false
  const newest = items.find((candidate) => Boolean(actionForRun(candidate)))
  const action = actionForRun(newest)
  if (newest && action && !(activeRun.value?.id === newest.id && isTerminalRun(activeRun.value))) {
    applyRunProgress(newest, action)
  }
  return expectedAction ? false : Boolean(newest && action)
}

async function loadRunPage(sourceID: number, offset: number, recovery?: RunPageRecoveryContext) {
  const generation = ++pageRequestGeneration
  pendingRunOffset.value = offset
  pendingRunPageActionGeneration = recovery?.generation ?? null
  runHistoryLoading.value = true
  runHistoryError.value = ''
  try {
    const res = await listDirectoryRuns(sourceID, { limit: RUN_PAGE_SIZE, offset })
    if (!pageRequestContextMatches(generation, sourceID, offset, recovery)) return false
    const page = res.data.data ?? {
      items: [],
      total: 0,
      page: Math.floor(offset / RUN_PAGE_SIZE),
      page_size: RUN_PAGE_SIZE,
      latest_active_run: null,
    }
    runSummaries.value = page.items
    runTotal.value = page.total
    runPage.value = page.page
    runPageSize.value = page.page_size
    runOffset.value = offset
    return applyPageRecovery(page.items, page.latest_active_run, sourceID, offset, recovery?.action)
  } catch (e: any) {
    if (pageRequestContextMatches(generation, sourceID, offset, recovery)) {
      runHistoryError.value = apiErrorMessage(e, t('directorySync.runHistoryLoadFailed'))
    }
    return false
  } finally {
    if (pageRequestContextMatches(generation, sourceID, offset, recovery)) {
      pendingRunOffset.value = null
      pendingRunPageActionGeneration = null
      runHistoryLoading.value = false
    }
  }
}

function loadPreviousRunPage() {
  if (!selectedSourceId.value || !canLoadPreviousRuns.value) return
  void loadRunPage(selectedSourceId.value, Math.max(0, runOffset.value - runPageSize.value))
}

function loadNextRunPage() {
  if (!selectedSourceId.value || !canLoadNextRuns.value) return
  void loadRunPage(selectedSourceId.value, runOffset.value + runPageSize.value)
}

async function selectRun(run: DirectoryRunSummary) {
  const sourceID = selectedSourceId.value
  if (!sourceID) return
  const generation = ++detailRequestGeneration
  selectedRunId.value = run.id
  selectedRunSummary.value = run
  selectedRunDetail.value = null
  selectedRunLoading.value = true
  selectedRunError.value = ''
  try {
    const res = await getDirectoryRun(run.id)
    const detail = res.data.data
    if (
      generation !== detailRequestGeneration
      || selectedSourceId.value !== sourceID
      || selectedRunId.value !== run.id
      || (detail?.source_id && detail.source_id !== sourceID)
    ) return
    selectedRunDetail.value = detail ?? null
  } catch (e: any) {
    if (generation === detailRequestGeneration && selectedSourceId.value === sourceID && selectedRunId.value === run.id) {
      selectedRunError.value = apiErrorMessage(e, t('directorySync.runDetailLoadFailed'))
    }
  } finally {
    if (generation === detailRequestGeneration && selectedSourceId.value === sourceID && selectedRunId.value === run.id) {
      selectedRunLoading.value = false
    }
  }
}

async function pollRunUntilDone(runID: number, action: 'preview' | 'apply', sourceID: number, generation: number) {
  if (!pollContextMatches(generation, sourceID, runID)) return
  pollInFlightGeneration = generation
  try {
    const res = await getDirectoryRun(runID)
    const run = res.data.data
    if (
      !pollContextMatches(generation, sourceID, runID)
      || (run?.source_id && run.source_id !== sourceID)
    ) return
    if (!run) return

    applyRunProgress(run, action)
    if (isActiveRun(run)) {
      latestActiveRun.value = summaryFromRun(run, sourceID)
      pollInFlightGeneration = null
      scheduleRunPolling(runID, action, sourceID, generation)
      return
    }

    latestActiveRun.value = null
    activePollRunId = null
    pollGeneration++
    pollInFlightGeneration = null
    if (selectedRunId.value === runID) {
      detailRequestGeneration++
      selectedRunSummary.value = summaryFromRun(run, sourceID)
      selectedRunDetail.value = run
      selectedRunLoading.value = false
      selectedRunError.value = ''
    }
    if (selectedSourceId.value === sourceID) {
      const completedPageOffset = runOffset.value
      await loadRunPage(sourceID, completedPageOffset)
    }
  } catch (e: any) {
    if (!pollContextMatches(generation, sourceID, runID)) return
    invalidateRunPolling()
    activeRun.value = null
    activeRunAction.value = null
    latestActiveRun.value = null
    error.value = apiErrorMessage(e, t('directorySync.runProgressFailed'))
  } finally {
    if (pollInFlightGeneration === generation) pollInFlightGeneration = null
  }
}

async function startCreatedRunPolling(run: DirectorySyncRun, action: 'preview' | 'apply', sourceID: number) {
  invalidateRunPolling()
  activePollRunId = run.id
  latestActiveRun.value = summaryFromRun(run, sourceID)
  applyRunProgress(run, action)
  await pollRunUntilDone(run.id, action, sourceID, pollGeneration)
}

async function saveSource() {
  saving.value = true
  clearFeedback()
  try {
    if (selectedSourceId.value) {
      await updateDirectorySource(selectedSourceId.value, form.value)
    } else {
      const res = await createDirectorySource(form.value)
      selectedSourceId.value = res.data.data?.id ?? null
    }
    message.value = t('directorySync.saved')
    await loadSources()
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
  const sourceID = selectedSourceId.value
  if (!sourceID) return
  const generation = beginActionRequest()
  clearFeedback()
  activeRunAction.value = 'preview'
  message.value = t('directorySync.previewStarted')
  try {
    const res = await previewDirectorySource(sourceID)
    if (!actionContextMatches(generation, sourceID)) return
    const run = res.data.data
    applyRunProgress(run, 'preview')
    if (run && isActiveRun(run)) {
      await startCreatedRunPolling(run, 'preview', sourceID)
    } else if (run && selectedSourceId.value === sourceID) {
      await loadRunPage(sourceID, 0)
    }
  } catch (e: any) {
    if (!actionContextMatches(generation, sourceID)) return
    if (e?.response?.status === 409) {
      const recovered = await loadRunPage(sourceID, 0, { action: 'preview', generation })
      if (!actionContextMatches(generation, sourceID)) return
      if (recovered) return
    }
    error.value = apiErrorMessage(e, t('directorySync.previewFailed'))
  }
}

async function runNow() {
  const sourceID = selectedSourceId.value
  if (!sourceID) return
  const generation = beginActionRequest()
  clearFeedback()
  activeRunAction.value = 'apply'
  message.value = t('directorySync.applyStarted')
  try {
    const res = await startDirectoryRun(sourceID, { mode: 'apply' })
    if (!actionContextMatches(generation, sourceID)) return
    const run = res.data.data
    applyRunProgress(run, 'apply')
    if (run && isActiveRun(run)) {
      await startCreatedRunPolling(run, 'apply', sourceID)
    } else if (run && selectedSourceId.value === sourceID) {
      await loadRunPage(sourceID, 0)
    }
  } catch (e: any) {
    if (!actionContextMatches(generation, sourceID)) return
    if (e?.response?.status === 409) {
      const recovered = await loadRunPage(sourceID, 0, { action: 'apply', generation })
      if (!actionContextMatches(generation, sourceID)) return
      if (recovered) return
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

        <section data-testid="directory-run-history" class="border-t border-gray-200 pt-4">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <h4 class="text-sm font-semibold text-gray-900">{{ t('directorySync.runHistoryTitle') }}</h4>
            <span class="text-xs text-gray-500">{{ t('directorySync.runHistoryTotal', { total: runTotal }) }}</span>
          </div>

          <p v-if="runHistoryError" class="mt-3 text-sm text-red-700">{{ runHistoryError }}</p>
          <p v-if="runHistoryLoading && runSummaries.length === 0" class="mt-3 text-sm text-gray-500">{{ t('directorySync.runHistoryLoading') }}</p>
          <p v-else-if="!runHistoryError && runSummaries.length === 0" class="mt-3 text-sm text-gray-500">{{ t('directorySync.runHistoryEmpty') }}</p>
          <div v-if="runSummaries.length > 0" class="mt-3 divide-y divide-gray-200 border-y border-gray-200" role="list">
            <button
              v-for="run in runSummaries"
              :key="run.id"
              :data-testid="`directory-run-row-${run.id}`"
              type="button"
              class="grid min-h-16 w-full grid-cols-[minmax(0,1fr)_auto] items-center gap-3 px-2 py-3 text-left hover:bg-gray-50"
              :class="selectedRunId === run.id ? 'bg-indigo-50' : ''"
              @click="selectRun(run)"
            >
              <span class="min-w-0">
                <span class="block text-sm font-medium text-gray-900">#{{ run.id }} · {{ modeLabel(run.mode) }}</span>
                <span class="mt-1 block text-xs text-gray-500">{{ runStartedLabel(run) }}</span>
                <span class="mt-1 block text-xs text-gray-500">
                  {{ t('directorySync.runProgressCounts', { departments: run.department_count, members: run.member_count, warnings: run.warning_count }) }}
                </span>
              </span>
              <span class="text-right text-xs font-medium text-gray-700">{{ statusLabel(run.status) }}</span>
            </button>
          </div>

          <div class="mt-3 flex min-h-9 flex-wrap items-center justify-between gap-2">
            <button
              data-testid="directory-run-prev"
              type="button"
              :disabled="!canLoadPreviousRuns"
              class="rounded-md border border-gray-300 px-3 py-1.5 text-xs text-gray-700 disabled:opacity-40"
              @click="loadPreviousRunPage"
            >
              {{ t('directorySync.runHistoryPrevious') }}
            </button>
            <span data-testid="directory-run-page-meta" class="text-xs text-gray-500">
              {{ t('directorySync.runHistoryPage', { page: runPage + 1, pages: runPageCount }) }}
            </span>
            <button
              data-testid="directory-run-next"
              type="button"
              :disabled="!canLoadNextRuns"
              class="rounded-md border border-gray-300 px-3 py-1.5 text-xs text-gray-700 disabled:opacity-40"
              @click="loadNextRunPage"
            >
              {{ t('directorySync.runHistoryNext') }}
            </button>
          </div>

          <div v-if="selectedRunSummary" data-testid="directory-run-detail" class="mt-4 border-t border-gray-200 pt-4">
            <div class="flex flex-wrap items-center justify-between gap-2">
              <h5 class="text-sm font-semibold text-gray-900">{{ t('directorySync.runDetailTitle', { id: selectedRunSummary.id }) }}</h5>
              <span class="text-xs text-gray-500">{{ statusLabel(selectedRunSummary.status) }}</span>
            </div>
            <p v-if="selectedRunLoading" class="mt-3 text-sm text-gray-500">{{ t('directorySync.runDetailLoading') }}</p>
            <p v-if="selectedRunError" class="mt-3 text-sm text-red-700">{{ selectedRunError }}</p>
            <div v-if="selectedRunDetail" class="mt-3 space-y-4 text-sm text-gray-700">
              <p>{{ t('directorySync.runProgressCounts', {
                departments: selectedRunDetail.department_count ?? 0,
                members: selectedRunDetail.member_count ?? 0,
                warnings: selectedRunDetail.warning_count ?? 0,
              }) }}</p>
              <div v-if="selectedRunDetail.warnings?.length">
                <h6 class="text-xs font-semibold uppercase text-gray-500">{{ t('directorySync.runDetailWarnings') }}</h6>
                <ul class="mt-2 space-y-1">
                  <li v-for="(warning, index) in selectedRunDetail.warnings" :key="`${warning.code}:${warning.step_id ?? ''}:${index}`" class="break-words">
                    <span class="font-medium">{{ warning.code }}</span><span v-if="warning.message">: {{ warning.message }}</span>
                  </li>
                </ul>
              </div>
              <div v-if="selectedRunDetail.summary">
                <h6 class="text-xs font-semibold uppercase text-gray-500">{{ t('directorySync.runDetailSummary') }}</h6>
                <pre class="mt-2 overflow-x-auto bg-gray-950 p-3 text-xs text-gray-100">{{ formatDiagnostic(selectedRunDetail.summary) }}</pre>
              </div>
              <div v-if="selectedRunDetail.preview_diff">
                <h6 class="text-xs font-semibold uppercase text-gray-500">{{ t('directorySync.runDetailPreviewDiff') }}</h6>
                <pre class="mt-2 overflow-x-auto bg-gray-950 p-3 text-xs text-gray-100">{{ formatDiagnostic(selectedRunDetail.preview_diff) }}</pre>
              </div>
              <div v-if="selectedRunDetail.error_message">
                <h6 class="text-xs font-semibold uppercase text-gray-500">{{ t('directorySync.runDetailError') }}</h6>
                <p class="mt-2 break-words text-red-700">{{ selectedRunDetail.error_message }}</p>
              </div>
            </div>
          </div>
        </section>
      </div>
    </div>
  </section>
</template>
