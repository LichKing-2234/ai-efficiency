<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { getRepo, repairWebhook, updateRepo } from '@/api/repo'
import { getLatestPRSyncJob, getPRSyncJob, syncPRs } from '@/api/pr'
import { listProviders } from '@/api/scmProvider'
import { useI18n } from '@/i18n'
import type { PRSyncJob, RepoConfig, SCMProvider } from '@/types'
import { repositoryStatusLabel } from '@/utils/displayLabels'

const props = defineProps<{ repoId: number; repo: RepoConfig }>()
const emit = defineEmits<{ repoUpdated: [RepoConfig] }>()
const { t } = useI18n()
const repo = ref(props.repo)
const providers = ref<SCMProvider[]>([])
const providersLoading = ref(false)
const providersError = ref('')
const selectedProviderId = ref<number>(props.repo.edges?.scm_provider?.id ?? props.repo.scm_provider_id ?? 0)
const bindingSaving = ref(false)
const bindingMessage = ref('')
const bindingError = ref('')
const syncing = ref(false)
const syncJob = ref<PRSyncJob | null>(null)
const syncMessage = ref('')
const syncError = ref('')
const webhookRepairing = ref(false)
const webhookForce = ref(false)
const webhookMessage = ref('')
const webhookError = ref('')
let syncPollTimer: number | undefined

const isUnbound = computed(() => repo.value.binding_state === 'unbound')
const webhookMissing = computed(() => !repo.value.webhook_id)
const canRepairWebhook = computed(() => !isUnbound.value && repo.value.status !== 'inactive' && (repo.value.status === 'webhook_failed' || webhookMissing.value))

onMounted(() => {
  void loadProviders()
  void recoverSyncJob()
})
onUnmounted(() => {
  if (syncPollTimer != null) window.clearTimeout(syncPollTimer)
})

async function loadProviders() {
  providersLoading.value = true
  providersError.value = ''
  try {
    const response = await listProviders()
    const data = response.data.data
    providers.value = Array.isArray(data) ? data : (data as { items?: SCMProvider[] } | null)?.items ?? []
  } catch (error: any) {
    providersError.value = error?.response?.data?.message || error?.message || t('repos.noScmProviders')
  } finally { providersLoading.value = false }
}

async function refreshRepo() {
  const response = await getRepo(props.repoId)
  const value = response.data.data
  if (!value) return
  repo.value = value
  selectedProviderId.value = value.edges?.scm_provider?.id ?? value.scm_provider_id ?? 0
  emit('repoUpdated', value)
}

async function saveBinding() {
  bindingSaving.value = true
  bindingMessage.value = ''
  bindingError.value = ''
  try {
    await updateRepo(props.repoId, selectedProviderId.value
      ? { scm_provider_id: selectedProviderId.value }
      : { clear_scm_provider: true } as any)
    await refreshRepo()
    bindingMessage.value = t('repoDetail.bindingSaved')
  } catch (error: any) {
    bindingError.value = error?.response?.data?.message || error?.message || t('repoDetail.bindingSaveFailed')
  } finally { bindingSaving.value = false }
}

function terminal(job: PRSyncJob) {
  return ['completed', 'failed', 'cancelled'].includes(job.status)
}
async function recoverSyncJob() {
  try {
    const response = await getLatestPRSyncJob(props.repoId)
    const job = response.data.data
    if (!job) return
    syncJob.value = job
    if (!terminal(job)) {
      syncing.value = true
      schedulePoll(job.id)
    }
  } catch (error: any) {
    syncError.value = error?.response?.data?.message || t('repoDetail.syncProgressFailed')
  }
}
function schedulePoll(id: number) {
  if (syncPollTimer != null) window.clearTimeout(syncPollTimer)
  syncPollTimer = window.setTimeout(() => void pollSyncJob(id), 1500)
}
async function startSync() {
  syncing.value = true
  syncMessage.value = ''
  syncError.value = ''
  try {
    const response = await syncPRs(props.repoId)
    const id = response.data.data?.job_id
    if (!id) throw new Error(t('repoDetail.prSyncJobMissing'))
    await pollSyncJob(id)
  } catch (error: any) {
    syncing.value = false
    syncError.value = error?.response?.data?.message || error?.message || t('repoDetail.syncFailed')
  }
}
async function pollSyncJob(id: number) {
  try {
    const response = await getPRSyncJob(id)
    const job = response.data.data
    if (!job) throw new Error(t('repoDetail.syncProgressFailed'))
    syncJob.value = job
    if (terminal(job)) {
      syncing.value = false
      if (job.status === 'completed') {
        syncMessage.value = t('repoDetail.syncCompleted', { created: job.created_prs, changed: job.changed_prs, unchanged: job.unchanged_prs })
        await refreshRepo()
      } else {
        syncError.value = job.last_error || t('repoDetail.syncStatusSummary', { status: job.status })
      }
      return
    }
    schedulePoll(job.id)
  } catch (error: any) {
    syncing.value = false
    syncError.value = error?.response?.data?.message || error?.message || t('repoDetail.syncProgressFailed')
  }
}

