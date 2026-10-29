<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { CopyDocument, Refresh, Setting } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import AppLayout from '@/components/AppLayout.vue'
import { useI18n } from '@/i18n'
import { relayPlanningMessages } from '@/locales/relayPlanning'
import { listAdminUserSubscriptionOptions } from '@/api/adminUsers'
import {
  applyRelayAccountConfiguration,
  listRelayAccountConfigurations,
  type RelayPlanningAccountConfigurationApplyResult,
  type RelayPlanningAccountConfigurationDiff,
  type RelayPlanningAccountConfigurationItem,
} from '@/api/relayPlanning'
import { createFeatureTranslator } from '@/utils/featureI18n'

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'relayPlanning.', relayPlanningMessages)

const loading = ref(false)
const applying = ref(false)
const error = ref('')
const providers = ref<Array<{ id: number; name: string; display_name: string; groups: Array<{ group_id: string; group_name: string; platform: string }> }>>([])
const items = ref<RelayPlanningAccountConfigurationItem[]>([])
const baselineAccountID = ref(0)
const selectedTargetIDs = ref<Set<number>>(new Set())
const expandedDiffIDs = ref<Set<number>>(new Set())
const onlyDeviating = ref(false)
const accountTypeFilter = ref('')
const groupFilter = ref('')
const resetCreditsFilter = ref('')
const applyDialogOpen = ref(false)
const applyResults = ref<RelayPlanningAccountConfigurationApplyResult[] | null>(null)
const ignoredKeys = ref<Set<string>>(new Set())
// A refresh supersedes reads; only context changes invalidate an in-flight copy.
let comparisonRequest = 0
let comparisonContext = 0

const form = reactive({
  provider_id: 0,
  platform: '',
})

const provider = computed(() => providers.value.find((item) => item.id === form.provider_id))
const platforms = computed(() => Array.from(new Set((provider.value?.groups ?? []).map((group) => group.platform).filter(Boolean))))
const hasBaseline = computed(() => baselineAccountID.value > 0)
// The type, group and reset-credit bucket filters narrow the working set, not
// just the rendering: the deviation count, the select-all action, and the copy
// targets all follow the filters so an administrator can work one account type,
// one group, or one reset-credit count class at a time. The reset-credit count is
// a cached upstream observation, and "unknown" (no snapshot observed yet) is its
// own bucket rather than being folded into zero.
const scopedItems = computed(() => items.value.filter((item) => {
  if (accountTypeFilter.value && item.account.type !== accountTypeFilter.value) return false
  if (groupFilter.value) {
    const groups = item.account.group_relationships ?? []
    if (!groups.some((relationship) => String(relationship.group_id) === groupFilter.value)) return false
  }
  if (resetCreditsFilter.value) {
    const available = item.reset_credits_available
    if (resetCreditsFilter.value === 'unknown') {
      if (available != null) return false
    } else if (available == null) {
      return false
    } else if (resetCreditsFilter.value === '3+') {
      if (available < 3) return false
    } else if (available !== Number(resetCreditsFilter.value)) {
      return false
    }
  }
  return true
}))
const accountTypeOptions = computed(() => Array.from(new Set(items.value.map((item) => item.account.type).filter(Boolean))).sort())
const groupOptions = computed(() => (provider.value?.groups ?? []).filter((group) => !form.platform || group.platform === form.platform))
const deviatingItems = computed(() => scopedItems.value.filter(isDeviating))
const visibleItems = computed(() => (hasBaseline.value && onlyDeviating.value ? deviatingItems.value : scopedItems.value))
const baselineItem = computed(() => items.value.find((item) => item.account.id === baselineAccountID.value) ?? null)
const baselineLabel = computed(() => {
  if (!hasBaseline.value) return t('relayPlanning.noBaseline')
  const item = baselineItem.value
  return item ? `${item.account.name} (#${item.account.id})` : `#${baselineAccountID.value}`
})
const ignoredKeyCount = computed(() => ignoredKeys.value.size)
const baselineConfigurationRows = computed(() => {
  const configuration = baselineItem.value?.configuration
  if (!configuration) return []
  const rows: Array<{ key: string; value: string }> = []
  const mapping = configuration.model_mapping
  if (mapping) {
    rows.push({ key: 'model_mapping', value: t('relayPlanning.mappingEntries', { count: Object.keys(mapping).length }) })
  }
  for (const [key, value] of Object.entries(configuration.extra ?? {})) {
    rows.push({ key: `extra.${key}`, value: typeof value === 'string' ? value : JSON.stringify(value) })
  }
  return rows.sort((left, right) => left.key.localeCompare(right.key))
})
const selectedItems = computed(() => deviatingItems.value.filter((item) => selectedTargetIDs.value.has(item.account.id)))
const allDeviatingSelected = computed(() => deviatingItems.value.length > 0 && deviatingItems.value.every((item) => selectedTargetIDs.value.has(item.account.id)))
const someDeviatingSelected = computed(() => deviatingItems.value.some((item) => selectedTargetIDs.value.has(item.account.id)))
const applyResultRows = computed(() => (applyResults.value ?? []).map((result) => ({
  ...result,
  name: items.value.find((item) => item.account.id === result.account_id)?.account.name ?? '',
})))

