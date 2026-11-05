<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElTreeSelect } from 'element-plus'
import { listAdminUserDepartments } from '@/api/adminUsers'
import { useI18n } from '@/i18n'
import { teamAccountsMessages } from '@/locales/teamAccounts'
import { createFeatureTranslator } from '@/utils/featureI18n'

interface DepartmentNode {
  value: string
  label: string
  children?: DepartmentNode[]
}

const props = withDefaults(defineProps<{
  modelValue: string
  placeholder?: string
  disabled?: boolean
}>(), {
  placeholder: '',
  disabled: false,
})

const emit = defineEmits<{
  'update:modelValue': [value: string]
  change: [value: string]
}>()

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'teamAccounts.', teamAccountsMessages)

const departments = ref<{ external_id: string; parent_external_id?: string | null; name: string; display_path: string }[]>([])
const loading = ref(false)

// The directory is small enough to hold in memory, and loading it in full keeps
// every department selectable instead of capping the list at one page.
const tree = computed<DepartmentNode[]>(() => {
  const nodes = new Map<string, DepartmentNode>()
  for (const department of departments.value) {
    nodes.set(department.external_id, { value: department.external_id, label: department.name })
  }
  const roots: DepartmentNode[] = []
  for (const department of departments.value) {
    const node = nodes.get(department.external_id)
    if (!node) continue
    const parent = department.parent_external_id ? nodes.get(department.parent_external_id) : undefined
    if (parent) {
      parent.children = parent.children ?? []
      parent.children.push(node)
    } else {
      roots.push(node)
    }
  }
  return roots
})

async function load() {
  if (loading.value || departments.value.length > 0) return
  loading.value = true
  try {
    const response = await listAdminUserDepartments()
    departments.value = response.data.data?.items ?? []
  } catch {
    departments.value = []
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void load()
})

function onChange(value: string) {
  emit('update:modelValue', value ?? '')
  emit('change', value ?? '')
}
</script>

<template>
  <ElTreeSelect
    :model-value="modelValue"
    :data="tree"
    check-strictly
    filterable
    clearable
    :render-after-expand="false"
    :loading="loading"
    :disabled="disabled"
    :placeholder="placeholder || t('teamAccounts.admin.departmentPlaceholder')"
    class="w-full"
    @update:model-value="onChange"
  />
</template>
