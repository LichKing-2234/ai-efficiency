<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { useWorkItemsStore } from '@/stores/workItems'
import { useRouter } from 'vue-router'
import { useI18n } from '@/i18n'

defineProps<{
  mobile?: boolean
}>()

const emit = defineEmits<{
  navigate: []
}>()

const auth = useAuthStore()
const workItems = useWorkItemsStore()
const router = useRouter()
const { languageToggleLabel, t, toggleLocale } = useI18n()
const displayUsername = computed(() => auth.user?.username ?? 'User')
const displayRole = computed(() => auth.user?.role ?? '')
const usageLinkClass = computed(() => [
  'flex items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800',
  router.currentRoute.value.path.startsWith('/usage') ? 'bg-gray-800' : '',
])
const workItemsLinkClass = computed(() => [
  'flex items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800',
  router.currentRoute.value.path.startsWith('/work-items') ? 'bg-gray-800' : '',
])

onMounted(() => {
  void workItems.loadCounts()
})

function handleLogout() {
  auth.logout()
  router.push('/login')
}

function handleNavigate() {
  emit('navigate')
}
</script>

<template>
  <aside class="flex h-full min-h-0 w-60 shrink-0 flex-col bg-gray-900 text-gray-100">
    <div
      data-testid="sidebar-header"
      class="flex h-14 items-center justify-between gap-3 px-4"
    >
      <div class="min-w-0 truncate text-lg font-semibold tracking-wide">
        {{ t('app.title') }}
      </div>
      <button
        type="button"
        data-testid="language-toggle"
        class="shrink-0 rounded-md border border-gray-700 px-2.5 py-1.5 text-xs font-medium text-gray-200 hover:bg-gray-800 focus:outline-none focus:ring-2 focus:ring-indigo-400 focus:ring-offset-2 focus:ring-offset-gray-900"
        @click="toggleLocale"
      >
        {{ languageToggleLabel }}
      </button>
    </div>

    <nav class="min-h-0 flex-1 space-y-1 overflow-y-auto px-2 py-4">
      <div class="px-3 pb-2 text-[11px] font-semibold uppercase tracking-wide text-gray-500">
        {{ t('nav.myWorkSection') }}
      </div>
      <RouterLink
        to="/usage"
        :class="usageLinkClass"
        @click="handleNavigate"
      >
        <svg class="mr-3 h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
            d="M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-4 0a1 1 0 01-1-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 01-1 1" />
        </svg>
        {{ t('nav.myUsage') }}
      </RouterLink>

      <RouterLink
        to="/user"
        class="flex items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800"
        active-class="bg-gray-800"
        @click="handleNavigate"
      >
        <svg class="mr-3 h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
            d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
        </svg>
        {{ t('nav.mySetup') }}
      </RouterLink>

      <RouterLink
        to="/work-items"
        :class="workItemsLinkClass"
        @click="handleNavigate"
      >
        <svg class="mr-3 h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
            d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-6 7l2 2 4-4" />
        </svg>
        <span class="min-w-0 flex-1 truncate">{{ t('nav.workItems') }}</span>
        <span
          v-if="workItems.totalCount > 0"
          data-testid="sidebar-work-items-badge"
          class="ml-2 inline-flex min-w-6 shrink-0 justify-center rounded-full bg-cyan-500 px-1.5 py-0.5 text-xs font-semibold text-white"
        >
          {{ workItems.badgeLabel }}
        </span>
      </RouterLink>

      <RouterLink
        to="/attribution"
        class="flex items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800"
        active-class="bg-gray-800"
        @click="handleNavigate"
      >
        <svg class="mr-3 h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
            d="M9 17v-6m3 6V7m3 10v-4m3 8H6a2 2 0 01-2-2V5a2 2 0 012-2h12a2 2 0 012 2v14a2 2 0 01-2 2z" />
        </svg>
        {{ t('nav.attribution') }}
      </RouterLink>

      <div class="mt-5 border-t border-gray-800 pt-4">
        <div class="px-3 pb-2 text-[11px] font-semibold uppercase tracking-wide text-gray-500">
          {{ t('nav.codeSection') }}
        </div>
        <RouterLink
          to="/repos"
          class="flex items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800"
          active-class="bg-gray-800"
          @click="handleNavigate"
        >
          <svg class="mr-3 h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
              d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z" />
          </svg>
          {{ t('nav.codeRepositories') }}
        </RouterLink>
      </div>

      <div v-if="auth.isAdmin" class="mt-5 border-t border-gray-800 pt-4">
        <div class="px-3 pb-2 text-[11px] font-semibold uppercase tracking-wide text-gray-500">
          {{ t('nav.adminSection') }}
        </div>

        <RouterLink
          to="/admin/users"
          class="flex items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800"
          active-class="bg-gray-800"
          @click="handleNavigate"
        >
          <svg class="mr-3 h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
              d="M17 20h5v-2a4 4 0 00-4-4h-1M9 20H4v-2a4 4 0 014-4h1m8-4a4 4 0 11-8 0 4 4 0 018 0zm-10 0a3 3 0 11-6 0 3 3 0 016 0z" />
          </svg>
          {{ t('nav.userManagement') }}
        </RouterLink>

        <RouterLink
          to="/admin/directory/offboarding"
          class="mt-1 flex items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800"
          active-class="bg-gray-800"
          @click="handleNavigate"
        >
          <svg class="mr-3 h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
              d="M18 8a6 6 0 10-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9zM13.73 21a2 2 0 01-3.46 0M9 11h6" />
          </svg>
          {{ t('nav.directoryOffboarding') }}
        </RouterLink>

        <RouterLink
          to="/settings"
          class="mt-1 flex items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800"
          active-class="bg-gray-800"
          @click="handleNavigate"
        >
          <svg class="mr-3 h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
              d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.066 2.573c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.573 1.066c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.066-2.573c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
          </svg>
          {{ t('nav.adminConsole') }}
        </RouterLink>
      </div>

    </nav>

    <div data-testid="sidebar-footer" class="border-t border-gray-700 p-4">
      <div class="flex items-center gap-3">
        <div
          data-testid="sidebar-account-summary"
          class="min-w-0 flex-1 px-1 py-1 text-sm"
        >
          <p class="truncate font-medium" :title="displayUsername">{{ displayUsername }}</p>
          <p class="truncate text-xs text-gray-400" :title="displayRole">{{ displayRole }}</p>
        </div>
        <div class="flex shrink-0 items-center gap-1">
          <button
            type="button"
            class="rounded-md p-1.5 text-gray-400 hover:bg-gray-800 hover:text-white focus:outline-none focus:ring-2 focus:ring-indigo-400 focus:ring-offset-2 focus:ring-offset-gray-900"
            :title="t('nav.logout')"
            :aria-label="t('nav.logout')"
            @click="handleLogout"
          >
            <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1" />
            </svg>
          </button>
        </div>
      </div>
    </div>
  </aside>
</template>
