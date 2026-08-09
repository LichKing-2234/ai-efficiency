<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from '@/i18n'
import { useRouter } from 'vue-router'
import { useMediaQuery } from '@/composables/useMediaQuery'

const props = defineProps<{
  active: 'personal' | 'team' | 'quota-reset'
  showTeam?: boolean
  showQuotaReset?: boolean
}>()

const { t } = useI18n()
const router = useRouter()
const isCompactTabs = useMediaQuery('(max-width: 639px)', true)
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
  <div class="w-full sm:inline-block sm:w-auto" data-testid="usage-center-tabs">
    <ElSegmented
      v-if="isCompactTabs"
      :model-value="active"
      :options="options"
      block
      class="w-full !min-h-11"
      @change="selectTab"
    >
      <template #default="{ item }">
        <span class="block whitespace-normal leading-tight">{{ item.label }}</span>
      </template>
    </ElSegmented>
    <ElSegmented
      v-else
      :model-value="active"
      :options="options"
      class="min-w-max"
      @change="selectTab"
    />
  </div>
</template>
