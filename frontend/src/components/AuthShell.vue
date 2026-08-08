<script setup lang="ts">
import { Switch } from '@element-plus/icons-vue'
import { useElementPlusLocale } from '@/composables/useElementPlusLocale'
import { useI18n } from '@/i18n'
import type { MessageKey } from '@/i18n'

defineProps<{
  titleKey: MessageKey
  subtitleKey: MessageKey
  eyebrowKey?: MessageKey
}>()

const { languageToggleLabel, locale, t, toggleLocale } = useI18n()
const elementLocale = useElementPlusLocale(locale)
</script>

<template>
  <el-config-provider :locale="elementLocale">
    <div class="min-h-screen bg-slate-50">
      <div class="mx-auto flex min-h-screen w-full max-w-6xl flex-col px-4 py-5 sm:px-6 lg:px-8">
        <header class="flex items-center justify-between gap-4">
          <div class="min-w-0">
            <div class="text-sm font-semibold text-blue-700">{{ t('app.fullTitle') }}</div>
            <div class="mt-1 text-xs text-slate-500">{{ t('app.subtitle') }}</div>
          </div>
          <el-button
            data-testid="auth-language-toggle"
            :icon="Switch"
            plain
            @click="toggleLocale"
          >
            {{ languageToggleLabel }}
          </el-button>
        </header>

        <main class="flex flex-1 items-center justify-center py-10">
          <section class="w-full max-w-md rounded-lg border border-slate-200 bg-white p-6 shadow-sm sm:p-8">
            <div class="mb-6">
              <p class="text-xs font-semibold uppercase tracking-wide text-blue-700">{{ t(eyebrowKey || 'auth.recommended') }}</p>
              <h1 class="mt-2 text-2xl font-bold text-slate-950">{{ t(titleKey) }}</h1>
              <p class="mt-2 text-sm text-slate-600">{{ t(subtitleKey) }}</p>
            </div>
            <slot />
          </section>
        </main>
      </div>
    </div>
  </el-config-provider>
</template>
