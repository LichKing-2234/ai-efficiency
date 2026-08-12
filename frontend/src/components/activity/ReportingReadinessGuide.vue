<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { getReportingReadiness } from '@/api/attribution'
import { useI18n } from '@/i18n'
import { reportingText, type ReportingTextKey } from './reportingText'
import type { ReportingReadiness, ReportingReadinessState } from '@/types/reporting'
import {
  buildDoctorCommand,
  buildHooksStatusUploadsCommand,
  buildLoginCommand,
  buildPreferredInstallCommand,
  buildRepoInitCommand,
  buildSyncCommand,
  detectInstallPlatform,
} from '@/utils/userSetupReview'

const props = withDefaults(defineProps<{
  variant?: 'compact' | 'full'
  readinessAvailable?: boolean
}>(), {
  variant: 'compact',
  readinessAvailable: true,
})

const { locale } = useI18n()
const readiness = ref<ReportingReadiness | null>(null)
const loading = ref(props.readinessAvailable)
const failed = ref(false)
const copiedCommand = ref('')
const copyFailed = ref(false)
let pollingTimer: number | undefined
let requestInFlight = false

const txt = (key: ReportingTextKey) => reportingText(locale.value, key)
const normalCommands = computed(() => [
  { key: 'install', label: txt('install'), value: buildPreferredInstallCommand(window.location.origin, detectInstallPlatform()) },
  { key: 'login', label: txt('login'), value: buildLoginCommand(window.location.origin) },
  { key: 'discover', label: txt('discover'), value: 'ae-cli discover' },
])
const advancedCommands = computed(() => [
  { key: 'status', label: txt('statusCommand'), value: 'ae-cli attribution status' },
  { key: 'doctor', label: txt('doctorCommand'), value: buildDoctorCommand() },
  { key: 'sync', label: txt('syncCommand'), value: buildSyncCommand() },
  { key: 'uploads', label: txt('uploadsCommand'), value: buildHooksStatusUploadsCommand() },
  { key: 'repo', label: txt('repoCommand'), value: buildRepoInitCommand(), help: txt('repoFallback') },
])
const stateTitle = computed(() => txt(`${readiness.value?.state ?? 'not_enrolled'}Title` as ReportingTextKey))
const stateDescription = computed(() => txt(`${readiness.value?.state ?? 'not_enrolled'}Description` as ReportingTextKey))
const latestAccepted = computed(() => {
  if (!readiness.value?.latest_accepted_at) return ''
  const value = new Date(readiness.value.latest_accepted_at)
  if (Number.isNaN(value.getTime())) return ''
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(value)
})

function clearPolling() {
  if (pollingTimer !== undefined) window.clearTimeout(pollingTimer)
  pollingTimer = undefined
}

function shouldPoll() {
  return props.readinessAvailable && readiness.value?.state !== 'active' && (readiness.value?.state === 'waiting_for_data' || failed.value)
}

function schedulePolling() {
  clearPolling()
  if (!shouldPoll() || document.visibilityState === 'hidden') return
  pollingTimer = window.setTimeout(() => void loadReadiness(), 30_000)
}

async function loadReadiness() {
  if (!props.readinessAvailable || requestInFlight) return
  requestInFlight = true
  failed.value = false
  try {
    const response = await getReportingReadiness()
    if (!response.data.data) throw new Error('missing reporting readiness')
    readiness.value = response.data.data
  } catch {
    failed.value = true
  } finally {
    requestInFlight = false
    loading.value = false
    schedulePolling()
  }
}

function onVisibilityChange() {
  if (document.visibilityState === 'hidden') {
    clearPolling()
  } else if (shouldPoll()) {
    void loadReadiness()
  }
}

function alertType(state: ReportingReadinessState): 'success' | 'warning' | 'info' {
  if (state === 'active') return 'success'
  if (state === 'waiting_for_data') return 'info'
  return 'warning'
}

async function copyCommand(key: string, value: string) {
  copyFailed.value = false
  try {
    await navigator.clipboard.writeText(value)
    copiedCommand.value = key
  } catch {
    copyFailed.value = true
  }
}

onMounted(() => {
  document.addEventListener('visibilitychange', onVisibilityChange)
  if (props.readinessAvailable) void loadReadiness()
})

onBeforeUnmount(() => {
  clearPolling()
  document.removeEventListener('visibilitychange', onVisibilityChange)
})
</script>

