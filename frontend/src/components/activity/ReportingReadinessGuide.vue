<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { getReportingReadiness } from '@/api/attribution'
import { useI18n } from '@/i18n'
import type { ReportingReadiness, ReportingReadinessState } from '@/types/reporting'
import { buildLoginCommand, buildPreferredInstallCommand, detectInstallPlatform } from '@/utils/userSetupReview'

const props = withDefaults(defineProps<{ variant?: 'compact' | 'full' }>(), {
  variant: 'compact',
})

const { t } = useI18n()
const readiness = ref<ReportingReadiness | null>(null)
const loading = ref(true)
const failed = ref(false)
const currentOrigin = window.location.origin
const setupCommands = computed(() => [
  {
    key: 'install',
    label: t('reporting.installCli'),
    value: buildPreferredInstallCommand(currentOrigin, detectInstallPlatform()),
  },
  { key: 'login', label: t('reporting.login'), value: buildLoginCommand(currentOrigin) },
  { key: 'discover', label: t('reporting.discover'), value: 'ae-cli discover' },
])

const setupState = computed(() => {
  const state = readiness.value?.state
  return state === 'not_enrolled' || state === 'disabled' || state === 'revoked'
})

const titleKey = computed(() => {
  const state = readiness.value?.state ?? 'not_enrolled'
  return `reporting.${state}.title` as const
})

const descriptionKey = computed(() => {
  const state = readiness.value?.state ?? 'not_enrolled'
  return `reporting.${state}.description` as const
})

function alertType(state: ReportingReadinessState | undefined) {
  if (state === 'active') return 'success'
  if (state === 'waiting_for_data') return 'info'
  return 'warning'
}

onMounted(async () => {
  try {
    const response = await getReportingReadiness()
    if (!response.data.data) throw new Error('missing reporting readiness')
    readiness.value = response.data.data
  } catch {
    failed.value = true
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <section
    data-testid="reporting-readiness-guide"
    :class="variant === 'full' ? 'border-t border-slate-200 pt-6' : ''"
    aria-live="polite"
  >
    <template v-if="variant === 'full'">
      <div>
        <p class="text-xs font-semibold uppercase text-cyan-700">{{ t('reporting.eyebrow') }}</p>
        <h2 class="mt-1 text-xl font-semibold text-slate-950">{{ t('reporting.title') }}</h2>
        <p class="mt-1 max-w-3xl text-sm text-slate-600">{{ t('reporting.fullDescription') }}</p>
      </div>
      <div data-testid="reporting-setup-commands" class="mt-4">
        <h3 class="text-sm font-semibold text-slate-900">{{ t('reporting.setupTitle') }}</h3>
        <div class="mt-3 grid gap-3 lg:grid-cols-3">
          <div
            v-for="command in setupCommands"
            :key="command.key"
            class="min-w-0 rounded-md border border-slate-200 p-3"
          >
            <div class="text-xs font-medium text-slate-600">{{ command.label }}</div>
            <pre class="mt-2 overflow-x-auto rounded-md bg-slate-950 px-3 py-2 font-mono text-xs text-green-300">{{ command.value }}</pre>
          </div>
        </div>
      </div>
    </template>

    <div v-if="loading" class="mt-4 text-sm text-slate-500">{{ t('reporting.loading') }}</div>
    <ElAlert
      v-else-if="failed"
      class="mt-4"
      type="error"
      :closable="false"
      show-icon
      :title="t('reporting.loadFailed')"
    />

    <template v-else-if="readiness">
      <ElCollapse
        v-if="readiness.state === 'active' && variant === 'compact'"
        data-testid="reporting-readiness-active"
        class="rounded-md border border-emerald-200 bg-emerald-50 px-4"
      >
        <ElCollapseItem name="reporting-active">
          <template #title>
            <span class="text-sm font-medium text-emerald-950">{{ t(titleKey) }}</span>
          </template>
          <p class="text-sm text-emerald-900">{{ t(descriptionKey) }}</p>
        </ElCollapseItem>
      </ElCollapse>

      <div v-else class="mt-4">
        <ElAlert
          :type="alertType(readiness.state)"
          :closable="false"
          show-icon
          :title="t(titleKey)"
          :description="t(descriptionKey)"
        />
        <ElButton
          v-if="variant === 'compact' && (setupState || readiness.state === 'waiting_for_data')"
          class="mt-3"
          tag="a"
          href="/user"
          type="primary"
          plain
        >
          {{ t('reporting.openSetup') }}
        </ElButton>
      </div>

      <ElCollapse
        v-if="variant === 'full'"
        data-testid="reporting-diagnostics"
        class="mt-4 rounded-md border border-slate-200 px-4"
      >
        <ElCollapseItem name="reporting-diagnostics">
          <template #title>
            <span class="text-sm font-medium text-slate-900">{{ t('reporting.diagnostics') }}</span>
          </template>
          <p class="text-sm text-slate-600">{{ t('reporting.diagnosticsHelp') }}</p>
          <div class="mt-3 grid gap-3 sm:grid-cols-2">
            <div class="min-w-0 rounded-md bg-slate-950 px-3 py-2 font-mono text-xs text-green-300">ae-cli attribution status</div>
            <div class="min-w-0 rounded-md bg-slate-950 px-3 py-2 font-mono text-xs text-green-300">ae-cli doctor</div>
          </div>
        </ElCollapseItem>
      </ElCollapse>
    </template>
  </section>
</template>
