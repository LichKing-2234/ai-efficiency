<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Check, Connection, Refresh, Setting } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import AppLayout from '@/components/AppLayout.vue'
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

const loading = ref(false)
const executing = ref(false)
const error = ref('')
const plan = ref<RelayPlanningPlan | null>(null)
const activeMappingID = ref<number | null>(null)
const selectedUserIDs = ref<Set<number>>(new Set())
const operationKey = ref('')
const mappings = ref<RelayPlanningMapping[]>([])
const departments = ref<Array<{ external_id: string; name: string; display_path: string }>>([])
const providers = ref<Array<{ id: number; name: string; display_name: string; groups: Array<{ group_id: string; group_name: string; platform: string }> }>>([])

const form = reactive({
  provider_id: 0,
  department_id: '',
  platform: '',
  source_group_id: 0,
  weekly_cost_target: 0,
  group_count: 0,
})

const provider = computed(() => providers.value.find((item) => item.id === form.provider_id))
const groups = computed(() => (provider.value?.groups ?? []).filter((group) => !form.platform || group.platform === form.platform))
const platforms = computed(() => Array.from(new Set((provider.value?.groups ?? []).map((group) => group.platform).filter(Boolean))))
const eligibleCandidates = computed(() => plan.value?.candidates.filter((candidate) => candidate.eligible) ?? [])

function planningRequest(): RelayPlanningRequest {
  return {
    provider_id: Number(form.provider_id),
    department_id: String(form.department_id || ''),
    platform: String(form.platform || ''),
    source_group_id: Number(form.source_group_id),
    weekly_cost_target: Number(form.weekly_cost_target || 0),
    group_count: Number(form.group_count || 0),
  }
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
  if (!request.provider_id || !request.department_id || !request.platform || !request.source_group_id) {
    ElMessage.warning('Provider, department, platform, and source group are required')
    return
  }
  loading.value = true
  error.value = ''
  try {
    const response = await previewRelayPlan(request)
    plan.value = response.data.data ?? null
    activeMappingID.value = null
    operationKey.value = crypto.randomUUID()
    selectedUserIDs.value = new Set((plan.value?.candidates ?? []).filter((candidate) => candidate.eligible).map((candidate) => candidate.user_id))
  } catch (err: any) {
    error.value = err.response?.data?.message || err.message || 'Preview failed'
  } finally {
    loading.value = false
  }
}

async function execute() {
  if (!plan.value) return
  await ElMessageBox.confirm('Create the groups and migrate the selected members now?', 'Confirm group plan', { type: 'warning' })
  executing.value = true
  try {
    const request = {
      provider_id: plan.value.provider_id,
      department_id: plan.value.department_id,
      platform: plan.value.platform,
      source_group_id: plan.value.source_group_id,
      weekly_cost_target: plan.value.weekly_cost_target,
      group_count: plan.value.group_count,
      selected_user_ids: Array.from(selectedUserIDs.value),
      operation_key: operationKey.value || crypto.randomUUID(),
    }
    const response = activeMappingID.value
      ? await executeRelayReplan(activeMappingID.value, request)
      : await executeRelayPlan(request)
    plan.value = response.data.data?.plan ?? plan.value
    operationKey.value = request.operation_key
    await loadMappings()
    ElMessage.success('Execution finished; review per-item results below')
  } catch (err: any) {
    ElMessage.error(err.response?.data?.message || err.message || 'Execution failed')
  } finally {
    executing.value = false
  }
}

async function replan(mapping: RelayPlanningMapping) {
  try {
    const response = await previewRelayReplan(mapping.id, { group_count: mapping.group_ids.length })
    plan.value = response.data.data ?? null
    activeMappingID.value = mapping.id
    operationKey.value = crypto.randomUUID()
    selectedUserIDs.value = new Set((plan.value?.candidates ?? []).filter((candidate) => candidate.eligible).map((candidate) => candidate.user_id))
    form.provider_id = mapping.provider_id
    form.department_id = mapping.department_id
    form.platform = mapping.platform
    form.source_group_id = mapping.source_group_id
    form.weekly_cost_target = mapping.weekly_cost_target
    form.group_count = mapping.group_ids.length
  } catch (err: any) {
    ElMessage.error(err.response?.data?.message || err.message || 'Replan failed')
  }
}

