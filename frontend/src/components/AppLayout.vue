<script setup lang="ts">
import { ref } from 'vue'
import AppSidebar from './AppSidebar.vue'
import { useI18n } from '@/i18n'
import { useModalFocus } from '@/composables/useModalFocus'

const mobileNavOpen = ref(false)
const mobileNavDialog = ref<HTMLElement | null>(null)
const mobileMenuButton = ref<HTMLElement | null>(null)
const { languageToggleLabel, t, toggleLocale } = useI18n()

function openMobileNav() {
  mobileNavOpen.value = true
}

function closeMobileNav() {
  mobileNavOpen.value = false
}

const { handleKeydown: handleMobileNavKeydown } = useModalFocus(mobileNavOpen, mobileNavDialog, {
  onClose: closeMobileNav,
})
</script>

<template>
  <div class="min-h-screen bg-slate-50 md:flex md:h-screen md:overflow-hidden">
    <header class="sticky top-0 z-30 flex h-14 items-center justify-between border-b border-slate-200 bg-white px-4 md:hidden">
      <button
        ref="mobileMenuButton"
        class="rounded-md border border-slate-300 px-3 py-2 text-sm font-medium text-slate-700"
        :aria-expanded="mobileNavOpen"
        aria-controls="mobile-navigation"
        @click="openMobileNav"
      >
        {{ t('nav.menu') }}
      </button>
      <div class="text-sm font-semibold text-slate-900">{{ t('app.title') }}</div>
      <button
        class="rounded-md border border-slate-300 px-3 py-2 text-sm font-medium text-slate-700"
        @click="toggleLocale"
      >
        {{ languageToggleLabel }}
      </button>
    </header>

    <AppSidebar class="hidden h-screen md:flex" />

    <main class="min-h-screen min-w-0 flex-1 overflow-auto p-4 sm:p-6 lg:p-8 md:h-screen md:min-h-0">
      <slot />
    </main>

    <div
      v-if="mobileNavOpen"
      id="mobile-navigation"
      ref="mobileNavDialog"
      class="fixed inset-0 z-40 md:hidden"
      role="dialog"
      aria-modal="true"
      :aria-label="t('nav.menu')"
      tabindex="-1"
      @keydown="handleMobileNavKeydown"
    >
      <button
        class="absolute inset-0 bg-slate-950/50"
        :aria-label="t('events.close')"
        @click="closeMobileNav"
      />
      <AppSidebar class="relative h-full w-80 max-w-[86vw]" @navigate="closeMobileNav" />
    </div>
  </div>
</template>
