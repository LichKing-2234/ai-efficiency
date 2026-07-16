<script setup lang="ts">
import { computed, defineAsyncComponent, ref, watch, type Component } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
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
const activeSection = ref<SettingsSection>(initialSettingsSection())
const activeSectionComponent = computed(() => settingsSectionComponents[activeSection.value])
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

function selectSection(section: SettingsSection) {
  activeSection.value = section
}

function onSettingsTabKeydown(event: KeyboardEvent, index: number) {
  const keys = ['ArrowRight', 'ArrowDown', 'ArrowLeft', 'ArrowUp', 'Home', 'End']
  if (!keys.includes(event.key)) return
  event.preventDefault()
  const sections = settingsSections.value
  let nextIndex = index
  if (event.key === 'ArrowRight' || event.key === 'ArrowDown') nextIndex = (index + 1) % sections.length
  if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') nextIndex = (index - 1 + sections.length) % sections.length
  if (event.key === 'Home') nextIndex = 0
  if (event.key === 'End') nextIndex = sections.length - 1
  activeSection.value = sections[nextIndex].id
  document.getElementById(`settings-tab-${sections[nextIndex].id}`)?.focus()
}
</script>

<template>
  <AppLayout>
    <div class="space-y-6">
      <div>
        <h1 class="text-2xl font-bold text-gray-900">{{ t('nav.adminConsole') }}</h1>
        <p class="mt-1 text-sm text-gray-500">{{ t('settings.subtitle') }}</p>
      </div>

      <div class="grid gap-2 lg:grid-cols-5" role="tablist" aria-label="Admin console sections">
        <button
          v-for="(section, index) in settingsSections"
          :key="section.id"
          :id="`settings-tab-${section.id}`"
          :data-testid="`settings-tab-${section.id}`"
          class="min-h-20 rounded-lg border px-3 py-3 text-left transition-colors"
          :class="activeSection === section.id ? 'border-blue-300 bg-blue-50 text-blue-950' : 'border-slate-200 bg-white text-slate-700 hover:bg-slate-50'"
          type="button"
          role="tab"
          :aria-selected="activeSection === section.id"
          :aria-controls="`settings-panel-${section.id}`"
          :tabindex="activeSection === section.id ? 0 : -1"
          @click="selectSection(section.id)"
          @keydown="onSettingsTabKeydown($event, index)"
        >
          <span class="block text-sm font-semibold">{{ section.label }}</span>
          <span class="mt-1 block text-xs text-slate-500">{{ section.description }}</span>
        </button>
      </div>

      <section
        :id="`settings-panel-${activeSection}`"
        role="tabpanel"
        :aria-labelledby="`settings-tab-${activeSection}`"
      >
        <component :is="activeSectionComponent" />
      </section>
    </div>
  </AppLayout>
</template>
