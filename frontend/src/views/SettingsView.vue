<script setup lang="ts">
import { computed, defineAsyncComponent, ref, watch, type Component } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import { useMediaQuery } from '@/composables/useMediaQuery'
import { useI18n } from '@/i18n'

type SettingsSection = 'ai-services' | 'code-platforms' | 'organization-login' | 'deployment-runtime' | 'advanced-credentials'

const settingsSectionLoaders = {
  'ai-services': () => import('@/components/settings/AIServiceSettings.vue'),
  'code-platforms': () => import('@/components/settings/CodePlatformSettings.vue'),
  'organization-login': () => import('@/components/settings/OrganizationLoginSettings.vue'),
  'deployment-runtime': () => import('@/components/settings/DeploymentRuntimeSettings.vue'),
  'advanced-credentials': () => import('@/components/settings/AdvancedCredentialSettings.vue'),
} satisfies Record<SettingsSection, () => Promise<{ default: Component }>>

const settingsSectionComponents: Record<SettingsSection, Component> = {
  'ai-services': defineAsyncComponent(settingsSectionLoaders['ai-services']),
  'code-platforms': defineAsyncComponent(settingsSectionLoaders['code-platforms']),
  'organization-login': defineAsyncComponent(settingsSectionLoaders['organization-login']),
  'deployment-runtime': defineAsyncComponent(settingsSectionLoaders['deployment-runtime']),
  'advanced-credentials': defineAsyncComponent(settingsSectionLoaders['advanced-credentials']),
}

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const hasWideSettingsNavigation = useMediaQuery('(min-width: 1280px)')
const activeSection = ref<SettingsSection>(initialSettingsSection())
const settingsSections = computed<Array<{ id: SettingsSection; label: string; description: string }>>(() => [
  { id: 'ai-services', label: t('settings.aiServices'), description: t('settings.aiServicesHelp') },
  { id: 'code-platforms', label: t('settings.codePlatforms'), description: t('settings.codePlatformsHelp') },
  { id: 'organization-login', label: t('settings.organizationLogin'), description: t('settings.organizationLoginHelp') },
  { id: 'deployment-runtime', label: t('settings.deploymentRuntime'), description: t('settings.deploymentRuntimeHelp') },
  { id: 'advanced-credentials', label: t('settings.advancedCredentials'), description: t('settings.advancedCredentialsHelp') },
])

watch(activeSection, replaceSettingsQuery)

function initialSettingsSection(): SettingsSection {
  const section = route.query.section
  return typeof section === 'string' && Object.prototype.hasOwnProperty.call(settingsSectionLoaders, section)
    ? section as SettingsSection
    : 'ai-services'
}

function replaceSettingsQuery() {
  const query = activeSection.value === 'ai-services' ? {} : { section: activeSection.value }
  void router.replace({ query })
}
</script>

<template>
  <AppLayout>
    <div class="space-y-6">
      <div>
        <h1 class="text-2xl font-bold text-gray-900">{{ t('nav.adminConsole') }}</h1>
        <p class="mt-1 text-sm text-gray-500">{{ t('settings.subtitle') }}</p>
      </div>

      <ElTabs
        v-if="hasWideSettingsNavigation"
        v-model="activeSection"
        class="settings-section-tabs"
        stretch
      >
        <ElTabPane
          v-for="section in settingsSections"
          :key="section.id"
          :name="section.id"
          lazy
        >
          <template #label>
            <span
              :data-testid="`settings-tab-${section.id}`"
              class="block min-w-0 py-2 text-left"
            >
              <span class="block text-sm font-semibold">{{ section.label }}</span>
              <span class="mt-1 block truncate text-xs text-slate-500">{{ section.description }}</span>
            </span>
          </template>

        </ElTabPane>
      </ElTabs>

      <ElSelect
        v-else
        v-model="activeSection"
        data-testid="settings-section-select"
        class="w-full"
        :aria-label="t('nav.adminConsole')"
      >
        <ElOption
          v-for="section in settingsSections"
          :key="section.id"
          :label="section.label"
          :value="section.id"
        />
      </ElSelect>

      <section :id="`settings-panel-${activeSection}`">
        <component :is="settingsSectionComponents[activeSection]" />
      </section>
    </div>
  </AppLayout>
</template>
