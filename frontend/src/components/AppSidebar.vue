<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { useWorkItemsStore } from '@/stores/workItems'
import { useRouter } from 'vue-router'
import { useI18n } from '@/i18n'
import {
  Bell,
  DataLine,
  Document,
  Folder,
  House,
  Setting,
  Switch,
  SwitchButton,
  User,
  UserFilled,
} from '@element-plus/icons-vue'

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
const activityLinkClass = computed(() => [
  'flex items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800',
  router.currentRoute.value.path.startsWith('/activity') ? 'bg-gray-800' : '',
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
      <el-button
        data-testid="language-toggle"
        class="shrink-0 !text-gray-200"
        :icon="Switch"
        size="small"
        text
        @click="toggleLocale"
      >
        {{ languageToggleLabel }}
      </el-button>
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
        <el-icon class="mr-3"><House /></el-icon>
        {{ t('nav.myUsage') }}
      </RouterLink>

      <RouterLink
        to="/user"
        class="flex items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800"
        active-class="bg-gray-800"
        @click="handleNavigate"
      >
        <el-icon class="mr-3"><User /></el-icon>
        {{ t('nav.mySetup') }}
      </RouterLink>

      <RouterLink
        to="/work-items"
        :class="workItemsLinkClass"
        @click="handleNavigate"
      >
        <el-icon class="mr-3"><Document /></el-icon>
        <span class="min-w-0 flex-1 truncate">{{ t('nav.workItems') }}</span>
        <el-badge
          v-if="workItems.totalCount > 0"
          data-testid="sidebar-work-items-badge"
          class="sidebar-count-badge ml-2 shrink-0"
          :value="workItems.badgeLabel"
          type="primary"
        />
      </RouterLink>

      <RouterLink
        to="/activity"
        :class="activityLinkClass"
        @click="handleNavigate"
      >
        <el-icon class="mr-3"><DataLine /></el-icon>
        {{ t('nav.activity') }}
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
          <el-icon class="mr-3"><Folder /></el-icon>
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
          <el-icon class="mr-3"><UserFilled /></el-icon>
          {{ t('nav.userManagement') }}
        </RouterLink>

        <RouterLink
          to="/admin/directory/offboarding"
          class="mt-1 flex items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800"
          active-class="bg-gray-800"
          @click="handleNavigate"
        >
          <el-icon class="mr-3"><Bell /></el-icon>
          {{ t('nav.directoryOffboarding') }}
        </RouterLink>

        <RouterLink
          to="/settings"
          class="mt-1 flex items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800"
          active-class="bg-gray-800"
          @click="handleNavigate"
        >
          <el-icon class="mr-3"><Setting /></el-icon>
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
          <el-button
            circle
            class="!text-gray-300"
            :icon="SwitchButton"
            :title="t('nav.logout')"
            :aria-label="t('nav.logout')"
            text
            @click="handleLogout"
          />
        </div>
      </div>
    </div>
  </aside>
</template>

<style scoped>
.sidebar-count-badge :deep(.el-badge__content) {
  position: static;
  transform: none;
}
</style>
