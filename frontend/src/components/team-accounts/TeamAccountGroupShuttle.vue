<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from '@/i18n'
import { teamAccountsMessages } from '@/locales/teamAccounts'
import { createFeatureTranslator } from '@/utils/featureI18n'
import type { TeamAccountGroupOption } from '@/types'

const props = withDefaults(defineProps<{
  modelValue: number[]
  groups: TeamAccountGroupOption[]
  defaultGroupIds?: number[]
  grantableGroupIds?: number[]
  disabled?: boolean
}>(), {
  defaultGroupIds: () => [],
  grantableGroupIds: () => [],
  disabled: false,
})

const emit = defineEmits<{ 'update:modelValue': [value: number[]] }>()

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'teamAccounts.', teamAccountsMessages)

const grantable = computed(() => new Set(props.grantableGroupIds))
const defaults = computed(() => new Set(props.defaultGroupIds))

// Top list: groups the creator may grant but the account does not have yet.
const available = computed(() => {
  const chosen = new Set(props.modelValue)
  return props.groups.filter((group) => grantable.value.has(group.id) && !chosen.has(group.id))
})

// Bottom list: everything the account will have. Defaults first, then extras.
const selected = computed(() => {
  const chosen = new Set(props.modelValue)
  return props.groups
    .filter((group) => chosen.has(group.id))
    .sort((a, b) => Number(defaults.value.has(b.id)) - Number(defaults.value.has(a.id)))
})

function add(group: TeamAccountGroupOption) {
  if (props.disabled || !grantable.value.has(group.id)) return
  emit('update:modelValue', [...props.modelValue, group.id])
}

function remove(group: TeamAccountGroupOption) {
  if (props.disabled || defaults.value.has(group.id)) return
  emit('update:modelValue', props.modelValue.filter((id) => id !== group.id))
}
</script>

<template>
  <div class="space-y-2" data-testid="team-account-group-shuttle">
    <div class="rounded border border-slate-200">
      <div class="border-b border-slate-100 bg-slate-50 px-2 py-1 text-xs font-medium text-slate-500">
        {{ t('teamAccounts.groups.available') }}
      </div>
      <ul class="h-40 overflow-auto p-1">
        <li v-if="available.length === 0" class="px-2 py-2 text-xs text-slate-400">
          {{ t('teamAccounts.groups.noneAvailable') }}
        </li>
        <li
          v-for="group in available"
          :key="group.id"
          :data-testid="`group-available-${group.id}`"
          class="cursor-pointer rounded px-2 py-1 text-sm text-slate-700 hover:bg-blue-50"
          @click="add(group)"
        >
          <span class="block truncate">{{ group.name }} · {{ group.platform }}</span>
        </li>
      </ul>
    </div>

    <div class="rounded border border-slate-200">
      <div class="border-b border-slate-100 bg-slate-50 px-2 py-1 text-xs font-medium text-slate-500">
        {{ t('teamAccounts.groups.selected') }}
      </div>
      <ul class="h-40 overflow-auto p-1">
        <li v-if="selected.length === 0" class="px-2 py-2 text-xs text-slate-400">
          {{ t('teamAccounts.groups.noneSelected') }}
        </li>
        <li
          v-for="group in selected"
          :key="group.id"
          :data-testid="`group-selected-${group.id}`"
          class="flex items-center justify-between gap-2 rounded px-2 py-1 text-sm"
          :class="defaults.has(group.id) ? 'text-slate-500' : 'cursor-pointer text-slate-800 hover:bg-blue-50'"
          @click="remove(group)"
        >
          <span class="block min-w-0 truncate">{{ group.name }} · {{ group.platform }}</span>
          <span v-if="defaults.has(group.id)" class="shrink-0 rounded bg-slate-100 px-1 text-[11px] text-slate-500">
            {{ t('teamAccounts.groups.defaultTag') }}
          </span>
        </li>
      </ul>
    </div>
  </div>
</template>