function isIgnoredKey(key: string): boolean {
  return ignoredKeys.value.has(key)
}

// Ignored keys stay per-operation: they are sent with the comparison and the
// copy, never persisted, so a later copy starts from the full baseline again.
async function toggleIgnoreKey(key: string, include: boolean) {
  const next = new Set(ignoredKeys.value)
  if (include) next.delete(key)
  else next.add(key)
  ignoredKeys.value = next
  invalidateComparison()
  await loadConfigurations()
}

function isDeviating(item: RelayPlanningAccountConfigurationItem): boolean {
  return Boolean(item.diff) && item.diff?.matches === false
}

function itemByID(accountID: number): RelayPlanningAccountConfigurationItem | null {
  return items.value.find((item) => item.account.id === accountID) ?? null
}

function isDeviatingID(accountID: number): boolean {
  const item = itemByID(accountID)
  return item ? isDeviating(item) : false
}

function deviationCounts(diff: RelayPlanningAccountConfigurationDiff) {
  return {
    missing: diff.missing_keys?.length ?? 0,
    differing: diff.differing_keys?.length ?? 0,
    additional: diff.additional_keys?.length ?? 0,
  }
}

function deviationText(diff: RelayPlanningAccountConfigurationDiff): string {
  return t('relayPlanning.deviationCounts', deviationCounts(diff))
}

// The count is display-only: it comes from Sub2API's cached reset-credit snapshot
// and is never part of the comparison or the copy. An account without a snapshot
// is unknown, shown as a dash rather than as zero.
function resetCreditsLabel(available: number | null | undefined): string {
  return available == null ? '—' : String(available)
}

function resetCreditsTitle(available: number | null | undefined): string {
  return available == null
    ? t('relayPlanning.resetCreditsUnknown')
    : t('relayPlanning.resetCreditsSnapshotHint')
}

function deviationKeyGroups(diff: RelayPlanningAccountConfigurationDiff) {
  return [
    { label: t('relayPlanning.missingKeys'), keys: diff.missing_keys ?? [] },
    { label: t('relayPlanning.differingKeys'), keys: diff.differing_keys ?? [] },
    { label: t('relayPlanning.additionalKeys'), keys: diff.additional_keys ?? [] },
  ].filter((group) => group.keys.length > 0)
}

function accountLabel(accountID: number): string {
  const item = items.value.find((candidate) => candidate.account.id === accountID)
  return item ? `${item.account.name} (#${item.account.id})` : `#${accountID}`
}

function pruneSelection() {
  applyDialogOpen.value = false
  const selectable = new Set(deviatingItems.value.map((item) => item.account.id))
  selectedTargetIDs.value = new Set(Array.from(selectedTargetIDs.value).filter((id) => selectable.has(id)))
}

