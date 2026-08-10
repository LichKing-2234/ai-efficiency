<script setup lang="ts">
import { useElementPlusLocale } from '@/composables/useElementPlusLocale'
import { useI18n } from '@/i18n'

const { locale } = useI18n()
const elementLocale = useElementPlusLocale(locale)
</script>

<template>
  <ElConfigProvider :locale="elementLocale">
    <slot />
  </ElConfigProvider>
</template>

<style>
@media (max-width: 767px) {
  body :is(.el-button, .el-input, .el-select, .el-radio-button > span) {
    min-height: 44px;
  }

  body :is(.el-select, .el-radio-button > span) {
    display: grid;
  }

  body .el-radio-button > span {
    place-items: center;
  }
}
</style>
