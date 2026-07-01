<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from '@/i18n'
import type { SystemVersionStatus } from '@/types'

const { t } = useI18n()

const props = defineProps<{
  systemVersion: SystemVersionStatus | null
  systemVersionLoading: boolean
  systemVersionChecking: boolean
  systemVersionMessage: string
  systemVersionMessageKind: 'success' | 'error' | ''
}>()

defineEmits<{
  (e: 'check-updates'): void
}>()

const checkDisabled = computed(() => (
  props.systemVersionChecking ||
  props.systemVersionLoading ||
  !props.systemVersion ||
  props.systemVersion.check_enabled === false
))

function formatBuildTime(date?: string) {
  if (!date) return t('settings.unknown')
  const parsed = new Date(date)
  if (Number.isNaN(parsed.getTime())) return date
  return parsed.toLocaleString()
}
</script>

<template>
  <div class="space-y-4">
    <h2 class="text-xl font-bold text-gray-900">{{ t('settings.deploymentRuntime') }}</h2>
    <div class="overflow-hidden rounded-lg bg-white shadow p-6">
      <div v-if="systemVersionLoading" class="text-sm text-gray-500">{{ t('settings.versionLoading') }}</div>

      <div v-else class="space-y-4">
        <div class="flex items-center justify-between">
          <div>
            <div class="text-sm text-gray-500">{{ t('settings.currentVersion') }}</div>
            <div class="text-lg font-semibold text-gray-900">{{ systemVersion?.version.version || t('settings.unknown') }}</div>
          </div>
          <span class="inline-flex rounded-full bg-gray-100 px-2 py-1 text-xs font-semibold uppercase tracking-wide text-gray-700">
            {{ systemVersion?.check_enabled === false ? t('settings.versionCheckUnavailable') : t('settings.checkUpdates') }}
          </span>
        </div>

        <div class="grid gap-3 md:grid-cols-2">
          <div class="rounded-md bg-gray-50 p-3">
            <div class="text-xs uppercase tracking-wide text-gray-500">{{ t('settings.commit') }}</div>
            <div class="mt-1 font-mono text-sm text-gray-700">{{ systemVersion?.version.commit || t('settings.unknown') }}</div>
          </div>
          <div class="rounded-md bg-gray-50 p-3">
            <div class="text-xs uppercase tracking-wide text-gray-500">{{ t('settings.built') }}</div>
            <div class="mt-1 text-sm text-gray-700">{{ formatBuildTime(systemVersion?.version.build_time) }}</div>
          </div>
        </div>

        <div v-if="systemVersion?.latest_release" class="rounded-md bg-blue-50 p-3 text-sm text-blue-800">
          {{ t('settings.latestRelease') }}: {{ systemVersion.latest_release.version }}
        </div>

        <div v-if="systemVersion?.check_enabled === false" class="rounded-md bg-yellow-50 p-3 text-sm text-yellow-800">
          {{ t('settings.versionCheckUnavailable') }}
        </div>

        <div
          v-if="systemVersionMessage"
          class="rounded-md p-3 text-sm"
          :class="systemVersionMessageKind === 'error' ? 'bg-red-50 text-red-700' : 'bg-green-50 text-green-700'"
        >
          {{ systemVersionMessage }}
        </div>

        <div class="flex flex-wrap justify-end gap-3">
          <button @click="$emit('check-updates')" :disabled="checkDisabled" class="rounded-md border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50">
            {{ systemVersionChecking ? t('settings.working') : t('settings.checkUpdates') }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