function invalidateComparison() {
  ++comparisonContext
  ++comparisonRequest
  loading.value = false
  applyDialogOpen.value = false
  selectedTargetIDs.value = new Set()
  applyResults.value = null
}

function resetView() {
  invalidateComparison()
  baselineAccountID.value = 0
  expandedDiffIDs.value = new Set()
  ignoredKeys.value = new Set()
  accountTypeFilter.value = ''
  groupFilter.value = ''
  resetCreditsFilter.value = ''
  items.value = []
  error.value = ''
}

async function loadOptions() {
  const response = await listAdminUserSubscriptionOptions()
  providers.value = response.data.data?.providers ?? []
  if (!form.provider_id) form.provider_id = providers.value[0]?.id ?? 0
}

async function loadConfigurations() {
  const request = ++comparisonRequest
  applyDialogOpen.value = false
  if (!form.provider_id || !form.platform) {
    items.value = []
    return
  }
  loading.value = true
  error.value = ''
  try {
    const response = await listRelayAccountConfigurations({
      provider_id: Number(form.provider_id),
      platform: form.platform,
      ...(baselineAccountID.value > 0 ? { baseline_account_id: baselineAccountID.value } : {}),
      ...(ignoredKeys.value.size > 0 ? { ignore_keys: Array.from(ignoredKeys.value).join(',') } : {}),
    })
    if (request !== comparisonRequest) return
    const data = response.data.data
    items.value = data?.items ?? []
    baselineAccountID.value = Number(data?.baseline_account_id || 0)
    pruneSelection()
  } catch (err: any) {
    if (request !== comparisonRequest) return
    items.value = []
    error.value = err.response?.data?.message || err.message || t('relayPlanning.accountConfigLoadFailed')
  } finally {
    if (request === comparisonRequest) loading.value = false
  }
}

function handleProviderChange() {
  form.platform = ''
  resetView()
}

function handlePlatformChange() {
  resetView()
  void loadConfigurations()
}

async function setBaseline(accountID: number) {
  if (baselineAccountID.value === accountID) return
  invalidateComparison()
  baselineAccountID.value = accountID
  expandedDiffIDs.value = new Set()
  ignoredKeys.value = new Set()
  await loadConfigurations()
}

async function clearBaseline() {
  invalidateComparison()
  baselineAccountID.value = 0
  expandedDiffIDs.value = new Set()
  ignoredKeys.value = new Set()
  onlyDeviating.value = false
  await loadConfigurations()
}

function isTargetSelected(accountID: number): boolean {
  return selectedTargetIDs.value.has(accountID)
}

// Mirrors the server's MaxAccountConfigurationCopyTargets. The endpoint rejects a
// larger selection outright, so the selection stops here and says why.
const MAX_COPY_TARGETS = 100

function toggleTarget(accountID: number, checked: boolean) {
  if (loading.value || applying.value || !isDeviatingID(accountID)) return
  applyDialogOpen.value = false
  const next = new Set(selectedTargetIDs.value)
  if (checked) {
    if (!next.has(accountID) && next.size >= MAX_COPY_TARGETS) {
      ElMessage.warning(t('relayPlanning.copyTargetLimit', { count: MAX_COPY_TARGETS }))
      return
    }
    next.add(accountID)
  } else {
    next.delete(accountID)
  }
  selectedTargetIDs.value = next
}

function toggleSelectAllDeviating(checked: boolean) {
  if (loading.value || applying.value) return
  applyDialogOpen.value = false
  if (!checked) {
    selectedTargetIDs.value = new Set()
    return
  }
  const selected = deviatingItems.value.slice(0, MAX_COPY_TARGETS).map((item) => item.account.id)
  if (deviatingItems.value.length > MAX_COPY_TARGETS) {
    ElMessage.warning(t('relayPlanning.copyTargetLimit', { count: MAX_COPY_TARGETS }))
  }
  selectedTargetIDs.value = new Set(selected)
}

