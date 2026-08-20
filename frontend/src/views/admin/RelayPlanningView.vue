<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Check, Connection, Refresh, Setting } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import AppLayout from '@/components/AppLayout.vue'
import { useI18n } from '@/i18n'
import { relayPlanningMessages } from '@/locales/relayPlanning'
import { listAdminUserDepartmentOptions, listAdminUserSubscriptionOptions } from '@/api/adminUsers'
import {
  executeRelayPlan,
  executeRelayReplan,
  listRelayGroupMappings,
  previewRelayPlan,
  previewRelayReplan,
  rebindRelayGroupMapping,
  type RelayPlanningRequest,
  type RelayPlanningMapping,
  type RelayPlanningPlan,
} from '@/api/relayPlanning'
import { createFeatureTranslator } from '@/utils/featureI18n'

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'relayPlanning.', relayPlanningMessages)

const loading = ref(false)
const confirming = ref(false)
const executing = ref(false)
const confirmDialogOpen = ref(false)
const error = ref('')
const plan = ref<RelayPlanningPlan | null>(null)
const activeMappingID = ref<number | null>(null)
const selectedUserIDs = ref<Set<number>>(new Set())
const selectedUnmanagedRelayIDs = ref<Set<number>>(new Set())
const operationKey = ref('')
const mappings = ref<RelayPlanningMapping[]>([])
const departments = ref<Array<{ external_id: string; name: string; display_path: string }>>([])
const providers = ref<Array<{ id: number; name: string; display_name: string; groups: Array<{ group_id: string; group_name: string; platform: string }> }>>([])

const form = reactive({
  provider_id: 0,
  department_id: '',
  platform: '',
  template_group_id: 0,
  source_group_id: 0,
  weekly_cost_target: 0,
})

const provider = computed(() => providers.value.find((item) => item.id === form.provider_id))
const groups = computed(() => (provider.value?.groups ?? []).filter((group) => !form.platform || group.platform === form.platform))
const platforms = computed(() => Array.from(new Set((provider.value?.groups ?? []).map((group) => group.platform).filter(Boolean))))
const eligibleCandidates = computed(() => plan.value?.candidates.filter((candidate) => candidate.eligible) ?? [])
const unassignedCandidates = computed(() => plan.value?.candidates.filter((candidate) => candidate.can_add && !selectedUserIDs.value.has(candidate.user_id)) ?? [])

function translateWarning(warning: string): string {
  void locale.value
  if (warning === 'no eligible member has a valid relay mapping and source-group membership') return t('relayPlanning.warningNoEligible')
  if (warning === 'user is not a member of the selected source group') return t('relayPlanning.warningNotSourceMember')
  if (warning === 'no migratable AE-managed API key') return t('relayPlanning.warningNoMigratableKey')
  if (warning === 'user belongs to multiple departments') return t('relayPlanning.warningMultipleDepartments')
  if (warning.includes(' has no relay mapping')) return t('relayPlanning.warningNoRelayMapping', { user: warning.replace(/ has no relay mapping$/, '') })
  if (warning.startsWith('relay groups unavailable: ')) return `${t('relayPlanning.warningRelayGroupsUnavailable')}: ${warning.slice('relay groups unavailable: '.length)}`
  const unavailable = warning.match(/^(template|migration source|target) group (\d+) is unavailable$/)
  if (unavailable) return t(`relayPlanning.warningUnavailable${unavailable[1] === 'template' ? 'Template' : unavailable[1] === 'migration source' ? 'Source' : 'Target'}`, { id: unavailable[2] })
  const conflict = warning.match(/^user (\d+) is assigned in multiple mappings$/)
  if (conflict) return t('relayPlanning.warningMappingConflict', { user: conflict[1] })
  if (warning === 'mapping has no target groups') return t('relayPlanning.warningNoTargetGroups')
  if (warning === 'mapping contains an invalid target group') return t('relayPlanning.warningInvalidTargetGroup')
  const invalidDepartment = warning.match(/^department (.+) is unavailable$/)
  if (invalidDepartment) return t('relayPlanning.warningUnavailableDepartment', { department: invalidDepartment[1] })
  const capacity = warning.match(/^user (\d+) exceeds remaining planning capacity$/)
  if (capacity) return t('relayPlanning.warningRemainingCapacity', { user: capacity[1] })
  const unmanagedRelay = warning.match(/^unmanaged relay member (\d+) in target group (\d+)$/)
  if (unmanagedRelay) return t('relayPlanning.warningUnmanagedRelayMember', { user: unmanagedRelay[1], group: unmanagedRelay[2] })
  const unmanaged = warning.match(/^unmanaged member (\d+) in target group (\d+)$/)
  if (unmanaged) return t('relayPlanning.warningUnmanagedMember', { user: unmanaged[1], group: unmanaged[2] })
  const wrongGroup = warning.match(/^member (\d+) is subscribed to target group (\d+) instead of (\d+)$/)
  if (wrongGroup) return t('relayPlanning.warningWrongTargetGroup', { user: wrongGroup[1], actual: wrongGroup[2], expected: wrongGroup[3] })
  const missing = warning.match(/^mapping member (\d+) is missing from target group (\d+)$/)
  if (missing) return t('relayPlanning.warningMissingTargetMembership', { user: missing[1], group: missing[2] })
  const targetMatch = warning.match(/^(.*) exceeds the planning target$/)
  if (targetMatch) return t('relayPlanning.warningExceedsTarget', { group: targetMatch[1] })
  return warning
}

