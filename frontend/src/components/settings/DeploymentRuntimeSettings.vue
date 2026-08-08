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
    <ElCard shadow="never">
      <div v-if="systemVersionLoading" class="text-sm text-gray-500">{{ t('settings.versionLoading') }}</div>

      <div v-else class="space-y-4">
        <div class="flex items-center justify-between">
          <div>
            <div class="text-sm text-gray-500">{{ t('settings.currentVersion') }}</div>
            <div class="text-lg font-semibold text-gray-900">{{ systemVersion?.version.version || t('settings.unknown') }}</div>
          </div>
          <ElTag type="info">
            {{ systemVersion?.check_enabled === false ? t('settings.versionCheckUnavailable') : t('settings.checkUpdates') }}
          </ElTag>
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

        <ElAlert v-if="systemVersion?.latest_release" type="info" :closable="false" :title="`${t('settings.latestRelease')}: ${systemVersion.latest_release.version}`" />

        <ElAlert v-if="systemVersion?.check_enabled === false" type="warning" :closable="false" :title="t('settings.versionCheckUnavailable')" />

        <ElAlert
          v-if="systemVersionMessage"
          :type="systemVersionMessageKind === 'error' ? 'error' : 'success'"
          :title="systemVersionMessage"
          :closable="false"
        />

        <div class="flex flex-wrap justify-end gap-3">
          <ElButton :loading="systemVersionChecking" :disabled="checkDisabled" @click="handleCheckUpdates">
            {{ t('settings.checkUpdates') }}
          </ElButton>
        </div>
      </div>
    </ElCard>
  </div>
</template>
