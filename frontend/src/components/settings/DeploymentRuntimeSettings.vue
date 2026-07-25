<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { checkSystemUpdate, getSystemVersion } from '@/api/system'
import { useI18n } from '@/i18n'
import type { SystemVersionStatus } from '@/types'

const { t } = useI18n()
const systemVersion = ref<SystemVersionStatus | null>(null)
const systemVersionLoading = ref(false)
const systemVersionChecking = ref(false)
const systemVersionMessage = ref('')
const systemVersionMessageKind = ref<'success' | 'error' | ''>('')

onMounted(fetchSystemVersion)

const checkDisabled = computed(() => (
  systemVersionChecking.value ||
  systemVersionLoading.value ||
  !systemVersion.value ||
  systemVersion.value.check_enabled === false
))

function formatBuildTime(date?: string) {
  if (!date) return t('settings.unknown')
  const parsed = new Date(date)
  if (Number.isNaN(parsed.getTime())) return date
  return parsed.toLocaleString()
}

async function fetchSystemVersion() {
  systemVersionLoading.value = true
  try {
    const response = await getSystemVersion()
    systemVersion.value = response.data.data ?? null
  } catch {
    systemVersion.value = null
  } finally {
    systemVersionLoading.value = false
  }
}

function setSystemVersionMessage(kind: 'success' | 'error', message: string) {
  systemVersionMessageKind.value = kind
  systemVersionMessage.value = message
}

async function handleCheckUpdates() {
  if (systemVersion.value?.check_enabled === false) {
    setSystemVersionMessage('error', t('settings.versionCheckUnavailable'))
    return
  }

  systemVersionChecking.value = true
  systemVersionMessage.value = ''
  systemVersionMessageKind.value = ''
  try {
    const response = await checkSystemUpdate()
    systemVersion.value = response.data.data ?? null
    if (systemVersion.value?.update_available) {
      setSystemVersionMessage('success', t('settings.updateAvailable'))
    } else if (systemVersion.value?.check_error) {
      setSystemVersionMessage('error', systemVersion.value.check_error)
    } else if (systemVersion.value?.checked) {
      setSystemVersionMessage('success', t('settings.alreadyCurrent'))
    } else if (systemVersion.value?.check_enabled === false) {
      setSystemVersionMessage('error', t('settings.versionCheckUnavailable'))
    }
  } catch (error: any) {
    setSystemVersionMessage('error', error.response?.data?.message || t('settings.checkUpdatesFailed'))
  } finally {
    systemVersionChecking.value = false
  }
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
          <button @click="handleCheckUpdates" :disabled="checkDisabled" class="rounded-md border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50">
            {{ systemVersionChecking ? t('settings.working') : t('settings.checkUpdates') }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