function resetPlan() {
  plan.value = null
  activeMappingID.value = null
  selectedUserIDs.value = new Set()
  operationKey.value = ''
  error.value = ''
}

function toggleCandidate(userID: number, checked: boolean) {
  const next = new Set(selectedUserIDs.value)
  if (checked) next.add(userID)
  else next.delete(userID)
  selectedUserIDs.value = next
}

async function rebind(mapping: RelayPlanningMapping) {
  try {
    const source = await ElMessageBox.prompt('Enter the source Group ID', 'Rebind source Group', { inputValue: String(mapping.source_group_id), inputPattern: /^[1-9][0-9]*$/, inputErrorMessage: 'A positive Group ID is required' })
    const groups = await ElMessageBox.prompt('Enter managed Group IDs separated by commas', 'Rebind managed Groups', { inputValue: mapping.group_ids.join(', '), inputPattern: /^[0-9 ,]+$/, inputErrorMessage: 'Use numeric Group IDs separated by commas' })
    await rebindRelayGroupMapping(mapping.id, { source_group_id: Number(source.value), group_ids: groups.value.split(',').map((value) => Number(value.trim())).filter((value) => value > 0) })
    await loadMappings()
    ElMessage.success('Mapping rebound; no relay members were changed')
  } catch (err: any) {
    if (err !== 'cancel' && err !== 'close') ElMessage.error(err.response?.data?.message || err.message || 'Rebind failed')
  }
}

onMounted(async () => {
  try {
    await loadOptions()
    await loadMappings()
  } catch (err: any) {
    error.value = err.response?.data?.message || err.message || 'Failed to load planning options'
  }
})
</script>