function translateMappingStatus(status: string): string {
  if (status === 'needs_retry') return t('relayPlanning.needsRetry')
  if (status === 'active') return t('relayPlanning.active')
  return status
}

function planningRequest(): RelayPlanningRequest {
  const request: RelayPlanningRequest = {
    provider_id: Number(form.provider_id),
    department_id: String(form.department_id || ''),
    platform: String(form.platform || ''),
    template_group_id: Number(form.template_group_id),
    source_group_id: Number(form.source_group_id),
    weekly_cost_target: Number(form.weekly_cost_target || 0),
  }
  return request
}

function assignmentPayload() {
  return (plan.value?.assignments ?? []).map((assignment) => ({
    index: assignment.index,
    total_cost: assignment.total_cost,
    user_ids: [...(assignment.user_ids ?? [])],
    target_group_id: assignment.target_group_id,
    target_group_name: assignment.target_group_name,
  }))
}

function recalculateAssignments() {
  if (!plan.value) return
  const costs = new Map(plan.value.candidates.map((candidate) => [candidate.user_id, candidate.range_cost]))
  const unmanagedCosts = new Map<number, number>()
  for (const member of plan.value.unmanaged_members ?? []) {
    for (const groupID of member.target_group_ids ?? []) {
      unmanagedCosts.set(groupID, (unmanagedCosts.get(groupID) ?? 0) + member.range_cost)
    }
  }
  for (const assignment of plan.value.assignments) {
    assignment.user_ids = [...new Set(assignment.user_ids ?? [])]
    assignment.total_cost = assignment.user_ids.reduce((total, userID) => total + (costs.get(userID) ?? 0), 0) + (unmanagedCosts.get(assignment.target_group_id ?? 0) ?? 0)
  }
  selectedUserIDs.value = new Set(plan.value.assignments.flatMap((assignment) => assignment.user_ids))
}

function moveCandidate(userID: number, targetIndex: number | null) {
  if (!plan.value) return
  for (const assignment of plan.value.assignments) assignment.user_ids = (assignment.user_ids ?? []).filter((id) => id !== userID)
  if (targetIndex !== null) plan.value.assignments[targetIndex]?.user_ids.push(userID)
  recalculateAssignments()
}

function candidateAssignmentIndex(userID: number): number | null {
  const index = plan.value?.assignments.findIndex((assignment) => assignment.user_ids?.includes(userID)) ?? -1
  return index >= 0 ? index : null
}

function candidateLabel(userID: number): string {
  const candidate = plan.value?.candidates.find((item) => item.user_id === userID)
  return candidate?.username || candidate?.email || `User ${userID}`
}

function toggleUnmanagedRelayUser(relayUserID: number, checked: boolean) {
  const next = new Set(selectedUnmanagedRelayIDs.value)
  if (checked) next.add(relayUserID)
  else next.delete(relayUserID)
  selectedUnmanagedRelayIDs.value = next
}

function departmentSuggestionLabel(item: { name: string; id: string }): string {
  return `${item.name} (${item.id})`
}

async function loadOptions() {
  const [departmentResponse, providerResponse] = await Promise.all([
    listAdminUserDepartmentOptions({ page: 1, page_size: 200 }),
    listAdminUserSubscriptionOptions(),
  ])
  departments.value = departmentResponse.data.data?.items ?? []
  providers.value = providerResponse.data.data?.providers ?? []
  if (!form.provider_id) form.provider_id = providers.value[0]?.id ?? 0
}

