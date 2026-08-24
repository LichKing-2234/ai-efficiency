<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { Calendar, CaretBottom, CaretTop, Check, Delete, Plus, Refresh, Setting, Switch } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import AppLayout from '@/components/AppLayout.vue'
import AdminDepartmentPicker from '@/components/admin/AdminDepartmentPicker.vue'
import { useI18n } from '@/i18n'
import { relayPlanningMessages } from '@/locales/relayPlanning'
import { listAdminUserSubscriptionOptions } from '@/api/adminUsers'
import {
	adoptCurrentRelayAccounts,
  executeRelayMappingRenewal,
  executeRelayPlan,
  executeRelayReplan,
  listRelayGroupMappings,
  previewRelayMappingRenewal,
  previewRelayPlan,
  previewRelayReplan,
  rebindRelayGroupMapping,
  saveRelayDesiredAccounts,
  searchRelayPlanningAccounts,
  searchRelayPlanningUsers,
  type RelayPlanningAccount,
  type RelayPlanningAccountIntent,
	type RelayPlanningExecution,
  type RelayPlanningRequest,
  type RelayPlanningMapping,
	type RelayPlanningMappingRenewalExecution,
	type RelayPlanningMappingRenewalMember,
	type RelayPlanningMappingRenewalReviewedMember,
	type RelayPlanningMappingRenewalPreview,
	type RelayPlanningMemberAction,
  type RelayPlanningPlan,
	type RelayPlanningTargetSummary,
  type RelayPlanningUserSearchItem,
} from '@/api/relayPlanning'
import { createFeatureTranslator } from '@/utils/featureI18n'
import { useWideContentLayout } from '@/composables/useMediaQuery'

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'relayPlanning.', relayPlanningMessages)
const wideContentLayout = useWideContentLayout()

const loading = ref(false)
const confirming = ref(false)
const executing = ref(false)
const confirmDialogOpen = ref(false)
const error = ref('')
const plan = ref<RelayPlanningPlan | null>(null)
const lastExecution = ref<RelayPlanningExecution | null>(null)
const activeMappingID = ref<number | null>(null)
const selectedUserIDs = ref<Set<number>>(new Set())
const selectedUnmanagedRelayIDs = ref<Set<number>>(new Set())
const removedUserIDs = ref<Set<number>>(new Set())
const memberActions = ref<Record<string, RelayPlanningMemberAction>>({})
const managedAssignmentsByUser = ref<Record<string, NonNullable<RelayPlanningUserSearchItem['managed_assignments']>>>({})
const memberSources = ref<Record<string, number>>({})
const targetSearchQueries = reactive<Record<number, string>>({})
const targetSearchResults = reactive<Record<number, RelayPlanningUserSearchItem[]>>({})
const targetSearchLoading = reactive<Record<number, boolean>>({})
const targetSearchPages = reactive<Record<number, { total: number; page: number; page_size: number }>>({})
const searchDelayMS = 300
const targetSearchTimers = new Map<number, ReturnType<typeof setTimeout>>()
const targetSearchRequestIDs = new Map<number, number>()
const operationKey = ref('')
const suggestedGroupAccountDefaults = ref<RelayPlanningAccount[]>([])
const mappings = ref<RelayPlanningMapping[]>([])
const mappingPage = ref(1)
const mappingPageSize = 10
const renewalDialogOpen = ref(false)
const renewalLoadingID = ref<number | null>(null)
const renewalPreviewLoading = ref(false)
const renewalMappingID = ref<number | null>(null)
const renewalDays = ref(365)
const renewalPreview = ref<RelayPlanningMappingRenewalPreview | null>(null)
const selectedRenewalUserIDs = ref<Set<number>>(new Set())
const renewalExecuting = ref(false)
const renewalExecution = ref<RelayPlanningMappingRenewalExecution | null>(null)
const renewalOperationKey = ref('')
const renewalReviewNotice = ref('')
const rebindPendingID = ref<number | null>(null)
const rebindDialogOpen = ref(false)
const rebindMappingID = ref<number | null>(null)
const rebindContext = reactive({ provider_id: 0, platform: '' })
const rebindForm = reactive({ department_id: '', template_group_id: 0, source_group_id: 0, group_ids: [] as number[] })
const accountMappingID = ref<number | null>(null)
const accountSaving = ref(false)
const accountDrafts = reactive<Record<number, Record<string, RelayPlanningAccount[]>>>({})
const accountSearchQueries = reactive<Record<string, string>>({})
const accountSearchResults = reactive<Record<string, RelayPlanningAccount[]>>({})
const accountSearchLoading = reactive<Record<string, boolean>>({})
const accountSearchTimers = new Map<string, ReturnType<typeof setTimeout>>()
const accountSearchRequestIDs = new Map<string, number>()
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
const displayedEligibleMemberCount = computed(() => plan.value?.candidates.filter((candidate) => candidate.eligible && (!activeMappingID.value || selectedUserIDs.value.has(candidate.user_id))).length ?? 0)
const unassignedCandidates = computed(() => plan.value?.candidates.filter((candidate) => candidate.can_add && !selectedUserIDs.value.has(candidate.user_id)) ?? [])
const accountMapping = computed(() => mappings.value.find((mapping) => mapping.id === accountMappingID.value) ?? null)
const paginatedMappings = computed(() => {
  const start = (mappingPage.value - 1) * mappingPageSize
  return mappings.value.slice(start, start + mappingPageSize)
})
const rebindGroups = computed(() => (providers.value.find((item) => item.id === rebindContext.provider_id)?.groups ?? [])
  .filter((group) => group.platform === rebindContext.platform))
const targetNameErrors = computed(() => Object.fromEntries((plan.value?.assignments ?? []).map((assignment) => [assignment.index, validateTargetName(assignment.index)])))
const hasTargetNameErrors = computed(() => Object.values(targetNameErrors.value).some(Boolean))
const failedRenewalMembers = computed(() => renewalExecution.value?.members.filter((member) => member.status === 'failed') ?? [])

function translateWarning(warning: string): string {
  void locale.value
  if (warning === 'no eligible member has a valid relay mapping and source-group membership') return t('relayPlanning.warningNoEligible')
  if (warning === 'user is not a member of the selected source group') return t('relayPlanning.warningNotSourceMember')
  if (warning === 'no migratable AE-managed API key') return t('relayPlanning.warningNoMigratableKey')
  if (warning === '30-day usage is unknown; capacity may be underestimated') return t('relayPlanning.warningUnknownUsage')
  if (warning === 'user belongs to multiple departments') return t('relayPlanning.warningMultipleDepartments')
  if (warning.includes(' has no relay mapping')) return t('relayPlanning.warningNoRelayMapping', { user: warning.replace(/ has no relay mapping$/, '') })
  if (warning.startsWith('relay groups unavailable: ')) return `${t('relayPlanning.warningRelayGroupsUnavailable')}: ${warning.slice('relay groups unavailable: '.length)}`
  const unavailable = warning.match(/^(template|migration source|target) group (\d+) is unavailable$/)
  if (unavailable) return t(`relayPlanning.warningUnavailable${unavailable[1] === 'template' ? 'Template' : unavailable[1] === 'migration source' ? 'Source' : 'Target'}`, { id: unavailable[2] })
  const conflict = warning.match(/^user (\d+) is assigned in multiple mappings$/)
  if (conflict) return t('relayPlanning.warningMappingConflict', { user: conflict[1] })
  const multipleAccounts = warning.match(/^target group (\d+) has multiple Accounts$/)
  if (multipleAccounts) return t('relayPlanning.warningMultipleTargetAccounts', { group: multipleAccounts[1] })
  const reusedAccount = warning.match(/^account (\d+) is reused across target groups ([0-9, ]+)$/)
  if (reusedAccount) return t('relayPlanning.warningReusedAccount', { account: reusedAccount[1], groups: reusedAccount[2].split(',').map((id) => `#${id.trim()}`).join(', ') })
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

function renameResultText(status?: string): string {
	if (status === 'succeeded') return t('relayPlanning.renameSucceeded')
	if (status === 'failed') return t('relayPlanning.renameNeedsRetry')
	return t('relayPlanning.renameSkipped')
}

function summaryUser(userID?: number, relayUserID?: number): string {
	if (userID) return t('relayPlanning.userNumber', { id: userID })
	return t('relayPlanning.relayUserNumber', { id: relayUserID ?? 0 })
}

function summaryGroup(groupID: number | undefined, summary: RelayPlanningTargetSummary): string {
	return groupID ? t('relayPlanning.groupNumber', { id: groupID }) : summary.target_group_name
}

function accountChangeText(change: RelayPlanningTargetSummary['accounts'][number]): string {
	if (change.action === 'add') return t('relayPlanning.accountAddEffect', { account: change.account_id, priority: change.new_priority ?? 0 })
	if (change.action === 'remove') return t('relayPlanning.accountRemoveEffect', { account: change.account_id })
	return t('relayPlanning.accountReorderEffect', { account: change.account_id, old: change.old_priority ?? 0, next: change.new_priority ?? 0 })
}

function memberChangeText(change: RelayPlanningTargetSummary['members'][number], summary: RelayPlanningTargetSummary): string {
	const user = summaryUser(change.user_id, change.relay_user_id)
	if (change.action === 'add') return t('relayPlanning.memberAddEffect', { user, target: summaryGroup(change.to_group_id, summary) })
	if (change.action === 'remove') return t('relayPlanning.memberRemoveEffect', { user, source: summaryGroup(change.from_group_id, summary) })
	return t('relayPlanning.memberMoveEffect', { user, source: summaryGroup(change.from_group_id, summary), target: summaryGroup(change.to_group_id, summary) })
}

function subscriptionChangeText(change: RelayPlanningTargetSummary['subscriptions'][number], summary: RelayPlanningTargetSummary): string {
	return t(change.action === 'add' ? 'relayPlanning.subscriptionAddEffect' : 'relayPlanning.subscriptionRemoveEffect', {
		group: summaryGroup(change.group_id, summary),
		user: summaryUser(change.user_id, change.relay_user_id),
	})
}

function apiKeyChangeText(change: RelayPlanningTargetSummary['api_keys'][number], summary: RelayPlanningTargetSummary): string {
	return t('relayPlanning.apiKeyMoveEffect', {
		count: change.count,
		source: summaryGroup(change.from_group_id, summary),
		target: summaryGroup(change.to_group_id, summary),
		user: summaryUser(change.user_id, change.relay_user_id),
	})
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
		rename_selected: Boolean(assignment.rename_selected),
		desired_accounts: (assignment.accounts ?? []).map((account, index) => ({ account_id: account.id, priority: Number(account.priority || index + 1) })),
		accounts: [],
  }))
}

function validateTargetName(targetIndex: number): string {
	const assignment = plan.value?.assignments.find((item) => item.index === targetIndex)
	if (!assignment) return ''
	const name = String(assignment.target_group_name || '').trim()
	if (!name) return t('relayPlanning.targetNameRequired')
	if (Array.from(name).length > 100) return t('relayPlanning.targetNameTooLong')
	if ((plan.value?.assignments ?? []).some((item) => item.index !== targetIndex && String(item.target_group_name || '').trim() === name)) return t('relayPlanning.targetNameDuplicate')
	if ((provider.value?.groups ?? []).some((group) => group.group_name === name && Number(group.group_id) !== Number(assignment.target_group_id || 0))) return t('relayPlanning.targetNameOccupied')
	return ''
}

