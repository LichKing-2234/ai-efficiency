<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { listAdminUsers } from '@/api/adminUsers'
import { useI18n } from '@/i18n'
import { teamAccountsMessages } from '@/locales/teamAccounts'
import { createFeatureTranslator } from '@/utils/featureI18n'
import type { AdminUser } from '@/types'

const props = withDefaults(defineProps<{
  modelValue: number | null
  placeholder?: string
  disabled?: boolean
  departmentId?: string
}>(), {
  placeholder: '',
  disabled: false,
  departmentId: '',
})

const emit = defineEmits<{
  'update:modelValue': [value: number | null]
  select: [user: AdminUser | null]
}>()

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'teamAccounts.', teamAccountsMessages)

const options = ref<AdminUser[]>([])
const loading = ref(false)
const query = ref('')

function label(user: AdminUser) {
  const department = user.department?.display_path || user.department?.name || ''
  return department ? `${user.username} · ${department}` : user.username
}

async function search(keyword: string) {
  query.value = keyword
  loading.value = true
  try {
    const response = await listAdminUsers({
      q: keyword.trim() || undefined,
      department_id: props.departmentId.trim() || undefined,
      page: 1,
      page_size: 50,
    })
    options.value = response.data.data?.items ?? []
  } catch {
    options.value = []
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void search('')
})

watch(
  () => props.modelValue,
  (value) => {
    if (!value) return
    if (!options.value.some((option) => option.id === value)) {
      void search('')
    }
  },
)

watch(
  () => props.departmentId,
  () => {
    void search(query.value)
  },
)

function onChange(value: number | null) {
  emit('update:modelValue', value)
  emit('select', value === null ? null : options.value.find((option) => option.id === value) ?? null)
}
</script>

<template>
  <ElSelect
    :model-value="modelValue"
    filterable
    remote
    clearable
    :remote-method="search"
    :loading="loading"
    :disabled="disabled"
    :placeholder="placeholder || t('teamAccounts.userPicker.placeholder')"
    :no-match-text="t('teamAccounts.userPicker.empty')"
    class="w-full"
    @update:model-value="onChange"
  >
    <ElOption
      v-for="user in options"
      :key="user.id"
      :label="`${label(user)} (${user.email})`"
      :value="user.id"
    />
  </ElSelect>
</template>