async function loadMappings() {
  const response = await listRelayGroupMappings(form.provider_id || undefined)
  mappings.value = response.data.data?.items ?? []
}

async function preview() {
  const request = planningRequest()
  if (!request.provider_id || !request.department_id || !request.platform || !request.template_group_id || !request.source_group_id) {
    ElMessage.warning(t('relayPlanning.requiredFields'))
    return
  }
  loading.value = true
  error.value = ''
  try {
    const response = await previewRelayPlan(request)
    plan.value = response.data.data ?? null
    activeMappingID.value = null
    selectedUnmanagedRelayIDs.value = new Set()
    operationKey.value = crypto.randomUUID()
    recalculateAssignments()
  } catch (err: any) {
    error.value = err.response?.data?.message || err.message || t('relayPlanning.previewFailed')
  } finally {
    loading.value = false
  }
}

async function requestExecution() {
  if (!plan.value) return
  confirming.value = true
  try {
    const selected_user_ids = Array.from(selectedUserIDs.value)
    const response = activeMappingID.value
      ? await previewRelayReplan(activeMappingID.value, { selected_user_ids, assignments: assignmentPayload(), adopt_relay_user_ids: Array.from(selectedUnmanagedRelayIDs.value) })
      : await previewRelayPlan({
          provider_id: plan.value.provider_id,
          department_id: plan.value.department_id,
          platform: plan.value.platform,
          template_group_id: plan.value.template_group_id,
          source_group_id: plan.value.source_group_id,
          weekly_cost_target: plan.value.weekly_cost_target,
          selected_user_ids,
          assignments: assignmentPayload(),
          adopt_relay_user_ids: Array.from(selectedUnmanagedRelayIDs.value),
        })
    plan.value = response.data.data ?? plan.value
    confirmDialogOpen.value = true
  } catch (err: any) {
    ElMessage.error(err.response?.data?.message || err.message || t('relayPlanning.refreshPlanFailed'))
  } finally {
    confirming.value = false
  }
}

async function executeConfirmed() {
  if (!plan.value) return
  executing.value = true
  try {
    const request = {
      provider_id: plan.value.provider_id,
      department_id: plan.value.department_id,
      platform: plan.value.platform,
      template_group_id: plan.value.template_group_id,
      source_group_id: plan.value.source_group_id,
      weekly_cost_target: plan.value.weekly_cost_target,
      selected_user_ids: Array.from(selectedUserIDs.value),
      assignments: assignmentPayload(),
      adopt_relay_user_ids: Array.from(selectedUnmanagedRelayIDs.value),
      operation_key: operationKey.value || crypto.randomUUID(),
    }
    const response = activeMappingID.value
      ? await executeRelayReplan(activeMappingID.value, request)
      : await executeRelayPlan(request)
    plan.value = response.data.data?.plan ?? plan.value
    operationKey.value = request.operation_key
    await loadMappings()
    confirmDialogOpen.value = false
    ElMessage.success(t('relayPlanning.executionFinished'))
  } catch (err: any) {
    ElMessage.error(err.response?.data?.message || err.message || t('relayPlanning.executionFailed'))
  } finally {
    executing.value = false
  }
}

async function replan(mapping: RelayPlanningMapping) {
  try {
    const response = await previewRelayReplan(mapping.id, {})
    plan.value = response.data.data ?? null
    activeMappingID.value = mapping.id
    selectedUnmanagedRelayIDs.value = new Set()
    operationKey.value = crypto.randomUUID()
    recalculateAssignments()
    form.provider_id = mapping.provider_id
    form.department_id = mapping.department_id
    form.platform = mapping.platform
    form.template_group_id = mapping.template_group_id || mapping.source_group_id
    form.source_group_id = mapping.source_group_id
    form.weekly_cost_target = mapping.weekly_cost_target
  } catch (err: any) {
    ElMessage.error(err.response?.data?.message || err.message || t('relayPlanning.replanFailed'))
  }
}

function resetPlan() {
  plan.value = null
  activeMappingID.value = null
  selectedUserIDs.value = new Set()
  selectedUnmanagedRelayIDs.value = new Set()
  operationKey.value = ''
  error.value = ''
  confirming.value = false
  confirmDialogOpen.value = false
}

function toggleCandidate(userID: number, checked: boolean) {
  if (!plan.value) return
  if (!checked) {
    moveCandidate(userID, null)
    return
  }
  const target = plan.value.assignments.reduce((best, assignment, index, all) => assignment.total_cost < all[best].total_cost ? index : best, 0)
  moveCandidate(userID, target)
}

