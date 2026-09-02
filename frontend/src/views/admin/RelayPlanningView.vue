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
	confirmRelayPlanningRecovery,
  executeRelayMappingRenewal,
  executeRelayPlan,
  executeRelayReplan,
  listRelayGroupMappings,
	previewRelayMappingRenewal,
	previewRelayPlanningRecovery,
  previewRelayPlan,
  previewRelayReplan,
  rebindRelayGroupMapping,
  saveRelayDesiredAccounts,
  searchRelayPlanningAccounts,
	searchRelayPlanningUsers,
	relayPlanningContainment,
	type RelayPlanningAccount,
	type RelayPlanningAccountIntent,
	type RelayPlanningCandidate,
	type RelayPlanningRequest,
	type RelayPlanningMapping,
	type RelayPlanningRecoveryPreview,
	type RelayPlanningMappingRenewalExecution,
	type RelayPlanningMappingRenewalMember,
	type RelayPlanningMappingRenewalReviewedMember,
	type RelayPlanningMappingRenewalPreview,
	type RelayPlanningTargetSummary,
  type RelayPlanningUserSearchItem,
} from '@/api/relayPlanning'
import { createFeatureTranslator } from '@/utils/featureI18n'
import { useMediaQuery, useWideContentLayout } from '@/composables/useMediaQuery'
import { useRelayPlanningWorkflow } from '@/composables/useRelayPlanningWorkflow'

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'relayPlanning.', relayPlanningMessages)
const wideContentLayout = useWideContentLayout()
const desktopPagination = useMediaQuery('(min-width: 768px)', true)

const error = ref('')
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
const accountSearchErrors = reactive<Record<string, string>>({})
const accountSearchPages = reactive<Record<string, { total: number; page: number; page_size: number }>>({})
const accountSearchTimers = new Map<string, ReturnType<typeof setTimeout>>()
const accountSearchRequestIDs = new Map<string, number>()
const searchDelayMS = 300
const providers = ref<Array<{ id: number; name: string; display_name: string; groups: Array<{ group_id: string; group_name: string; platform: string }> }>>([])
const recoveryDialogOpen = ref(false)
const recoveryMapping = ref<RelayPlanningMapping | null>(null)
const recoveryDirection = ref<'resume' | 'restore'>('resume')
const recoveryPreview = ref<RelayPlanningRecoveryPreview | null>(null)
const recoveryLoading = ref(false)
const recoveryConfirming = ref(false)
const recoveryError = ref('')

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
const reviewedPlanWorkflow = useRelayPlanningWorkflow({
	previewInitial: async (request) => (await previewRelayPlan(request)).data.data ?? null,
	previewReplan: async (mappingID, request) => (await previewRelayReplan(mappingID, request)).data.data ?? null,
	executeInitial: async (request) => (await executeRelayPlan(request)).data.data ?? null,
	executeReplan: async (mappingID, request) => (await executeRelayReplan(mappingID, request)).data.data ?? null,
	searchUsers: async (params) => (await searchRelayPlanningUsers(params)).data.data ?? { items: [], total: 0, page: params.page, page_size: params.page_size },
	searchAccounts: async (params) => (await searchRelayPlanningAccounts(params)).data.data ?? { items: [], total: 0, page: params.page, page_size: params.page_size },
	createOperationKey: () => crypto.randomUUID(),
	reservedGroups: () => (provider.value?.groups ?? []).map((group) => ({ id: Number(group.group_id), name: group.group_name })),
	searchError: (requestError, kind) => requestErrorMessage(requestError, t(kind === 'user' ? 'relayPlanning.searchFailed' : 'relayPlanning.accountSearchFailed')),
	onPlanApplied: clearManagedAccountSearchState,
})
const {
	loading,
	confirming,
	executing,
	confirmDialogOpen,
	plan,
	lastExecution,
	activeMappingID,
	reviewLocked,
	selectedUserIDs,
	selectedUnmanagedRelayIDs,
	removedUserIDs,
	removalSources,
	lockedRemovalSourceUserIDs,
	memberActions,
	managedAssignmentsByUser,
	memberSources,
	targetSearches,
	previewAccountSearches,
	displayedEligibleMemberCount,
	unassignedCandidates,
	hasTargetNameErrors,
	hasUnreviewedRemovalSources,
	preview: previewReviewedPlan,
	openReplan: openReviewedReplan,
	requestConfirmation: requestReviewedConfirmation,
	executeConfirmed: executeReviewedPlan,
	closeConfirmation,
	reset: resetReviewedPlan,
	toggleTargetRename,
	applyAllTargetNames,
	addSuggestedGroup,
	removeSuggestedGroup,
	moveCandidate,
	toggleCandidate,
	candidateAssignmentIndex,
	candidateLabel,
	setMemberSource,
	toggleUnmanagedRelayUser,
	setTargetName,
	addPreviewAccount: addAccountToPreviewTarget,
	movePreviewAccount: reorderPreviewAccounts,
	removePreviewAccount: removeAccountFromPreviewTarget,
	setMemberAction,
	scheduleUserSearch,
	searchUserPage,
	addSearchedUser: addSearchedUserToReview,
	schedulePreviewAccountSearch,
	searchPreviewAccountPage,
	dispose: disposeReviewedPlan,
} = reviewedPlanWorkflow
const accountMapping = computed(() => mappings.value.find((mapping) => mapping.id === accountMappingID.value) ?? null)
const paginatedMappings = computed(() => {
  const start = (mappingPage.value - 1) * mappingPageSize
  return mappings.value.slice(start, start + mappingPageSize)
})
const rebindGroups = computed(() => (providers.value.find((item) => item.id === rebindContext.provider_id)?.groups ?? [])
  .filter((group) => group.platform === rebindContext.platform))
const targetNameErrors = computed(() => Object.fromEntries(Object.entries(reviewedPlanWorkflow.targetNameErrorCodes.value).map(([index, code]) => [
	Number(index),
	t(code === 'required'
		? 'relayPlanning.targetNameRequired'
		: code === 'too_long'
			? 'relayPlanning.targetNameTooLong'
			: code === 'control'
				? 'relayPlanning.targetNameControl'
			: code === 'duplicate'
				? 'relayPlanning.targetNameDuplicate'
				: 'relayPlanning.targetNameOccupied'),
])))
const failedRenewalMembers = computed(() => renewalExecution.value?.members.filter((member) => member.status === 'failed') ?? [])
const activeMapping = computed(() => mappings.value.find((mapping) => mapping.id === activeMappingID.value) ?? null)

function containmentMode(mapping: RelayPlanningMapping) {
	return relayPlanningContainment(mapping).mode
}

function containmentTitle(mapping: RelayPlanningMapping) {
	return t(containmentMode(mapping) === 'resume_exact' ? 'relayPlanning.exactResumeAvailable' : 'relayPlanning.manualInterventionRequired')
}

function mappingLocked(mapping: RelayPlanningMapping) {
	return containmentMode(mapping) !== 'none' || mapping.alignment === 'operating' || mapping.alignment === 'drifted'
}

function alignmentText(mapping: RelayPlanningMapping) {
	return t(mapping.alignment === 'operating' ? 'relayPlanning.alignmentOperating' : mapping.alignment === 'drifted' ? 'relayPlanning.alignmentDrifted' : 'relayPlanning.alignmentAligned')
}

