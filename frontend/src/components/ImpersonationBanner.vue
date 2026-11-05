<script setup lang="ts">
import { ref } from 'vue'
import { readImpersonationReturn, switchBackFromImpersonation } from '@/auth/impersonation'
import { useI18n } from '@/i18n'
import { teamAccountsMessages } from '@/locales/teamAccounts'
import { createFeatureTranslator } from '@/utils/featureI18n'

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'teamAccounts.', teamAccountsMessages)
const current = ref(readImpersonationReturn())

function switchBack() {
  if (!switchBackFromImpersonation()) return
  current.value = null
  window.location.assign('/usage')
}
</script>

<template>
  <div
    v-if="current"
    data-testid="impersonation-banner"
    class="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-amber-200 bg-amber-50 px-4 py-2 text-sm text-amber-900"
  >
    <span class="min-w-0 break-words">
      {{ t('teamAccounts.impersonating', { name: current.accountName }) }}
    </span>
    <el-button data-testid="impersonation-switch-back" size="small" type="warning" plain @click="switchBack">
      {{ t('teamAccounts.switchBack') }}
    </el-button>
  </div>
</template>
