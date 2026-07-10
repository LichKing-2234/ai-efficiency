<script setup lang="ts">
import { useI18n } from '@/i18n'

defineProps<{
  active: 'personal' | 'team' | 'quota-reset'
  showTeam?: boolean
  showQuotaReset?: boolean
}>()

const { t } = useI18n()

function linkClass(active: boolean) {
  return [
    'inline-flex rounded-md px-3 py-2 text-sm font-medium transition-colors',
    active
      ? 'bg-cyan-700 text-white'
      : 'border border-slate-300 bg-white text-slate-700 hover:bg-slate-50',
  ]
}
</script>

<template>
  <div class="flex flex-wrap gap-2" data-testid="usage-center-tabs">
    <RouterLink to="/usage" :class="linkClass(active === 'personal')">
      {{ t('usageDashboard.personalTab') }}
    </RouterLink>
    <RouterLink v-if="showTeam || active === 'team'" to="/usage/team" :class="linkClass(active === 'team')">
      {{ t('usageDashboard.teamTab') }}
    </RouterLink>
    <RouterLink
      v-if="showQuotaReset || active === 'quota-reset'"
      to="/usage/quota-reset"
      :class="linkClass(active === 'quota-reset')"
    >
      {{ t('quotaReset.tab') }}
    </RouterLink>
  </div>
</template>
