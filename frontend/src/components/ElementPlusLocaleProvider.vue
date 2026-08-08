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
