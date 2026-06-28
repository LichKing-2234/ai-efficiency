<script setup lang="ts">
import type { TeamUsageSubject } from '@/types'

const props = defineProps<{
  modelValue: string
  subjects: TeamUsageSubject[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
  select: [subject: TeamUsageSubject]
}>()

function subjectValue(subject: TeamUsageSubject) {
  return `${subject.subject_type}:${subject.user_id}`
}

function subjectLabel(subject: TeamUsageSubject) {
  return subject.subject_type === 'self' ? 'My Usage' : subject.display_name
}

function onChange(event: Event) {
  const value = (event.target as HTMLSelectElement).value
  emit('update:modelValue', value)
  const selected = props.subjects.find((subject) => subjectValue(subject) === value)
  if (selected) {
    emit('select', selected)
  }
}
</script>

<template>
  <select
    class="h-9 rounded-md border border-slate-300 bg-white px-3 text-sm text-slate-900"
    :value="modelValue"
    data-testid="usage-subject-selector"
    @change="onChange"
  >
    <option
      v-for="subject in props.subjects"
      :key="subjectValue(subject)"
      :value="subjectValue(subject)"
      :disabled="!subject.selectable"
    >
      {{ subjectLabel(subject) }}
    </option>
  </select>
</template>
