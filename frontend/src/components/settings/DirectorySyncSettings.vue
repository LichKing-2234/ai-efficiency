<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  createDirectorySource,
  listDirectorySources,
  previewDirectorySource,
  startDirectoryRun,
  updateDirectorySource,
  validateDirectorySource,
} from '@/api/directory'
import type { Credential, DirectorySource, DirectorySourceRequest } from '@/types'

defineProps<{
  credentials: Credential[]
}>()

const sources = ref<DirectorySource[]>([])
const selectedSourceId = ref<number | null>(null)
const loading = ref(false)
const saving = ref(false)
const message = ref('')
const error = ref('')
const form = ref<DirectorySourceRequest>({
  name: '',
  description: '',
  scope: 'full_company',
  enabled: false,
  dsl: '',
  schedule_enabled: false,
  schedule_interval: 'daily',
  schedule_timezone: 'UTC',
})

const selectedSource = computed(() => sources.value.find((source) => source.id === selectedSourceId.value) || null)

const templates = [
  {
    name: 'Departments then members',
    dsl: `version: 1
scope: full_company
auth:
  type: header
  header: X-Directory-API-Key
  credential_ref: directory_api_key
limits:
  timeout_seconds: 30
  max_response_bytes: 1048576
  max_items: 50000
steps:
  - id: departments
    request:
      method: GET
      url: https://directory.example.com/api/v1/departments
    extract:
      items: $.data.departments
    map:
      department:
        external_id: $.id
        parent_external_id: $.parent_id
        name: $.name
        path: $.path
  - id: members
    foreach: departments.items
    request:
      method: GET
      url: https://directory.example.com/api/v1/users
      query:
        department_id: "{{ item.external_id }}"
    extract:
      items: $.data.users
    map:
      member:
        external_id: $.id
        email: $.email
        display_name: $.name
        department_external_id: "{{ item.external_id }}"
        status: $.status
`,
  },
  {
    name: 'Single members endpoint',
    dsl: `version: 1
scope: full_company
auth:
  type: header
  header: X-Directory-API-Key
  credential_ref: directory_api_key
limits:
  timeout_seconds: 30
  max_response_bytes: 1048576
  max_items: 50000
steps:
  - id: members
    request:
      method: GET
      url: https://directory.example.com/api/v1/members
    extract:
      items: $.data.members
    map:
      member:
        external_id: $.id
        email: $.email
        display_name: $.name
        department_external_id: $.department_id
        status: $.status
`,
  },
  {
    name: 'Paged members endpoint',
    dsl: `version: 1
scope: full_company
auth:
  type: header
  header: X-Directory-API-Key
  credential_ref: directory_api_key
limits:
  timeout_seconds: 30
  max_response_bytes: 1048576
  max_items: 50000
steps:
  - id: members
    request:
      method: GET
      url: https://directory.example.com/api/v1/members?page=1
    extract:
      items: $.data.items
    map:
      member:
        external_id: $.id
        email: $.email
        display_name: $.name
        department_external_id: $.department_id
        status: $.status
`,
  },
]

onMounted(loadSources)

async function loadSources() {
  loading.value = true
  try {
    const res = await listDirectorySources()
    sources.value = res.data.data?.items ?? []
    if (sources.value.length > 0) {
      selectSource(sources.value[0])
    } else {
      applyTemplate(templates[0].dsl)
    }
  } catch (e: any) {
    error.value = e?.message || 'Failed to load directory sources'
  } finally {
    loading.value = false
  }
}

function selectSource(source: DirectorySource) {
  selectedSourceId.value = source.id
  form.value = {
    name: source.name,
    description: source.description || '',
    scope: 'full_company',
    enabled: source.enabled,
    dsl: source.dsl || templates[0].dsl,
    schedule_enabled: source.schedule_enabled,
    schedule_interval: source.schedule_interval || 'daily',
    schedule_timezone: source.schedule_timezone || 'UTC',
  }
}

function applyTemplate(dsl: string) {
  form.value.dsl = dsl
  if (!form.value.name) form.value.name = 'Example Directory'
  if (!form.value.description) form.value.description = 'Synthetic directory source'
}

async function saveSource() {
  saving.value = true
  message.value = ''
  error.value = ''
  try {
    if (selectedSourceId.value) {
      await updateDirectorySource(selectedSourceId.value, form.value)
    } else {
      const res = await createDirectorySource(form.value)
      selectedSourceId.value = res.data.data?.id ?? null
    }
    message.value = 'Directory source saved'
    await loadSources()
  } catch (e: any) {
    error.value = e?.response?.data?.message || e?.message || 'Failed to save directory source'
  } finally {
    saving.value = false
  }
}

async function validateSource() {
  if (!selectedSourceId.value) return
  const res = await validateDirectorySource(selectedSourceId.value)
  const data = res.data.data
  message.value = data?.valid ? 'Validation passed' : `Validation found ${data?.issues.length ?? 0} issue(s)`
}

