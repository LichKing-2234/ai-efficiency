<script setup lang="ts">
import { ref } from 'vue'
import DepartmentApproverSettings from './DepartmentApproverSettings.vue'
import SubscriptionGroupApprovalChains from './SubscriptionGroupApprovalChains.vue'
import QuotaResetNotificationSettings from './QuotaResetNotificationSettings.vue'
import { useI18n } from '@/i18n'
import type { Credential } from '@/types'

defineProps<{
  credentials: Credential[]
}>()

const { t } = useI18n()
const approverRevision = ref(0)
</script>

<template>
  <section class="space-y-4" data-testid="quota-reset-approval-settings">
    <div>
      <h3 class="text-lg font-semibold text-gray-900">{{ t('quotaResetSettings.title') }}</h3>
      <p class="mt-1 text-sm text-gray-500">{{ t('quotaResetSettings.subtitle') }}</p>
    </div>

    <DepartmentApproverSettings @saved="approverRevision += 1" />
    <SubscriptionGroupApprovalChains :approver-revision="approverRevision" />
    <QuotaResetNotificationSettings :credentials="credentials" />
  </section>
</template>
