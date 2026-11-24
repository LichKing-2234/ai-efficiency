<script setup lang="ts">
import { QuestionFilled } from '@element-plus/icons-vue'
import { useI18n } from '@/i18n'

const { t } = useI18n()
</script>

<template>
  <ElTooltip
    :content="t('usageDashboard.usagePairHelp')"
    placement="top"
    :show-after="200"
  >
    <el-icon
      class="ml-1 inline-flex cursor-help align-middle text-slate-400"
      :size="14"
      :aria-label="t('usageDashboard.usagePairHelp')"
    >
      <QuestionFilled />
    </el-icon>
  </ElTooltip>
</template>
