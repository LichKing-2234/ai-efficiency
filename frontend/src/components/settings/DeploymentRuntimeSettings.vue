<script setup lang="ts">
import { useI18n } from '@/i18n'
import type { DeploymentStatus } from '@/types'

type DeploymentAction = 'apply' | 'rollback' | 'restart'

const { t } = useI18n()

defineProps<{
  deployment: DeploymentStatus | null
  deploymentLoading: boolean
  deploymentActionLoading: boolean
  deploymentMessage: string
  deploymentMessageKind: 'success' | 'error' | ''
  deploymentConfirmAction: DeploymentAction | null
}>()

defineEmits<{
  (e: 'check-updates'): void
  (e: 'request-action', action: DeploymentAction): void
  (e: 'confirm-action'): void
  (e: 'cancel-action'): void
}>()

function deploymentConfirmTitle(action: DeploymentAction | null) {
  switch (action) {
    case 'apply':
      return t('settings.confirmApplyUpdate')
    case 'rollback':
      return t('settings.confirmRollback')
    default:
      return t('settings.confirmRestartService')
  }
}

function deploymentConfirmBody(action: DeploymentAction | null) {
  switch (action) {
    case 'apply':
      return t('settings.confirmApplyBody')
    case 'rollback':
      return t('settings.confirmRollbackBody')
    default:
      return t('settings.confirmRestartBody')
  }
}
</script>

<template>
  <div class="space-y-4">
    <h2 class="text-xl font-bold text-gray-900">{{ t('settings.deploymentRuntime') }}</h2>
    <div class="overflow-hidden rounded-lg bg-white shadow p-6">
      <div v-if="deploymentLoading" class="text-sm text-gray-500">{{ t('settings.deploymentLoading') }}</div>

      <div v-else class="space-y-4">
        <div class="flex items-center justify-between">
          <div>
            <div class="text-sm text-gray-500">{{ t('settings.currentVersion') }}</div>
            <div class="text-lg font-semibold text-gray-900">{{ deployment?.version.version || t('settings.unknown') }}</div>
          </div>
          <span class="inline-flex rounded-full bg-gray-100 px-2 py-1 text-xs font-semibold uppercase tracking-wide text-gray-700">
            {{ deployment?.mode || t('settings.unknown') }}
          </span>
        </div>

        <div class="grid gap-3 md:grid-cols-2">
          <div class="rounded-md bg-gray-50 p-3">
            <div class="text-xs uppercase tracking-wide text-gray-500">{{ t('settings.commit') }}</div>
            <div class="mt-1 font-mono text-sm text-gray-700">{{ deployment?.version.commit || t('settings.unknown') }}</div>
          </div>
          <div class="rounded-md bg-gray-50 p-3">
            <div class="text-xs uppercase tracking-wide text-gray-500">{{ t('settings.updatePhase') }}</div>
            <div class="mt-1 text-sm text-gray-700">{{ deployment?.update_status.phase || t('settings.unknown') }}</div>
          </div>
        </div>

        <div v-if="deployment?.latest_release" class="rounded-md bg-blue-50 p-3 text-sm text-blue-800">
          {{ t('settings.latestRelease') }}: {{ deployment.latest_release.version }}
        </div>

        <div
          v-if="deploymentMessage"
          class="rounded-md p-3 text-sm"
          :class="deploymentMessageKind === 'error' ? 'bg-red-50 text-red-700' : 'bg-green-50 text-green-700'"
        >
          {{ deploymentMessage }}
        </div>

        <div v-if="deploymentConfirmAction" class="rounded-lg border border-amber-200 bg-amber-50 p-4">
          <div class="text-sm font-semibold text-amber-950">
            {{ deploymentConfirmTitle(deploymentConfirmAction) }}
          </div>
          <p class="mt-1 text-sm text-amber-800">
            {{ deploymentConfirmBody(deploymentConfirmAction) }}
          </p>
          <div class="mt-3 flex flex-wrap justify-end gap-2">
            <button
              class="rounded-md border border-amber-300 bg-white px-3 py-2 text-sm font-medium text-amber-900 hover:bg-amber-100 disabled:opacity-50"
              type="button"
              :disabled="deploymentActionLoading"
              @click="$emit('confirm-action')"
            >
              {{ deploymentConfirmTitle(deploymentConfirmAction) }}
            </button>
            <button
              class="rounded-md border border-transparent px-3 py-2 text-sm text-amber-800 hover:bg-amber-100"
              type="button"
              :disabled="deploymentActionLoading"
              @click="$emit('cancel-action')"
            >
              {{ t('settings.cancel') }}
            </button>
          </div>
        </div>

        <div class="flex flex-wrap justify-end gap-3">
          <button @click="$emit('check-updates')" :disabled="deploymentActionLoading" class="rounded-md border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50">
            {{ deploymentActionLoading ? t('settings.working') : t('settings.checkUpdates') }}
          </button>
          <button @click="$emit('request-action', 'apply')" :disabled="deploymentActionLoading" class="rounded-md bg-emerald-600 px-4 py-2 text-sm font-medium text-white hover:bg-emerald-700 disabled:opacity-50">
            {{ t('settings.applyUpdate') }}
          </button>
          <button @click="$emit('request-action', 'rollback')" :disabled="deploymentActionLoading" class="rounded-md bg-amber-600 px-4 py-2 text-sm font-medium text-white hover:bg-amber-700 disabled:opacity-50">
            {{ t('settings.rollback') }}
          </button>
          <button @click="$emit('request-action', 'restart')" :disabled="deploymentActionLoading" class="rounded-md bg-slate-700 px-4 py-2 text-sm font-medium text-white hover:bg-slate-800 disabled:opacity-50">
            {{ t('settings.restartService') }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
