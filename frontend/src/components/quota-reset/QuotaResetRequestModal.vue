<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from '@/i18n'
import type { QuotaResetOptionGroup } from '@/types'

const props = defineProps<{
  open: boolean
  groups: QuotaResetOptionGroup[]
  submitting?: boolean
}>()

const emit = defineEmits<{
  close: []
  submit: [{ group_id: string; reason: string }]
}>()

const { t } = useI18n()
const selectedGroupID = ref('')
const reason = ref('')
const error = ref('')

const selectedGroup = computed(() => props.groups.find((group) => group.group_id === selectedGroupID.value) ?? null)

watch(
  () => [props.open, props.groups] as const,
  () => {
    if (!props.open) return
    selectedGroupID.value = props.groups[0]?.group_id ?? ''
    reason.value = ''
    error.value = ''
  },
  { immediate: true },
)

function submit() {
  error.value = ''
  if (!selectedGroupID.value) {
    error.value = t('quotaReset.groupRequired')
    return
  }
  const trimmedReason = reason.value.trim()
  if (!trimmedReason) {
    error.value = t('quotaReset.reasonRequired')
    return
  }
  emit('submit', { group_id: selectedGroupID.value, reason: trimmedReason })
}
</script>

<template>
  <div
    v-if="props.open"
    class="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/50 p-4"
    role="dialog"
    aria-modal="true"
    :aria-label="t('quotaReset.requestReset')"
  >
    <div class="w-full max-w-xl rounded-lg bg-white p-5 shadow-xl">
      <div class="flex items-start justify-between gap-4">
        <div>
          <h2 class="text-lg font-semibold text-slate-950">{{ t('quotaReset.requestReset') }}</h2>
          <p class="mt-1 text-sm text-slate-600">{{ t('quotaReset.modalHelp') }}</p>
        </div>
        <button type="button" class="text-sm font-medium text-slate-500 hover:text-slate-900" @click="emit('close')">
          {{ t('app.close') }}
        </button>
      </div>

      <div class="mt-5 space-y-4">
        <label class="block">
          <span class="text-sm font-medium text-slate-700">{{ t('quotaReset.subscriptionGroup') }}</span>
          <select
            v-model="selectedGroupID"
            class="mt-1 w-full rounded-md border border-slate-300 px-3 py-2 text-sm text-slate-900"
            :disabled="props.submitting"
          >
            <option v-for="group in props.groups" :key="group.group_id" :value="group.group_id">
              {{ group.group_name }} · {{ group.platform }}
            </option>
          </select>
        </label>

        <div v-if="selectedGroup" class="rounded-md border border-slate-200 bg-slate-50 px-3 py-2 text-xs text-slate-600">
          <span>{{ t('quotaReset.currentUsage') }}</span>
          <span class="ml-2 font-medium text-slate-900">
            {{ selectedGroup.daily_usage_usd.toFixed(2) }} / {{ selectedGroup.weekly_usage_usd.toFixed(2) }} /
            {{ selectedGroup.monthly_usage_usd.toFixed(2) }}
          </span>
        </div>

        <label class="block">
          <span class="text-sm font-medium text-slate-700">{{ t('quotaReset.reason') }}</span>
          <textarea
            v-model="reason"
            class="mt-1 min-h-28 w-full rounded-md border border-slate-300 px-3 py-2 text-sm text-slate-900"
            :placeholder="t('quotaReset.reasonPlaceholder')"
            :disabled="props.submitting"
          />
        </label>

        <p v-if="error" class="text-sm font-medium text-red-600">{{ error }}</p>
      </div>

      <div class="mt-5 flex justify-end gap-2">
        <button
          type="button"
          class="rounded-md border border-slate-300 px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
          :disabled="props.submitting"
          @click="emit('close')"
        >
          {{ t('settings.cancel') }}
        </button>
        <button
          type="button"
          data-testid="quota-reset-submit"
          class="rounded-md bg-cyan-700 px-3 py-2 text-sm font-medium text-white hover:bg-cyan-800 disabled:opacity-60"
          :disabled="props.submitting || props.groups.length === 0"
          @click="submit"
        >
          {{ props.submitting ? t('settings.saving') : t('quotaReset.submitRequest') }}
        </button>
      </div>
    </div>
  </div>
</template>
