<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { useWorkItemsStore } from '@/stores/workItems'
import { useRouter } from 'vue-router'
import { useI18n } from '@/i18n'
import {
  Bell,
  Connection,
  DataLine,
  Document,
  House,
  Setting,
  Switch,
  SwitchButton,
  User,
  UserFilled,
} from '@element-plus/icons-vue'

const props = defineProps<{
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
const activeNavigationClass = computed(() => (
  props.mobile ? 'bg-blue-50 text-blue-700' : 'bg-gray-800'
))
const usageLinkClass = computed(() => [
  'flex min-h-11 items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800',
  router.currentRoute.value.path.startsWith('/usage') ? activeNavigationClass.value : '',
])
const workItemsLinkClass = computed(() => [
  'flex min-h-11 items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800',
  router.currentRoute.value.path.startsWith('/work-items') ? activeNavigationClass.value : '',
])
const activityLinkClass = computed(() => [
  'flex min-h-11 items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800',
  router.currentRoute.value.path.startsWith('/activity') ? activeNavigationClass.value : '',
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
  <aside
    class="flex h-full min-h-0 shrink-0 flex-col"
    :class="mobile
      ? 'w-full overflow-y-auto bg-white text-slate-700'
      : 'w-60 bg-gray-900 text-gray-100'"
  >
    <div
      v-if="!mobile"
      class="flex h-14 items-center justify-between gap-3 px-4"
    >
      <div class="min-w-0 truncate text-lg font-semibold">
        {{ t('app.title') }}
      </div>
      <el-button
        class="shrink-0 !text-gray-200"
        :icon="Switch"
        size="small"
        text
        @click="toggleLocale"
      >
        {{ languageToggleLabel }}
      </el-button>
    </div>

    <nav
      class="space-y-1"
      :class="mobile
        ? 'flex-none px-3 py-3'
        : 'min-h-0 flex-1 overflow-y-auto px-2 py-4'"
    >
      <div class="px-3 pb-2 text-[11px] font-semibold uppercase text-gray-500">
        {{ t('nav.myWorkSection') }}
      </div>
      <RouterLink to="/usage" :class="usageLinkClass" @click="handleNavigate">
        <el-icon class="mr-3"><House /></el-icon>
        {{ t('nav.myUsage') }}
      </RouterLink>

      <RouterLink
        to="/user"
        class="flex min-h-11 items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800"
        :active-class="activeNavigationClass"
        @click="handleNavigate"
      >
        <el-icon class="mr-3"><User /></el-icon>
        {{ t('nav.mySetup') }}
      </RouterLink>

      <RouterLink to="/work-items" :class="workItemsLinkClass" @click="handleNavigate">
        <el-icon class="mr-3"><Document /></el-icon>
        <span class="min-w-0 flex-1 truncate">{{ t('nav.workItems') }}</span>
        <el-badge
          v-if="workItems.totalCount > 0"
          data-testid="sidebar-work-items-badge"
          class="ml-2 shrink-0"
          :value="workItems.badgeLabel"
          :badge-style="{ position: 'static', transform: 'none' }"
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

      <div
        v-if="auth.isAdmin"
        class="mt-3 border-t pt-3"
        :class="mobile ? 'border-slate-200' : 'border-gray-800 md:mt-5 md:pt-4'"
      >
        <div class="px-3 pb-2 text-[11px] font-semibold uppercase text-gray-500">
          {{ t('nav.adminSection') }}
        </div>
        <RouterLink
          to="/admin/users"
          class="flex min-h-11 items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800"
          :active-class="activeNavigationClass"
          @click="handleNavigate"
        >
          <el-icon class="mr-3"><UserFilled /></el-icon>
          {{ t('nav.userManagement') }}
        </RouterLink>

        <RouterLink
          to="/admin/directory/offboarding"
          class="mt-1 flex min-h-11 items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800"
          :active-class="activeNavigationClass"
          @click="handleNavigate"
        >
          <el-icon class="mr-3"><Bell /></el-icon>
          {{ t('nav.directoryOffboarding') }}
        </RouterLink>

        <RouterLink
          to="/admin/relay-planning"
          class="mt-1 flex min-h-11 items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800"
          :active-class="activeNavigationClass"
          @click="handleNavigate"
        >
          <el-icon class="mr-3"><Connection /></el-icon>
          {{ t('nav.relayPlanning') }}
        </RouterLink>

        <RouterLink
          to="/repos"
          class="mt-1 flex min-h-11 items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800"
          :active-class="activeNavigationClass"
          @click="handleNavigate"
        >
          <el-icon class="mr-3"><Document /></el-icon>
          {{ t('nav.codeRepositories') }}
        </RouterLink>

        <RouterLink
          to="/settings"
          class="mt-1 flex min-h-11 items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800"
          :active-class="activeNavigationClass"
          @click="handleNavigate"
        >
          <el-icon class="mr-3"><Setting /></el-icon>
          {{ t('nav.adminConsole') }}
        </RouterLink>
      </div>
    </nav>

    <div
      class="border-t p-4"
      :class="mobile ? 'flex-none border-slate-200' : 'border-gray-700'"
    >
      <div class="flex items-center gap-3">
        <div
          class="min-w-0 flex-1 px-1 py-1 text-sm"
        >
          <p class="truncate font-medium" :title="displayUsername">{{ displayUsername }}</p>
        </div>
        <el-button
          circle
          class="!text-gray-300"
          :icon="SwitchButton"
          :title="t('nav.logout')"
          text
          @click="handleLogout"
        />
      </div>
    </div>
  </aside>
</template>