async function previewSource() {
  if (!selectedSourceId.value) return
  const res = await previewDirectorySource(selectedSourceId.value)
  message.value = `Preview run ${res.data.data?.status || 'started'}`
}

async function runNow() {
  if (!selectedSourceId.value) return
  const res = await startDirectoryRun(selectedSourceId.value, { mode: 'apply' })
  message.value = `Apply run ${res.data.data?.status || 'started'}`
}

async function copyAIPrompt() {
  const prompt = `Generate a YAML DSL for AI Efficiency Directory Sync.
Use only version: 1, scope: full_company, GET requests, header auth with credential_ref, JSONPath-like item extraction, and department/member mappings.
Do not include real API keys, real employee data, real tokens, real company domains, or real internal URLs.
Use placeholders such as https://directory.example.com, X-Directory-API-Key, directory_api_key, alice@example.com, bob@example.org, Department Alpha, and Department Beta.
Return only the YAML DSL.`
  await navigator.clipboard.writeText(prompt)
  message.value = 'AI prompt copied'
}
</script>

<template>
  <section class="space-y-4 rounded-lg border border-gray-200 bg-white p-5">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <h3 class="text-base font-semibold text-gray-900">Directory Sync</h3>
        <p class="text-sm text-gray-500">Configure a generic organization API and review offboarding candidates separately.</p>
      </div>
      <button data-testid="directory-copy-ai-prompt" type="button" class="rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 hover:bg-gray-50" @click="copyAIPrompt">
        Copy AI Prompt
      </button>
    </div>

    <div class="grid gap-4 lg:grid-cols-[220px_1fr]">
      <div class="space-y-2">
        <button
          v-for="source in sources"
          :key="source.id"
          type="button"
          class="block w-full rounded-md border px-3 py-2 text-left text-sm"
          :class="source.id === selectedSourceId ? 'border-indigo-500 bg-indigo-50 text-indigo-900' : 'border-gray-200 text-gray-700 hover:bg-gray-50'"
          @click="selectSource(source)"
        >
          <span class="block font-medium">{{ source.name }}</span>
          <span class="block text-xs text-gray-500">{{ source.enabled ? 'Enabled' : 'Disabled' }}</span>
        </button>
        <p v-if="!loading && sources.length === 0" class="text-sm text-gray-500">No directory source configured.</p>
      </div>

      <div class="space-y-4">
        <div class="grid gap-3 md:grid-cols-2">
          <label class="text-sm font-medium text-gray-700">
            Name
            <input data-testid="directory-source-name" v-model="form.name" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
          </label>
          <label class="text-sm font-medium text-gray-700">
            Schedule
            <select v-model="form.schedule_interval" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm">
              <option value="hourly">Hourly</option>
              <option value="daily">Daily</option>
              <option value="weekly">Weekly</option>
            </select>
          </label>
        </div>

        <div class="flex flex-wrap items-center gap-4 text-sm text-gray-700">
          <label class="inline-flex items-center gap-2">
            <input v-model="form.enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-indigo-600" />
            Enabled
          </label>
          <label class="inline-flex items-center gap-2">
            <input v-model="form.schedule_enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-indigo-600" />
            Scheduled apply
          </label>
          <span class="text-gray-500">Credential ref: directory_api_key</span>
        </div>

        <div class="rounded-md border border-gray-200 p-3">
          <div class="mb-2 flex flex-wrap gap-2">
            <button v-for="template in templates" :key="template.name" type="button" class="rounded-md border border-gray-300 px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-50" @click="applyTemplate(template.dsl)">
              {{ template.name }}
            </button>
          </div>
          <p class="mb-2 text-xs text-gray-500">Templates use synthetic placeholders such as https://directory.example.com and alice@example.com.</p>
          <textarea v-model="form.dsl" class="h-72 w-full rounded-md border border-gray-300 px-3 py-2 font-mono text-xs" />
        </div>

        <div v-if="message" class="rounded-md bg-green-50 p-3 text-sm text-green-700">{{ message }}</div>
        <div v-if="error" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</div>

        <div class="flex flex-wrap justify-end gap-2">
          <button data-testid="directory-validate" type="button" :disabled="!selectedSourceId" class="rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 disabled:opacity-50" @click="validateSource">
            Validate
          </button>
          <button data-testid="directory-preview" type="button" :disabled="!selectedSourceId" class="rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 disabled:opacity-50" @click="previewSource">
            Preview
          </button>
          <button data-testid="directory-run-now" type="button" :disabled="!selectedSourceId" class="rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 disabled:opacity-50" @click="runNow">
            Run Now
          </button>
          <button data-testid="directory-save" type="button" :disabled="saving" class="rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white disabled:opacity-50" @click="saveSource">
            {{ saving ? 'Saving' : 'Save' }}
          </button>
        </div>
      </div>
    </div>
  </section>
</template>