function toggleDiffKeys(accountID: number) {
  const next = new Set(expandedDiffIDs.value)
  if (next.has(accountID)) next.delete(accountID)
  else next.add(accountID)
  expandedDiffIDs.value = next
}

function openApplyDialog() {
  if (loading.value || applying.value) return
  if (!hasBaseline.value) {
    ElMessage.warning(t('relayPlanning.baselineRequired'))
    return
  }
  if (selectedItems.value.length === 0) {
    ElMessage.warning(t('relayPlanning.targetsRequired'))
    return
  }
  applyDialogOpen.value = true
}

async function confirmApply() {
  if (!applyDialogOpen.value || loading.value || applying.value || !hasBaseline.value || selectedItems.value.length === 0) return
  const context = comparisonContext
  applying.value = true
  try {
    const response = await applyRelayAccountConfiguration({
      provider_id: Number(form.provider_id),
      platform: form.platform,
      baseline_account_id: baselineAccountID.value,
      target_account_ids: selectedItems.value.map((item) => item.account.id),
      ...(ignoredKeys.value.size > 0 ? { ignore_keys: Array.from(ignoredKeys.value) } : {}),
    })
    if (context !== comparisonContext) return
    const results = response.data.data?.results ?? []
    applyResults.value = results
    applyDialogOpen.value = false
    selectedTargetIDs.value = new Set()
    const applied = results.filter((result) => result.applied).length
    const failed = results.length - applied
    if (failed > 0) ElMessage.warning(t('relayPlanning.copyFinished', { applied, failed }))
    else ElMessage.success(t('relayPlanning.copyFinished', { applied, failed }))
    await loadConfigurations()
  } catch (err: any) {
    if (context !== comparisonContext) return
    ElMessage.error(err.response?.data?.message || err.message || t('relayPlanning.copyFailedMessage'))
  } finally {
    applying.value = false
  }
}

onUnmounted(invalidateComparison)

