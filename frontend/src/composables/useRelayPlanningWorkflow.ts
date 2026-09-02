import { computed, reactive, ref } from 'vue'
import type {
  RelayPlanningAccount,
  RelayPlanningAccountSearchPage,
  RelayPlanningAssignment,
  RelayPlanningExecution,
  RelayPlanningMapping,
  RelayPlanningMemberAction,
  RelayPlanningPlan,
  RelayPlanningRequest,
  RelayPlanningUserSearchItem,
  RelayPlanningUserSearchPage,
} from '@/api/relayPlanning'

export interface RelayPlanningReviewedPreviewRequest {
  selected_user_ids?: number[]
  assignments?: RelayPlanningAssignment[]
  member_sources?: Record<string, number>
  removed_user_ids?: number[]
  member_actions?: Record<string, RelayPlanningMemberAction>
  adopt_relay_user_ids?: number[]
}

export interface RelayPlanningExecuteRequest extends RelayPlanningRequest {
  operation_key: string
  expected_relationship_fingerprint: string
}

export interface RelayPlanningWorkflowOptions {
  previewInitial: (request: RelayPlanningRequest) => Promise<RelayPlanningPlan | null>
  previewReplan: (mappingID: number, request: RelayPlanningReviewedPreviewRequest) => Promise<RelayPlanningPlan | null>
  executeInitial: (request: RelayPlanningExecuteRequest) => Promise<RelayPlanningExecution | null>
  executeReplan: (mappingID: number, request: RelayPlanningExecuteRequest) => Promise<RelayPlanningExecution | null>
  searchUsers: (params: { provider_id: number; platform: string; q: string; page: number; page_size: number }) => Promise<RelayPlanningUserSearchPage>
  searchAccounts: (params: { provider_id: number; platform: string; q: string; page: number; page_size: number }) => Promise<RelayPlanningAccountSearchPage>
  createOperationKey: () => string
  reservedGroups: () => Array<{ id: number; name: string }>
  searchError: (error: unknown, kind: 'user' | 'account') => string
  onPlanApplied?: () => void
}

interface RelayPlanningSearchState<T> {
  query: string
  items: T[]
  total: number
  page: number
  page_size: number
  loading: boolean
  error: string
}

