<script setup lang="ts">
import { useI18n } from '@/i18n'
import type { TeamUsageSubject } from '@/types'

const props = defineProps<{
  modelValue: string
  subjects: TeamUsageSubject[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
  select: [subject: TeamUsageSubject]
}>()

const { t } = useI18n()

function subjectValue(subject: TeamUsageSubject) {
  if (subject.subject_type === 'self') return `self:${subject.user_id}`
  const id = subject.user_id > 0 ? String(subject.user_id) : `directory:${subject.directory_member_external_id || subject.email}`
  return `${subject.subject_type}:${id}`
}

function subjectLabel(subject: TeamUsageSubject) {
  return subject.subject_type === 'self' ? t('teamUsage.myUsage') : subject.display_name
}

function onChange(value: string) {
  emit('update:modelValue', value)
  const selected = props.subjects.find((subject) => subjectValue(subject) === value)
  if (selected) {
    emit('select', selected)
  }
}
</script>

<template>
  <ElSelect
    class="w-full sm:w-72"
    :model-value="modelValue"
    data-testid="usage-subject-selector"
    @change="onChange"
  >
    <ElOption
      v-for="subject in props.subjects"
      :key="subjectValue(subject)"
      :label="subjectLabel(subject)"
      :value="subjectValue(subject)"
      :disabled="!subject.selectable"
    />
  </ElSelect>
</template>
