<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from '@/i18n'
import { useRouter } from 'vue-router'
import { createFeatureTranslator } from '@/utils/featureI18n'
import { ownerAccountsTabMessages } from '@/locales/ownerAccountsTab'

const props = defineProps<{
  active: 'personal' | 'team' | 'quota-reset' | 'accounts'
  showTeam?: boolean
  showQuotaReset?: boolean
  showAccounts?: boolean
}>()

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'ownerAccounts', ownerAccountsTabMessages)
const router = useRouter()
const options = computed(() => {
  const items = [{ label: baseT('usageDashboard.personalTab'), value: 'personal' }]
  if (props.showTeam || props.active === 'team') items.push({ label: baseT('usageDashboard.teamTab'), value: 'team' })
  if (props.showQuotaReset || props.active === 'quota-reset') items.push({ label: baseT('quotaReset.tab'), value: 'quota-reset' })
  if (props.showAccounts || props.active === 'accounts') items.push({ label: t('ownerAccounts.tab'), value: 'accounts' })
  return items
})

function selectTab(name: string | number) {
  if (name === 'team') void router.push('/usage/team')
  else if (name === 'quota-reset') void router.push('/usage/quota-reset')
  else if (name === 'accounts') void router.push('/usage/accounts')
  else void router.push('/usage')
}
</script>

<template>
  <ElSegmented
    data-testid="usage-center-tabs"
    :model-value="active"
    :options="options"
    class="w-full !min-h-11 sm:w-auto sm:min-w-max sm:!min-h-8"
    @change="selectTab"
  >
    <template #default="{ item }">
      <span class="whitespace-normal leading-tight sm:whitespace-nowrap">{{ item.label }}</span>
    </template>
  </ElSegmented>
</template>
