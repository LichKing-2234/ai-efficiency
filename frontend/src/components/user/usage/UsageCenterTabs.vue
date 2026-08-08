<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from '@/i18n'
import { useRouter } from 'vue-router'

const props = defineProps<{
  active: 'personal' | 'team' | 'quota-reset'
  showTeam?: boolean
  showQuotaReset?: boolean
}>()

const { t } = useI18n()
const router = useRouter()
const options = computed(() => {
  const items = [{ label: t('usageDashboard.personalTab'), value: 'personal' }]
  if (props.showTeam || props.active === 'team') items.push({ label: t('usageDashboard.teamTab'), value: 'team' })
  if (props.showQuotaReset || props.active === 'quota-reset') items.push({ label: t('quotaReset.tab'), value: 'quota-reset' })
  return items
})

function selectTab(name: string | number) {
  if (name === 'team') void router.push('/usage/team')
  else if (name === 'quota-reset') void router.push('/usage/quota-reset')
  else void router.push('/usage')
}
</script>

<template>
  <div class="max-w-full overflow-x-auto" data-testid="usage-center-tabs">
  <ElSegmented
    :model-value="active"
    :options="options"
    class="min-w-max"
    @change="selectTab"
  />
  </div>
</template>