export function useRelayPlanningWorkflow(options: RelayPlanningWorkflowOptions) {
  const loading = ref(false)
  const confirming = ref(false)
  const executing = ref(false)
  const confirmDialogOpen = ref(false)
  const plan = ref<RelayPlanningPlan | null>(null)
  const lastExecution = ref<RelayPlanningExecution | null>(null)
  const activeMappingID = ref<number | null>(null)
	const reviewLocked = ref(false)
  const activeMappingMemberAssignments = ref<Record<string, number>>({})
  const activeMappingMemberSources = ref<Record<string, number>>({})
  const selectedUserIDs = ref<Set<number>>(new Set())
  const selectedUnmanagedRelayIDs = ref<Set<number>>(new Set())
  const removedUserIDs = ref<Set<number>>(new Set())
  const removalSources = ref<Record<string, number | null>>({})
  const lockedRemovalSourceUserIDs = ref<Set<number>>(new Set())
  const memberActions = ref<Record<string, RelayPlanningMemberAction>>({})
  const managedAssignmentsByUser = ref<Record<string, NonNullable<RelayPlanningUserSearchItem['managed_assignments']>>>({})
  const memberSources = ref<Record<string, number>>({})
  const operationKey = ref('')
  const suggestedGroupAccountDefaults = ref<RelayPlanningAccount[]>([])
  const targetSearches = reactive<Record<number, RelayPlanningSearchState<RelayPlanningUserSearchItem>>>({})
  const previewAccountSearches = reactive<Record<number, RelayPlanningSearchState<RelayPlanningAccount>>>({})
  const targetSearchTimers = new Map<number, ReturnType<typeof setTimeout>>()
  const accountSearchTimers = new Map<number, ReturnType<typeof setTimeout>>()
  const targetSearchRequestIDs = new Map<number, number>()
  const accountSearchRequestIDs = new Map<number, number>()
  const searchDelayMS = 300
  let planRequestGeneration = 0

  function invalidatePlanRequests(closeConfirmation = true) {
    planRequestGeneration += 1
    loading.value = false
    confirming.value = false
    executing.value = false
    if (closeConfirmation) confirmDialogOpen.value = false
    return planRequestGeneration
  }

  function isCurrentPlanRequest(generation: number) {
    return generation === planRequestGeneration
  }

  function markPlanEdited() {
		if (reviewLocked.value) return false
    invalidatePlanRequests()
		return true
  }

  const displayedEligibleMemberCount = computed(() => plan.value?.candidates.filter((candidate) => (
    candidate.eligible && (!activeMappingID.value || selectedUserIDs.value.has(candidate.user_id))
  )).length ?? 0)
  const unassignedCandidates = computed(() => plan.value?.candidates.filter((candidate) => (
    candidate.can_add && !selectedUserIDs.value.has(candidate.user_id)
  )) ?? [])
  const targetNameErrorCodes = computed(() => Object.fromEntries((plan.value?.assignments ?? []).flatMap((assignment) => {
    const code = targetNameErrorCode(assignment.index)
    return code ? [[assignment.index, code]] : []
  })))
  const hasTargetNameErrors = computed(() => Object.keys(targetNameErrorCodes.value).length > 0)
  const hasUnreviewedRemovalSources = computed(() => Array.from(removedUserIDs.value).some((userID) => (
    removalSources.value[String(userID)] == null
  )))

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
      assignment.total_cost = assignment.user_ids.reduce((total, userID) => total + (costs.get(userID) ?? 0), 0)
        + (unmanagedCosts.get(assignment.target_group_id ?? 0) ?? 0)
    }
    selectedUserIDs.value = new Set(plan.value.assignments.flatMap((assignment) => assignment.user_ids))
  }

  function emptySearchState<T>(): RelayPlanningSearchState<T> {
    return { query: '', items: [], total: 0, page: 1, page_size: 20, loading: false, error: '' }
  }

  function targetSearch(targetIndex: number) {
    return targetSearches[targetIndex] ??= emptySearchState<RelayPlanningUserSearchItem>()
  }

  function previewAccountSearch(targetIndex: number) {
    return previewAccountSearches[targetIndex] ??= emptySearchState<RelayPlanningAccount>()
  }

  function clearReviewedSearchState() {
    for (const timer of targetSearchTimers.values()) clearTimeout(timer)
    for (const timer of accountSearchTimers.values()) clearTimeout(timer)
    targetSearchTimers.clear()
    accountSearchTimers.clear()
    for (const key of Object.keys(targetSearches)) delete targetSearches[Number(key)]
    for (const key of Object.keys(previewAccountSearches)) delete previewAccountSearches[Number(key)]
  }

  function applyPlan(next: RelayPlanningPlan | null) {
    clearReviewedSearchState()
    options.onPlanApplied?.()
    plan.value = next
    if (!next) return
		suggestedGroupAccountDefaults.value = (next.template_accounts ?? next.assignments[0]?.accounts ?? []).map((account) => ({ ...account }))
    for (const assignment of next.assignments) {
      assignment.accounts ??= []
      assignment.desired_accounts = assignment.accounts.map((account, index) => ({
        account_id: account.id,
        priority: Number(account.priority || index + 1),
      }))
    }
    memberSources.value = Object.fromEntries(next.candidates.map((candidate) => [
      String(candidate.user_id),
      Number(candidate.source_group_id || 0),
    ]))
    recalculateAssignments()
  }

  function assignmentPayload(): RelayPlanningAssignment[] {
    return (plan.value?.assignments ?? []).map((assignment) => ({
      index: assignment.index,
      total_cost: assignment.total_cost,
      user_ids: [...(assignment.user_ids ?? [])],
      target_group_id: assignment.target_group_id,
      target_group_name: assignment.target_group_name,
      rename_selected: Boolean(assignment.rename_selected),
      desired_accounts: (assignment.accounts ?? []).map((account, index) => ({
        account_id: account.id,
        priority: Number(account.priority || index + 1),
      })),
      accounts: [],
    }))
  }

  function memberSourcesPayload(
    userIDs = selectedUserIDs.value,
    sources = memberSources.value,
  ): Record<string, number> {
    return Object.fromEntries(Array.from(userIDs).map((userID) => [
      String(userID),
      Number(sources[String(userID)] || 0),
    ]))
  }

  function reviewedState(
    assignments = assignmentPayload(),
    selected = selectedUserIDs.value,
    sources = memberSources.value,
    actions = memberActions.value,
  ): Required<Pick<RelayPlanningReviewedPreviewRequest,
    'selected_user_ids' | 'assignments' | 'member_sources' | 'removed_user_ids' | 'member_actions' | 'adopt_relay_user_ids'>> {
    const sourceUserIDs = new Set(selected)
    const reviewedSources = { ...sources }
    for (const userID of removedUserIDs.value) {
      const sourceGroupID = removalSources.value[String(userID)]
      if (sourceGroupID != null) {
        sourceUserIDs.add(userID)
        reviewedSources[String(userID)] = sourceGroupID
      }
    }
    return {
      selected_user_ids: Array.from(selected),
      assignments,
      member_sources: memberSourcesPayload(sourceUserIDs, reviewedSources),
      removed_user_ids: Array.from(removedUserIDs.value),
      member_actions: actions,
      adopt_relay_user_ids: Array.from(selectedUnmanagedRelayIDs.value),
    }
  }

  function operationEntryNeedsRetry(entry: Record<string, string>): boolean {
    return Boolean(
      entry.error
      || entry.status === 'failed'
      || entry.subscription === 'failed'
      || entry.source_removal === 'failed'
      || entry.api_keys?.includes(':failed:'),
    )
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
      if (
        !key.startsWith('member:')
        || entry.action !== 'move_here'
        || !operationEntryNeedsRetry(entry)
        || userID <= 0
        || fromMappingID <= 0
      ) return []
      return [[String(userID), { mode: 'move_here' as const, from_mapping_id: fromMappingID }]]
    }))
  }

  async function preview(request: RelayPlanningRequest) {
    const generation = invalidatePlanRequests()
    loading.value = true
    try {
      lastExecution.value = null
      const nextPlan = await options.previewInitial(request)
      if (!isCurrentPlanRequest(generation)) return
      applyPlan(nextPlan)
      activeMappingID.value = null
		reviewLocked.value = false
      activeMappingMemberAssignments.value = {}
      activeMappingMemberSources.value = {}
      selectedUnmanagedRelayIDs.value = new Set()
      removedUserIDs.value = new Set()
      removalSources.value = {}
      lockedRemovalSourceUserIDs.value = new Set()
      memberActions.value = {}
      managedAssignmentsByUser.value = {}
      operationKey.value = options.createOperationKey()
    } catch (error) {
      if (isCurrentPlanRequest(generation)) throw error
    } finally {
      if (isCurrentPlanRequest(generation)) loading.value = false
    }
  }

  async function openReplan(mapping: RelayPlanningMapping) {
    const generation = invalidatePlanRequests()
    lastExecution.value = null
    const retryRemovedUserIDs = retryRemovalUserIDs(mapping)
    const lockedRetryRemovedUserIDs = retryRemovedUserIDs.filter((userID) => {
      const entry = mapping.operation_state?.[`member:${userID}`]
      return entry?.source_reviewed === 'true' || entry?.source_group_id !== undefined
    })
    const retryActions = retryMemberActions(mapping)
    const retryRequest: RelayPlanningReviewedPreviewRequest = {
      ...(retryRemovedUserIDs.length ? { removed_user_ids: retryRemovedUserIDs } : {}),
      ...(Object.keys(retryActions).length ? { member_actions: retryActions } : {}),
    }
    try {
      const nextPlan = await options.previewReplan(mapping.id, retryRequest)
      if (!isCurrentPlanRequest(generation)) return null
      applyPlan(nextPlan)
      activeMappingID.value = mapping.id
		reviewLocked.value = mapping.status === 'needs_retry'
      activeMappingMemberAssignments.value = { ...(mapping.member_assignments ?? {}) }
      activeMappingMemberSources.value = { ...(mapping.member_sources ?? {}) }
      selectedUnmanagedRelayIDs.value = new Set()
      removedUserIDs.value = new Set(retryRemovedUserIDs)
      lockedRemovalSourceUserIDs.value = new Set(lockedRetryRemovedUserIDs)
		removalSources.value = Object.fromEntries(retryRemovedUserIDs.map((userID) => {
        const key = String(userID)
        const entry = mapping.operation_state?.[`member:${key}`]
			if (Object.prototype.hasOwnProperty.call(mapping.member_sources ?? {}, key)) return [key, Number(mapping.member_sources?.[key] ?? 0)]
			if (entry?.source_reviewed === 'true' || entry?.source_group_id !== undefined) return [key, Number(entry.source_group_id || 0)]
			return [key, null]
		}))
      memberActions.value = retryActions
      managedAssignmentsByUser.value = {}
      operationKey.value = options.createOperationKey()
      return nextPlan
    } catch (error) {
      if (isCurrentPlanRequest(generation)) throw error
      return null
    }
  }

  function setTargetName(targetIndex: number, name: string) {
    const assignment = plan.value?.assignments.find((item) => item.index === targetIndex)
    if (!assignment || assignment.target_group_name === name) return
    if (!markPlanEdited()) return
    assignment.target_group_name = name
  }

  function targetNameErrorCode(targetIndex: number): 'required' | 'too_long' | 'control' | 'duplicate' | 'occupied' | '' {
    const assignment = plan.value?.assignments.find((item) => item.index === targetIndex)
    if (!assignment || assignment.target_unavailable) return ''
    const name = String(assignment.target_group_name || '').trim()
    if (!name) return 'required'
    if (Array.from(name).length > 100) return 'too_long'
		if (/[\u0000-\u001f\u007f-\u009f]/.test(name)) return 'control'
    if ((plan.value?.assignments ?? []).some((item) => (
      item.index !== targetIndex && String(item.target_group_name || '').trim() === name
    ))) return 'duplicate'
    if (options.reservedGroups().some((group) => (
      group.name === name && group.id !== Number(assignment.target_group_id || 0)
    ))) return 'occupied'
    return ''
  }

  function recalculateProposedTargetNames() {
		if (!plan.value) return
    const used = new Set(options.reservedGroups().map((group) => group.name))
    let sequence = 1
    for (const assignment of plan.value.assignments) {
			if (assignment.target_group_id) continue
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

  function addSuggestedGroup() {
		if (!plan.value) return
    if (!markPlanEdited()) return
    const index = plan.value.assignments.length
    const accounts = suggestedGroupAccountDefaults.value.map((account) => ({ ...account }))
    plan.value.assignments.push({
      index,
      total_cost: 0,
      user_ids: [],
      target_group_name: '',
      desired_accounts: accounts.map((account, accountIndex) => ({
        account_id: account.id,
        priority: Number(account.priority || accountIndex + 1),
      })),
      accounts,
    })
    plan.value.group_count = plan.value.assignments.length
    recalculateProposedTargetNames()
  }

  function removeSuggestedGroup(targetIndex: number) {
		if (!plan.value || plan.value.assignments.length <= 1) return
		const target = plan.value.assignments.find((assignment) => assignment.index === targetIndex)
		if (!target || (activeMappingID.value && target.target_group_id)) return
    if (!markPlanEdited()) return
    clearReviewedSearchState()
    options.onPlanApplied?.()
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

  function moveCandidate(userID: number, targetIndex: number | null) {
    if (!plan.value) return
    if (candidateAssignmentIndex(userID) === targetIndex) return
    if (!markPlanEdited()) return
    for (const assignment of plan.value.assignments) {
      assignment.user_ids = (assignment.user_ids ?? []).filter((id) => id !== userID)
    }
    if (targetIndex !== null) plan.value.assignments[targetIndex]?.user_ids.push(userID)
    const nextRemoved = new Set(removedUserIDs.value)
		const nextRemovalSources = { ...removalSources.value }
    if (targetIndex === null && activeMappingMemberAssignments.value[String(userID)]) {
      nextRemoved.add(userID)
      if (Object.prototype.hasOwnProperty.call(activeMappingMemberSources.value, String(userID))) {
				nextRemovalSources[String(userID)] = Number(activeMappingMemberSources.value[String(userID)] || 0)
			} else {
				nextRemovalSources[String(userID)] = null
			}
    } else {
      nextRemoved.delete(userID)
			delete nextRemovalSources[String(userID)]
    }
    removedUserIDs.value = nextRemoved
    removalSources.value = nextRemovalSources
		const candidate = plan.value.candidates.find((item) => item.user_id === userID)
		if (candidate) {
			const targetID = targetIndex === null ? 0 : Number(plan.value.assignments[targetIndex]?.target_group_id || 0)
			const baselineTarget = Number(activeMappingMemberAssignments.value[String(userID)] || 0)
			candidate.disposition = targetIndex === null
				? candidate.can_add ? 'available' : 'excluded'
				: baselineTarget > 0 && targetID === baselineTarget && candidate.current_group_ids?.includes(targetID)
					? 'retained'
					: Number(memberSources.value[String(userID)] ?? candidate.source_group_id ?? 0) > 0 ? 'migration' : 'target_only'
		}
    recalculateAssignments()
  }

  function toggleCandidate(userID: number, checked: boolean) {
    if (!plan.value) return
    if (!checked) {
      moveCandidate(userID, null)
      return
    }
    const target = plan.value.assignments.reduce((best, assignment, index, all) => (
      assignment.total_cost < all[best].total_cost ? index : best
    ), 0)
    moveCandidate(userID, target)
  }

  function candidateAssignmentIndex(userID: number): number | null {
    const index = plan.value?.assignments.findIndex((assignment) => assignment.user_ids?.includes(userID)) ?? -1
    return index >= 0 ? index : null
  }

  function candidateLabel(userID: number): string {
    const candidate = plan.value?.candidates.find((item) => item.user_id === userID)
    return candidate?.username || candidate?.email || `User ${userID}`
  }

  function setMemberSource(userID: number, groupID: number) {
		const key = String(userID)
		if (removedUserIDs.value.has(userID)) {
			if (lockedRemovalSourceUserIDs.value.has(userID)) return
			if (removalSources.value[key] === groupID) return
			if (!markPlanEdited()) return
			removalSources.value = { ...removalSources.value, [key]: groupID }
			return
		}
		if (Number(memberSources.value[key] || 0) === groupID) return
    if (!markPlanEdited()) return
		memberSources.value = { ...memberSources.value, [key]: groupID }
		const candidate = plan.value?.candidates.find((item) => item.user_id === userID)
		if (candidate) candidate.disposition = groupID > 0 ? 'migration' : 'target_only'
  }

  function toggleUnmanagedRelayUser(relayUserID: number, checked: boolean) {
    if (selectedUnmanagedRelayIDs.value.has(relayUserID) === checked) return
    if (!markPlanEdited()) return
    const next = new Set(selectedUnmanagedRelayIDs.value)
    if (checked) next.add(relayUserID)
    else next.delete(relayUserID)
    selectedUnmanagedRelayIDs.value = next
  }

  function toggleTargetRename(targetIndex: number, checked: boolean) {
    const assignment = plan.value?.assignments.find((item) => item.index === targetIndex)
    if (!assignment?.target_group_id || assignment.target_unavailable) return
    if (Boolean(assignment.rename_selected) === checked) return
    if (!markPlanEdited()) return
    assignment.rename_selected = checked
    assignment.target_group_name = checked
      ? assignment.suggested_target_group_name || assignment.current_target_group_name || ''
      : assignment.current_target_group_name || ''
  }

  function applyAllTargetNames() {
    const assignments = (plan.value?.assignments ?? []).filter((assignment) => (
      assignment.target_group_id
      && !assignment.target_unavailable
      && (!assignment.rename_selected || assignment.target_group_name !== (assignment.suggested_target_group_name || assignment.current_target_group_name || ''))
    ))
    if (assignments.length === 0) return
    if (!markPlanEdited()) return
    for (const assignment of assignments) {
      assignment.rename_selected = true
      assignment.target_group_name = assignment.suggested_target_group_name || assignment.current_target_group_name || ''
    }
  }

  function syncPreviewAccountPriorities(targetIndex: number) {
    const assignment = plan.value?.assignments.find((item) => item.index === targetIndex)
    if (!assignment) return
    assignment.accounts.forEach((account, index) => { account.priority = index + 1 })
    assignment.desired_accounts = assignment.accounts.map((account, index) => ({ account_id: account.id, priority: index + 1 }))
  }

  function addPreviewAccount(targetIndex: number, account: RelayPlanningAccount) {
    const assignment = plan.value?.assignments.find((item) => item.index === targetIndex)
    if (!assignment || assignment.accounts.some((item) => item.id === account.id)) return
    if (!markPlanEdited()) return
    assignment.accounts.push({ ...account, priority: assignment.accounts.length + 1 })
    syncPreviewAccountPriorities(targetIndex)
    const search = previewAccountSearch(targetIndex)
    Object.assign(search, emptySearchState<RelayPlanningAccount>())
  }

  function movePreviewAccount(targetIndex: number, accountID: number, offset: number) {
    const assignment = plan.value?.assignments.find((item) => item.index === targetIndex)
    if (!assignment) return
    const index = assignment.accounts.findIndex((account) => account.id === accountID)
    const nextIndex = index + offset
    if (index < 0 || nextIndex < 0 || nextIndex >= assignment.accounts.length) return
    if (!markPlanEdited()) return
    ;[assignment.accounts[index], assignment.accounts[nextIndex]] = [assignment.accounts[nextIndex], assignment.accounts[index]]
    syncPreviewAccountPriorities(targetIndex)
  }

  function removePreviewAccount(targetIndex: number, accountID: number) {
    const assignment = plan.value?.assignments.find((item) => item.index === targetIndex)
    if (!assignment || !assignment.accounts.some((account) => account.id === accountID)) return
    if (!markPlanEdited()) return
    assignment.accounts = assignment.accounts.filter((account) => account.id !== accountID)
    syncPreviewAccountPriorities(targetIndex)
  }

  function setMemberAction(userID: number, mode: RelayPlanningMemberAction['mode']) {
    const current = memberActions.value[String(userID)]
    if (!current || current.mode === mode) return
    if (!markPlanEdited()) return
    memberActions.value = {
      ...memberActions.value,
      [String(userID)]: { ...current, mode },
    }
  }

  async function addSearchedUser(targetIndex: number, item: RelayPlanningUserSearchItem) {
		if (!plan.value || !item.selectable || reviewLocked.value) return
    const assignments = assignmentPayload()
    for (const assignment of assignments) {
      assignment.user_ids = assignment.user_ids.filter((userID) => userID !== item.user_id)
    }
    const target = assignments.find((assignment) => assignment.index === targetIndex)
    if (!target) return
    const search = targetSearch(targetIndex)
    const previous = targetSearchTimers.get(targetIndex)
    if (previous) clearTimeout(previous)
    const searchRequestID = (targetSearchRequestIDs.get(targetIndex) ?? 0) + 1
    targetSearchRequestIDs.set(targetIndex, searchRequestID)
    const generation = invalidatePlanRequests()
    search.loading = true
    target.user_ids.push(item.user_id)
    const selected = new Set(assignments.flatMap((assignment) => assignment.user_ids))
    const nextMemberSources = { ...memberSources.value }
    nextMemberSources[String(item.user_id)] ??= 0
    let nextManagedAssignmentsByUser = managedAssignmentsByUser.value
    let nextMemberActions = memberActions.value
    const managedAssignments = (item.managed_assignments ?? []).filter((assignment) => assignment.mapping_id !== activeMappingID.value)
    if (managedAssignments.length > 0) {
      nextManagedAssignmentsByUser = {
        ...managedAssignmentsByUser.value,
        [String(item.user_id)]: managedAssignments,
      }
      nextMemberActions = {
        ...memberActions.value,
        [String(item.user_id)]: memberActions.value[String(item.user_id)] ?? {
          mode: 'move_here',
          from_mapping_id: managedAssignments[0].mapping_id,
        },
      }
    }
    const request = reviewedState(assignments, selected, nextMemberSources, nextMemberActions)
    request.selected_user_ids.sort((left, right) => left - right)
    try {
      const nextPlan = activeMappingID.value
        ? await options.previewReplan(activeMappingID.value, request)
        : await options.previewInitial({
            provider_id: plan.value.provider_id,
            department_id: plan.value.department_id,
            platform: plan.value.platform,
            template_group_id: plan.value.template_group_id,
            source_group_id: plan.value.source_group_id,
            weekly_cost_target: plan.value.weekly_cost_target,
            ...request,
          })
      if (!isCurrentPlanRequest(generation) || !nextPlan) return
      applyPlan(nextPlan)
      managedAssignmentsByUser.value = nextManagedAssignmentsByUser
      memberActions.value = nextMemberActions
    } catch (error) {
      if (isCurrentPlanRequest(generation)) throw error
    } finally {
      if (targetSearchRequestIDs.get(targetIndex) === searchRequestID) search.loading = false
    }
  }

  function scheduleUserSearch(targetIndex: number, value: string | number) {
    if (!plan.value) return
    const query = String(value || '').trim()
    const search = targetSearch(targetIndex)
    search.query = query
    search.error = ''
    const previous = targetSearchTimers.get(targetIndex)
    if (previous) clearTimeout(previous)
    const requestID = (targetSearchRequestIDs.get(targetIndex) ?? 0) + 1
    targetSearchRequestIDs.set(targetIndex, requestID)
    if (!query) {
      Object.assign(search, emptySearchState<RelayPlanningUserSearchItem>())
      return
    }
    targetSearchTimers.set(targetIndex, setTimeout(() => {
      targetSearchTimers.delete(targetIndex)
      void runUserSearch(targetIndex, query, 1, requestID)
    }, searchDelayMS))
  }

  async function searchUserPage(targetIndex: number, page: number) {
    const search = targetSearch(targetIndex)
    const query = search.query.trim()
    if (!plan.value || !query) return
    const previous = targetSearchTimers.get(targetIndex)
    if (previous) clearTimeout(previous)
    const requestID = (targetSearchRequestIDs.get(targetIndex) ?? 0) + 1
    targetSearchRequestIDs.set(targetIndex, requestID)
    await runUserSearch(targetIndex, query, page, requestID)
  }

  async function runUserSearch(targetIndex: number, query: string, page: number, requestID: number) {
    if (!plan.value) return
    const search = targetSearch(targetIndex)
    search.loading = true
    search.error = ''
    try {
      const result = await options.searchUsers({
        provider_id: plan.value.provider_id,
        platform: plan.value.platform,
        q: query,
        page,
        page_size: 20,
      })
      if (targetSearchRequestIDs.get(targetIndex) !== requestID) return
      search.items = result.items ?? []
      search.total = result.total ?? 0
      search.page = result.page ?? page
      search.page_size = result.page_size ?? 20
    } catch (error) {
      if (targetSearchRequestIDs.get(targetIndex) === requestID) search.error = options.searchError(error, 'user')
    } finally {
      if (targetSearchRequestIDs.get(targetIndex) === requestID) search.loading = false
    }
  }

  function schedulePreviewAccountSearch(targetIndex: number, value: string | number) {
    if (!plan.value) return
    const query = String(value || '').trim()
    const search = previewAccountSearch(targetIndex)
    search.query = query
    search.error = ''
    const previous = accountSearchTimers.get(targetIndex)
    if (previous) clearTimeout(previous)
    const requestID = (accountSearchRequestIDs.get(targetIndex) ?? 0) + 1
    accountSearchRequestIDs.set(targetIndex, requestID)
    if (!query) {
      Object.assign(search, emptySearchState<RelayPlanningAccount>())
      return
    }
    accountSearchTimers.set(targetIndex, setTimeout(() => {
      accountSearchTimers.delete(targetIndex)
      void runPreviewAccountSearch(targetIndex, query, 1, requestID)
    }, searchDelayMS))
  }

  async function searchPreviewAccountPage(targetIndex: number, page: number) {
    const search = previewAccountSearch(targetIndex)
    const query = search.query.trim()
    if (!plan.value || !query) return
    const previous = accountSearchTimers.get(targetIndex)
    if (previous) clearTimeout(previous)
    const requestID = (accountSearchRequestIDs.get(targetIndex) ?? 0) + 1
    accountSearchRequestIDs.set(targetIndex, requestID)
    await runPreviewAccountSearch(targetIndex, query, page, requestID)
  }

  async function runPreviewAccountSearch(targetIndex: number, query: string, page: number, requestID: number) {
    if (!plan.value) return
    const search = previewAccountSearch(targetIndex)
    search.loading = true
    search.error = ''
    try {
      const result = await options.searchAccounts({
        provider_id: plan.value.provider_id,
        platform: plan.value.platform,
        q: query,
        page,
        page_size: 20,
      })
      if (accountSearchRequestIDs.get(targetIndex) !== requestID) return
      search.items = result.items ?? []
      search.total = result.total ?? 0
      search.page = result.page ?? page
      search.page_size = result.page_size ?? 20
    } catch (error) {
      if (accountSearchRequestIDs.get(targetIndex) === requestID) search.error = options.searchError(error, 'account')
    } finally {
      if (accountSearchRequestIDs.get(targetIndex) === requestID) search.loading = false
    }
  }

  async function requestConfirmation() {
    if (!plan.value) return
    if (hasUnreviewedRemovalSources.value) return { kind: 'unreviewed_removal_sources' as const }
    const generation = invalidatePlanRequests()
    confirming.value = true
    try {
      const reviewedRequest = reviewedState()
      const nextPlan = activeMappingID.value
        ? await options.previewReplan(activeMappingID.value, reviewedRequest)
        : await options.previewInitial({
            provider_id: plan.value.provider_id,
            department_id: plan.value.department_id,
            platform: plan.value.platform,
            template_group_id: plan.value.template_group_id,
            source_group_id: plan.value.source_group_id,
            weekly_cost_target: plan.value.weekly_cost_target,
            ...reviewedRequest,
          })
      if (!isCurrentPlanRequest(generation)) return
      applyPlan(nextPlan ?? plan.value)
      confirmDialogOpen.value = true
    } catch (error) {
      if (isCurrentPlanRequest(generation)) throw error
    } finally {
      if (isCurrentPlanRequest(generation)) confirming.value = false
    }
  }

  function staleRefreshedPlan(error: unknown): RelayPlanningPlan | null | undefined {
    const response = (error as {
      response?: {
        status?: number
        data?: { details?: { error_code?: string; refreshed_plan?: RelayPlanningPlan | null } }
      }
    }).response
    if (response?.status !== 409 || response.data?.details?.error_code !== 'stale_relay_plan') return undefined
    return response.data.details.refreshed_plan ?? null
  }

  async function executeConfirmed() {
    if (!plan.value) return { kind: 'empty' as const }
    if (hasUnreviewedRemovalSources.value) return { kind: 'unreviewed_removal_sources' as const }
    const generation = invalidatePlanRequests(false)
    executing.value = true
    const request: RelayPlanningExecuteRequest = {
      provider_id: plan.value.provider_id,
      department_id: plan.value.department_id,
      platform: plan.value.platform,
      template_group_id: plan.value.template_group_id,
      source_group_id: plan.value.source_group_id,
      weekly_cost_target: plan.value.weekly_cost_target,
      ...reviewedState(),
      expected_relationship_fingerprint: plan.value.relationship_fingerprint,
      operation_key: operationKey.value || options.createOperationKey(),
    }
    try {
      const execution = activeMappingID.value
        ? await options.executeReplan(activeMappingID.value, request)
        : await options.executeInitial(request)
      if (!isCurrentPlanRequest(generation)) return { kind: 'superseded' as const }
      lastExecution.value = execution
      applyPlan(execution?.plan ?? plan.value)
      operationKey.value = request.operation_key
      confirmDialogOpen.value = false
      return { kind: 'success' as const, execution }
    } catch (error) {
      if (!isCurrentPlanRequest(generation)) return { kind: 'superseded' as const }
      const refreshedPlan = staleRefreshedPlan(error)
      if (refreshedPlan !== undefined) {
        if (refreshedPlan) applyPlan(refreshedPlan)
        confirmDialogOpen.value = false
        return { kind: 'stale' as const }
      }
      throw error
    } finally {
      if (isCurrentPlanRequest(generation)) executing.value = false
    }
  }

  function reset() {
    invalidatePlanRequests()
    clearReviewedSearchState()
    options.onPlanApplied?.()
    plan.value = null
    lastExecution.value = null
    activeMappingID.value = null
		reviewLocked.value = false
    activeMappingMemberAssignments.value = {}
    activeMappingMemberSources.value = {}
    selectedUserIDs.value = new Set()
    selectedUnmanagedRelayIDs.value = new Set()
    removedUserIDs.value = new Set()
    removalSources.value = {}
    lockedRemovalSourceUserIDs.value = new Set()
    memberActions.value = {}
    managedAssignmentsByUser.value = {}
    memberSources.value = {}
    operationKey.value = ''
    suggestedGroupAccountDefaults.value = []
    confirming.value = false
    confirmDialogOpen.value = false
  }

  function closeConfirmation() {
    if (!executing.value) confirmDialogOpen.value = false
  }

  function dispose() {
    invalidatePlanRequests()
    clearReviewedSearchState()
  }

  return {
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
    operationKey,
    suggestedGroupAccountDefaults,
    targetSearches,
    previewAccountSearches,
    displayedEligibleMemberCount,
    unassignedCandidates,
    targetNameErrorCodes,
    hasTargetNameErrors,
    hasUnreviewedRemovalSources,
    preview,
    openReplan,
    requestConfirmation,
    executeConfirmed,
    closeConfirmation,
    reset,
    setTargetName,
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
    addPreviewAccount,
    movePreviewAccount,
    removePreviewAccount,
    setMemberAction,
    addSearchedUser,
    scheduleUserSearch,
    searchUserPage,
    schedulePreviewAccountSearch,
    searchPreviewAccountPage,
    dispose,
  }
}