onMounted(async () => {
  try {
    await loadOptions()
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
            <el-icon><CopyDocument /></el-icon>
            {{ t('relayPlanning.accountConfigEyebrow') }}
          </div>
          <h1 class="mt-1 text-2xl font-semibold text-slate-900">{{ t('relayPlanning.accountConfigTitle') }}</h1>
          <p class="mt-1 text-sm text-slate-500">{{ t('relayPlanning.accountConfigSubtitle') }}</p>
        </div>
        <el-button data-testid="refresh-accounts" :icon="Refresh" :loading="loading" :disabled="!form.platform" @click="loadConfigurations">{{ t('relayPlanning.accountConfigRefresh') }}</el-button>
      </header>

      <section class="rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
        <div class="mb-4 flex items-center gap-2 text-sm font-semibold text-slate-900"><el-icon><Setting /></el-icon> {{ t('relayPlanning.inputs') }}</div>
        <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          <el-form-item :label="t('relayPlanning.provider')" class="!mb-0">
            <el-select v-model="form.provider_id" data-testid="provider-select" class="w-full" :placeholder="t('relayPlanning.selectProvider')" @change="handleProviderChange">
              <el-option v-for="item in providers" :key="item.id" :label="item.display_name || item.name" :value="item.id" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('relayPlanning.platform')" class="!mb-0">
            <el-select v-model="form.platform" data-testid="platform-select" class="w-full" :placeholder="t('relayPlanning.selectPlatform')" @change="handlePlatformChange">
              <el-option v-for="item in platforms" :key="item" :label="item" :value="item" />
            </el-select>
          </el-form-item>
        </div>
        <el-alert v-if="error" class="mt-4" type="error" :closable="false" :title="error" />
      </section>

      <section v-if="!form.platform" class="rounded-lg border border-slate-200 bg-white p-5 text-sm text-slate-500">
        {{ t('relayPlanning.accountConfigNoPlatform') }}
      </section>

      <section v-else class="space-y-4">
        <div class="rounded-lg border border-slate-200 bg-white p-4">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div class="min-w-0">
              <div class="text-xs text-slate-500">{{ t('relayPlanning.baselineAccount') }}</div>
              <div class="mt-1 break-words text-sm font-medium text-slate-900" data-testid="baseline-account">{{ baselineLabel }}</div>
            </div>
            <el-button v-if="hasBaseline" data-testid="clear-baseline" size="small" @click="clearBaseline">{{ t('relayPlanning.clearBaseline') }}</el-button>
          </div>
          <div v-if="baselineConfigurationRows.length > 0" class="mt-3 border-t border-slate-100 pt-3" data-testid="baseline-configuration">
            <div class="text-xs text-slate-500">{{ t('relayPlanning.baselineConfiguration') }}</div>
            <div class="mt-1 text-xs text-slate-500">{{ t('relayPlanning.baselineConfigurationHint') }}</div>
            <dl class="mt-2 grid grid-cols-1 gap-x-6 gap-y-1 sm:grid-cols-2">
              <div v-for="row in baselineConfigurationRows" :key="row.key" class="flex min-w-0 items-center gap-2">
                <dt class="shrink-0 font-mono text-xs text-slate-500">{{ row.key }}</dt>
                <dd class="min-w-0 break-words text-xs text-slate-900">{{ row.value }}</dd>
                <el-checkbox
                  class="ml-auto shrink-0"
                  :data-testid="`include-key-${row.key}`"
                  :model-value="!isIgnoredKey(row.key)"
                  @change="(value) => toggleIgnoreKey(row.key, value === true)"
                >{{ t('relayPlanning.includeKey') }}</el-checkbox>
              </div>
            </dl>
          </div>
        </div>

        <div class="rounded-lg border border-slate-200 bg-white p-4">
          <div class="mb-3 flex flex-wrap items-center justify-between gap-3">
            <div class="flex flex-wrap items-center gap-x-6 gap-y-2">
              <div class="flex flex-wrap items-center gap-2">
                <el-select v-model="accountTypeFilter" data-testid="account-type-filter" size="small" class="w-36" @change="pruneSelection">
                  <el-option :label="t('relayPlanning.allAccountTypes')" value="" />
                  <el-option v-for="type in accountTypeOptions" :key="type" :label="type" :value="type" />
                </el-select>
                <el-select v-model="groupFilter" data-testid="group-filter" size="small" class="w-44" @change="pruneSelection">
                  <el-option :label="t('relayPlanning.allGroups')" value="" />
                  <el-option v-for="group in groupOptions" :key="group.group_id" :label="group.group_name" :value="String(group.group_id)" />
                </el-select>
                <el-select v-model="resetCreditsFilter" data-testid="reset-credits-filter" size="small" class="w-40" @change="pruneSelection">
                  <el-option :label="t('relayPlanning.allResetCredits')" value="" />
                  <el-option :label="t('relayPlanning.resetCreditsNone')" value="0" />
                  <el-option :label="t('relayPlanning.resetCreditsOne')" value="1" />
                  <el-option :label="t('relayPlanning.resetCreditsTwo')" value="2" />
                  <el-option :label="t('relayPlanning.resetCreditsThreePlus')" value="3+" />
                  <el-option :label="t('relayPlanning.resetCreditsUnknown')" value="unknown" />
                </el-select>
              </div>
              <el-checkbox data-testid="only-deviating-toggle" :model-value="onlyDeviating" :disabled="!hasBaseline" @change="(value) => (onlyDeviating = value === true)">{{ t('relayPlanning.onlyDeviating') }}</el-checkbox>
              <el-checkbox
                v-if="hasBaseline"
                data-testid="select-all-deviating"
                :model-value="allDeviatingSelected"
                :indeterminate="someDeviatingSelected && !allDeviatingSelected"
                :disabled="loading || applying || deviatingItems.length === 0"
                @change="(value) => toggleSelectAllDeviating(value === true)"
              >
                {{ t('relayPlanning.selectAllDeviating') }}
              </el-checkbox>
            </div>
            <div v-if="hasBaseline" class="text-xs text-slate-500" data-testid="deviation-summary">
              {{ t('relayPlanning.deviatingAccounts', { count: deviatingItems.length }) }} · {{ t('relayPlanning.selectedTargets', { count: selectedTargetIDs.size }) }}
            </div>
          </div>

          <el-table data-testid="account-config-table" :data="visibleItems" stripe :row-key="(row) => String(row.account.id)">
            <el-table-column :label="t('relayPlanning.baseline')" width="150">
              <template #default="scope">
                <el-tag v-if="scope.row.account.id === baselineAccountID" type="warning" data-testid="baseline-tag">{{ t('relayPlanning.baseline') }}</el-tag>
                <el-button v-else size="small" :data-testid="`set-baseline-${scope.row.account.id}`" @click="setBaseline(scope.row.account.id)">{{ t('relayPlanning.setAsBaseline') }}</el-button>
              </template>
            </el-table-column>
            <el-table-column :label="t('relayPlanning.accountName')" min-width="200">
              <template #default="scope">
                <div :data-testid="`account-row-${scope.row.account.id}`">
                  <div class="break-words font-medium text-slate-900">{{ scope.row.account.name }}</div>
                  <div class="text-xs text-slate-500">#{{ scope.row.account.id }}</div>
                </div>
              </template>
            </el-table-column>
            <el-table-column :label="t('relayPlanning.accountType')" width="110">
              <template #default="scope">{{ scope.row.account.type }}</template>
            </el-table-column>
            <el-table-column :label="t('relayPlanning.status')" width="110">
              <template #default="scope">{{ scope.row.account.status }}</template>
            </el-table-column>
            <el-table-column :label="t('relayPlanning.schedulable')" width="140">
              <template #default="scope">
                <el-tag :type="scope.row.account.schedulable ? 'success' : 'info'">{{ scope.row.account.schedulable ? t('relayPlanning.schedulableYes') : t('relayPlanning.schedulableNo') }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('relayPlanning.resetCredits')" width="120">
              <template #default="scope">
                <span :data-testid="`reset-credits-${scope.row.account.id}`" :title="resetCreditsTitle(scope.row.reset_credits_available)" class="tabular-nums">{{ resetCreditsLabel(scope.row.reset_credits_available) }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('relayPlanning.deviation')" min-width="280">
              <template #default="scope">
                <div v-if="!scope.row.diff" class="text-xs text-slate-400">-</div>
                <div v-else-if="scope.row.diff.matches" :data-testid="`deviation-match-${scope.row.account.id}`">
                  <el-tag type="success">{{ t('relayPlanning.matchesBaseline') }}</el-tag>
                </div>
                <div v-else :data-testid="`deviation-diff-${scope.row.account.id}`">
                  <div class="font-medium text-amber-700" :data-testid="`deviation-counts-${scope.row.account.id}`">{{ deviationText(scope.row.diff) }}</div>
                  <el-button link type="primary" size="small" :data-testid="`toggle-keys-${scope.row.account.id}`" @click="toggleDiffKeys(scope.row.account.id)">
                    {{ expandedDiffIDs.has(scope.row.account.id) ? t('relayPlanning.hideDeviationKeys') : t('relayPlanning.viewDeviationKeys') }}
                  </el-button>
                  <ul v-if="expandedDiffIDs.has(scope.row.account.id)" class="mt-1 space-y-1 text-xs text-slate-600" :data-testid="`deviation-keys-${scope.row.account.id}`">
                    <li v-for="group in deviationKeyGroups(scope.row.diff)" :key="group.label">
                      <span class="font-medium text-slate-700">{{ group.label }}:</span>
                      <span class="break-all">{{ group.keys.join(', ') }}</span>
                    </li>
                  </ul>
                </div>
              </template>
            </el-table-column>
            <el-table-column :label="t('relayPlanning.select')" width="90">
              <template #default="scope">
                <el-checkbox
                  :data-testid="`select-target-${scope.row.account.id}`"
                  :model-value="isTargetSelected(scope.row.account.id)"
                  :disabled="loading || applying || !isDeviatingID(scope.row.account.id)"
                  @change="(value) => toggleTarget(scope.row.account.id, value === true)"
                />
              </template>
            </el-table-column>
            <template #empty>{{ t('relayPlanning.accountConfigNoAccounts') }}</template>
          </el-table>

          <div class="mt-4 flex flex-wrap items-center gap-2">
            <el-button data-testid="open-apply-dialog" type="primary" :icon="CopyDocument" :disabled="loading || applying || !hasBaseline || selectedItems.length === 0" @click="openApplyDialog">{{ t('relayPlanning.copyBaselineConfig') }}</el-button>
            <span class="text-xs text-slate-500">{{ t('relayPlanning.selectedTargets', { count: selectedTargetIDs.size }) }}</span>
          </div>
        </div>

        <div v-if="applyResults" class="rounded-lg border border-slate-200 bg-white p-4" data-testid="apply-results">
          <div class="text-sm font-semibold text-slate-900">{{ t('relayPlanning.copyConfigResultTitle') }}</div>
          <div class="mt-1 text-xs text-slate-500">{{ t('relayPlanning.copyAdjusted') }}</div>
          <ul class="mt-3 divide-y divide-slate-200">
            <li v-for="result in applyResultRows" :key="result.account_id" class="flex flex-wrap items-center gap-2 py-2 text-sm" :data-testid="`apply-result-${result.account_id}`">
              <span class="font-medium text-slate-900">{{ result.name ? `${result.name} ` : '' }}#{{ result.account_id }}</span>
              <el-tag :type="result.applied ? 'success' : 'danger'">{{ result.applied ? t('relayPlanning.copyApplied') : t('relayPlanning.copyFailed') }}</el-tag>
              <span v-if="!result.applied" class="break-words text-xs text-red-600">{{ result.error || t('relayPlanning.copyFailedMessage') }}</span>
            </li>
          </ul>
        </div>
      </section>

      <el-dialog
        v-model="applyDialogOpen"
        data-testid="apply-dialog"
        :title="t('relayPlanning.confirmCopyConfig')"
        append-to-body
        align-center
        width="min(100%, 34rem)"
        :close-on-click-modal="!applying"
        :close-on-press-escape="!applying"
      >
        <el-alert type="warning" :closable="false" show-icon :title="t('relayPlanning.copyConfigWarning')" />
        <el-alert class="mt-3" type="info" :closable="false" :title="t('relayPlanning.copyConfigCredentialNotice')" />
        <dl class="mt-4 grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
          <dt class="text-slate-500">{{ t('relayPlanning.baselineAccount') }}</dt>
          <dd class="min-w-0 break-words font-medium text-slate-900" data-testid="confirm-baseline">{{ baselineLabel }}</dd>
          <dt class="text-slate-500">{{ t('relayPlanning.targetAccounts') }}</dt>
          <dd class="min-w-0 break-words font-medium text-slate-900" data-testid="confirm-target-count">{{ t('relayPlanning.selectedTargets', { count: selectedItems.length }) }}</dd>
          <template v-if="ignoredKeyCount > 0">
            <dt class="text-slate-500">{{ t('relayPlanning.ignoredKeys') }}</dt>
            <dd class="min-w-0 break-words font-medium text-slate-900" data-testid="confirm-ignored-keys">{{ t('relayPlanning.ignoredKeysCount', { count: ignoredKeyCount }) }}</dd>
          </template>
        </dl>
        <ul class="mt-3 max-h-48 space-y-1 overflow-y-auto text-sm text-slate-700" data-testid="confirm-target-list">
          <li v-for="item in selectedItems" :key="item.account.id" class="break-words">{{ accountLabel(item.account.id) }}</li>
        </ul>
        <template #footer>
          <el-button :disabled="applying" @click="applyDialogOpen = false">{{ t('relayPlanning.cancel') }}</el-button>
          <el-button data-testid="confirm-apply" type="primary" :loading="applying" @click="confirmApply">{{ t('relayPlanning.confirm') }}</el-button>
        </template>
      </el-dialog>
    </div>
  </AppLayout>
</template>
