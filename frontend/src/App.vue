<script setup lang="ts">
import { RouterView } from 'vue-router'
import { useElementPlusLocale } from '@/composables/useElementPlusLocale'
import { useI18n } from '@/i18n'

const { locale } = useI18n()
const elementLocale = useElementPlusLocale(locale)
</script>

<template>
  <ElConfigProvider :locale="elementLocale">
    <RouterView />
  </ElConfigProvider>
</template>