async function rebind(mapping: RelayPlanningMapping) {
  try {
    const department = await ElMessageBox.prompt(t('relayPlanning.departmentIdPrompt'), t('relayPlanning.rebindDepartment'), { inputValue: mapping.department_id, inputPattern: /^[^\s]+$/, inputErrorMessage: t('relayPlanning.departmentIdRequired') })
    const template = await ElMessageBox.prompt(t('relayPlanning.templateGroupIdPrompt'), t('relayPlanning.rebindTemplateGroup'), { inputValue: String(mapping.template_group_id || mapping.source_group_id), inputPattern: /^[1-9][0-9]*$/, inputErrorMessage: t('relayPlanning.positiveGroupIdRequired') })
    const source = await ElMessageBox.prompt(t('relayPlanning.sourceGroupIdPrompt'), t('relayPlanning.rebindSourceGroup'), { inputValue: String(mapping.source_group_id), inputPattern: /^[1-9][0-9]*$/, inputErrorMessage: t('relayPlanning.positiveGroupIdRequired') })
    const groups = await ElMessageBox.prompt(t('relayPlanning.managedGroupIdsPrompt'), t('relayPlanning.rebindManagedGroups'), { inputValue: mapping.group_ids.join(', '), inputPattern: /^[0-9 ,]+$/, inputErrorMessage: t('relayPlanning.numericGroupIdsRequired') })
    const payload = { department_id: department.value.trim(), template_group_id: Number(template.value), source_group_id: Number(source.value), group_ids: groups.value.split(',').map((value) => Number(value.trim())).filter((value) => value > 0) }
    await ElMessageBox.confirm(t('relayPlanning.confirmRebindMessage'), t('relayPlanning.confirmRebind'), { type: 'warning', confirmButtonText: t('relayPlanning.confirm'), cancelButtonText: t('relayPlanning.cancel'), closeOnClickModal: false })
    await rebindRelayGroupMapping(mapping.id, payload)
    await loadMappings()
    ElMessage.success(t('relayPlanning.mappingRebound'))
  } catch (err: any) {
    if (err !== 'cancel' && err !== 'close') ElMessage.error(err.response?.data?.message || err.message || t('relayPlanning.rebindFailed'))
  }
}

onMounted(async () => {
  try {
    await loadOptions()
    await loadMappings()
  } catch (err: any) {
    error.value = err.response?.data?.message || err.message || t('relayPlanning.loadFailed')
  }
})
</script>