function operationLifecycleText(mapping: RelayPlanningMapping) {
	const lifecycle = mapping.active_operation?.lifecycle
	if (!lifecycle) return ''
	const keys = {
		applying: 'relayPlanning.lifecycleApplying', interrupted: 'relayPlanning.lifecycleInterrupted', resuming: 'relayPlanning.lifecycleResuming', restoring: 'relayPlanning.lifecycleRestoring', applied: 'relayPlanning.lifecycleApplied', restored: 'relayPlanning.lifecycleRestored', blocked_external: 'relayPlanning.lifecycleBlockedExternal',
	} as const
	return t(keys[lifecycle])
}

function replanLocked(mapping: RelayPlanningMapping) {
	return containmentMode(mapping) === 'manual_intervention' || mapping.alignment === 'operating' || mapping.alignment === 'drifted'
}

function recoveryAvailable(mapping: RelayPlanningMapping, direction: 'resume' | 'restore') {
	return mapping.active_operation?.lifecycle === 'interrupted' && mapping.active_operation.supported_directions.includes(direction)
}

async function reviewRecovery(mapping: RelayPlanningMapping, direction: 'resume' | 'restore') {
	if (!mapping.active_operation || recoveryLoading.value) return
	recoveryMapping.value = mapping
	recoveryDirection.value = direction
	recoveryPreview.value = null
	recoveryError.value = ''
	recoveryDialogOpen.value = true
	recoveryLoading.value = true
	try {
		recoveryPreview.value = (await previewRelayPlanningRecovery(mapping.active_operation.id, direction)).data.data ?? null
	} catch (requestError) {
		recoveryError.value = requestErrorMessage(requestError, t('relayPlanning.recoveryPreviewFailed'))
	} finally {
		recoveryLoading.value = false
	}
}

async function confirmRecovery() {
	const preview = recoveryPreview.value
	if (!preview || preview.external_blocker || recoveryConfirming.value) return
	recoveryConfirming.value = true
	recoveryError.value = ''
	try {
		await confirmRelayPlanningRecovery(preview.operation.id, {
			direction: preview.direction,
			expected_baseline_revisions: preview.baseline_revisions,
			expected_relationship_fingerprint: preview.relationship_fingerprint,
		})
		ElMessage.success(t(preview.direction === 'resume' ? 'relayPlanning.resumeCompleted' : 'relayPlanning.restoreCompleted'))
		recoveryDialogOpen.value = false
		await loadMappings()
	} catch (requestError) {
		const typed = requestError as { response?: { status?: number; data?: { details?: { current_preview?: RelayPlanningRecoveryPreview }; message?: string } } }
		if (typed.response?.status === 409 && typed.response.data?.details?.current_preview) {
			recoveryPreview.value = typed.response.data.details.current_preview
		}
		recoveryError.value = requestErrorMessage(requestError, t('relayPlanning.recoveryConfirmFailed'))
	} finally {
		recoveryConfirming.value = false
	}
}