<template>
  <section
    :data-testid="variant === 'full' ? 'reporting-full-guide' : 'reporting-compact-guide'"
    :class="variant === 'full' ? 'w-full rounded-xl border border-slate-200 bg-white p-4 sm:p-6' : 'min-w-0'"
  >
    <header v-if="variant === 'full'">
      <p class="text-xs font-semibold uppercase tracking-wide text-cyan-700">{{ txt('eyebrow') }}</p>
      <h2 class="mt-1 text-xl font-semibold text-slate-950">{{ txt('title') }}</h2>
      <p class="mt-1 max-w-3xl text-sm leading-6 text-slate-600">{{ txt('fullDescription') }}</p>
    </header>

    <div v-if="readinessAvailable" :class="variant === 'full' ? 'mt-5' : ''" aria-live="polite" aria-atomic="false">
      <p v-if="loading && !readiness" class="text-sm text-slate-500">{{ txt('loading') }}</p>
      <ElAlert
        v-if="failed"
        type="error"
        :closable="false"
        show-icon
        :title="txt('loadFailed')"
      >
        <template #default>
          <ElButton class="mt-2" size="small" @click="loadReadiness">{{ txt('retry') }}</ElButton>
        </template>
      </ElAlert>

      <template v-if="readiness">
        <div
          v-if="readiness.state === 'active'"
          data-testid="reporting-active-state"
          class="flex min-w-0 items-start gap-3 rounded-lg border border-emerald-200 bg-emerald-50 px-4 py-3 text-emerald-900"
        >
          <span aria-hidden="true" class="mt-0.5 shrink-0 text-emerald-700">●</span>
          <div class="min-w-0">
            <p class="text-sm font-semibold">{{ stateTitle }}</p>
            <p class="mt-0.5 text-sm leading-5 text-emerald-900">{{ stateDescription }}</p>
            <p v-if="latestAccepted" class="mt-1 text-xs text-emerald-700">{{ txt('latestAccepted') }} · {{ latestAccepted }}</p>
          </div>
        </div>
        <ElAlert
          v-else
          :type="alertType(readiness.state)"
          :closable="false"
          show-icon
          :title="stateTitle"
          :description="stateDescription"
        />
        <ElButton
          v-if="variant === 'compact' && readiness.state !== 'active'"
          class="mt-3"
          tag="a"
          href="/user"
          type="primary"
          plain
        >
          {{ txt('openSetup') }}
        </ElButton>
      </template>
    </div>

    <template v-if="variant === 'full'">
      <section data-testid="reporting-normal-commands" class="mt-6">
        <h3 class="text-base font-semibold text-slate-900">{{ txt('normalTitle') }}</h3>
        <p class="mt-1 text-sm leading-5 text-slate-600">{{ txt('normalHelp') }}</p>
        <ol class="mt-4 grid gap-3 lg:grid-cols-3">
          <li v-for="command in normalCommands" :key="command.key" class="min-w-0 rounded-lg border border-slate-200 p-3">
            <div class="flex items-center justify-between gap-3">
              <span class="text-xs font-semibold text-slate-600">{{ command.label }}</span>
              <ElButton link type="primary" @click="copyCommand(command.key, command.value)">
                {{ copiedCommand === command.key ? txt('copied') : txt('copy') }}
              </ElButton>
            </div>
            <pre class="mt-2 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 font-mono text-xs leading-5 text-green-300"><code>{{ command.value }}</code></pre>
          </li>
        </ol>
      </section>

      <ElAlert v-if="copyFailed" class="mt-4" type="error" :closable="false" :title="txt('copyFailed')" />

      <ElCollapse data-testid="reporting-advanced" class="mt-5 overflow-hidden rounded-lg border border-slate-200">
        <ElCollapseItem name="advanced-reporting">
          <template #title>
            <span data-testid="reporting-advanced-title" class="block w-full px-4 text-base font-semibold text-slate-900">{{ txt('advancedTitle') }}</span>
          </template>
          <div data-testid="reporting-advanced-content" class="px-4 pb-4">
            <p class="text-sm leading-5 text-slate-600">{{ txt('advancedHelp') }}</p>
            <div class="mt-4 grid gap-3 sm:grid-cols-2">
              <div v-for="command in advancedCommands" :key="command.key" class="min-w-0 rounded-lg border border-slate-200 p-3">
                <div class="flex items-start justify-between gap-3">
                  <div class="min-w-0">
                    <p class="text-sm font-medium text-slate-900">{{ command.label }}</p>
                    <p v-if="command.help" class="mt-1 text-xs leading-5 text-slate-600">{{ command.help }}</p>
                  </div>
                  <ElButton link type="primary" @click="copyCommand(command.key, command.value)">
                    {{ copiedCommand === command.key ? txt('copied') : txt('copy') }}
                  </ElButton>
                </div>
                <pre class="mt-2 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 font-mono text-xs leading-5 text-green-300"><code>{{ command.value }}</code></pre>
              </div>
            </div>
          </div>
        </ElCollapseItem>
      </ElCollapse>
    </template>
  </section>
</template>
