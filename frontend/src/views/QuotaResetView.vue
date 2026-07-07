<script setup lang="ts">
import AppLayout from '@/components/AppLayout.vue'
import UsageCenterTabs from '@/components/user/usage/UsageCenterTabs.vue'
import { useI18n } from '@/i18n'

const { t } = useI18n()
</script>

<template>
  <AppLayout>
    <div class="space-y-6">
      <UsageCenterTabs active="quota-reset" show-quota-reset />
      <div>
        <h1 class="text-2xl font-semibold text-slate-950">{{ t('quotaReset.title') }}</h1>
        <p class="mt-1 text-sm text-slate-600">{{ t('quotaReset.subtitle') }}</p>
      </div>
    </div>
  </AppLayout>
</template>
