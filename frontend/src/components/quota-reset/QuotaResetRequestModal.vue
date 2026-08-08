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
  <ElDialog
    :model-value="props.open"
    :teleported="false"
    width="min(36rem, calc(100vw - 2rem))"
    :title="t('quotaReset.requestReset')"
    @close="emit('close')"
  >
      <p class="text-sm text-slate-600">{{ t('quotaReset.modalHelp') }}</p>

      <div class="mt-5 space-y-4">
        <label class="block">
          <span class="text-sm font-medium text-slate-700">{{ t('quotaReset.subscriptionGroup') }}</span>
          <ElSelect
            v-model="selectedGroupID"
            class="mt-1 w-full"
            :teleported="false"
            :disabled="props.submitting"
          >
            <ElOption
              v-for="group in props.groups"
              :key="group.group_id"
              :value="group.group_id"
              :label="`${group.group_name} · ${group.platform}`"
            />
          </ElSelect>
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
          <ElInput
            v-model="reason"
            class="mt-1 w-full"
            type="textarea"
            :rows="4"
            :placeholder="t('quotaReset.reasonPlaceholder')"
            :disabled="props.submitting"
          />
        </label>

        <ElAlert v-if="error" type="error" :closable="false" :title="error" />
      </div>

      <template #footer>
        <ElButton
          :disabled="props.submitting"
          @click="emit('close')"
        >
          {{ t('settings.cancel') }}
        </ElButton>
        <ElButton
          data-testid="quota-reset-submit"
          type="primary"
          :loading="props.submitting"
          :disabled="props.submitting || props.groups.length === 0"
          @click="submit"
        >
          {{ props.submitting ? t('settings.saving') : t('quotaReset.submitRequest') }}
        </ElButton>
      </template>
  </ElDialog>
</template>