<template>
  <AppLayout>
    <div class="mx-auto max-w-7xl space-y-6">
      <header class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div class="flex items-center gap-2 text-xs font-semibold uppercase tracking-wide text-blue-700">
            <el-icon><Connection /></el-icon>
            {{ t('relayPlanning.eyebrow') }}
          </div>
          <h1 class="mt-1 text-2xl font-semibold text-slate-900">{{ t('relayPlanning.title') }}</h1>
          <p class="mt-1 text-sm text-slate-500">{{ t('relayPlanning.subtitle') }}</p>
        </div>
        <el-button :icon="Refresh" :loading="loading" @click="loadMappings">{{ t('relayPlanning.refreshMappings') }}</el-button>
      </header>

      <section class="rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
        <div class="mb-4 flex items-center gap-2 text-sm font-semibold text-slate-900"><el-icon><Setting /></el-icon> {{ t('relayPlanning.inputs') }}</div>
        <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          <el-form-item :label="t('relayPlanning.provider')" class="!mb-0">
            <el-select v-model="form.provider_id" data-testid="provider-select" class="w-full" :placeholder="t('relayPlanning.selectProvider')" @change="resetPlan">
              <el-option v-for="item in providers" :key="item.id" :label="item.display_name || item.name" :value="item.id" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('relayPlanning.department')" class="!mb-0">
            <el-select v-model="form.department_id" data-testid="department-select" class="w-full" filterable :placeholder="t('relayPlanning.selectDepartment')" @change="resetPlan">
              <el-option v-for="item in departments" :key="item.external_id" :label="item.display_path || item.name" :value="item.external_id" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('relayPlanning.platform')" class="!mb-0">
            <el-select v-model="form.platform" data-testid="platform-select" class="w-full" :placeholder="t('relayPlanning.selectPlatform')" @change="form.template_group_id = 0; form.source_group_id = 0; resetPlan()">
              <el-option v-for="item in platforms" :key="item" :label="item" :value="item" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('relayPlanning.templateGroup')" class="!mb-0">
            <el-select v-model="form.template_group_id" data-testid="template-group-select" class="w-full" filterable :placeholder="t('relayPlanning.selectTemplateGroup')" @change="resetPlan">
              <el-option v-for="item in groups" :key="item.group_id" :label="`${item.group_name} (#${item.group_id})`" :value="Number(item.group_id)" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('relayPlanning.migrationSource')" class="!mb-0">
            <el-select v-model="form.source_group_id" data-testid="source-group-select" class="w-full" filterable :placeholder="t('relayPlanning.selectMigrationSource')" @change="resetPlan">
              <el-option v-for="item in groups" :key="item.group_id" :label="`${item.group_name} (#${item.group_id})`" :value="Number(item.group_id)" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('relayPlanning.costTarget')" class="!mb-0">
            <el-input-number v-model="form.weekly_cost_target" data-testid="cost-target-input" class="!w-full" :min="0" :precision="2" controls-position="right" />
          </el-form-item>
        </div>
        <div class="mt-4 flex flex-wrap gap-2">
          <el-button data-testid="preview-allocation" type="primary" :loading="loading" @click="preview">{{ t('relayPlanning.preview') }}</el-button>
          <el-button v-if="plan" data-testid="open-execution-confirmation" :icon="Check" type="success" :loading="confirming" :disabled="plan.group_count === 0 || (!activeMappingID && selectedUserIDs.size === 0 && selectedUnmanagedRelayIDs.size === 0)" @click="requestExecution">{{ t('relayPlanning.confirmExecute') }}</el-button>
        </div>
        <el-alert v-if="error" class="mt-4" type="error" :closable="false" :title="error" />
      </section>

      <section v-if="plan" class="space-y-4">
        <div class="grid gap-4 sm:grid-cols-3">
          <div class="rounded-lg border border-slate-200 bg-white p-4"><div class="text-xs text-slate-500">{{ t('relayPlanning.plannedGroups') }}</div><div class="mt-1 text-2xl font-semibold">{{ plan.group_count }}</div><div v-if="plan.group_count !== plan.recommended_group_count" class="mt-1 text-xs text-slate-500">{{ t('relayPlanning.recommended') }}: {{ plan.recommended_group_count }}</div></div>
          <div class="rounded-lg border border-slate-200 bg-white p-4"><div class="text-xs text-slate-500">{{ t('relayPlanning.selectedEligibleMembers') }}</div><div class="mt-1 text-2xl font-semibold">{{ eligibleCandidates.length }}</div></div>
          <div class="rounded-lg border border-slate-200 bg-white p-4"><div class="text-xs text-slate-500">{{ t('relayPlanning.planningTarget') }}</div><div class="mt-1 text-2xl font-semibold">${{ plan.weekly_cost_target.toFixed(2) }}</div></div>
        </div>
        <el-alert v-if="plan.warnings?.length" type="warning" :closable="false" :title="t('relayPlanning.reviewWarnings')" class="whitespace-pre-line">
          <template #default>{{ plan.warnings.map(translateWarning).join('\n') }}</template>
        </el-alert>
        <div class="rounded-lg border border-slate-200 bg-white p-4">
          <div class="mb-2 text-sm font-semibold text-slate-900">{{ t('relayPlanning.candidatesRank') }}</div>
          <div class="space-y-3 md:hidden">
            <article v-for="candidate in plan.candidates" :key="candidate.user_id" class="rounded-md border border-slate-200 p-3">
              <div class="flex items-start justify-between gap-3">
                <div class="min-w-0"><div class="break-words font-medium text-slate-900">{{ candidate.username || candidate.email }}</div><div class="break-words text-xs text-slate-500">{{ candidate.email }}</div></div>
                <el-checkbox :model-value="selectedUserIDs.has(candidate.user_id)" :disabled="!candidate.can_add" @change="(value) => toggleCandidate(candidate.user_id, value === true)" />
              </div>
              <dl class="mt-3 grid grid-cols-2 gap-x-4 gap-y-2 text-xs"><div><dt class="text-slate-500">{{ t('relayPlanning.cost30d') }}</dt><dd class="font-medium">${{ candidate.range_cost.toFixed(2) }}</dd></div><div><dt class="text-slate-500">{{ t('relayPlanning.tokens30d') }}</dt><dd class="font-medium">{{ candidate.range_tokens }}</dd></div><div><dt class="text-slate-500">{{ t('relayPlanning.globalRank') }}</dt><dd class="font-medium">{{ candidate.global_token_rank || '-' }}</dd></div><div><dt class="text-slate-500">{{ t('relayPlanning.keys') }}</dt><dd class="font-medium">{{ candidate.migratable_key_count }}</dd></div></dl>
              <div class="mt-3"><el-select v-if="candidate.can_add" :model-value="candidateAssignmentIndex(candidate.user_id)" class="w-full" clearable :placeholder="t('relayPlanning.unassigned')" @change="(value) => moveCandidate(candidate.user_id, value === null || value === undefined || value === '' ? null : Number(value))"><el-option v-for="assignment in plan.assignments" :key="assignment.index" :label="assignment.target_group_name || `${t('relayPlanning.group')} ${assignment.index + 1}`" :value="assignment.index" /></el-select><span v-else class="text-xs text-slate-400">{{ t('relayPlanning.notAvailable') }}</span></div>
              <div class="mt-2"><el-tag :type="candidate.eligible ? 'success' : candidate.can_add ? 'warning' : 'info'">{{ candidate.eligible ? t('relayPlanning.eligible') : candidate.can_add ? t('relayPlanning.addOnly') : t('relayPlanning.excluded') }}</el-tag><div v-if="candidate.warnings?.length" class="mt-1 text-xs text-amber-700">{{ candidate.warnings.map(translateWarning).join('; ') }}</div></div>
            </article>
          </div>
          <el-table class="hidden md:block" :data="plan.candidates" stripe>
            <el-table-column :label="t('relayPlanning.select')" width="70"><template #default="scope"><el-checkbox :model-value="selectedUserIDs.has(scope.row.user_id)" :disabled="!scope.row.can_add" @change="(value) => toggleCandidate(scope.row.user_id, value === true)" /></template></el-table-column>
            <el-table-column prop="username" :label="t('relayPlanning.user')" min-width="140" />
            <el-table-column prop="email" :label="t('relayPlanning.email')" min-width="190" />
            <el-table-column prop="range_cost" :label="t('relayPlanning.cost30d')" width="120"><template #default="scope">${{ scope.row.range_cost.toFixed(2) }}</template></el-table-column>
            <el-table-column prop="range_tokens" :label="t('relayPlanning.tokens30d')" width="130" />
            <el-table-column prop="global_token_rank" :label="t('relayPlanning.globalRank')" width="110" />
            <el-table-column prop="migratable_key_count" :label="t('relayPlanning.keys')" width="80" />
            <el-table-column :label="t('relayPlanning.target')" min-width="170"><template #default="scope"><el-select v-if="scope.row.can_add" :model-value="candidateAssignmentIndex(scope.row.user_id)" clearable :placeholder="t('relayPlanning.unassigned')" @change="(value) => moveCandidate(scope.row.user_id, value === null || value === undefined || value === '' ? null : Number(value))"><el-option v-for="assignment in plan.assignments" :key="assignment.index" :label="assignment.target_group_name || `${t('relayPlanning.group')} ${assignment.index + 1}`" :value="assignment.index" /></el-select><span v-else class="text-xs text-slate-400">{{ t('relayPlanning.notAvailable') }}</span></template></el-table-column>
            <el-table-column :label="t('relayPlanning.status')" min-width="180"><template #default="scope"><el-tag :type="scope.row.eligible ? 'success' : scope.row.can_add ? 'warning' : 'info'">{{ scope.row.eligible ? t('relayPlanning.eligible') : scope.row.can_add ? t('relayPlanning.addOnly') : t('relayPlanning.excluded') }}</el-tag><div v-if="scope.row.warnings?.length" class="mt-1 text-xs text-amber-700">{{ scope.row.warnings.map(translateWarning).join('; ') }}</div></template></el-table-column>
          </el-table>
        </div>
        <div class="rounded-lg border border-slate-200 bg-white p-4">
          <div class="mb-2 text-sm font-semibold text-slate-900">{{ t('relayPlanning.proposedGroups') }}</div>
          <div class="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            <div v-for="assignment in plan.assignments" :key="assignment.index" class="rounded-md border border-slate-200 p-3"><div class="flex justify-between gap-3 text-sm font-medium"><span class="min-w-0 break-words">{{ assignment.target_group_name || `${t('relayPlanning.group')} ${assignment.index + 1}` }}<span v-if="assignment.target_group_id" class="text-slate-500"> (#{{ assignment.target_group_id }})</span></span><span class="shrink-0">${{ assignment.total_cost.toFixed(2) }}</span></div><div class="mt-2 text-xs text-slate-500">{{ t('relayPlanning.memberCount', { count: assignment.user_ids?.length ?? 0 }) }}</div><div v-if="assignment.user_ids?.length" class="mt-2 space-y-1 text-sm text-slate-700"><div v-for="userID in assignment.user_ids" :key="userID" class="break-words">{{ candidateLabel(userID) }}</div></div></div>
          </div>
        </div>
        <div class="rounded-lg border border-dashed border-slate-300 bg-slate-50 p-4">
          <div class="mb-2 text-sm font-semibold text-slate-900">{{ t('relayPlanning.unassigned') }}</div>
          <div v-if="unassignedCandidates.length" class="flex flex-wrap gap-2"><el-tag v-for="candidate in unassignedCandidates" :key="candidate.user_id" type="warning">{{ candidate.username || candidate.email }}</el-tag></div>
          <div v-else class="text-sm text-slate-500">{{ t('relayPlanning.noUnassigned') }}</div>
        </div>
        <div v-if="plan.unmanaged_members?.length" class="rounded-lg border border-amber-200 bg-amber-50 p-4">
          <div class="mb-2 text-sm font-semibold text-slate-900">{{ t('relayPlanning.unmanagedMembers') }}</div>
          <p class="mb-3 text-xs text-slate-600">{{ t('relayPlanning.unmanagedMembersHelp') }}</p>
          <div class="space-y-2">
            <label v-for="member in plan.unmanaged_members" :key="member.relay_user_id" class="flex items-start gap-3 rounded-md border border-amber-200 bg-white p-3 text-sm">
              <el-checkbox :model-value="selectedUnmanagedRelayIDs.has(member.relay_user_id)" @change="(value) => toggleUnmanagedRelayUser(member.relay_user_id, value === true)" />
              <span class="min-w-0"><span class="block break-words font-medium">{{ member.username || member.email || `Relay #${member.relay_user_id}` }}</span><span class="block break-words text-xs text-slate-500">{{ member.email }} · ${{ member.range_cost.toFixed(2) }} · {{ t('relayPlanning.targetGroups') }}: {{ member.target_group_ids.join(', ') }}</span></span>
            </label>
          </div>
        </div>
      </section>

      <section class="rounded-lg border border-slate-200 bg-white p-4">
        <div class="mb-2 flex items-center justify-between"><div class="text-sm font-semibold text-slate-900">{{ t('relayPlanning.managedMappings') }}</div><span class="text-xs text-slate-500">{{ t('relayPlanning.groupIdsAuthoritative') }}</span></div>
        <el-empty v-if="!mappings.length" :description="t('relayPlanning.noMappings')" />
        <div v-if="mappings.length" class="space-y-3 md:hidden">
          <article v-for="mapping in mappings" :key="mapping.id" class="rounded-md border border-slate-200 p-3">
            <div class="flex items-start justify-between gap-3"><div class="min-w-0"><div class="break-words font-medium text-slate-900">{{ mapping.department_name || mapping.department_id }}</div><div class="text-xs text-slate-500">{{ mapping.platform }}</div></div><el-tag :type="mapping.warnings?.length || mapping.status === 'needs_retry' ? 'warning' : 'success'">{{ mapping.warnings?.length ? t('relayPlanning.reviewNeeded') : translateMappingStatus(mapping.status) }}</el-tag></div>
            <dl class="mt-3 space-y-2 text-sm"><div><dt class="text-xs text-slate-500">{{ t('relayPlanning.templateGroup') }}</dt><dd class="break-words">{{ mapping.template_group_name || '-' }} (#{{ mapping.template_group_id }})</dd></div><div><dt class="text-xs text-slate-500">{{ t('relayPlanning.migrationSource') }}</dt><dd class="break-words">{{ mapping.source_group_name }} (#{{ mapping.source_group_id }})</dd></div><div><dt class="text-xs text-slate-500">{{ t('relayPlanning.managedGroups') }}</dt><dd class="break-words">{{ mapping.group_ids.join(', ') || '-' }}</dd></div></dl>
            <div v-if="mapping.warnings?.length" class="mt-2 text-xs text-amber-700">{{ mapping.warnings.map(translateWarning).join('; ') }}</div>
            <div v-if="mapping.department_suggestions?.length" class="mt-2 text-xs text-slate-500">{{ t('relayPlanning.departmentSuggestions') }}: {{ mapping.department_suggestions.map(departmentSuggestionLabel).join(', ') }}</div>
            <div class="mt-3 flex flex-wrap gap-2"><el-button size="small" type="primary" @click="replan(mapping)">{{ t('relayPlanning.replan') }}</el-button><el-button size="small" @click="rebind(mapping)">{{ t('relayPlanning.rebind') }}</el-button></div>
          </article>
        </div>
        <el-table v-if="mappings.length" class="hidden md:block" :data="mappings" stripe>
          <el-table-column prop="department_name" :label="t('relayPlanning.department')" min-width="150" />
          <el-table-column prop="platform" :label="t('relayPlanning.platform')" width="110" />
          <el-table-column :label="t('relayPlanning.templateGroup')" min-width="160"><template #default="scope">{{ scope.row.template_group_name || '-' }} (#{{ scope.row.template_group_id }})</template></el-table-column>
          <el-table-column :label="t('relayPlanning.migrationSource')" min-width="160"><template #default="scope">{{ scope.row.source_group_name }} (#{{ scope.row.source_group_id }})</template></el-table-column>
          <el-table-column :label="t('relayPlanning.managedGroups')" min-width="180"><template #default="scope">{{ scope.row.group_ids.join(', ') }}</template></el-table-column>
          <el-table-column :label="t('relayPlanning.status')" min-width="150"><template #default="scope"><el-tag :type="scope.row.warnings?.length || scope.row.status === 'needs_retry' ? 'warning' : 'success'">{{ scope.row.warnings?.length ? t('relayPlanning.reviewNeeded') : translateMappingStatus(scope.row.status) }}</el-tag><div v-if="scope.row.warnings?.length" class="mt-1 text-xs text-amber-700">{{ scope.row.warnings.map(translateWarning).join('; ') }}</div><div v-if="scope.row.department_suggestions?.length" class="mt-1 text-xs text-slate-500">{{ t('relayPlanning.departmentSuggestions') }}: {{ scope.row.department_suggestions.map(departmentSuggestionLabel).join(', ') }}</div></template></el-table-column>
          <el-table-column :label="t('relayPlanning.actions')" width="170"><template #default="scope"><el-button link type="primary" @click="replan(scope.row as RelayPlanningMapping)">{{ t('relayPlanning.replan') }}</el-button><el-button link type="primary" @click="rebind(scope.row as RelayPlanningMapping)">{{ t('relayPlanning.rebind') }}</el-button></template></el-table-column>
        </el-table>
      </section>

      <el-dialog
        v-model="confirmDialogOpen"
        :title="t('relayPlanning.confirmPlan')"
        append-to-body
        align-center
        width="min(100%, 32rem)"
        :close-on-click-modal="!executing"
        :close-on-press-escape="!executing"
      >
        <el-alert type="warning" :closable="false" show-icon :title="t('relayPlanning.executeWarning')" />
        <dl v-if="plan" class="mt-4 grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
          <dt class="text-slate-500">{{ t('relayPlanning.templateGroup') }}</dt><dd class="min-w-0 break-words font-medium text-slate-900">{{ plan.template_group_name }} (#{{ plan.template_group_id }})</dd>
          <dt class="text-slate-500">{{ t('relayPlanning.migrationSource') }}</dt><dd class="min-w-0 break-words font-medium text-slate-900">{{ plan.source_group_name }} (#{{ plan.source_group_id }})</dd>
          <dt class="text-slate-500">{{ t('relayPlanning.members') }}</dt><dd class="font-medium text-slate-900">{{ selectedUserIDs.size }}</dd>
          <dt class="text-slate-500">{{ t('relayPlanning.targetGroups') }}</dt>
          <dd class="max-h-48 space-y-1 overflow-y-auto font-medium text-slate-900">
            <div v-for="assignment in plan.assignments" :key="assignment.index" class="break-words">{{ assignment.target_group_name || `Group ${assignment.index + 1}` }} ({{ t('relayPlanning.memberCount', { count: assignment.user_ids?.length ?? 0 }) }})</div>
          </dd>
        </dl>
        <template #footer>
          <el-button :disabled="executing" @click="confirmDialogOpen = false">{{ t('relayPlanning.cancel') }}</el-button>
          <el-button data-testid="confirm-execution" type="danger" :loading="executing" @click="executeConfirmed">{{ t('relayPlanning.createAndMigrate') }}</el-button>
        </template>
      </el-dialog>
    </div>
  </AppLayout>
</template>