async function repair() {
  webhookRepairing.value = true
  webhookMessage.value = ''
  webhookError.value = ''
  try {
    await repairWebhook(props.repoId, { force: webhookForce.value })
    await refreshRepo()
    webhookMessage.value = t('repoDetail.webhookRepairComplete')
  } catch (error: any) {
    webhookError.value = error?.response?.data?.message || error?.message || t('repoDetail.webhookRepairFailed')
  } finally { webhookRepairing.value = false }
}

function formatDate(value?: string | null) {
  return value ? new Date(value).toLocaleString() : '—'
}
</script>

<template>
  <section class="space-y-5" data-testid="repo-operations">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div><h2 class="text-lg font-semibold text-slate-950">{{ t('activity.operationsTab') }}</h2><p class="mt-1 text-sm text-slate-600">{{ t('repoDetail.healthHelp') }}</p></div>
      <ElButton data-testid="repo-sync-prs" :loading="syncing" :disabled="isUnbound" @click="startSync">{{ t('repoDetail.syncPrs') }}</ElButton>
    </div>

    <ElAlert v-if="syncMessage" type="success" :closable="false" show-icon :title="syncMessage" />
    <ElAlert v-if="syncError" type="error" :closable="false" show-icon :title="syncError" />
    <ElAlert v-if="syncJob && syncing" type="info" :closable="false" show-icon :title="t('repoDetail.syncStatusSummary', { status: syncJob.status })" />

    <div class="rounded-xl border border-slate-200 bg-white p-5">
      <h3 class="font-semibold text-slate-950">{{ t('repoDetail.healthHelp') }}</h3>
      <div data-testid="repo-detail-health-metrics" class="mt-4 grid grid-cols-2 gap-3 xl:grid-cols-4">
        <div class="rounded-lg bg-slate-50 p-3"><p class="text-xs uppercase tracking-wide text-slate-500">{{ t('repos.defaultBranch') }}</p><p class="mt-1 font-medium text-slate-900">{{ repo.default_branch || '—' }}</p></div>
        <div class="rounded-lg bg-slate-50 p-3"><p class="text-xs uppercase tracking-wide text-slate-500">{{ t('repoDetail.repositoryStatus') }}</p><p class="mt-1 font-medium text-slate-900">{{ repositoryStatusLabel(repo.status, t) }}</p></div>
        <div class="rounded-lg bg-slate-50 p-3"><p class="text-xs uppercase tracking-wide text-slate-500">{{ t('repoDetail.created') }}</p><p class="mt-1 font-medium text-slate-900">{{ formatDate(repo.created_at) }}</p></div>
        <div class="rounded-lg bg-slate-50 p-3"><p class="text-xs uppercase tracking-wide text-slate-500">Webhook</p><p class="mt-1 font-medium text-slate-900">{{ repo.webhook_id || '—' }}</p></div>
      </div>
    </div>

    <div class="rounded-xl border border-slate-200 bg-white p-5">
      <h3 class="font-semibold text-slate-950">{{ t('repoDetail.scmBinding') }}</h3>
      <p class="mt-1 text-sm text-slate-600">{{ isUnbound ? t('repoDetail.bindingUnboundHelp') : t('repoDetail.bindingBoundHelp') }}</p>
      <div class="mt-4 flex flex-col gap-3 sm:flex-row sm:items-center">
        <ElSelect v-model="selectedProviderId" class="w-full sm:max-w-sm" :loading="providersLoading" data-testid="repo-provider-select">
          <ElOption :label="t('repoDetail.unbound')" :value="0" />
          <ElOption v-for="provider in providers" :key="provider.id" :label="provider.name" :value="provider.id" />
        </ElSelect>
        <ElButton type="primary" class="!ml-0" data-testid="repo-save-binding" :loading="bindingSaving" @click="saveBinding">{{ t('repoDetail.saveBinding') }}</ElButton>
      </div>
      <ElAlert v-if="providersError || bindingError" class="mt-4" type="error" :closable="false" :title="providersError || bindingError" />
      <ElAlert v-if="bindingMessage" class="mt-4" type="success" :closable="false" :title="bindingMessage" />
    </div>

    <div class="rounded-xl border border-slate-200 bg-white p-5">
      <h3 class="font-semibold text-slate-950">Webhook</h3>
      <p class="mt-1 text-sm text-slate-600">{{ t('repoDetail.webhookRepairNeeded') }}</p>
      <div class="mt-4 flex flex-col gap-3 sm:flex-row sm:items-center">
        <ElCheckbox v-model="webhookForce">{{ t('repoDetail.forceReplaceWebhook') }}</ElCheckbox>
        <ElButton class="!ml-0" :disabled="!canRepairWebhook" :loading="webhookRepairing" @click="repair">{{ t('repoDetail.repairWebhook') }}</ElButton>
      </div>
      <ElAlert v-if="webhookError" class="mt-4" type="error" :closable="false" :title="webhookError" />
      <ElAlert v-if="webhookMessage" class="mt-4" type="success" :closable="false" :title="webhookMessage" />
    </div>
  </section>
</template>