function translateWarning(warning: string): string {
  void locale.value
  if (warning === 'no eligible member has a valid relay mapping and source-group membership') return t('relayPlanning.warningNoEligible')
  if (warning === 'user is not a member of the selected source group') return t('relayPlanning.warningNotSourceMember')
  if (warning === 'no migratable AE-managed API key') return t('relayPlanning.warningNoMigratableKey')
  if (warning === '30-day usage is unknown; capacity may be underestimated') return t('relayPlanning.warningUnknownUsage')
  if (warning === 'user is not in the selected department') return t('relayPlanning.warningNotSelectedDepartment')
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

function candidateDispositionText(candidate: unknown): string {
	const disposition = (candidate as RelayPlanningCandidate).disposition
	const keys = {
		retained: 'relayPlanning.dispositionRetained',
		target_only: 'relayPlanning.dispositionTargetOnly',
		migration: 'relayPlanning.dispositionMigration',
		available: 'relayPlanning.dispositionAvailable',
		excluded: 'relayPlanning.excluded',
	} as const
	return t(keys[disposition])
}

function candidateDispositionTag(candidate: unknown): 'success' | 'warning' | 'info' {
	const disposition = (candidate as RelayPlanningCandidate).disposition
	if (disposition === 'retained') return 'success'
	if (disposition === 'target_only' || disposition === 'migration') return 'warning'
	return 'info'
}

function translateMappingStatus(status: string): string {
  if (status === 'needs_retry') return t('relayPlanning.needsRetry')
  if (status === 'active') return t('relayPlanning.active')
  return status
}

function mappingStatusText(mapping: RelayPlanningMapping): string {
	if (mapping.status === 'needs_retry') return translateMappingStatus(mapping.status)
	if (mapping.warnings?.length) return t('relayPlanning.reviewNeeded')
	return translateMappingStatus(mapping.status)
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

function removalSourceGroups() {
	const blocked = new Set([Number(plan.value?.template_group_id || 0), ...(plan.value?.assignments ?? []).map((assignment) => Number(assignment.target_group_id || 0))])
	return groups.value.filter((group) => !blocked.has(Number(group.group_id)))
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

function requestErrorMessage(requestError: unknown, fallback: string) {
	const error = requestError as { response?: { data?: { message?: string } }; message?: string }
	return error.response?.data?.message || error.message || fallback
}

function clearManagedAccountSearchState() {
	for (const timer of accountSearchTimers.values()) clearTimeout(timer)
	accountSearchTimers.clear()
	accountSearchRequestIDs.clear()
	for (const key of Object.keys(accountSearchQueries)) delete accountSearchQueries[key]
	for (const key of Object.keys(accountSearchResults)) delete accountSearchResults[key]
	for (const key of Object.keys(accountSearchLoading)) delete accountSearchLoading[key]
	for (const key of Object.keys(accountSearchErrors)) delete accountSearchErrors[key]
	for (const key of Object.keys(accountSearchPages)) delete accountSearchPages[key]
}

function departmentSuggestionLabel(item: { name: string; id: string }): string {
  return `${item.name} (${item.id})`
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

function existingMappingFor(request: RelayPlanningRequest) {
	return mappings.value.find((mapping) => (
		mapping.provider_id === request.provider_id
		&& mapping.department_id === request.department_id.trim()
		&& mapping.platform === request.platform.trim()
	))
}

function conflictingMappingID(err: any) {
	const details = err.response?.data?.details
	const mappingID = Number(details?.mapping_id)
	return err.response?.status === 409 && details?.error_code === 'existing_mapping' && mappingID > 0
		? mappingID
		: null
}

async function recoverExistingMapping(err: any) {
	const mappingID = conflictingMappingID(err)
	if (!mappingID) return false
	try {
		await loadMappings()
	} catch {
		return false
	}
	const mapping = mappings.value.find((item) => item.id === mappingID)
	if (!mapping) return false
	closeConfirmation()
	await replan(mapping)
	return true
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
	if (mappingLocked(mapping)) return
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
	if (mappingLocked(mapping)) return
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
	if (mappingLocked(mapping)) return
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

function scheduleAccountSearch(key: string, providerID: number, platform: string, value: string | number) {
	const query = String(value || '').trim()
	accountSearchQueries[key] = query
	accountSearchErrors[key] = ''
	const previous = accountSearchTimers.get(key)
	if (previous) clearTimeout(previous)
	const requestID = (accountSearchRequestIDs.get(key) ?? 0) + 1
	accountSearchRequestIDs.set(key, requestID)
	if (!query) {
		accountSearchResults[key] = []
		accountSearchLoading[key] = false
		delete accountSearchPages[key]
		return
	}
	accountSearchTimers.set(key, setTimeout(() => void runAccountSearch(key, providerID, platform, query, 1, requestID), searchDelayMS))
}

function scheduleManagedAccountSearch(mapping: RelayPlanningMapping | null, targetGroupID: number, value: string | number) {
	if (!mapping || mappingLocked(mapping)) return
	scheduleAccountSearch(accountSearchKey(mapping.id, targetGroupID), mapping.provider_id, mapping.platform, value)
}

function searchAccountPage(key: string, providerID: number, platform: string, page: number) {
	const query = String(accountSearchQueries[key] || '').trim()
	if (!query) return
	const previous = accountSearchTimers.get(key)
	if (previous) clearTimeout(previous)
	const requestID = (accountSearchRequestIDs.get(key) ?? 0) + 1
	accountSearchRequestIDs.set(key, requestID)
	void runAccountSearch(key, providerID, platform, query, page, requestID)
}

function searchManagedAccountPage(mapping: RelayPlanningMapping | null, targetGroupID: number, page: number) {
	if (!mapping || mappingLocked(mapping)) return
	searchAccountPage(accountSearchKey(mapping.id, targetGroupID), mapping.provider_id, mapping.platform, page)
}

async function runAccountSearch(key: string, providerID: number, platform: string, query: string, page: number, requestID: number) {
	accountSearchLoading[key] = true
	accountSearchErrors[key] = ''
	try {
		const response = await searchRelayPlanningAccounts({ provider_id: providerID, platform, q: query, page, page_size: 20 })
		if (accountSearchRequestIDs.get(key) === requestID) {
			const result = response.data.data
			accountSearchResults[key] = result?.items ?? []
			accountSearchPages[key] = { total: result?.total ?? 0, page: result?.page ?? page, page_size: result?.page_size ?? 20 }
		}
	} catch (err: any) {
		if (accountSearchRequestIDs.get(key) === requestID) accountSearchErrors[key] = err.response?.data?.message || err.message || t('relayPlanning.accountSearchFailed')
	} finally {
		if (accountSearchRequestIDs.get(key) === requestID) accountSearchLoading[key] = false
	}
}

function addAccountToTarget(mappingID: number, targetGroupID: number, account: RelayPlanningAccount) {
	const mapping = mappings.value.find((item) => item.id === mappingID)
	if (!mapping || mappingLocked(mapping)) return
	const groupKey = String(targetGroupID)
	const items = accountDrafts[mappingID]?.[groupKey]
	if (!items || items.some((item) => item.id === account.id)) return
	items.push({ ...account, priority: items.length + 1 })
	const searchKey = accountSearchKey(mappingID, targetGroupID)
	accountSearchQueries[searchKey] = ''
	accountSearchResults[searchKey] = []
	delete accountSearchPages[searchKey]
}

function reorderAccounts(mappingID: number, targetGroupID: number, accountID: number, offset: number) {
	const mapping = mappings.value.find((item) => item.id === mappingID)
	if (!mapping || mappingLocked(mapping)) return
	const items = accountDrafts[mappingID]?.[String(targetGroupID)]
	if (!items) return
	const index = items.findIndex((item) => item.id === accountID)
	const nextIndex = index + offset
	if (index < 0 || nextIndex < 0 || nextIndex >= items.length) return
	;[items[index], items[nextIndex]] = [items[nextIndex], items[index]]
	items.forEach((item, itemIndex) => { item.priority = itemIndex + 1 })
}

function removeAccountFromTarget(mappingID: number, targetGroupID: number, accountID: number) {
	const mapping = mappings.value.find((item) => item.id === mappingID)
	if (!mapping || mappingLocked(mapping)) return
	const items = accountDrafts[mappingID]?.[String(targetGroupID)]
	if (!items) return
	accountDrafts[mappingID][String(targetGroupID)] = items.filter((item) => item.id !== accountID)
	accountDrafts[mappingID][String(targetGroupID)].forEach((item, index) => { item.priority = index + 1 })
}

async function saveDesiredAccounts(mapping: RelayPlanningMapping) {
	if (mappingLocked(mapping)) return
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
  error.value = ''
	const existingMapping = existingMappingFor(request)
	if (existingMapping) {
		await replan(existingMapping)
		return
	}
  try {
    await previewReviewedPlan(request)
  } catch (err: any) {
		if (await recoverExistingMapping(err)) return
    error.value = err.response?.data?.message || err.message || t('relayPlanning.previewFailed')
  }
}

function resetPlan() {
	resetReviewedPlan()
	error.value = ''
}

async function addSearchedUser(targetIndex: number, item: RelayPlanningUserSearchItem) {
	try {
		await addSearchedUserToReview(targetIndex, item)
	} catch (err: any) {
		ElMessage.error(err.response?.data?.message || err.message || t('relayPlanning.refreshPlanFailed'))
	}
}

async function requestExecution() {
  if (!plan.value) return
  try {
    await requestReviewedConfirmation()
  } catch (err: any) {
		if (await recoverExistingMapping(err)) return
    ElMessage.error(err.response?.data?.message || err.message || t('relayPlanning.refreshPlanFailed'))
  }
}

async function executeConfirmed() {
  if (!plan.value) return
	try {
		const outcome = await executeReviewedPlan()
		if (outcome.kind === 'empty' || outcome.kind === 'superseded') return
		if (outcome.kind === 'stale') {
      ElMessage.warning(t('relayPlanning.stalePlan'))
      return
    }
    await loadMappings()
		ElMessage.success(t('relayPlanning.executionFinished'))
  } catch (err: any) {
		if (await recoverExistingMapping(err)) return
    ElMessage.error(err.response?.data?.message || err.message || t('relayPlanning.executionFailed'))
  }
}

async function replan(mapping: RelayPlanningMapping) {
	if (containmentMode(mapping) === 'manual_intervention') return
  try {
    const openedPlan = await openReviewedReplan(mapping)
		if (!openedPlan) return
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

function rebind(mapping: RelayPlanningMapping) {
	if (mappingLocked(mapping)) return
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

onBeforeUnmount(() => {
	disposeReviewedPlan()
	clearManagedAccountSearchState()
})
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
          <el-button v-if="plan" data-testid="open-execution-confirmation" :icon="Check" type="success" :loading="confirming" :disabled="plan.group_count === 0 || hasTargetNameErrors || hasUnreviewedRemovalSources || (!activeMappingID && selectedUserIDs.size === 0 && selectedUnmanagedRelayIDs.size === 0)" @click="requestExecution">{{ t(reviewLocked ? 'relayPlanning.continueExactOperation' : 'relayPlanning.confirmExecute') }}</el-button>
        </div>
        <el-alert v-if="error" class="mt-4" type="error" :closable="false" :title="error" />
      </section>

      <section v-if="plan" class="space-y-4">
		<el-alert v-if="reviewLocked && activeMapping" data-testid="legacy-review-lock" type="warning" :closable="false" show-icon :title="containmentTitle(activeMapping)" :description="t('relayPlanning.exactResumeLockedHelp')" />
		<fieldset class="contents" :disabled="reviewLocked" :aria-disabled="reviewLocked">
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
              <div v-if="selectedUserIDs.has(candidate.user_id)" class="mt-3"><span v-if="candidate.disposition === 'retained'" :data-testid="`candidate-source-${candidate.user_id}`" class="text-xs text-slate-600">{{ candidateDispositionText(candidate) }}</span><el-select v-else :data-testid="`candidate-source-${candidate.user_id}`" :model-value="memberSources[String(candidate.user_id)] ?? 0" class="w-full" @change="(value) => setMemberSource(candidate.user_id, Number(value || 0))"><el-option :label="t('relayPlanning.targetOnly')" :value="0" /><el-option v-for="item in groups" :key="item.group_id" :label="`${item.group_name} (#${item.group_id})`" :value="Number(item.group_id)" /></el-select></div>
              <div class="mt-3"><el-select v-if="candidate.can_add" :data-testid="`candidate-target-${candidate.user_id}`" :model-value="candidateAssignmentIndex(candidate.user_id)" class="w-full" clearable :placeholder="t('relayPlanning.unassigned')" @change="(value) => moveCandidate(candidate.user_id, value === null || value === undefined || value === '' ? null : Number(value))"><el-option v-for="assignment in plan.assignments" :key="assignment.index" :label="assignment.target_group_name || `${t('relayPlanning.group')} ${assignment.index + 1}`" :value="assignment.index" /></el-select><span v-else class="text-xs text-slate-400">{{ t('relayPlanning.notAvailable') }}</span></div>
              <div class="mt-2"><el-tag :type="candidateDispositionTag(candidate)">{{ candidateDispositionText(candidate) }}</el-tag><div v-if="candidate.warnings?.length" :data-testid="`candidate-warning-${candidate.user_id}`" class="mt-1 text-xs text-amber-700">{{ candidate.warnings.map(translateWarning).join('; ') }}</div></div>
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
            <el-table-column :label="t('relayPlanning.sourceGroup')" min-width="180"><template #default="scope"><span v-if="scope.row.disposition === 'retained'" :data-testid="`candidate-source-${scope.row.user_id}`" class="text-xs text-slate-600">{{ candidateDispositionText(scope.row) }}</span><el-select v-else-if="selectedUserIDs.has(scope.row.user_id)" :data-testid="`candidate-source-${scope.row.user_id}`" :model-value="memberSources[String(scope.row.user_id)] ?? 0" @change="(value) => setMemberSource(scope.row.user_id, Number(value || 0))"><el-option :label="t('relayPlanning.targetOnly')" :value="0" /><el-option v-for="item in groups" :key="item.group_id" :label="`${item.group_name} (#${item.group_id})`" :value="Number(item.group_id)" /></el-select><span v-else>-</span></template></el-table-column>
            <el-table-column :label="t('relayPlanning.status')" min-width="180"><template #default="scope"><el-tag :type="candidateDispositionTag(scope.row)">{{ candidateDispositionText(scope.row) }}</el-tag><div v-if="scope.row.warnings?.length" :data-testid="`candidate-warning-${scope.row.user_id}`" class="mt-1 text-xs text-amber-700">{{ scope.row.warnings.map(translateWarning).join('; ') }}</div></template></el-table-column>
          </el-table>
        </div>
        <div class="rounded-lg border border-slate-200 bg-white p-4">
          <div class="mb-2 flex items-center justify-between gap-3"><div class="text-sm font-semibold text-slate-900">{{ t('relayPlanning.proposedGroups') }}</div><div class="flex items-center gap-2"><el-button v-if="activeMappingID" data-testid="apply-all-target-names" size="small" type="primary" plain @click="applyAllTargetNames">{{ t('relayPlanning.applyAllNames') }}</el-button><el-button data-testid="add-suggested-group" size="small" type="primary" plain :icon="Plus" @click="addSuggestedGroup">{{ t('relayPlanning.addSuggestedGroup') }}</el-button></div></div>
          <div class="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            <div v-for="assignment in plan.assignments" :key="assignment.index" :data-testid="`suggested-group-${assignment.index}`" class="rounded-md border border-slate-200 p-3">
              <div class="flex justify-between gap-3 text-sm font-medium"><span class="min-w-0 break-words">{{ assignment.target_group_name || `${t('relayPlanning.group')} ${assignment.index + 1}` }}<span v-if="assignment.target_group_id" class="text-slate-500"> (#{{ assignment.target_group_id }})</span></span><span class="flex shrink-0 items-center gap-2"><span>${{ assignment.total_cost.toFixed(2) }}</span><el-tooltip v-if="(!activeMappingID || !assignment.target_group_id) && plan.assignments.length > 1" :content="t('relayPlanning.removeSuggestedGroup')"><el-button :data-testid="`remove-suggested-group-${assignment.index}`" circle size="small" type="danger" plain :icon="Delete" :aria-label="t('relayPlanning.removeSuggestedGroup')" @click="removeSuggestedGroup(assignment.index)" /></el-tooltip></span></div>
				<div v-if="activeMappingID" class="mt-3 space-y-2">
					<div class="grid gap-1 text-xs text-slate-500"><div>{{ t('relayPlanning.currentName') }}: <span class="break-words text-slate-700">{{ assignment.current_target_group_name }}</span></div><div>{{ t('relayPlanning.suggestedName') }}: <span class="break-words text-slate-700">{{ assignment.suggested_target_group_name }}</span></div></div>
					<el-checkbox :data-testid="`rename-target-${assignment.index}`" :model-value="Boolean(assignment.rename_selected)" :disabled="assignment.target_unavailable" @change="(value) => toggleTargetRename(assignment.index, value === true)">{{ t('relayPlanning.renameTarget') }}</el-checkbox>
				</div>
				<el-input v-if="!activeMappingID || !assignment.target_group_id || assignment.rename_selected" :model-value="assignment.target_group_name" :data-testid="`target-name-${assignment.index}`" class="mt-2" maxlength="100" show-word-limit :placeholder="t('relayPlanning.targetName')" @update:model-value="(value) => setTargetName(assignment.index, String(value))" />
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
						<el-input :data-testid="`target-account-search-${assignment.index}`" :model-value="previewAccountSearches[assignment.index]?.query || ''" :loading="previewAccountSearches[assignment.index]?.loading" clearable class="mt-3" :placeholder="t('relayPlanning.searchAccounts')" @input="(value) => schedulePreviewAccountSearch(assignment.index, value)" />
						<el-alert v-if="previewAccountSearches[assignment.index]?.error" class="mt-2" type="error" :closable="false" show-icon :title="previewAccountSearches[assignment.index]?.error" />
						<el-button v-if="previewAccountSearches[assignment.index]?.error" class="mt-1 !ml-0" size="small" type="primary" link @click="searchPreviewAccountPage(assignment.index, previewAccountSearches[assignment.index]?.page ?? 1)">{{ t('relayPlanning.retry') }}</el-button>
						<div v-if="previewAccountSearches[assignment.index]?.items.length" class="mt-2 divide-y divide-slate-100 border-y border-slate-100">
						<div v-for="account in previewAccountSearches[assignment.index]?.items ?? []" :key="account.id" class="flex items-center justify-between gap-3 py-2 text-sm">
							<span class="min-w-0"><span class="block truncate font-medium">{{ account.name }} (#{{ account.id }})</span><span class="block truncate text-xs" :class="account.status !== 'active' || !account.schedulable ? 'text-amber-700' : 'text-slate-500'">{{ account.type }} · {{ account.status }} · {{ account.schedulable ? t('relayPlanning.schedulable') : t('relayPlanning.notSchedulable') }}</span></span>
							<el-tooltip :content="t('relayPlanning.add')"><el-button :data-testid="`add-target-account-${assignment.index}-${account.id}`" circle size="small" type="primary" :icon="Plus" :disabled="assignment.accounts.some((item) => item.id === account.id)" :aria-label="t('relayPlanning.add')" @click="addAccountToPreviewTarget(assignment.index, account)" /></el-tooltip>
						</div>
						<el-pagination
							v-if="(previewAccountSearches[assignment.index]?.total ?? 0) > (previewAccountSearches[assignment.index]?.page_size ?? 20)"
							:data-testid="`target-account-pagination-${assignment.index}`"
							class="mt-2 justify-end"
							size="small"
							background
							:layout="desktopPagination ? 'prev, pager, next' : 'prev, slot, next'"
							:pager-count="5"
							:current-page="previewAccountSearches[assignment.index]?.page ?? 1"
							:page-size="previewAccountSearches[assignment.index]?.page_size ?? 20"
							:total="previewAccountSearches[assignment.index]?.total ?? 0"
							:disabled="previewAccountSearches[assignment.index]?.loading"
							@current-change="(page) => searchPreviewAccountPage(assignment.index, page)"
						>
							<span v-if="!desktopPagination" class="px-1 text-xs text-slate-500">{{ baseT('pagination.pageOf', { page: previewAccountSearches[assignment.index]?.page ?? 1, pages: Math.ceil((previewAccountSearches[assignment.index]?.total ?? 0) / (previewAccountSearches[assignment.index]?.page_size ?? 20)) }) }}</span>
						</el-pagination>
					</div>
				</div>
						<div v-if="assignment.user_ids?.length" class="mt-2 space-y-2 text-sm text-slate-700"><div v-for="userID in assignment.user_ids" :key="userID"><div class="flex items-center justify-between gap-2"><span class="min-w-0 break-words">{{ candidateLabel(userID) }}</span><el-tooltip v-if="activeMappingID" :content="t('relayPlanning.removeMember')"><el-button :data-testid="`remove-member-${userID}`" circle size="small" type="danger" plain :icon="Delete" :aria-label="t('relayPlanning.removeMember')" @click="moveCandidate(userID, null)" /></el-tooltip></div><div v-if="memberActions[String(userID)]" class="mt-1"><el-radio-group :model-value="memberActions[String(userID)].mode" size="small" @change="(value) => setMemberAction(userID, value === 'add_additionally' ? 'add_additionally' : 'move_here')"><el-radio-button value="move_here">{{ t('relayPlanning.moveHere') }}</el-radio-button><el-radio-button value="add_additionally">{{ t('relayPlanning.addAdditionally') }}</el-radio-button></el-radio-group><div class="mt-1 text-xs text-amber-700">{{ managedAssignmentsByUser[String(userID)]?.map((item) => `${item.department_name || item.department_id} · #${item.target_group_id}`).join(', ') }}</div><div v-if="memberActions[String(userID)].mode === 'add_additionally'" class="mt-1 text-xs text-amber-700">{{ t('relayPlanning.addAdditionallyWarning') }}</div></div></div></div>
						<el-input :data-testid="`target-user-search-${assignment.index}`" :model-value="targetSearches[assignment.index]?.query || ''" :loading="targetSearches[assignment.index]?.loading" clearable class="mt-3" :placeholder="t('relayPlanning.searchUsers')" @input="(value) => scheduleUserSearch(assignment.index, value)" />
						<el-alert v-if="targetSearches[assignment.index]?.error" class="mt-2" type="error" :closable="false" show-icon :title="targetSearches[assignment.index]?.error" />
						<el-button v-if="targetSearches[assignment.index]?.error" class="mt-1 !ml-0" size="small" type="primary" link @click="searchUserPage(assignment.index, targetSearches[assignment.index]?.page ?? 1)">{{ t('relayPlanning.retry') }}</el-button>
						<div v-if="targetSearches[assignment.index]?.items.length" class="mt-2 divide-y divide-slate-100 border-y border-slate-100">
							<div v-for="item in targetSearches[assignment.index]?.items ?? []" :key="item.user_id" class="flex items-center justify-between gap-3 py-2 text-sm">
                  <span class="min-w-0"><span class="block truncate font-medium">{{ item.username || item.email }}</span><span class="block truncate text-xs text-slate-500">{{ item.department?.display_path || item.department?.name || '-' }}</span><span v-if="item.disabled_reason" class="block text-xs text-amber-700">{{ item.disabled_reason }}</span></span>
                  <el-button :data-testid="`add-searched-user-${assignment.index}-${item.user_id}`" size="small" type="primary" :disabled="!item.selectable" @click="addSearchedUser(assignment.index, item)">{{ t('relayPlanning.add') }}</el-button>
                </div>
							<el-pagination v-if="(targetSearches[assignment.index]?.total ?? 0) > (targetSearches[assignment.index]?.page_size ?? 20)" :data-testid="`target-user-pagination-${assignment.index}`" class="mt-2 justify-end" size="small" background :layout="desktopPagination ? 'prev, pager, next' : 'prev, slot, next'" :pager-count="5" :current-page="targetSearches[assignment.index]?.page ?? 1" :page-size="targetSearches[assignment.index]?.page_size ?? 20" :total="targetSearches[assignment.index]?.total ?? 0" :disabled="targetSearches[assignment.index]?.loading" @current-change="(page) => searchUserPage(assignment.index, page)">
								<span v-if="!desktopPagination" class="px-1 text-xs text-slate-500">{{ baseT('pagination.pageOf', { page: targetSearches[assignment.index]?.page ?? 1, pages: Math.ceil((targetSearches[assignment.index]?.total ?? 0) / (targetSearches[assignment.index]?.page_size ?? 20)) }) }}</span>
                </el-pagination>
              </div>
            </div>
          </div>
			</div>
			<div v-if="removedUserIDs.size" data-testid="removed-member-source-review" class="rounded-md border border-amber-200 bg-amber-50 p-3">
				<div class="text-sm font-semibold text-slate-900">{{ t('relayPlanning.removalDestination') }}</div>
				<el-alert v-if="hasUnreviewedRemovalSources" class="mt-2" type="warning" :closable="false" show-icon :title="t('relayPlanning.removalDestinationRequired')" />
				<div class="mt-3 space-y-2">
					<div v-for="userID in removedUserIDs" :key="userID" class="grid gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(12rem,1fr)] sm:items-center">
						<span class="min-w-0 break-words text-sm text-slate-700">{{ candidateLabel(userID) }}</span>
						<el-select
							:data-testid="`removed-member-source-${userID}`"
							:model-value="removalSources[String(userID)] ?? undefined"
							class="w-full"
							:disabled="lockedRemovalSourceUserIDs.has(userID)"
							:placeholder="t('relayPlanning.removalDestinationRequired')"
							@change="(value) => setMemberSource(userID, Number(value || 0))"
						>
							<el-option :label="t('relayPlanning.targetOnly')" :value="0" />
							<el-option v-for="item in removalSourceGroups()" :key="item.group_id" :label="`${item.group_name} (#${item.group_id})`" :value="Number(item.group_id)" />
						</el-select>
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
		</fieldset>
      </section>

		<section v-if="lastExecution" class="border-y border-slate-200 bg-white py-4">
			<div class="mb-3 text-sm font-semibold text-slate-900">{{ t('relayPlanning.executionResults') }}</div>
			<el-alert v-if="lastExecution.mapping?.status === 'needs_retry'" data-testid="execution-needs-retry" class="mb-3" type="error" :closable="false" show-icon :title="t('relayPlanning.needsRetry')" />
			<div v-if="lastExecution.members.some((member) => member.error)" class="mb-3 divide-y divide-red-100 border-y border-red-100">
				<div v-for="member in lastExecution.members.filter((item) => item.error)" :key="`${member.user_id || 0}:${member.relay_user_id || 0}:${member.target_group_id || 0}`" :data-testid="`execution-member-error-${member.user_id || member.relay_user_id}`" class="py-2 text-sm">
					<div class="font-medium text-slate-900">{{ member.user_id ? candidateLabel(member.user_id) : `Relay #${member.relay_user_id}` }}</div>
					<div class="mt-1 break-words text-xs text-red-600">{{ member.error }}</div>
				</div>
			</div>
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
            <div class="flex items-start justify-between gap-3"><div class="min-w-0"><div class="break-words font-medium text-slate-900">{{ mapping.department_name || mapping.department_id }}</div><div class="text-xs text-slate-500">{{ mapping.platform }}</div></div><span class="flex shrink-0 flex-wrap justify-end gap-1"><el-tag :type="mapping.alignment === 'drifted' ? 'danger' : mapping.alignment === 'operating' ? 'warning' : 'success'">{{ alignmentText(mapping) }}</el-tag><el-tag v-if="mapping.active_operation" type="info">{{ operationLifecycleText(mapping) }}</el-tag><el-tag v-if="mapping.status === 'needs_retry'" type="warning">{{ t('relayPlanning.needsRetry') }}</el-tag></span></div>
            <dl class="mt-3 space-y-2 text-sm"><div><dt class="text-xs text-slate-500">{{ t('relayPlanning.templateGroup') }}</dt><dd class="break-words">{{ mapping.template_group_name || '-' }} (#{{ mapping.template_group_id }})</dd></div><div><dt class="text-xs text-slate-500">{{ t('relayPlanning.migrationSource') }}</dt><dd class="break-words">{{ mapping.source_group_name }} (#{{ mapping.source_group_id }})</dd></div><div><dt class="text-xs text-slate-500">{{ t('relayPlanning.managedGroups') }}</dt><dd class="break-words">{{ mapping.group_ids.join(', ') || '-' }}</dd></div></dl>
            <div v-if="mapping.warnings?.length" class="mt-2 break-words text-xs text-amber-700">{{ mapping.warnings.map(translateWarning).join('; ') }}</div>
			<el-alert v-if="containmentMode(mapping) !== 'none'" :data-testid="`mapping-containment-${mapping.id}`" class="mt-2" type="warning" :closable="false" show-icon :title="containmentTitle(mapping)" :description="t(containmentMode(mapping) === 'resume_exact' ? 'relayPlanning.exactResumeHelp' : 'relayPlanning.manualInterventionHelp')" />
			<el-alert v-else-if="mapping.alignment === 'drifted'" :data-testid="`mapping-drift-${mapping.id}`" class="mt-2" type="error" :closable="false" show-icon :title="t('relayPlanning.alignmentDrifted')" :description="mapping.alignment_differences?.map(translateWarning).join('; ')" />
			<el-alert v-if="mapping.active_operation?.lifecycle === 'blocked_external'" :data-testid="`mapping-external-blocker-${mapping.id}`" class="mt-2" type="error" :closable="false" show-icon :title="t('relayPlanning.externalBlocker')" :description="t('relayPlanning.externalBlockerDetail', { type: mapping.active_operation.external_blocker?.resource_type || '-', id: mapping.active_operation.external_blocker?.resource_id || '-' })" />
            <div v-if="mapping.department_suggestions?.length" class="mt-2 text-xs text-slate-500">{{ t('relayPlanning.departmentSuggestions') }}: {{ mapping.department_suggestions.map(departmentSuggestionLabel).join(', ') }}</div>
            <div class="mt-3 flex flex-wrap gap-2"><el-button v-if="recoveryAvailable(mapping, 'resume')" :data-testid="`resume-operation-${mapping.id}`" size="small" type="primary" :loading="recoveryLoading && recoveryMapping?.id === mapping.id" @click="reviewRecovery(mapping, 'resume')">{{ t('relayPlanning.continueToTarget') }}</el-button><el-button v-if="recoveryAvailable(mapping, 'restore')" :data-testid="`restore-operation-${mapping.id}`" size="small" type="warning" plain :loading="recoveryLoading && recoveryMapping?.id === mapping.id" @click="reviewRecovery(mapping, 'restore')">{{ t('relayPlanning.restoreBaseline') }}</el-button><el-button :data-testid="`replan-mapping-${mapping.id}`" size="small" type="primary" :disabled="replanLocked(mapping)" @click="replan(mapping)">{{ t(containmentMode(mapping) === 'resume_exact' ? 'relayPlanning.continueExactOperation' : 'relayPlanning.replan') }}</el-button><el-button :data-testid="`renew-mapping-${mapping.id}`" size="small" :icon="Calendar" :loading="renewalLoadingID === mapping.id" :disabled="mappingLocked(mapping) || (renewalLoadingID !== null && renewalLoadingID !== mapping.id)" @click="renewMapping(mapping)">{{ t('relayPlanning.renewSubscriptions') }}</el-button><el-button :data-testid="`rebind-mapping-${mapping.id}`" size="small" :loading="rebindPendingID === mapping.id" :disabled="mappingLocked(mapping) || (rebindPendingID !== null && rebindPendingID !== mapping.id)" @click="rebind(mapping)">{{ t('relayPlanning.rebind') }}</el-button><el-button :data-testid="`manage-accounts-${mapping.id}`" size="small" :disabled="mappingLocked(mapping)" @click="manageAccounts(mapping)">{{ t('relayPlanning.manageAccounts') }}</el-button></div>
          </article>
        </div>
        <el-table v-else-if="mappings.length" data-testid="mapping-table-layout" :data="paginatedMappings" stripe>
          <el-table-column prop="department_name" :label="t('relayPlanning.department')" min-width="150" />
          <el-table-column prop="platform" :label="t('relayPlanning.platform')" width="110" />
          <el-table-column :label="t('relayPlanning.templateGroup')" min-width="160"><template #default="scope">{{ scope.row.template_group_name || '-' }} (#{{ scope.row.template_group_id }})</template></el-table-column>
          <el-table-column :label="t('relayPlanning.migrationSource')" min-width="160"><template #default="scope">{{ scope.row.source_group_name }} (#{{ scope.row.source_group_id }})</template></el-table-column>
          <el-table-column :label="t('relayPlanning.managedGroups')" min-width="180"><template #default="scope">{{ scope.row.group_ids.join(', ') }}</template></el-table-column>
          <el-table-column :label="t('relayPlanning.status')" min-width="210"><template #default="scope"><span class="flex flex-wrap gap-1"><el-tag :type="scope.row.alignment === 'drifted' ? 'danger' : scope.row.alignment === 'operating' ? 'warning' : 'success'">{{ alignmentText(scope.row as RelayPlanningMapping) }}</el-tag><el-tag v-if="scope.row.active_operation" type="info">{{ operationLifecycleText(scope.row as RelayPlanningMapping) }}</el-tag><el-tag v-if="scope.row.status === 'needs_retry'" type="warning">{{ t('relayPlanning.needsRetry') }}</el-tag></span><div v-if="containmentMode(scope.row as RelayPlanningMapping) !== 'none'" :data-testid="`mapping-containment-${scope.row.id}`" class="mt-1 break-words text-xs font-medium text-amber-700">{{ containmentTitle(scope.row as RelayPlanningMapping) }}</div><div v-else-if="scope.row.alignment === 'drifted'" :data-testid="`mapping-drift-${scope.row.id}`" class="mt-1 break-words text-xs font-medium text-red-700">{{ scope.row.alignment_differences?.map(translateWarning).join('; ') }}</div><div v-if="scope.row.warnings?.length" class="mt-1 break-words text-xs text-amber-700">{{ scope.row.warnings.map(translateWarning).join('; ') }}</div><div v-if="scope.row.department_suggestions?.length" class="mt-1 break-words text-xs text-slate-500">{{ t('relayPlanning.departmentSuggestions') }}: {{ scope.row.department_suggestions.map(departmentSuggestionLabel).join(', ') }}</div></template></el-table-column>
          <el-table-column :label="t('relayPlanning.actions')" min-width="440"><template #default="scope"><el-button v-if="recoveryAvailable(scope.row as RelayPlanningMapping, 'resume')" :data-testid="`resume-operation-${scope.row.id}`" link type="primary" @click="reviewRecovery(scope.row as RelayPlanningMapping, 'resume')">{{ t('relayPlanning.continueToTarget') }}</el-button><el-button v-if="recoveryAvailable(scope.row as RelayPlanningMapping, 'restore')" :data-testid="`restore-operation-${scope.row.id}`" link type="warning" @click="reviewRecovery(scope.row as RelayPlanningMapping, 'restore')">{{ t('relayPlanning.restoreBaseline') }}</el-button><el-button :data-testid="`replan-mapping-${scope.row.id}`" link type="primary" :disabled="replanLocked(scope.row as RelayPlanningMapping)" @click="replan(scope.row as RelayPlanningMapping)">{{ t(containmentMode(scope.row as RelayPlanningMapping) === 'resume_exact' ? 'relayPlanning.continueExactOperation' : 'relayPlanning.replan') }}</el-button><el-button :data-testid="`renew-mapping-${scope.row.id}`" link type="primary" :icon="Calendar" :loading="renewalLoadingID === scope.row.id" :disabled="mappingLocked(scope.row as RelayPlanningMapping) || (renewalLoadingID !== null && renewalLoadingID !== scope.row.id)" @click="renewMapping(scope.row as RelayPlanningMapping)">{{ t('relayPlanning.renewSubscriptions') }}</el-button><el-button :data-testid="`rebind-mapping-${scope.row.id}`" link type="primary" :loading="rebindPendingID === scope.row.id" :disabled="mappingLocked(scope.row as RelayPlanningMapping) || (rebindPendingID !== null && rebindPendingID !== scope.row.id)" @click="rebind(scope.row as RelayPlanningMapping)">{{ t('relayPlanning.rebind') }}</el-button><el-button :data-testid="`manage-accounts-${scope.row.id}`" link type="primary" :disabled="mappingLocked(scope.row as RelayPlanningMapping)" @click="manageAccounts(scope.row as RelayPlanningMapping)">{{ t('relayPlanning.manageAccounts') }}</el-button></template></el-table-column>
        </el-table>
        <el-pagination v-if="mappings.length > mappingPageSize" data-testid="mapping-pagination" class="mt-4 justify-end" size="small" background :layout="desktopPagination ? 'prev, pager, next' : 'prev, slot, next'" :pager-count="5" :current-page="mappingPage" :page-size="mappingPageSize" :total="mappings.length" @current-change="mappingPage = $event">
          <span v-if="!desktopPagination" class="px-1 text-xs text-slate-500">{{ baseT('pagination.pageOf', { page: mappingPage, pages: Math.ceil(mappings.length / mappingPageSize) }) }}</span>
        </el-pagination>
        <div v-if="accountMapping" class="mt-4 border-t border-slate-200 pt-4">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div><div class="text-sm font-semibold text-slate-900">{{ t('relayPlanning.accountRelationships') }}</div><div class="text-xs text-slate-500">{{ accountMapping.department_name }} · {{ accountMapping.platform }}</div></div>
			<div class="flex gap-2">
				<el-button v-if="!accountMapping.account_management_initialized" :data-testid="`adopt-current-accounts-${accountMapping.id}`" type="primary" :loading="accountSaving" :disabled="mappingLocked(accountMapping)" @click="adoptCurrentAccounts(accountMapping)">{{ t('relayPlanning.adoptCurrent') }}</el-button>
				<el-button v-else :data-testid="`save-desired-accounts-${accountMapping.id}`" type="primary" :loading="accountSaving" :disabled="mappingLocked(accountMapping)" @click="saveDesiredAccounts(accountMapping)">{{ t('relayPlanning.saveDesiredAccounts') }}</el-button>
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
								<el-tooltip :content="t('relayPlanning.moveUp')"><el-button :data-testid="`move-account-up-${accountMapping.id}-${pool.target_group_id}-${account.id}`" circle size="small" :icon="CaretTop" :disabled="mappingLocked(accountMapping) || accountIndex === 0" :aria-label="t('relayPlanning.moveUp')" @click="reorderAccounts(accountMapping.id, pool.target_group_id, account.id, -1)" /></el-tooltip>
								<el-tooltip :content="t('relayPlanning.moveDown')"><el-button circle size="small" :icon="CaretBottom" :disabled="mappingLocked(accountMapping) || accountIndex === accountDrafts[accountMapping.id][String(pool.target_group_id)].length - 1" :aria-label="t('relayPlanning.moveDown')" @click="reorderAccounts(accountMapping.id, pool.target_group_id, account.id, 1)" /></el-tooltip>
								<el-tooltip :content="t('relayPlanning.remove')"><el-button circle size="small" type="danger" plain :icon="Delete" :disabled="mappingLocked(accountMapping)" :aria-label="t('relayPlanning.remove')" @click="removeAccountFromTarget(accountMapping.id, pool.target_group_id, account.id)" /></el-tooltip>
							</span>
						</div>
					</div>
					<el-empty v-else :description="t('relayPlanning.noDesiredAccounts')" :image-size="48" />
						<el-input :data-testid="`account-search-${accountMapping.id}-${pool.target_group_id}`" :model-value="accountSearchQueries[accountSearchKey(accountMapping.id, pool.target_group_id)] || ''" :loading="accountSearchLoading[accountSearchKey(accountMapping.id, pool.target_group_id)]" :disabled="mappingLocked(accountMapping)" clearable class="mt-3" :placeholder="t('relayPlanning.searchAccounts')" @input="(value) => scheduleManagedAccountSearch(accountMapping, pool.target_group_id, value)" />
						<el-alert v-if="accountSearchErrors[accountSearchKey(accountMapping.id, pool.target_group_id)]" class="mt-2" type="error" :closable="false" show-icon :title="accountSearchErrors[accountSearchKey(accountMapping.id, pool.target_group_id)]" />
						<el-button v-if="accountSearchErrors[accountSearchKey(accountMapping.id, pool.target_group_id)]" class="mt-1 !ml-0" size="small" type="primary" link @click="searchManagedAccountPage(accountMapping, pool.target_group_id, accountSearchPages[accountSearchKey(accountMapping.id, pool.target_group_id)]?.page ?? 1)">{{ t('relayPlanning.retry') }}</el-button>
						<div v-if="accountSearchResults[accountSearchKey(accountMapping.id, pool.target_group_id)]?.length" class="mt-2 divide-y divide-slate-100 border-y border-slate-100">
						<div v-for="account in accountSearchResults[accountSearchKey(accountMapping.id, pool.target_group_id)]" :key="account.id" class="flex items-center justify-between gap-3 py-2 text-sm">
							<span class="min-w-0"><span class="block truncate font-medium">{{ account.name }} (#{{ account.id }})</span><span class="block truncate text-xs" :class="account.status !== 'active' || !account.schedulable ? 'text-amber-700' : 'text-slate-500'">{{ account.type }} · {{ account.status }} · {{ account.schedulable ? t('relayPlanning.schedulable') : t('relayPlanning.notSchedulable') }}</span></span>
							<el-tooltip :content="t('relayPlanning.add')"><el-button :data-testid="`add-account-${accountMapping.id}-${pool.target_group_id}-${account.id}`" circle size="small" type="primary" :icon="Plus" :disabled="mappingLocked(accountMapping) || accountDrafts[accountMapping.id]?.[String(pool.target_group_id)]?.some((item) => item.id === account.id)" :aria-label="t('relayPlanning.add')" @click="addAccountToTarget(accountMapping.id, pool.target_group_id, account)" /></el-tooltip>
						</div>
						<el-pagination
							v-if="accountSearchPages[accountSearchKey(accountMapping.id, pool.target_group_id)]?.total > accountSearchPages[accountSearchKey(accountMapping.id, pool.target_group_id)]?.page_size"
							:data-testid="`account-pagination-${accountMapping.id}-${pool.target_group_id}`"
							class="mt-2 justify-end"
							size="small"
							background
							:layout="desktopPagination ? 'prev, pager, next' : 'prev, slot, next'"
							:pager-count="5"
							:current-page="accountSearchPages[accountSearchKey(accountMapping.id, pool.target_group_id)].page"
							:page-size="accountSearchPages[accountSearchKey(accountMapping.id, pool.target_group_id)].page_size"
							:total="accountSearchPages[accountSearchKey(accountMapping.id, pool.target_group_id)].total"
							:disabled="mappingLocked(accountMapping) || accountSearchLoading[accountSearchKey(accountMapping.id, pool.target_group_id)]"
							@current-change="(page) => searchManagedAccountPage(accountMapping, pool.target_group_id, page)"
						>
							<span v-if="!desktopPagination" class="px-1 text-xs text-slate-500">{{ baseT('pagination.pageOf', { page: accountSearchPages[accountSearchKey(accountMapping.id, pool.target_group_id)].page, pages: Math.ceil(accountSearchPages[accountSearchKey(accountMapping.id, pool.target_group_id)].total / accountSearchPages[accountSearchKey(accountMapping.id, pool.target_group_id)].page_size) }) }}</span>
						</el-pagination>
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
        :model-value="confirmDialogOpen"
        :title="t('relayPlanning.confirmPlan')"
        append-to-body
        align-center
        width="min(100%, 32rem)"
        :close-on-click-modal="!executing"
        :close-on-press-escape="!executing"
        @update:model-value="(value) => { if (!value) closeConfirmation() }"
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
	          <el-button :disabled="executing" @click="closeConfirmation">{{ t('relayPlanning.cancel') }}</el-button>
          <el-button data-testid="confirm-execution" type="danger" :loading="executing" @click="executeConfirmed">{{ t(activeMappingID ? 'relayPlanning.applyReviewedChanges' : 'relayPlanning.createAndMigrate') }}</el-button>
			</template>
		</el-dialog>

		<el-dialog
			v-model="recoveryDialogOpen"
			data-testid="recovery-dialog"
			:title="t(recoveryDirection === 'resume' ? 'relayPlanning.continueToTarget' : 'relayPlanning.restoreBaseline')"
			append-to-body
			align-center
			width="min(100%, 38rem)"
			:show-close="!recoveryConfirming"
			:close-on-click-modal="!recoveryConfirming"
			:close-on-press-escape="!recoveryConfirming"
		>
			<div v-loading="recoveryLoading" class="min-h-24">
				<el-alert v-if="recoveryError" data-testid="recovery-error" type="error" :closable="false" show-icon :title="recoveryError" />
				<template v-if="recoveryPreview">
					<el-alert v-if="recoveryPreview.resume_only" class="mb-3" type="warning" :closable="false" show-icon :title="t('relayPlanning.resumeOnly')" />
					<el-alert v-if="recoveryPreview.external_blocker" data-testid="recovery-external-blocker" class="mb-3" type="error" :closable="false" show-icon :title="t('relayPlanning.externalBlocker')" :description="t('relayPlanning.externalBlockerDetail', { type: recoveryPreview.external_blocker.resource_type, id: recoveryPreview.external_blocker.resource_id })" />
					<dl class="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm">
						<dt class="text-slate-500">{{ t('relayPlanning.operation') }}</dt><dd class="break-all font-medium text-slate-900">#{{ recoveryPreview.operation.id }}</dd>
						<dt class="text-slate-500">{{ t('relayPlanning.direction') }}</dt><dd class="font-medium text-slate-900">{{ t(recoveryPreview.direction === 'resume' ? 'relayPlanning.continueToTarget' : 'relayPlanning.restoreBaseline') }}</dd>
						<dt class="text-slate-500">{{ t('relayPlanning.affectedMappings') }}</dt><dd class="break-all font-medium text-slate-900">{{ recoveryPreview.operation.affected_mapping_ids.join(', ') }}</dd>
					</dl>
					<div class="mt-4 max-h-64 divide-y divide-slate-200 overflow-y-auto border-y border-slate-200">
						<div v-for="step in recoveryPreview.operation.steps" :key="step.id" class="flex flex-wrap items-start justify-between gap-3 py-3 text-sm">
							<span class="min-w-0"><span class="block break-all font-medium text-slate-900">{{ step.step_key }}</span><span class="block break-words text-xs text-slate-500">{{ step.relationship_type }} · {{ step.action }}</span></span>
							<el-tag size="small" :type="step.lifecycle === 'blocked_external' || step.lifecycle === 'failed' ? 'danger' : step.lifecycle === 'readback_verified' ? 'success' : 'info'">{{ step.lifecycle }}</el-tag>
						</div>
					</div>
				</template>
			</div>
			<template #footer>
				<div class="flex flex-wrap justify-end gap-2">
					<el-button :disabled="recoveryConfirming" @click="recoveryDialogOpen = false">{{ t('relayPlanning.cancel') }}</el-button>
					<el-button data-testid="confirm-recovery" :type="recoveryDirection === 'restore' ? 'warning' : 'primary'" :loading="recoveryConfirming" :disabled="!recoveryPreview || Boolean(recoveryPreview.external_blocker)" @click="confirmRecovery">{{ t('relayPlanning.confirmRecovery') }}</el-button>
				</div>
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
