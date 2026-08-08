<script setup lang="ts">
import { ref, watch } from 'vue'
import { Close, Menu, Switch } from '@element-plus/icons-vue'
import AppSidebar from './AppSidebar.vue'
import { useMediaQuery } from '@/composables/useMediaQuery'
import { useI18n } from '@/i18n'

const mobileNavOpen = ref(false)
const desktopLayout = useMediaQuery('(min-width: 768px)')
const { languageToggleLabel, t, toggleLocale } = useI18n()

function openMobileNav() {
  mobileNavOpen.value = true
}

function closeMobileNav() {
  mobileNavOpen.value = false
}

watch(desktopLayout, (isDesktop) => {
  if (isDesktop) closeMobileNav()
})

</script>

<template>
  <div class="min-h-screen bg-slate-50 md:flex md:h-screen md:overflow-hidden">
    <header class="sticky top-0 z-30 flex h-14 items-center justify-between border-b border-slate-200 bg-white px-4 md:hidden">
      <el-button
        :icon="Menu"
        :aria-expanded="mobileNavOpen"
        aria-controls="mobile-navigation"
        @click="openMobileNav"
      >
        {{ t('nav.menu') }}
      </el-button>
      <div class="text-sm font-semibold text-slate-900">{{ t('app.title') }}</div>
      <el-button :icon="Switch" plain @click="toggleLocale">
        {{ languageToggleLabel }}
      </el-button>
    </header>

    <AppSidebar class="hidden h-screen md:flex" />

    <main class="min-h-screen min-w-0 flex-1 overflow-auto p-4 sm:p-6 lg:p-8 md:h-screen md:min-h-0">
      <slot />
    </main>

    <el-drawer
      v-model="mobileNavOpen"
      destroy-on-close
      direction="ltr"
      :show-close="false"
      size="min(20rem, 86vw)"
    >
      <template #header>
        <span class="text-sm font-semibold text-slate-900">{{ t('nav.menu') }}</span>
        <el-button
          data-testid="mobile-nav-close"
          :icon="Close"
          :title="t('app.close')"
          text
          @click="closeMobileNav"
        />
      </template>
      <div id="mobile-navigation" class="h-full">
        <AppSidebar v-if="mobileNavOpen" class="h-full w-full" mobile @navigate="closeMobileNav" />
      </div>
    </el-drawer>
  </div>
</template>