function toggleTargetRename(targetIndex: number, checked: boolean) {
	const assignment = plan.value?.assignments.find((item) => item.index === targetIndex)
	if (!assignment?.target_group_id) return
	assignment.rename_selected = checked
	assignment.target_group_name = checked ? assignment.suggested_target_group_name || assignment.current_target_group_name || '' : assignment.current_target_group_name || ''
}

function applyAllTargetNames() {
	for (const assignment of plan.value?.assignments ?? []) {
		if (!assignment.target_group_id) continue
		assignment.rename_selected = true
		assignment.target_group_name = assignment.suggested_target_group_name || assignment.current_target_group_name || ''
	}
}

function memberSourcesPayload(userIDs = selectedUserIDs.value): Record<string, number> {
  return Object.fromEntries(Array.from(userIDs).map((userID) => [String(userID), Number(memberSources.value[String(userID)] || 0)]))
}

function clearSearchState() {
	for (const timer of targetSearchTimers.values()) clearTimeout(timer)
	for (const timer of accountSearchTimers.values()) clearTimeout(timer)
	targetSearchTimers.clear()
	accountSearchTimers.clear()
	targetSearchRequestIDs.clear()
	accountSearchRequestIDs.clear()
	for (const key of Object.keys(targetSearchQueries)) delete targetSearchQueries[Number(key)]
	for (const key of Object.keys(targetSearchResults)) delete targetSearchResults[Number(key)]
	for (const key of Object.keys(targetSearchLoading)) delete targetSearchLoading[Number(key)]
	for (const key of Object.keys(targetSearchPages)) delete targetSearchPages[Number(key)]
	for (const key of Object.keys(accountSearchQueries)) delete accountSearchQueries[key]
	for (const key of Object.keys(accountSearchResults)) delete accountSearchResults[key]
	for (const key of Object.keys(accountSearchLoading)) delete accountSearchLoading[key]
}

