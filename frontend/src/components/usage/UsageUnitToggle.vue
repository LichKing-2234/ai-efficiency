<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from '@/i18n'
import type { UsageUnit } from '@/composables/useUsageUnit'

const props = defineProps<{
  modelValue: UsageUnit
}>()

const emit = defineEmits<{
  'update:modelValue': [value: UsageUnit]
}>()

const { t } = useI18n()

const options = computed(() => [
  { label: t('usageDashboard.unitToken'), value: 'token' },
  { label: t('usageDashboard.unitCredits'), value: 'credits' },
])

function change(value: string | number | boolean) {
  if (value === 'token' || value === 'credits') emit('update:modelValue', value)
}
</script>

<template>
  <ElSegmented
    :model-value="props.modelValue"
    :options="options"
    class="shrink-0"
    @change="change"
  />
</template>