<template>
  <AppLayout>
    <div class="mx-auto max-w-7xl space-y-6">
      <header class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div class="flex items-center gap-2 text-xs font-semibold uppercase tracking-wide text-blue-600">
            <el-icon><Connection /></el-icon>
            Relay planning
          </div>
          <h1 class="mt-1 text-2xl font-semibold text-slate-900">Department Group Allocation</h1>
          <p class="mt-1 text-sm text-slate-500">Preview a 30-day usage allocation, then explicitly confirm relay changes.</p>
        </div>
        <el-button :icon="Refresh" :loading="loading" @click="loadMappings">Refresh mappings</el-button>
      </header>

      <section class="rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
        <div class="mb-4 flex items-center gap-2 text-sm font-semibold text-slate-900"><el-icon><Setting /></el-icon> Planning inputs</div>
        <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          <el-select v-model="form.provider_id" placeholder="Relay provider" @change="resetPlan">
            <el-option v-for="item in providers" :key="item.id" :label="item.display_name || item.name" :value="item.id" />
          </el-select>
          <el-select v-model="form.department_id" filterable placeholder="Department" @change="resetPlan">
            <el-option v-for="item in departments" :key="item.external_id" :label="item.display_path || item.name" :value="item.external_id" />
          </el-select>
          <el-select v-model="form.platform" placeholder="Platform" @change="form.source_group_id = 0; resetPlan()">
            <el-option v-for="item in platforms" :key="item" :label="item" :value="item" />
          </el-select>
          <el-select v-model="form.source_group_id" filterable placeholder="Source group" @change="resetPlan">
            <el-option v-for="item in groups" :key="item.group_id" :label="`${item.group_name} (#${item.group_id})`" :value="Number(item.group_id)" />
          </el-select>
          <el-input-number v-model="form.weekly_cost_target" :min="0" :precision="2" controls-position="right" placeholder="Single-group 30-day cost target" />
          <el-input-number v-model="form.group_count" :min="0" controls-position="right" placeholder="Group count (0 = recommend)" />
        </div>
        <div class="mt-4 flex flex-wrap gap-2">
          <el-button type="primary" :loading="loading" @click="preview">Preview allocation</el-button>
          <el-button v-if="plan" :icon="Check" type="success" :loading="executing" @click="execute">Confirm and execute</el-button>
        </div>
        <el-alert v-if="error" class="mt-4" type="error" :closable="false" :title="error" />
      </section>

      <section v-if="plan" class="space-y-4">
        <div class="grid gap-4 sm:grid-cols-3">
          <div class="rounded-lg border border-slate-200 bg-white p-4"><div class="text-xs text-slate-500">Recommended groups</div><div class="mt-1 text-2xl font-semibold">{{ plan.recommended_group_count }}</div></div>
          <div class="rounded-lg border border-slate-200 bg-white p-4"><div class="text-xs text-slate-500">Selected eligible members</div><div class="mt-1 text-2xl font-semibold">{{ eligibleCandidates.length }}</div></div>
          <div class="rounded-lg border border-slate-200 bg-white p-4"><div class="text-xs text-slate-500">Planning target</div><div class="mt-1 text-2xl font-semibold">${{ plan.weekly_cost_target.toFixed(2) }}</div></div>
        </div>
        <el-alert v-if="plan.warnings?.length" type="warning" :closable="false" title="Review planning warnings" class="whitespace-pre-line">
          <template #default>{{ plan.warnings.join('\n') }}</template>
        </el-alert>
        <div class="rounded-lg border border-slate-200 bg-white p-4">
          <div class="mb-3 text-sm font-semibold text-slate-900">Candidates and global token rank</div>
          <el-table :data="plan.candidates" stripe>
            <el-table-column label="Select" width="70"><template #default="scope"><el-checkbox :model-value="selectedUserIDs.has(scope.row.user_id)" :disabled="!scope.row.eligible" @change="(value) => toggleCandidate(scope.row.user_id, value === true)" /></template></el-table-column>
            <el-table-column prop="username" label="User" min-width="140" />
            <el-table-column prop="email" label="Email" min-width="190" />
            <el-table-column prop="range_cost" label="30-day cost" width="120"><template #default="scope">${{ scope.row.range_cost.toFixed(2) }}</template></el-table-column>
            <el-table-column prop="range_tokens" label="30-day tokens" width="130" />
            <el-table-column prop="global_token_rank" label="Global rank" width="110" />
            <el-table-column prop="migratable_key_count" label="Keys" width="80" />
            <el-table-column label="Status" min-width="180"><template #default="scope"><el-tag :type="scope.row.eligible ? 'success' : 'info'">{{ scope.row.eligible ? 'Eligible' : 'Excluded' }}</el-tag><div v-if="scope.row.warnings?.length" class="mt-1 text-xs text-amber-700">{{ scope.row.warnings.join('; ') }}</div></template></el-table-column>
          </el-table>
        </div>
        <div class="rounded-lg border border-slate-200 bg-white p-4">
          <div class="mb-3 text-sm font-semibold text-slate-900">Proposed groups</div>
          <div class="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            <div v-for="assignment in plan.assignments" :key="assignment.index" class="rounded-md border border-slate-200 p-3"><div class="flex justify-between text-sm font-medium"><span>Group {{ assignment.index + 1 }}</span><span>${{ assignment.total_cost.toFixed(2) }}</span></div><div class="mt-2 text-xs text-slate-500">{{ assignment.user_ids.length }} member(s)</div></div>
          </div>
        </div>
      </section>

      <section class="rounded-lg border border-slate-200 bg-white p-4">
        <div class="mb-3 flex items-center justify-between"><div class="text-sm font-semibold text-slate-900">Managed mappings</div><span class="text-xs text-slate-500">Group IDs are authoritative</span></div>
        <el-empty v-if="!mappings.length" description="No department mappings yet" />
        <el-table v-else :data="mappings" stripe>
          <el-table-column prop="department_name" label="Department" min-width="150" />
          <el-table-column prop="platform" label="Platform" width="110" />
          <el-table-column label="Source" min-width="150"><template #default="scope">{{ scope.row.source_group_name }} (#{{ scope.row.source_group_id }})</template></el-table-column>
          <el-table-column label="Managed groups" min-width="180"><template #default="scope">{{ scope.row.group_ids.join(', ') }}</template></el-table-column>
          <el-table-column prop="status" label="Status" width="100" />
          <el-table-column label="Actions" width="170"><template #default="scope"><el-button link type="primary" @click="replan(scope.row as RelayPlanningMapping)">Replan</el-button><el-button link type="primary" @click="rebind(scope.row as RelayPlanningMapping)">Rebind</el-button></template></el-table-column>
        </el-table>
      </section>
    </div>
  </AppLayout>
</template>