function applyPlan(next: RelayPlanningPlan | null) {
	clearSearchState()
  plan.value = next
  if (!next) return
	for (const assignment of next.assignments) {
		assignment.accounts ??= []
		assignment.desired_accounts = assignment.accounts.map((account, index) => ({ account_id: account.id, priority: Number(account.priority || index + 1) }))
	}
  const nextSources: Record<string, number> = {}
  for (const candidate of next.candidates) nextSources[String(candidate.user_id)] = Number(candidate.source_group_id || 0)
  memberSources.value = nextSources
  recalculateAssignments()
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

function addSuggestedGroup() {
  if (!plan.value || activeMappingID.value) return
  const index = plan.value.assignments.length
  const accounts = suggestedGroupAccountDefaults.value.map((account) => ({ ...account }))
  plan.value.assignments.push({
    index,
    total_cost: 0,
    user_ids: [],
    target_group_name: '',
    desired_accounts: accounts.map((account, accountIndex) => ({ account_id: account.id, priority: Number(account.priority || accountIndex + 1) })),
    accounts,
  })
  plan.value.group_count = plan.value.assignments.length
	recalculateProposedTargetNames()
}

function removeSuggestedGroup(targetIndex: number) {
  if (!plan.value || activeMappingID.value || plan.value.assignments.length <= 1) return
  clearSearchState()
  plan.value.assignments = plan.value.assignments
    .filter((assignment) => assignment.index !== targetIndex)
    .map((assignment, index) => ({
      ...assignment,
      index,
      target_group_name: assignment.index === index ? assignment.target_group_name : '',
    }))
  plan.value.group_count = plan.value.assignments.length
	recalculateProposedTargetNames()
  recalculateAssignments()
}

function recalculateProposedTargetNames() {
	if (!plan.value || activeMappingID.value) return
	const used = new Set((provider.value?.groups ?? []).map((group) => group.group_name))
	let sequence = 1
	for (const assignment of plan.value.assignments) {
		while (true) {
			const suffix = `-${plan.value.platform.trim().toLowerCase()}-${String(sequence).padStart(2, '0')}`
			sequence += 1
			const department = Array.from(plan.value.department_name.trim().replace(/[\u0000-\u001f\u007f-\u009f]/g, ''))
			const name = `${department.slice(0, Math.max(0, 100 - Array.from(suffix).length)).join('')}${suffix}`
			if (used.has(name)) continue
			used.add(name)
			assignment.target_group_name = name
			break
		}
	}
}

function moveCandidate(userID: number, targetIndex: number | null) {
  if (!plan.value) return
  for (const assignment of plan.value.assignments) assignment.user_ids = (assignment.user_ids ?? []).filter((id) => id !== userID)
  if (targetIndex !== null) plan.value.assignments[targetIndex]?.user_ids.push(userID)
	const mapping = mappings.value.find((item) => item.id === activeMappingID.value)
	const nextRemoved = new Set(removedUserIDs.value)
	if (targetIndex === null && mapping?.member_assignments?.[String(userID)]) nextRemoved.add(userID)
	else nextRemoved.delete(userID)
	removedUserIDs.value = nextRemoved
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

function operationEntryNeedsRetry(entry: Record<string, string>): boolean {
  return Boolean(entry.error || entry.status === 'failed' || entry.subscription === 'failed' || entry.source_removal === 'failed' || entry.api_keys?.includes(':failed:'))
}

function retryRemovalUserIDs(mapping: RelayPlanningMapping): number[] {
  return Object.entries(mapping.operation_state ?? {}).flatMap(([key, entry]) => {
    if (!key.startsWith('member:') || entry.action !== 'remove' || !operationEntryNeedsRetry(entry)) return []
    const userID = Number(key.slice('member:'.length))
    return userID > 0 ? [userID] : []
  })
}

function retryMemberActions(mapping: RelayPlanningMapping): Record<string, RelayPlanningMemberAction> {
  return Object.fromEntries(Object.entries(mapping.operation_state ?? {}).flatMap(([key, entry]) => {
    const userID = Number(key.slice('member:'.length))
    const fromMappingID = Number(entry.from_mapping_id)
    if (!key.startsWith('member:') || entry.action !== 'move_here' || !operationEntryNeedsRetry(entry) || userID <= 0 || fromMappingID <= 0) return []
    return [[String(userID), { mode: 'move_here' as const, from_mapping_id: fromMappingID }]]
  }))
}

async function loadOptions() {
  const providerResponse = await listAdminUserSubscriptionOptions()
  providers.value = providerResponse.data.data?.providers ?? []
  if (!form.provider_id) form.provider_id = providers.value[0]?.id ?? 0
}

async function loadMappings() {
  const response = await listRelayGroupMappings(form.provider_id || undefined)
  mappings.value = response.data.data?.items ?? []
  mappingPage.value = Math.min(mappingPage.value, Math.max(1, Math.ceil(mappings.value.length / mappingPageSize)))
}

function applyRenewalPreview(next: RelayPlanningMappingRenewalPreview, selectAll: boolean) {
	const previous = selectedRenewalUserIDs.value
	renewalPreview.value = next
	renewalDays.value = next.renewal_days
	selectedRenewalUserIDs.value = new Set(next.members
		.filter((member) => selectAll || previous.has(member.user_id))
		.map((member) => member.user_id))
}

async function renewMapping(mapping: RelayPlanningMapping) {
	if (renewalLoadingID.value !== null) return
	renewalLoadingID.value = mapping.id
	renewalMappingID.value = mapping.id
	renewalDays.value = 365
	renewalPreview.value = null
	selectedRenewalUserIDs.value = new Set()
	renewalExecution.value = null
	renewalOperationKey.value = crypto.randomUUID()
	renewalReviewNotice.value = ''
	try {
		const response = await previewRelayMappingRenewal(mapping.id, { renewal_days: 365 })
		if (!response.data.data) throw new Error(t('relayPlanning.renewalPreviewFailed'))
		applyRenewalPreview(response.data.data, true)
		renewalDialogOpen.value = true
	} catch (err: any) {
		ElMessage.error(err.response?.data?.message || err.message || t('relayPlanning.renewalPreviewFailed'))
	} finally {
		renewalLoadingID.value = null
	}
}

function resetRenewalOperation() {
	renewalMappingID.value = null
	renewalDays.value = 365
	renewalPreview.value = null
	selectedRenewalUserIDs.value = new Set()
	renewalExecution.value = null
	renewalOperationKey.value = ''
	renewalReviewNotice.value = ''
	renewalPreviewLoading.value = false
}

function closeRenewalOperation() {
	if (renewalExecuting.value || renewalPreviewLoading.value) return
	renewalDialogOpen.value = false
	resetRenewalOperation()
}

async function refreshRenewalPreview() {
	if (renewalMappingID.value === null || !Number.isInteger(renewalDays.value) || renewalDays.value <= 0 || renewalDays.value > 36500) {
		ElMessage.error(t('relayPlanning.positiveRenewalDaysRequired'))
		return
	}
	renewalPreviewLoading.value = true
	try {
		const response = await previewRelayMappingRenewal(renewalMappingID.value, { renewal_days: renewalDays.value })
		if (!response.data.data) throw new Error(t('relayPlanning.renewalPreviewFailed'))
		applyRenewalPreview(response.data.data, false)
	} catch (err: any) {
		ElMessage.error(err.response?.data?.message || err.message || t('relayPlanning.renewalPreviewFailed'))
	} finally {
		renewalPreviewLoading.value = false
	}
}

function toggleRenewalMember(userID: number, checked: boolean) {
	const next = new Set(selectedRenewalUserIDs.value)
	if (checked) next.add(userID)
	else next.delete(userID)
	selectedRenewalUserIDs.value = next
}

function reviewedRenewalMembers(): RelayPlanningMappingRenewalReviewedMember[] {
	return (renewalPreview.value?.members ?? [])
		.filter((member) => selectedRenewalUserIDs.value.has(member.user_id))
		.map((member) => ({ user_id: member.user_id, target_group_id: member.expected_target_group_id, planned_action: member.planned_action }))
}

async function confirmMappingRenewal() {
	if (!renewalPreview.value || selectedRenewalUserIDs.value.size === 0) {
		ElMessage.error(t('relayPlanning.renewalNoSelection'))
		return
	}
	await submitMappingRenewal(reviewedRenewalMembers(), renewalPreview.value.relationship_fingerprint, false)
}

async function retryMappingRenewalFailures() {
	if (!renewalExecution.value || failedRenewalMembers.value.length === 0 || renewalMappingID.value === null) return
	if (!renewalExecution.value.preview) {
		renewalPreviewLoading.value = true
		try {
			const response = await previewRelayMappingRenewal(renewalMappingID.value, { renewal_days: renewalDays.value })
			if (!response.data.data) throw new Error(t('relayPlanning.renewalPreviewFailed'))
			applyRenewalPreview(response.data.data, false)
			renewalExecution.value = { ...renewalExecution.value, preview: response.data.data, preview_error: undefined }
			renewalReviewNotice.value = t('relayPlanning.staleRenewal')
			ElMessage.warning(t('relayPlanning.staleRenewal'))
			return
		} catch (err: any) {
			ElMessage.error(err.response?.data?.message || err.message || t('relayPlanning.renewalPreviewFailed'))
			return
		} finally {
			renewalPreviewLoading.value = false
		}
	}
	const members = failedRenewalMembers.value.map((member) => ({ user_id: member.user_id, target_group_id: member.target_group_id, planned_action: member.action }))
	await submitMappingRenewal(members, renewalExecution.value.preview!.relationship_fingerprint, true)
}

async function submitMappingRenewal(members: RelayPlanningMappingRenewalReviewedMember[], fingerprint: string, retry: boolean) {
	if (renewalMappingID.value === null || renewalExecuting.value) return
	renewalReviewNotice.value = ''
	renewalExecuting.value = true
	try {
		const response = await executeRelayMappingRenewal(renewalMappingID.value, {
			renewal_days: renewalDays.value,
			members,
			expected_relationship_fingerprint: fingerprint,
			operation_key: renewalOperationKey.value,
			retry,
		})
		const next = response.data.data
		if (!next) throw new Error(t('relayPlanning.renewalExecutionFailed'))
		if (retry && renewalExecution.value) {
			const replacements = new Map(next.members.map((member) => [member.user_id, member]))
			renewalExecution.value = { ...next, members: renewalExecution.value.members.map((member) => replacements.get(member.user_id) ?? member) }
		} else {
			renewalExecution.value = next
		}
		if (next.preview) applyRenewalPreview(next.preview, false)
		ElMessage.success(t('relayPlanning.renewalExecutionFinished'))
	} catch (err: any) {
		const details = err.response?.data?.details
		if (err.response?.status === 409 && details?.error_code === 'stale_relay_plan' && details.refreshed_preview) {
			applyRenewalPreview(details.refreshed_preview, false)
			if (renewalExecution.value) renewalExecution.value = { ...renewalExecution.value, preview: details.refreshed_preview }
			renewalReviewNotice.value = t('relayPlanning.staleRenewal')
			ElMessage.warning(t('relayPlanning.staleRenewal'))
			return
		}
		ElMessage.error(err.response?.data?.message || err.message || t('relayPlanning.renewalExecutionFailed'))
	} finally {
		renewalExecuting.value = false
	}
}

function renewalStatusText(status: RelayPlanningMappingRenewalMember['status']): string {
	if (status === 'active') return t('relayPlanning.renewalStatusActive')
	if (status === 'expired') return t('relayPlanning.renewalStatusExpired')
	if (status === 'suspended') return t('relayPlanning.renewalStatusSuspended')
	return t('relayPlanning.renewalStatusMissing')
}

function renewalActionText(action: RelayPlanningMappingRenewalMember['planned_action']): string {
	if (action === 'extend') return t('relayPlanning.renewalActionExtend')
	if (action === 'renew') return t('relayPlanning.renewalActionRenew')
	if (action === 'skip') return t('relayPlanning.renewalActionSkip')
	return t('relayPlanning.renewalActionCreate')
}

function renewalStatusTag(status: RelayPlanningMappingRenewalMember['status']): 'success' | 'warning' | 'danger' | 'info' {
	if (status === 'active') return 'success'
	if (status === 'suspended') return 'warning'
	if (status === 'expired') return 'danger'
	return 'info'
}

function renewalResultText(status: 'succeeded' | 'skipped' | 'failed'): string {
	if (status === 'succeeded') return t('relayPlanning.renewalSucceeded')
	if (status === 'skipped') return t('relayPlanning.renewalSkipped')
	return t('relayPlanning.renewalFailed')
}

function renewalResultTag(status: 'succeeded' | 'skipped' | 'failed'): 'success' | 'info' | 'danger' {
	if (status === 'succeeded') return 'success'
	if (status === 'skipped') return 'info'
	return 'danger'
}

function formatRenewalDate(value?: string): string {
	if (!value) return '-'
	const parsed = new Date(value)
	if (Number.isNaN(parsed.getTime())) return value
	return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(parsed)
}

function manageAccounts(mapping: RelayPlanningMapping) {
	accountMappingID.value = accountMappingID.value === mapping.id ? null : mapping.id
	if (accountMappingID.value === mapping.id) initializeAccountDraft(mapping)
}

function initializeAccountDraft(mapping: RelayPlanningMapping) {
	const draft: Record<string, RelayPlanningAccount[]> = {}
	for (const pool of mapping.account_pools ?? []) {
		const currentByID = new Map(pool.current.map((account) => [account.id, account]))
		const desired = mapping.account_management_initialized
			? pool.desired.map((intent) => ({
				...(currentByID.get(intent.account_id) ?? { id: intent.account_id, name: `Account #${intent.account_id}`, platform: mapping.platform, type: 'unknown', status: 'unknown', schedulable: false }),
				priority: intent.priority,
			}))
			: pool.current.map((account) => ({ ...account }))
		draft[String(pool.target_group_id)] = desired.sort((left, right) => Number(left.priority ?? 0) - Number(right.priority ?? 0))
	}
	accountDrafts[mapping.id] = draft
}

function replaceMapping(next: RelayPlanningMapping) {
	const index = mappings.value.findIndex((mapping) => mapping.id === next.id)
	if (index >= 0) mappings.value[index] = next
}

async function adoptCurrentAccounts(mapping: RelayPlanningMapping) {
	accountSaving.value = true
	try {
		const response = await adoptCurrentRelayAccounts(mapping.id)
		if (response.data.data) {
			replaceMapping(response.data.data)
			initializeAccountDraft(response.data.data)
		}
		ElMessage.success(t('relayPlanning.accountsAdopted'))
	} catch (err: any) {
		ElMessage.error(err.response?.data?.message || err.message || t('relayPlanning.accountSaveFailed'))
	} finally {
		accountSaving.value = false
	}
}

function accountSearchKey(mappingID: number, targetGroupID: number) {
	return `${mappingID}:${targetGroupID}`
}

function previewAccountSearchKey(targetIndex: number) {
	return `preview:${targetIndex}`
}

function scheduleAccountSearch(key: string, providerID: number, platform: string, value: string | number) {
	const query = String(value || '').trim()
	accountSearchQueries[key] = query
	const previous = accountSearchTimers.get(key)
	if (previous) clearTimeout(previous)
	const requestID = (accountSearchRequestIDs.get(key) ?? 0) + 1
	accountSearchRequestIDs.set(key, requestID)
	if (!query) {
		accountSearchResults[key] = []
		accountSearchLoading[key] = false
		return
	}
	accountSearchTimers.set(key, setTimeout(() => void runAccountSearch(key, providerID, platform, query, requestID), searchDelayMS))
}

function schedulePreviewAccountSearch(targetIndex: number, value: string | number) {
	if (!plan.value) return
	scheduleAccountSearch(previewAccountSearchKey(targetIndex), plan.value.provider_id, plan.value.platform, value)
}

function scheduleManagedAccountSearch(mapping: RelayPlanningMapping | null, targetGroupID: number, value: string | number) {
	if (!mapping) return
	scheduleAccountSearch(accountSearchKey(mapping.id, targetGroupID), mapping.provider_id, mapping.platform, value)
}

async function runAccountSearch(key: string, providerID: number, platform: string, query: string, requestID: number) {
	accountSearchLoading[key] = true
	try {
		const response = await searchRelayPlanningAccounts({ provider_id: providerID, platform, q: query, page: 1, page_size: 20 })
		if (accountSearchRequestIDs.get(key) === requestID) accountSearchResults[key] = response.data.data?.items ?? []
	} catch (err: any) {
		if (accountSearchRequestIDs.get(key) === requestID) ElMessage.error(err.response?.data?.message || err.message || t('relayPlanning.accountSearchFailed'))
	} finally {
		if (accountSearchRequestIDs.get(key) === requestID) accountSearchLoading[key] = false
	}
}

function syncPreviewAccountPriorities(targetIndex: number) {
	const assignment = plan.value?.assignments.find((item) => item.index === targetIndex)
	if (!assignment) return
	assignment.accounts.forEach((account, index) => { account.priority = index + 1 })
	assignment.desired_accounts = assignment.accounts.map((account, index) => ({ account_id: account.id, priority: index + 1 }))
}

function addAccountToPreviewTarget(targetIndex: number, account: RelayPlanningAccount) {
	const assignment = plan.value?.assignments.find((item) => item.index === targetIndex)
	if (!assignment || assignment.accounts.some((item) => item.id === account.id)) return
	assignment.accounts.push({ ...account, priority: assignment.accounts.length + 1 })
	syncPreviewAccountPriorities(targetIndex)
	const key = previewAccountSearchKey(targetIndex)
	accountSearchQueries[key] = ''
	accountSearchResults[key] = []
}

function reorderPreviewAccounts(targetIndex: number, accountID: number, offset: number) {
	const assignment = plan.value?.assignments.find((item) => item.index === targetIndex)
	if (!assignment) return
	const index = assignment.accounts.findIndex((account) => account.id === accountID)
	const nextIndex = index + offset
	if (index < 0 || nextIndex < 0 || nextIndex >= assignment.accounts.length) return
	;[assignment.accounts[index], assignment.accounts[nextIndex]] = [assignment.accounts[nextIndex], assignment.accounts[index]]
	syncPreviewAccountPriorities(targetIndex)
}

function removeAccountFromPreviewTarget(targetIndex: number, accountID: number) {
	const assignment = plan.value?.assignments.find((item) => item.index === targetIndex)
	if (!assignment) return
	assignment.accounts = assignment.accounts.filter((account) => account.id !== accountID)
	syncPreviewAccountPriorities(targetIndex)
}

function addAccountToTarget(mappingID: number, targetGroupID: number, account: RelayPlanningAccount) {
	const groupKey = String(targetGroupID)
	const items = accountDrafts[mappingID]?.[groupKey]
	if (!items || items.some((item) => item.id === account.id)) return
	items.push({ ...account, priority: items.length + 1 })
	const searchKey = accountSearchKey(mappingID, targetGroupID)
	accountSearchQueries[searchKey] = ''
	accountSearchResults[searchKey] = []
}

function reorderAccounts(mappingID: number, targetGroupID: number, accountID: number, offset: number) {
	const items = accountDrafts[mappingID]?.[String(targetGroupID)]
	if (!items) return
	const index = items.findIndex((item) => item.id === accountID)
	const nextIndex = index + offset
	if (index < 0 || nextIndex < 0 || nextIndex >= items.length) return
	;[items[index], items[nextIndex]] = [items[nextIndex], items[index]]
	items.forEach((item, itemIndex) => { item.priority = itemIndex + 1 })
}

function removeAccountFromTarget(mappingID: number, targetGroupID: number, accountID: number) {
	const items = accountDrafts[mappingID]?.[String(targetGroupID)]
	if (!items) return
	accountDrafts[mappingID][String(targetGroupID)] = items.filter((item) => item.id !== accountID)
	accountDrafts[mappingID][String(targetGroupID)].forEach((item, index) => { item.priority = index + 1 })
}

async function saveDesiredAccounts(mapping: RelayPlanningMapping) {
	const desired: Record<string, RelayPlanningAccountIntent[]> = {}
	for (const groupID of mapping.group_ids) {
		desired[String(groupID)] = (accountDrafts[mapping.id]?.[String(groupID)] ?? []).map((account, index) => ({ account_id: account.id, priority: index + 1 }))
	}
	accountSaving.value = true
	try {
		const response = await saveRelayDesiredAccounts(mapping.id, desired)
		if (response.data.data) replaceMapping(response.data.data)
		ElMessage.success(t('relayPlanning.desiredAccountsSaved'))
	} catch (err: any) {
		ElMessage.error(err.response?.data?.message || err.message || t('relayPlanning.accountSaveFailed'))
	} finally {
		accountSaving.value = false
	}
}

async function preview() {
  const request = planningRequest()
  if (!request.provider_id || !request.department_id || !request.platform || !request.template_group_id) {
    ElMessage.warning(t('relayPlanning.requiredFields'))
    return
  }
  loading.value = true
  error.value = ''
  try {
		lastExecution.value = null
    const response = await previewRelayPlan(request)
    const nextPlan = response.data.data ?? null
    suggestedGroupAccountDefaults.value = (nextPlan?.assignments[0]?.accounts ?? []).map((account) => ({ ...account }))
    applyPlan(nextPlan)
    activeMappingID.value = null
    selectedUnmanagedRelayIDs.value = new Set()
    operationKey.value = crypto.randomUUID()
  } catch (err: any) {
    error.value = err.response?.data?.message || err.message || t('relayPlanning.previewFailed')
  } finally {
    loading.value = false
  }
}

function scheduleUserSearch(targetIndex: number, value: string | number) {
  if (!plan.value) return
  const query = String(value || '').trim()
  targetSearchQueries[targetIndex] = query
	const previous = targetSearchTimers.get(targetIndex)
	if (previous) clearTimeout(previous)
	const requestID = (targetSearchRequestIDs.get(targetIndex) ?? 0) + 1
	targetSearchRequestIDs.set(targetIndex, requestID)
  if (!query) {
    targetSearchResults[targetIndex] = []
    delete targetSearchPages[targetIndex]
		targetSearchLoading[targetIndex] = false
    return
  }
	targetSearchTimers.set(targetIndex, setTimeout(() => void runUserSearch(targetIndex, query, 1, requestID), searchDelayMS))
}

function searchUserPage(targetIndex: number, page: number) {
	const query = String(targetSearchQueries[targetIndex] || '').trim()
	if (!query) return
	const previous = targetSearchTimers.get(targetIndex)
	if (previous) clearTimeout(previous)
	const requestID = (targetSearchRequestIDs.get(targetIndex) ?? 0) + 1
	targetSearchRequestIDs.set(targetIndex, requestID)
	void runUserSearch(targetIndex, query, page, requestID)
}

async function runUserSearch(targetIndex: number, query: string, page: number, requestID: number) {
	if (!plan.value) return
  targetSearchLoading[targetIndex] = true
  try {
    const response = await searchRelayPlanningUsers({
      provider_id: plan.value.provider_id,
      platform: plan.value.platform,
      q: query,
      page,
      page_size: 20,
    })
    const result = response.data.data
		if (targetSearchRequestIDs.get(targetIndex) === requestID) {
			targetSearchResults[targetIndex] = result?.items ?? []
			targetSearchPages[targetIndex] = { total: result?.total ?? 0, page: result?.page ?? page, page_size: result?.page_size ?? 20 }
		}
  } catch (err: any) {
		if (targetSearchRequestIDs.get(targetIndex) === requestID) ElMessage.error(err.response?.data?.message || err.message || t('relayPlanning.searchFailed'))
  } finally {
		if (targetSearchRequestIDs.get(targetIndex) === requestID) targetSearchLoading[targetIndex] = false
  }
}

async function addSearchedUser(targetIndex: number, item: RelayPlanningUserSearchItem) {
  if (!plan.value || !item.selectable) return
  const assignments = assignmentPayload()
  for (const assignment of assignments) assignment.user_ids = assignment.user_ids.filter((userID) => userID !== item.user_id)
  const target = assignments.find((assignment) => assignment.index === targetIndex)
  if (!target) return
  target.user_ids.push(item.user_id)
  const selected = new Set(assignments.flatMap((assignment) => assignment.user_ids))
  memberSources.value[String(item.user_id)] ??= 0
	const managedAssignments = (item.managed_assignments ?? []).filter((assignment) => assignment.mapping_id !== activeMappingID.value)
	if (managedAssignments.length > 0) {
		managedAssignmentsByUser.value[String(item.user_id)] = managedAssignments
		memberActions.value[String(item.user_id)] ??= { mode: 'move_here', from_mapping_id: managedAssignments[0].mapping_id }
	}
  const request = {
    selected_user_ids: Array.from(selected).sort((left, right) => left - right),
    assignments,
    member_sources: memberSourcesPayload(selected),
		removed_user_ids: Array.from(removedUserIDs.value),
		member_actions: memberActions.value,
  }
  targetSearchLoading[targetIndex] = true
  try {
    const response = activeMappingID.value
      ? await previewRelayReplan(activeMappingID.value, request)
      : await previewRelayPlan({
          provider_id: plan.value.provider_id,
          department_id: plan.value.department_id,
          platform: plan.value.platform,
          template_group_id: plan.value.template_group_id,
          source_group_id: plan.value.source_group_id,
          weekly_cost_target: plan.value.weekly_cost_target,
          ...request,
        })
    applyPlan(response.data.data ?? plan.value)
    targetSearchQueries[targetIndex] = ''
    targetSearchResults[targetIndex] = []
  } catch (err: any) {
    ElMessage.error(err.response?.data?.message || err.message || t('relayPlanning.refreshPlanFailed'))
  } finally {
    targetSearchLoading[targetIndex] = false
  }
}

async function requestExecution() {
  if (!plan.value) return
  confirming.value = true
  try {
    const selected_user_ids = Array.from(selectedUserIDs.value)
    const response = activeMappingID.value
		? await previewRelayReplan(activeMappingID.value, { selected_user_ids, assignments: assignmentPayload(), member_sources: memberSourcesPayload(), removed_user_ids: Array.from(removedUserIDs.value), member_actions: memberActions.value, adopt_relay_user_ids: Array.from(selectedUnmanagedRelayIDs.value) })
      : await previewRelayPlan({
          provider_id: plan.value.provider_id,
          department_id: plan.value.department_id,
          platform: plan.value.platform,
          template_group_id: plan.value.template_group_id,
          source_group_id: plan.value.source_group_id,
          weekly_cost_target: plan.value.weekly_cost_target,
          selected_user_ids,
          assignments: assignmentPayload(),
          member_sources: memberSourcesPayload(),
          adopt_relay_user_ids: Array.from(selectedUnmanagedRelayIDs.value),
        })
    applyPlan(response.data.data ?? plan.value)
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
      member_sources: memberSourcesPayload(),
		removed_user_ids: Array.from(removedUserIDs.value),
		member_actions: memberActions.value,
      adopt_relay_user_ids: Array.from(selectedUnmanagedRelayIDs.value),
		expected_relationship_fingerprint: plan.value.relationship_fingerprint,
      operation_key: operationKey.value || crypto.randomUUID(),
    }
    const response = activeMappingID.value
      ? await executeRelayReplan(activeMappingID.value, request)
      : await executeRelayPlan(request)
		lastExecution.value = response.data.data ?? null
    applyPlan(response.data.data?.plan ?? plan.value)
    operationKey.value = request.operation_key
    await loadMappings()
		confirmDialogOpen.value = false
		ElMessage.success(t('relayPlanning.executionFinished'))
  } catch (err: any) {
		const details = err.response?.data?.details
		if (err.response?.status === 409 && details?.error_code === 'stale_relay_plan') {
			if (details.refreshed_plan) applyPlan(details.refreshed_plan)
			confirmDialogOpen.value = false
			ElMessage.warning(t('relayPlanning.stalePlan'))
			return
		}
    ElMessage.error(err.response?.data?.message || err.message || t('relayPlanning.executionFailed'))
  } finally {
    executing.value = false
  }
}

async function replan(mapping: RelayPlanningMapping) {
  try {
		lastExecution.value = null
    const retryRemovedUserIDs = retryRemovalUserIDs(mapping)
    const retryActions = retryMemberActions(mapping)
    const retryRequest = {
      ...(retryRemovedUserIDs.length ? { removed_user_ids: retryRemovedUserIDs } : {}),
      ...(Object.keys(retryActions).length ? { member_actions: retryActions } : {}),
    }
    const response = await previewRelayReplan(mapping.id, retryRequest)
    applyPlan(response.data.data ?? null)
    activeMappingID.value = mapping.id
    selectedUnmanagedRelayIDs.value = new Set()
    removedUserIDs.value = new Set(retryRemovedUserIDs)
    memberActions.value = retryActions
    managedAssignmentsByUser.value = {}
    operationKey.value = crypto.randomUUID()
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
	clearSearchState()
  plan.value = null
	lastExecution.value = null
  activeMappingID.value = null
  selectedUserIDs.value = new Set()
  selectedUnmanagedRelayIDs.value = new Set()
	removedUserIDs.value = new Set()
	memberActions.value = {}
	managedAssignmentsByUser.value = {}
  memberSources.value = {}
  operationKey.value = ''
  suggestedGroupAccountDefaults.value = []
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

function rebind(mapping: RelayPlanningMapping) {
	if (rebindPendingID.value !== null) return
	rebindMappingID.value = mapping.id
	rebindContext.provider_id = mapping.provider_id
	rebindContext.platform = mapping.platform
	rebindForm.department_id = mapping.department_id
	rebindForm.template_group_id = mapping.template_group_id || mapping.source_group_id
	rebindForm.source_group_id = mapping.source_group_id
	rebindForm.group_ids = [...mapping.group_ids]
	rebindDialogOpen.value = true
}

async function submitRebind() {
	if (rebindMappingID.value === null || rebindPendingID.value !== null) return
	if (!rebindForm.department_id.trim()) {
		ElMessage.error(t('relayPlanning.departmentIdRequired'))
		return
	}
	if (rebindForm.template_group_id <= 0 || rebindForm.source_group_id <= 0) {
		ElMessage.error(t('relayPlanning.positiveGroupIdRequired'))
		return
	}
	if (rebindForm.group_ids.length === 0) {
		ElMessage.error(t('relayPlanning.numericGroupIdsRequired'))
		return
	}
	const mappingID = rebindMappingID.value
	rebindPendingID.value = mappingID
	try {
		await rebindRelayGroupMapping(mappingID, {
			department_id: rebindForm.department_id.trim(),
			template_group_id: rebindForm.template_group_id,
			source_group_id: rebindForm.source_group_id,
			group_ids: [...rebindForm.group_ids],
		})
		await loadMappings()
		rebindDialogOpen.value = false
		ElMessage.success(t('relayPlanning.mappingRebound'))
	} catch (err: any) {
		ElMessage.error(err.response?.data?.message || err.message || t('relayPlanning.rebindFailed'))
	} finally {
		rebindPendingID.value = null
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

onBeforeUnmount(clearSearchState)
</script>

<template>
  <AppLayout>
    <div class="mx-auto max-w-7xl space-y-6">
      <header class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div class="flex items-center gap-2 text-xs font-semibold uppercase tracking-wide text-blue-700">
            <el-icon><Switch /></el-icon>
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
          <el-form-item :label="t('relayPlanning.department')" class="!mb-0 min-w-0">
            <AdminDepartmentPicker
              v-model="form.department_id"
              data-testid="department-select"
              class="w-full"
              :allow-all="false"
              :placeholder="t('relayPlanning.selectDepartment')"
              @change="resetPlan"
            />
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
            <el-select v-model="form.source_group_id" data-testid="source-group-select" class="w-full" filterable clearable :placeholder="t('relayPlanning.targetOnly')" @change="resetPlan">
              <el-option v-for="item in groups" :key="item.group_id" :label="`${item.group_name} (#${item.group_id})`" :value="Number(item.group_id)" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('relayPlanning.costTarget')" class="!mb-0">
            <el-input-number v-model="form.weekly_cost_target" data-testid="cost-target-input" class="!w-full" :min="0" :precision="2" controls-position="right" />
          </el-form-item>
        </div>
        <div class="mt-4 flex flex-wrap gap-2">
          <el-button data-testid="preview-allocation" type="primary" :loading="loading" @click="preview">{{ t('relayPlanning.preview') }}</el-button>
          <el-button v-if="plan" data-testid="open-execution-confirmation" :icon="Check" type="success" :loading="confirming" :disabled="plan.group_count === 0 || hasTargetNameErrors || (!activeMappingID && selectedUserIDs.size === 0 && selectedUnmanagedRelayIDs.size === 0)" @click="requestExecution">{{ t('relayPlanning.confirmExecute') }}</el-button>
        </div>
        <el-alert v-if="error" class="mt-4" type="error" :closable="false" :title="error" />
      </section>

      <section v-if="plan" class="space-y-4">
        <div class="grid gap-4 sm:grid-cols-3">
          <div class="rounded-lg border border-slate-200 bg-white p-4"><div class="text-xs text-slate-500">{{ t('relayPlanning.plannedGroups') }}</div><div class="mt-1 text-2xl font-semibold">{{ plan.group_count }}</div><div v-if="plan.group_count !== plan.recommended_group_count" class="mt-1 text-xs text-slate-500">{{ t('relayPlanning.recommended') }}: {{ plan.recommended_group_count }}</div></div>
          <div class="rounded-lg border border-slate-200 bg-white p-4"><div class="text-xs text-slate-500">{{ t('relayPlanning.selectedEligibleMembers') }}</div><div class="mt-1 text-2xl font-semibold">{{ displayedEligibleMemberCount }}</div></div>
          <div class="rounded-lg border border-slate-200 bg-white p-4"><div class="text-xs text-slate-500">{{ t('relayPlanning.planningTarget') }}</div><div class="mt-1 text-2xl font-semibold">${{ plan.weekly_cost_target.toFixed(2) }}</div></div>
        </div>
        <el-alert v-if="plan.warnings?.length" type="warning" :closable="false" :title="t('relayPlanning.reviewWarnings')" class="whitespace-pre-line">
          <template #default>{{ plan.warnings.map(translateWarning).join('\n') }}</template>
        </el-alert>
        <div class="rounded-lg border border-slate-200 bg-white p-4">
          <div class="mb-2 text-sm font-semibold text-slate-900">{{ t('relayPlanning.candidatesRank') }}</div>
          <div v-if="!wideContentLayout" data-testid="candidate-card-layout" class="space-y-3">
            <article v-for="candidate in plan.candidates" :key="candidate.user_id" class="rounded-md border border-slate-200 p-3">
              <div class="flex items-start justify-between gap-3">
                <div class="min-w-0"><div class="break-words font-medium text-slate-900">{{ candidate.username || candidate.email }}</div><div class="break-words text-xs text-slate-500">{{ candidate.email }}</div></div>
                <el-checkbox :model-value="selectedUserIDs.has(candidate.user_id)" :disabled="!candidate.can_add" @change="(value) => toggleCandidate(candidate.user_id, value === true)" />
              </div>
              <dl class="mt-3 grid grid-cols-2 gap-x-4 gap-y-2 text-xs"><div><dt class="text-slate-500">{{ t('relayPlanning.cost30d') }}</dt><dd class="font-medium">{{ candidate.usage_known === false ? t('relayPlanning.unknown') : `$${candidate.range_cost.toFixed(2)}` }}</dd></div><div><dt class="text-slate-500">{{ t('relayPlanning.tokens30d') }}</dt><dd class="font-medium">{{ candidate.usage_known === false ? t('relayPlanning.unknown') : candidate.range_tokens }}</dd></div><div><dt class="text-slate-500">{{ t('relayPlanning.globalRank') }}</dt><dd class="font-medium">{{ candidate.global_token_rank || '-' }}</dd></div><div><dt class="text-slate-500">{{ t('relayPlanning.keys') }}</dt><dd class="font-medium">{{ candidate.migratable_key_count }}</dd></div></dl>
              <div v-if="selectedUserIDs.has(candidate.user_id)" class="mt-3"><el-select v-model="memberSources[String(candidate.user_id)]" class="w-full"><el-option :label="t('relayPlanning.targetOnly')" :value="0" /><el-option v-for="item in groups" :key="item.group_id" :label="`${item.group_name} (#${item.group_id})`" :value="Number(item.group_id)" /></el-select></div>
              <div class="mt-3"><el-select v-if="candidate.can_add" :data-testid="`candidate-target-${candidate.user_id}`" :model-value="candidateAssignmentIndex(candidate.user_id)" class="w-full" clearable :placeholder="t('relayPlanning.unassigned')" @change="(value) => moveCandidate(candidate.user_id, value === null || value === undefined || value === '' ? null : Number(value))"><el-option v-for="assignment in plan.assignments" :key="assignment.index" :label="assignment.target_group_name || `${t('relayPlanning.group')} ${assignment.index + 1}`" :value="assignment.index" /></el-select><span v-else class="text-xs text-slate-400">{{ t('relayPlanning.notAvailable') }}</span></div>
              <div class="mt-2"><el-tag :type="candidate.eligible ? 'success' : candidate.can_add ? 'warning' : 'info'">{{ candidate.eligible ? t('relayPlanning.eligible') : candidate.can_add ? t('relayPlanning.addOnly') : t('relayPlanning.excluded') }}</el-tag><div v-if="candidate.warnings?.length" class="mt-1 text-xs text-amber-700">{{ candidate.warnings.map(translateWarning).join('; ') }}</div></div>
            </article>
          </div>
          <el-table v-else data-testid="candidate-table-layout" :data="plan.candidates" stripe>
            <el-table-column :label="t('relayPlanning.select')" width="70"><template #default="scope"><el-checkbox :model-value="selectedUserIDs.has(scope.row.user_id)" :disabled="!scope.row.can_add" @change="(value) => toggleCandidate(scope.row.user_id, value === true)" /></template></el-table-column>
            <el-table-column prop="username" :label="t('relayPlanning.user')" min-width="140" />
            <el-table-column prop="email" :label="t('relayPlanning.email')" min-width="190" />
            <el-table-column prop="range_cost" :label="t('relayPlanning.cost30d')" width="120"><template #default="scope">{{ scope.row.usage_known === false ? t('relayPlanning.unknown') : `$${scope.row.range_cost.toFixed(2)}` }}</template></el-table-column>
            <el-table-column prop="range_tokens" :label="t('relayPlanning.tokens30d')" width="130"><template #default="scope">{{ scope.row.usage_known === false ? t('relayPlanning.unknown') : scope.row.range_tokens }}</template></el-table-column>
            <el-table-column prop="global_token_rank" :label="t('relayPlanning.globalRank')" width="110" />
            <el-table-column prop="migratable_key_count" :label="t('relayPlanning.keys')" width="80" />
            <el-table-column :label="t('relayPlanning.target')" min-width="170"><template #default="scope"><el-select v-if="scope.row.can_add" :data-testid="`candidate-target-${scope.row.user_id}`" :model-value="candidateAssignmentIndex(scope.row.user_id)" clearable :placeholder="t('relayPlanning.unassigned')" @change="(value) => moveCandidate(scope.row.user_id, value === null || value === undefined || value === '' ? null : Number(value))"><el-option v-for="assignment in plan.assignments" :key="assignment.index" :label="assignment.target_group_name || `${t('relayPlanning.group')} ${assignment.index + 1}`" :value="assignment.index" /></el-select><span v-else class="text-xs text-slate-400">{{ t('relayPlanning.notAvailable') }}</span></template></el-table-column>
            <el-table-column :label="t('relayPlanning.sourceGroup')" min-width="180"><template #default="scope"><el-select v-if="selectedUserIDs.has(scope.row.user_id)" v-model="memberSources[String(scope.row.user_id)]"><el-option :label="t('relayPlanning.targetOnly')" :value="0" /><el-option v-for="item in groups" :key="item.group_id" :label="`${item.group_name} (#${item.group_id})`" :value="Number(item.group_id)" /></el-select><span v-else>-</span></template></el-table-column>
            <el-table-column :label="t('relayPlanning.status')" min-width="180"><template #default="scope"><el-tag :type="scope.row.eligible ? 'success' : scope.row.can_add ? 'warning' : 'info'">{{ scope.row.eligible ? t('relayPlanning.eligible') : scope.row.can_add ? t('relayPlanning.addOnly') : t('relayPlanning.excluded') }}</el-tag><div v-if="scope.row.warnings?.length" class="mt-1 text-xs text-amber-700">{{ scope.row.warnings.map(translateWarning).join('; ') }}</div></template></el-table-column>
          </el-table>
        </div>
        <div class="rounded-lg border border-slate-200 bg-white p-4">
          <div class="mb-2 flex items-center justify-between gap-3"><div class="text-sm font-semibold text-slate-900">{{ t('relayPlanning.proposedGroups') }}</div><el-button v-if="activeMappingID" data-testid="apply-all-target-names" size="small" type="primary" plain @click="applyAllTargetNames">{{ t('relayPlanning.applyAllNames') }}</el-button><el-button v-else data-testid="add-suggested-group" size="small" type="primary" plain :icon="Plus" @click="addSuggestedGroup">{{ t('relayPlanning.addSuggestedGroup') }}</el-button></div>
          <div class="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            <div v-for="assignment in plan.assignments" :key="assignment.index" :data-testid="`suggested-group-${assignment.index}`" class="rounded-md border border-slate-200 p-3">
              <div class="flex justify-between gap-3 text-sm font-medium"><span class="min-w-0 break-words">{{ assignment.target_group_name || `${t('relayPlanning.group')} ${assignment.index + 1}` }}<span v-if="assignment.target_group_id" class="text-slate-500"> (#{{ assignment.target_group_id }})</span></span><span class="flex shrink-0 items-center gap-2"><span>${{ assignment.total_cost.toFixed(2) }}</span><el-tooltip v-if="!activeMappingID && plan.assignments.length > 1" :content="t('relayPlanning.removeSuggestedGroup')"><el-button :data-testid="`remove-suggested-group-${assignment.index}`" circle size="small" type="danger" plain :icon="Delete" :aria-label="t('relayPlanning.removeSuggestedGroup')" @click="removeSuggestedGroup(assignment.index)" /></el-tooltip></span></div>
				<div v-if="activeMappingID" class="mt-3 space-y-2">
					<div class="grid gap-1 text-xs text-slate-500"><div>{{ t('relayPlanning.currentName') }}: <span class="break-words text-slate-700">{{ assignment.current_target_group_name }}</span></div><div>{{ t('relayPlanning.suggestedName') }}: <span class="break-words text-slate-700">{{ assignment.suggested_target_group_name }}</span></div></div>
					<el-checkbox :data-testid="`rename-target-${assignment.index}`" :model-value="Boolean(assignment.rename_selected)" @change="(value) => toggleTargetRename(assignment.index, value === true)">{{ t('relayPlanning.renameTarget') }}</el-checkbox>
				</div>
				<el-input v-if="!activeMappingID || assignment.rename_selected" v-model="assignment.target_group_name" :data-testid="`target-name-${assignment.index}`" class="mt-2" maxlength="100" show-word-limit :placeholder="t('relayPlanning.targetName')" />
				<div v-if="targetNameErrors[assignment.index]" class="mt-1 text-xs text-red-600">{{ targetNameErrors[assignment.index] }}</div>
              <div class="mt-2 text-xs text-slate-500">{{ t('relayPlanning.memberCount', { count: assignment.user_ids?.length ?? 0 }) }}</div>
				<div class="mt-3 border-t border-slate-200 pt-3">
					<div class="text-xs font-semibold text-slate-500">{{ t('relayPlanning.desiredAccounts') }}</div>
					<div v-if="assignment.accounts.length" class="mt-2 space-y-2">
						<div v-for="(account, accountIndex) in assignment.accounts" :key="account.id" class="flex items-center justify-between gap-3 text-sm">
							<span class="min-w-0"><span class="block break-words font-medium">{{ account.name }} (#{{ account.id }})</span><span class="block text-xs" :class="account.status !== 'active' || !account.schedulable ? 'text-amber-700' : 'text-slate-500'">{{ account.type }} · {{ account.status }} · {{ account.schedulable ? t('relayPlanning.schedulable') : t('relayPlanning.notSchedulable') }}</span></span>
							<span class="flex shrink-0 gap-1">
								<el-tooltip :content="t('relayPlanning.moveUp')"><el-button circle size="small" :icon="CaretTop" :disabled="accountIndex === 0" :aria-label="t('relayPlanning.moveUp')" @click="reorderPreviewAccounts(assignment.index, account.id, -1)" /></el-tooltip>
								<el-tooltip :content="t('relayPlanning.moveDown')"><el-button circle size="small" :icon="CaretBottom" :disabled="accountIndex === assignment.accounts.length - 1" :aria-label="t('relayPlanning.moveDown')" @click="reorderPreviewAccounts(assignment.index, account.id, 1)" /></el-tooltip>
								<el-tooltip :content="t('relayPlanning.remove')"><el-button :data-testid="`remove-target-account-${assignment.index}-${account.id}`" circle size="small" type="danger" plain :icon="Delete" :aria-label="t('relayPlanning.remove')" @click="removeAccountFromPreviewTarget(assignment.index, account.id)" /></el-tooltip>
							</span>
						</div>
					</div>
					<el-empty v-else :description="t('relayPlanning.noDesiredAccounts')" :image-size="48" />
					<el-input :data-testid="`target-account-search-${assignment.index}`" :model-value="accountSearchQueries[previewAccountSearchKey(assignment.index)] || ''" :loading="accountSearchLoading[previewAccountSearchKey(assignment.index)]" clearable class="mt-3" :placeholder="t('relayPlanning.searchAccounts')" @input="(value) => schedulePreviewAccountSearch(assignment.index, value)" />
					<div v-if="accountSearchResults[previewAccountSearchKey(assignment.index)]?.length" class="mt-2 divide-y divide-slate-100 border-y border-slate-100">
						<div v-for="account in accountSearchResults[previewAccountSearchKey(assignment.index)]" :key="account.id" class="flex items-center justify-between gap-3 py-2 text-sm">
							<span class="min-w-0"><span class="block truncate font-medium">{{ account.name }} (#{{ account.id }})</span><span class="block truncate text-xs" :class="account.status !== 'active' || !account.schedulable ? 'text-amber-700' : 'text-slate-500'">{{ account.type }} · {{ account.status }} · {{ account.schedulable ? t('relayPlanning.schedulable') : t('relayPlanning.notSchedulable') }}</span></span>
							<el-tooltip :content="t('relayPlanning.add')"><el-button :data-testid="`add-target-account-${assignment.index}-${account.id}`" circle size="small" type="primary" :icon="Plus" :disabled="assignment.accounts.some((item) => item.id === account.id)" :aria-label="t('relayPlanning.add')" @click="addAccountToPreviewTarget(assignment.index, account)" /></el-tooltip>
						</div>
					</div>
				</div>
					<div v-if="assignment.user_ids?.length" class="mt-2 space-y-2 text-sm text-slate-700"><div v-for="userID in assignment.user_ids" :key="userID"><div class="flex items-center justify-between gap-2"><span class="min-w-0 break-words">{{ candidateLabel(userID) }}</span><el-tooltip v-if="activeMappingID" :content="t('relayPlanning.removeMember')"><el-button :data-testid="`remove-member-${userID}`" circle size="small" type="danger" plain :icon="Delete" :aria-label="t('relayPlanning.removeMember')" @click="moveCandidate(userID, null)" /></el-tooltip></div><div v-if="memberActions[String(userID)]" class="mt-1"><el-radio-group v-model="memberActions[String(userID)].mode" size="small"><el-radio-button value="move_here">{{ t('relayPlanning.moveHere') }}</el-radio-button><el-radio-button value="add_additionally">{{ t('relayPlanning.addAdditionally') }}</el-radio-button></el-radio-group><div class="mt-1 text-xs text-amber-700">{{ managedAssignmentsByUser[String(userID)]?.map((item) => `${item.department_name || item.department_id} · #${item.target_group_id}`).join(', ') }}</div><div v-if="memberActions[String(userID)].mode === 'add_additionally'" class="mt-1 text-xs text-amber-700">{{ t('relayPlanning.addAdditionallyWarning') }}</div></div></div></div>
              <el-input :data-testid="`target-user-search-${assignment.index}`" :model-value="targetSearchQueries[assignment.index] || ''" :loading="targetSearchLoading[assignment.index]" clearable class="mt-3" :placeholder="t('relayPlanning.searchUsers')" @input="(value) => scheduleUserSearch(assignment.index, value)" />
              <div v-if="targetSearchResults[assignment.index]?.length" class="mt-2 divide-y divide-slate-100 border-y border-slate-100">
                <div v-for="item in targetSearchResults[assignment.index]" :key="item.user_id" class="flex items-center justify-between gap-3 py-2 text-sm">
                  <span class="min-w-0"><span class="block truncate font-medium">{{ item.username || item.email }}</span><span class="block truncate text-xs text-slate-500">{{ item.department?.display_path || item.department?.name || '-' }}</span><span v-if="item.disabled_reason" class="block text-xs text-amber-700">{{ item.disabled_reason }}</span></span>
                  <el-button :data-testid="`add-searched-user-${assignment.index}-${item.user_id}`" size="small" type="primary" :disabled="!item.selectable" @click="addSearchedUser(assignment.index, item)">{{ t('relayPlanning.add') }}</el-button>
                </div>
                <el-pagination v-if="targetSearchPages[assignment.index]?.total > targetSearchPages[assignment.index]?.page_size" :data-testid="`target-user-pagination-${assignment.index}`" class="mt-2 justify-end" small background layout="prev, pager, next" :current-page="targetSearchPages[assignment.index].page" :page-size="targetSearchPages[assignment.index].page_size" :total="targetSearchPages[assignment.index].total" @current-change="(page) => searchUserPage(assignment.index, page)" />
              </div>
            </div>
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

		<section v-if="lastExecution" class="border-y border-slate-200 bg-white py-4">
			<div class="mb-3 text-sm font-semibold text-slate-900">{{ t('relayPlanning.executionResults') }}</div>
			<div class="divide-y divide-slate-200">
				<div v-for="group in lastExecution.groups" :key="group.index" class="flex flex-wrap items-start justify-between gap-3 py-3 first:pt-0 last:pb-0">
					<div class="min-w-0"><div class="break-words text-sm font-medium text-slate-900">{{ group.name || t('relayPlanning.groupNumber', { id: group.id ?? group.index + 1 }) }}</div><div v-if="group.current_name && group.current_name !== group.name" class="break-words text-xs text-slate-500">{{ group.current_name }} -> {{ group.name }}</div><div v-if="group.error" class="mt-1 break-words text-xs text-red-600">{{ group.error }}</div></div>
					<el-tag :type="group.rename === 'failed' ? 'danger' : group.rename === 'succeeded' ? 'success' : 'info'">{{ renameResultText(group.rename) }}</el-tag>
				</div>
			</div>
		</section>

      <section class="rounded-lg border border-slate-200 bg-white p-4">
        <div class="mb-2 flex items-center justify-between"><div class="text-sm font-semibold text-slate-900">{{ t('relayPlanning.managedMappings') }}</div><span class="text-xs text-slate-500">{{ t('relayPlanning.groupIdsAuthoritative') }}</span></div>
        <el-empty v-if="!mappings.length" :description="t('relayPlanning.noMappings')" />
        <div v-if="mappings.length && !wideContentLayout" data-testid="mapping-card-layout" class="space-y-3">
          <article v-for="mapping in paginatedMappings" :key="mapping.id" class="rounded-md border border-slate-200 p-3">
            <div class="flex items-start justify-between gap-3"><div class="min-w-0"><div class="break-words font-medium text-slate-900">{{ mapping.department_name || mapping.department_id }}</div><div class="text-xs text-slate-500">{{ mapping.platform }}</div></div><el-tag :type="mapping.warnings?.length || mapping.status === 'needs_retry' ? 'warning' : 'success'">{{ mapping.warnings?.length ? t('relayPlanning.reviewNeeded') : translateMappingStatus(mapping.status) }}</el-tag></div>
            <dl class="mt-3 space-y-2 text-sm"><div><dt class="text-xs text-slate-500">{{ t('relayPlanning.templateGroup') }}</dt><dd class="break-words">{{ mapping.template_group_name || '-' }} (#{{ mapping.template_group_id }})</dd></div><div><dt class="text-xs text-slate-500">{{ t('relayPlanning.migrationSource') }}</dt><dd class="break-words">{{ mapping.source_group_name }} (#{{ mapping.source_group_id }})</dd></div><div><dt class="text-xs text-slate-500">{{ t('relayPlanning.managedGroups') }}</dt><dd class="break-words">{{ mapping.group_ids.join(', ') || '-' }}</dd></div></dl>
            <div v-if="mapping.warnings?.length" class="mt-2 text-xs text-amber-700">{{ mapping.warnings.map(translateWarning).join('; ') }}</div>
            <div v-if="mapping.department_suggestions?.length" class="mt-2 text-xs text-slate-500">{{ t('relayPlanning.departmentSuggestions') }}: {{ mapping.department_suggestions.map(departmentSuggestionLabel).join(', ') }}</div>
            <div class="mt-3 flex flex-wrap gap-2"><el-button :data-testid="`replan-mapping-${mapping.id}`" size="small" type="primary" @click="replan(mapping)">{{ t('relayPlanning.replan') }}</el-button><el-button :data-testid="`renew-mapping-${mapping.id}`" size="small" :icon="Calendar" :loading="renewalLoadingID === mapping.id" :disabled="renewalLoadingID !== null && renewalLoadingID !== mapping.id" @click="renewMapping(mapping)">{{ t('relayPlanning.renewSubscriptions') }}</el-button><el-button :data-testid="`rebind-mapping-${mapping.id}`" size="small" :loading="rebindPendingID === mapping.id" :disabled="rebindPendingID !== null && rebindPendingID !== mapping.id" @click="rebind(mapping)">{{ t('relayPlanning.rebind') }}</el-button><el-button :data-testid="`manage-accounts-${mapping.id}`" size="small" @click="manageAccounts(mapping)">{{ t('relayPlanning.manageAccounts') }}</el-button></div>
          </article>
        </div>
        <el-table v-else-if="mappings.length" data-testid="mapping-table-layout" :data="paginatedMappings" stripe>
          <el-table-column prop="department_name" :label="t('relayPlanning.department')" min-width="150" />
          <el-table-column prop="platform" :label="t('relayPlanning.platform')" width="110" />
          <el-table-column :label="t('relayPlanning.templateGroup')" min-width="160"><template #default="scope">{{ scope.row.template_group_name || '-' }} (#{{ scope.row.template_group_id }})</template></el-table-column>
          <el-table-column :label="t('relayPlanning.migrationSource')" min-width="160"><template #default="scope">{{ scope.row.source_group_name }} (#{{ scope.row.source_group_id }})</template></el-table-column>
          <el-table-column :label="t('relayPlanning.managedGroups')" min-width="180"><template #default="scope">{{ scope.row.group_ids.join(', ') }}</template></el-table-column>
          <el-table-column :label="t('relayPlanning.status')" min-width="150"><template #default="scope"><el-tag :type="scope.row.warnings?.length || scope.row.status === 'needs_retry' ? 'warning' : 'success'">{{ scope.row.warnings?.length ? t('relayPlanning.reviewNeeded') : translateMappingStatus(scope.row.status) }}</el-tag><div v-if="scope.row.warnings?.length" class="mt-1 text-xs text-amber-700">{{ scope.row.warnings.map(translateWarning).join('; ') }}</div><div v-if="scope.row.department_suggestions?.length" class="mt-1 text-xs text-slate-500">{{ t('relayPlanning.departmentSuggestions') }}: {{ scope.row.department_suggestions.map(departmentSuggestionLabel).join(', ') }}</div></template></el-table-column>
          <el-table-column :label="t('relayPlanning.actions')" min-width="360"><template #default="scope"><el-button :data-testid="`replan-mapping-${scope.row.id}`" link type="primary" @click="replan(scope.row as RelayPlanningMapping)">{{ t('relayPlanning.replan') }}</el-button><el-button :data-testid="`renew-mapping-${scope.row.id}`" link type="primary" :icon="Calendar" :loading="renewalLoadingID === scope.row.id" :disabled="renewalLoadingID !== null && renewalLoadingID !== scope.row.id" @click="renewMapping(scope.row as RelayPlanningMapping)">{{ t('relayPlanning.renewSubscriptions') }}</el-button><el-button :data-testid="`rebind-mapping-${scope.row.id}`" link type="primary" :loading="rebindPendingID === scope.row.id" :disabled="rebindPendingID !== null && rebindPendingID !== scope.row.id" @click="rebind(scope.row as RelayPlanningMapping)">{{ t('relayPlanning.rebind') }}</el-button><el-button :data-testid="`manage-accounts-${scope.row.id}`" link type="primary" @click="manageAccounts(scope.row as RelayPlanningMapping)">{{ t('relayPlanning.manageAccounts') }}</el-button></template></el-table-column>
        </el-table>
        <el-pagination v-if="mappings.length > mappingPageSize" data-testid="mapping-pagination" class="mt-4 justify-end" size="small" background layout="prev, pager, next" :pager-count="5" :current-page="mappingPage" :page-size="mappingPageSize" :total="mappings.length" @current-change="mappingPage = $event" />
        <div v-if="accountMapping" class="mt-4 border-t border-slate-200 pt-4">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div><div class="text-sm font-semibold text-slate-900">{{ t('relayPlanning.accountRelationships') }}</div><div class="text-xs text-slate-500">{{ accountMapping.department_name }} · {{ accountMapping.platform }}</div></div>
			<div class="flex gap-2">
				<el-button v-if="!accountMapping.account_management_initialized" :data-testid="`adopt-current-accounts-${accountMapping.id}`" type="primary" :loading="accountSaving" @click="adoptCurrentAccounts(accountMapping)">{{ t('relayPlanning.adoptCurrent') }}</el-button>
				<el-button v-else :data-testid="`save-desired-accounts-${accountMapping.id}`" type="primary" :loading="accountSaving" @click="saveDesiredAccounts(accountMapping)">{{ t('relayPlanning.saveDesiredAccounts') }}</el-button>
			</div>
          </div>
          <el-alert v-if="!accountMapping.account_management_initialized" class="mt-3" type="info" :closable="false" :title="t('relayPlanning.accountsUninitialized')" />
          <div class="mt-3 grid gap-3 md:grid-cols-2">
            <div v-for="pool in accountMapping.account_pools" :key="pool.target_group_id" class="rounded-md border border-slate-200 p-3">
              <div class="flex items-center justify-between gap-2"><span class="text-sm font-medium text-slate-900">{{ t('relayPlanning.targetGroupNumber', { id: pool.target_group_id }) }}</span><el-tag v-if="pool.drift" type="warning">{{ t('relayPlanning.accountDrift') }}</el-tag></div>
              <div v-if="pool.current.length" class="mt-2 space-y-2">
                <div v-for="account in pool.current" :key="account.id" class="flex items-start justify-between gap-3 border-t border-slate-100 pt-2 text-sm first:border-0 first:pt-0">
                  <span class="min-w-0"><span class="block break-words font-medium">{{ account.name }} (#{{ account.id }})</span><span class="block text-xs text-slate-500">{{ account.type }} · {{ account.status }} · {{ account.schedulable ? t('relayPlanning.schedulable') : t('relayPlanning.notSchedulable') }}</span></span>
                  <span class="shrink-0 text-xs text-slate-500">{{ t('relayPlanning.priorityNumber', { priority: account.priority ?? '-' }) }}</span>
                </div>
              </div>
              <el-empty v-else :description="t('relayPlanning.noCurrentAccounts')" :image-size="48" />
				<template v-if="accountMapping.account_management_initialized">
					<div class="mt-4 text-xs font-semibold text-slate-500">{{ t('relayPlanning.desiredAccounts') }}</div>
					<div v-if="accountDrafts[accountMapping.id]?.[String(pool.target_group_id)]?.length" class="mt-2 space-y-2">
						<div v-for="(account, accountIndex) in accountDrafts[accountMapping.id][String(pool.target_group_id)]" :key="account.id" class="flex items-center justify-between gap-3 border-t border-slate-100 pt-2 text-sm first:border-0 first:pt-0">
							<span class="min-w-0"><span class="block break-words font-medium">{{ account.name }} (#{{ account.id }})</span><span class="block text-xs" :class="account.status !== 'active' || !account.schedulable ? 'text-amber-700' : 'text-slate-500'">{{ account.type }} · {{ account.status }} · {{ account.schedulable ? t('relayPlanning.schedulable') : t('relayPlanning.notSchedulable') }}</span></span>
							<span class="flex shrink-0 gap-1">
								<el-tooltip :content="t('relayPlanning.moveUp')"><el-button :data-testid="`move-account-up-${accountMapping.id}-${pool.target_group_id}-${account.id}`" circle size="small" :icon="CaretTop" :disabled="accountIndex === 0" :aria-label="t('relayPlanning.moveUp')" @click="reorderAccounts(accountMapping.id, pool.target_group_id, account.id, -1)" /></el-tooltip>
								<el-tooltip :content="t('relayPlanning.moveDown')"><el-button circle size="small" :icon="CaretBottom" :disabled="accountIndex === accountDrafts[accountMapping.id][String(pool.target_group_id)].length - 1" :aria-label="t('relayPlanning.moveDown')" @click="reorderAccounts(accountMapping.id, pool.target_group_id, account.id, 1)" /></el-tooltip>
								<el-tooltip :content="t('relayPlanning.remove')"><el-button circle size="small" type="danger" plain :icon="Delete" :aria-label="t('relayPlanning.remove')" @click="removeAccountFromTarget(accountMapping.id, pool.target_group_id, account.id)" /></el-tooltip>
							</span>
						</div>
					</div>
					<el-empty v-else :description="t('relayPlanning.noDesiredAccounts')" :image-size="48" />
					<el-input :data-testid="`account-search-${accountMapping.id}-${pool.target_group_id}`" :model-value="accountSearchQueries[accountSearchKey(accountMapping.id, pool.target_group_id)] || ''" :loading="accountSearchLoading[accountSearchKey(accountMapping.id, pool.target_group_id)]" clearable class="mt-3" :placeholder="t('relayPlanning.searchAccounts')" @input="(value) => scheduleManagedAccountSearch(accountMapping, pool.target_group_id, value)" />
					<div v-if="accountSearchResults[accountSearchKey(accountMapping.id, pool.target_group_id)]?.length" class="mt-2 divide-y divide-slate-100 border-y border-slate-100">
						<div v-for="account in accountSearchResults[accountSearchKey(accountMapping.id, pool.target_group_id)]" :key="account.id" class="flex items-center justify-between gap-3 py-2 text-sm">
							<span class="min-w-0"><span class="block truncate font-medium">{{ account.name }} (#{{ account.id }})</span><span class="block truncate text-xs" :class="account.status !== 'active' || !account.schedulable ? 'text-amber-700' : 'text-slate-500'">{{ account.type }} · {{ account.status }} · {{ account.schedulable ? t('relayPlanning.schedulable') : t('relayPlanning.notSchedulable') }}</span></span>
							<el-tooltip :content="t('relayPlanning.add')"><el-button :data-testid="`add-account-${accountMapping.id}-${pool.target_group_id}-${account.id}`" circle size="small" type="primary" :icon="Plus" :disabled="accountDrafts[accountMapping.id]?.[String(pool.target_group_id)]?.some((item) => item.id === account.id)" :aria-label="t('relayPlanning.add')" @click="addAccountToTarget(accountMapping.id, pool.target_group_id, account)" /></el-tooltip>
						</div>
					</div>
				</template>
            </div>
          </div>
        </div>
      </section>

		<el-dialog
			v-model="renewalDialogOpen"
			data-testid="renewal-preview-dialog"
			:title="t('relayPlanning.renewalPreviewTitle')"
			append-to-body
			align-center
			width="min(100%, 56rem)"
			:show-close="!renewalExecuting && !renewalPreviewLoading"
			:close-on-click-modal="!renewalExecuting && !renewalPreviewLoading"
			:close-on-press-escape="!renewalExecuting && !renewalPreviewLoading"
			@closed="resetRenewalOperation"
		>
			<div class="flex flex-wrap items-end justify-between gap-4">
				<el-form-item :label="t('relayPlanning.renewalTermDays')" class="!mb-0">
					<el-input-number v-model="renewalDays" data-testid="renewal-days-input" :min="1" :max="36500" :precision="0" :disabled="renewalPreviewLoading || renewalExecuting || renewalExecution !== null" controls-position="right" @change="refreshRenewalPreview" />
				</el-form-item>
				<div data-testid="renewal-selected-count" class="text-sm font-medium text-slate-700">{{ t('relayPlanning.renewalSelectedCount', { count: selectedRenewalUserIDs.size }) }}</div>
			</div>
			<el-alert v-if="renewalReviewNotice" data-testid="renewal-review-alert" class="mt-3" type="warning" :closable="false" show-icon :title="renewalReviewNotice" />
			<div v-if="renewalPreview" class="mt-4 max-h-[65vh] divide-y divide-slate-200 overflow-y-auto border-y border-slate-200">
				<div v-for="member in renewalPreview.members" :key="member.user_id" :data-testid="`renewal-member-${member.user_id}`" class="flex items-start gap-3 py-4 first:pt-3 last:pb-3">
					<el-checkbox :model-value="selectedRenewalUserIDs.has(member.user_id)" :disabled="renewalExecuting || renewalExecution !== null" class="mt-0.5" @change="(checked) => toggleRenewalMember(member.user_id, checked === true)" />
					<span class="min-w-0 flex-1">
						<span class="flex flex-wrap items-start justify-between gap-2">
							<span class="min-w-0"><span class="block break-words text-sm font-semibold text-slate-900">{{ member.username || member.email }}</span><span class="block break-all text-xs text-slate-500">{{ member.email }}</span></span>
							<span class="flex shrink-0 flex-wrap gap-2"><el-tag :type="renewalStatusTag(member.status)" size="small">{{ renewalStatusText(member.status) }}</el-tag><el-tag type="info" size="small">{{ renewalActionText(member.planned_action) }}</el-tag></span>
						</span>
						<span class="mt-3 grid gap-3 text-sm sm:grid-cols-3">
							<span class="min-w-0"><span class="block text-xs text-slate-500">{{ t('relayPlanning.expectedTargetGroup') }}</span><span class="block break-words text-slate-800">{{ member.expected_target_group_name || t('relayPlanning.groupNumber', { id: member.expected_target_group_id }) }} (#{{ member.expected_target_group_id }})</span></span>
							<span class="min-w-0"><span class="block text-xs text-slate-500">{{ t('relayPlanning.currentExpiry') }}</span><span :data-testid="`renewal-current-expiry-${member.user_id}`" class="block break-words text-slate-800">{{ formatRenewalDate(member.current_expiry) }}</span></span>
							<span class="min-w-0"><span class="block text-xs text-slate-500">{{ t('relayPlanning.resultingExpiry') }}</span><span :data-testid="`renewal-resulting-expiry-${member.user_id}`" class="block break-words text-slate-800">{{ formatRenewalDate(member.resulting_expiry) }}</span></span>
						</span>
						<span v-if="member.drift?.length" class="mt-3 block text-xs text-amber-700"><span class="font-semibold">{{ t('relayPlanning.unexpectedSubscriptions') }}:</span> {{ member.drift.map((item) => `${item.group_name || t('relayPlanning.groupNumber', { id: item.group_id })} (#${item.group_id}) · ${item.status}`).join('; ') }}</span>
					</span>
				</div>
			</div>
			<div v-if="renewalExecution" class="mt-4 border-t border-slate-200 pt-3">
				<div class="text-sm font-semibold text-slate-900">{{ t('relayPlanning.renewalResults') }}</div>
				<div class="mt-2 divide-y divide-slate-100">
					<div v-for="member in renewalExecution.members" :key="member.user_id" :data-testid="`renewal-result-${member.user_id}`" class="flex flex-wrap items-start justify-between gap-3 py-2 text-sm">
						<span class="min-w-0"><span class="block font-medium text-slate-800">{{ t('relayPlanning.userNumber', { id: member.user_id }) }} · {{ renewalActionText(member.action) }}</span><span v-if="member.error" class="block break-words text-xs text-red-600">{{ member.error }}</span></span>
						<el-tag :type="renewalResultTag(member.status)" size="small">{{ renewalResultText(member.status) }}</el-tag>
					</div>
				</div>
				<el-alert v-if="renewalExecution.preview_error" class="mt-2" type="warning" :closable="false" show-icon :title="renewalExecution.preview_error" />
			</div>
			<template #footer>
				<el-button data-testid="close-renewal" :disabled="renewalExecuting || renewalPreviewLoading" @click="closeRenewalOperation">{{ t('relayPlanning.close') }}</el-button>
				<el-button v-if="!renewalExecution" data-testid="confirm-renewal" type="primary" :loading="renewalExecuting" :disabled="selectedRenewalUserIDs.size === 0" @click="confirmMappingRenewal">{{ t('relayPlanning.confirmRenewal') }}</el-button>
				<el-button v-else-if="failedRenewalMembers.length" data-testid="retry-renewal-failures" type="primary" :loading="renewalExecuting || renewalPreviewLoading" @click="retryMappingRenewalFailures">{{ t('relayPlanning.retryFailedRenewals') }}</el-button>
			</template>
		</el-dialog>

      <el-dialog
        v-model="confirmDialogOpen"
        :title="t('relayPlanning.confirmPlan')"
        append-to-body
        align-center
        width="min(100%, 32rem)"
        :close-on-click-modal="!executing"
        :close-on-press-escape="!executing"
      >
		<el-alert type="warning" :closable="false" show-icon :title="t(activeMappingID ? 'relayPlanning.executeReviewedWarning' : 'relayPlanning.executeWarning')" />
        <dl v-if="plan" class="mt-4 grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
          <dt class="text-slate-500">{{ t('relayPlanning.templateGroup') }}</dt><dd class="min-w-0 break-words font-medium text-slate-900">{{ plan.template_group_name }} (#{{ plan.template_group_id }})</dd>
          <dt class="text-slate-500">{{ t('relayPlanning.migrationSource') }}</dt><dd class="min-w-0 break-words font-medium text-slate-900">{{ plan.source_group_name }} (#{{ plan.source_group_id }})</dd>
          <dt class="text-slate-500">{{ t('relayPlanning.members') }}</dt><dd class="font-medium text-slate-900">{{ selectedUserIDs.size }}</dd>
			<dt v-if="removedUserIDs.size" class="text-slate-500">{{ t('relayPlanning.removals') }}</dt><dd v-if="removedUserIDs.size" class="font-medium text-slate-900">{{ removedUserIDs.size }}</dd>
          <dt class="text-slate-500">{{ t('relayPlanning.targetGroups') }}</dt>
          <dd class="max-h-48 space-y-1 overflow-y-auto font-medium text-slate-900">
				<div v-for="assignment in plan.assignments" :key="assignment.index" class="break-words">{{ assignment.target_group_name || `Group ${assignment.index + 1}` }} ({{ t('relayPlanning.memberCount', { count: assignment.user_ids?.length ?? 0 }) }})</div>
			  </dd>
			</dl>
			<div v-if="plan?.target_summaries?.length" class="mt-5 max-h-72 divide-y divide-slate-200 overflow-y-auto border-y border-slate-200">
				<section v-for="summary in plan.target_summaries" :key="summary.index" class="py-3 first:pt-0 last:pb-0">
					<h4 class="text-sm font-semibold text-slate-900">{{ summary.target_group_name || t('relayPlanning.groupNumber', { id: summary.target_group_id ?? summary.index + 1 }) }}</h4>
					<div v-if="summary.rename" class="mt-2"><div class="text-xs font-semibold text-slate-500">{{ t('relayPlanning.renameChanges') }}</div><div class="mt-1 break-words text-sm text-slate-700">{{ summary.rename.from_name }} -> {{ summary.rename.to_name }}</div></div>
					<div v-if="summary.accounts.length" class="mt-2">
						<div class="text-xs font-semibold text-slate-500">{{ t('relayPlanning.accountChanges') }}</div>
						<ul class="mt-1 space-y-1 text-sm text-slate-700"><li v-for="change in summary.accounts" :key="`${change.account_id}-${change.action}`">{{ accountChangeText(change) }}</li></ul>
					</div>
					<div v-if="summary.members.length" class="mt-2">
						<div class="text-xs font-semibold text-slate-500">{{ t('relayPlanning.memberChanges') }}</div>
						<ul class="mt-1 space-y-1 text-sm text-slate-700"><li v-for="change in summary.members" :key="`${change.user_id || change.relay_user_id}-${change.action}`">{{ memberChangeText(change, summary) }}</li></ul>
					</div>
					<div v-if="summary.subscriptions.length" class="mt-2">
						<div class="text-xs font-semibold text-slate-500">{{ t('relayPlanning.subscriptionChanges') }}</div>
						<ul class="mt-1 space-y-1 text-sm text-slate-700"><li v-for="change in summary.subscriptions" :key="`${change.relay_user_id}-${change.group_id}-${change.action}`">{{ subscriptionChangeText(change, summary) }}</li></ul>
					</div>
					<div v-if="summary.api_keys.length" class="mt-2">
						<div class="text-xs font-semibold text-slate-500">{{ t('relayPlanning.apiKeyChanges') }}</div>
						<ul class="mt-1 space-y-1 text-sm text-slate-700"><li v-for="change in summary.api_keys" :key="`${change.relay_user_id}-${change.from_group_id}-${change.to_group_id}`">{{ apiKeyChangeText(change, summary) }}</li></ul>
					</div>
				</section>
			</div>
			<template #footer>
          <el-button :disabled="executing" @click="confirmDialogOpen = false">{{ t('relayPlanning.cancel') }}</el-button>
          <el-button data-testid="confirm-execution" type="danger" :loading="executing" @click="executeConfirmed">{{ t(activeMappingID ? 'relayPlanning.applyReviewedChanges' : 'relayPlanning.createAndMigrate') }}</el-button>
			</template>
		</el-dialog>

		<el-dialog
			v-model="rebindDialogOpen"
			data-testid="rebind-dialog"
			:title="t('relayPlanning.confirmRebind')"
			append-to-body
			align-center
			width="min(100%, 34rem)"
			:show-close="rebindPendingID === null"
			:close-on-click-modal="rebindPendingID === null"
			:close-on-press-escape="rebindPendingID === null"
		>
			<el-alert type="warning" :closable="false" show-icon :title="t('relayPlanning.confirmRebindMessage')" />
			<div class="mt-5 grid gap-4">
				<el-form-item :label="t('relayPlanning.department')" class="!mb-0 min-w-0">
					<AdminDepartmentPicker
						v-model="rebindForm.department_id"
						data-testid="rebind-department-select"
						class="w-full"
						:allow-all="false"
						:placeholder="t('relayPlanning.selectDepartment')"
					/>
				</el-form-item>
				<el-form-item :label="t('relayPlanning.templateGroup')" class="!mb-0">
					<el-select v-model="rebindForm.template_group_id" data-testid="rebind-template-select" class="w-full" filterable :placeholder="t('relayPlanning.selectTemplateGroup')">
						<el-option v-for="item in rebindGroups" :key="item.group_id" :label="`${item.group_name} (#${item.group_id})`" :value="Number(item.group_id)" />
					</el-select>
				</el-form-item>
				<el-form-item :label="t('relayPlanning.migrationSource')" class="!mb-0">
					<el-select v-model="rebindForm.source_group_id" data-testid="rebind-source-select" class="w-full" filterable :placeholder="t('relayPlanning.selectMigrationSource')">
						<el-option v-for="item in rebindGroups" :key="item.group_id" :label="`${item.group_name} (#${item.group_id})`" :value="Number(item.group_id)" />
					</el-select>
				</el-form-item>
				<el-form-item :label="t('relayPlanning.managedGroups')" class="!mb-0">
					<el-select v-model="rebindForm.group_ids" data-testid="rebind-targets-select" class="w-full" multiple filterable :placeholder="t('relayPlanning.managedGroups')">
						<el-option v-for="item in rebindGroups" :key="item.group_id" :label="`${item.group_name} (#${item.group_id})`" :value="Number(item.group_id)" />
					</el-select>
				</el-form-item>
			</div>
			<template #footer>
				<el-button :disabled="rebindPendingID !== null" @click="rebindDialogOpen = false">{{ t('relayPlanning.cancel') }}</el-button>
				<el-button data-testid="confirm-rebind" type="primary" :loading="rebindPendingID !== null" @click="submitRebind">{{ t('relayPlanning.confirm') }}</el-button>
			</template>
		</el-dialog>
	</div>
  </AppLayout>
</template>
