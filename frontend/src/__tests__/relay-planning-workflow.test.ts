import { afterEach, describe, expect, it, vi } from 'vitest'
import { useRelayPlanningWorkflow } from '@/composables/useRelayPlanningWorkflow'
import type { RelayPlanningWorkflowOptions } from '@/composables/useRelayPlanningWorkflow'
import type {
  RelayPlanningAccount,
  RelayPlanningExecution,
  RelayPlanningMapping,
  RelayPlanningPlan,
  RelayPlanningRequest,
  RelayPlanningUserSearchItem,
  RelayPlanningUserSearchPage,
} from '@/api/relayPlanning'

function reviewedPlan(overrides: Partial<RelayPlanningPlan> = {}): RelayPlanningPlan {
  return {
    provider_id: 7,
    department_id: 'dept-alpha',
    department_name: 'Department Alpha',
    platform: 'openai',
    template_group_id: 42,
    template_group_name: 'Group Alpha',
    source_group_id: 42,
    source_group_name: 'Group Alpha',
    weekly_cost_target: 2500,
    recommended_group_count: 1,
    group_count: 1,
    candidates: [{
      user_id: 1,
      relay_user_id: 101,
      username: 'alice',
      email: 'alice@example.com',
      range_cost: 1200,
      range_tokens: 100,
      usage_known: true,
      global_token_rank: 1,
      migratable_key_count: 1,
      source_member: true,
      source_group_id: 42,
      can_add: true,
      selected: true,
      eligible: true,
		disposition: 'migration',
    }],
    assignments: [{
      index: 0,
      total_cost: 1200,
      user_ids: [1],
      target_group_name: 'Department Alpha-openai-01',
      desired_accounts: [{ account_id: 11, priority: 1 }],
      accounts: [{ id: 11, name: 'Account Alpha', platform: 'openai', type: 'oauth', status: 'active', schedulable: true, priority: 1 }],
    }],
		template_accounts: [{ id: 11, name: 'Account Alpha', platform: 'openai', type: 'oauth', status: 'active', schedulable: true, priority: 1 }],
    target_summaries: [],
    relationship_fingerprint: 'v2:preview-fingerprint',
    accounts_reviewed: true,
    generated_at: '2026-08-27T00:00:00Z',
    ...overrides,
  }
}

function relayMapping(overrides: Partial<RelayPlanningMapping> = {}): RelayPlanningMapping {
  return {
    id: 9,
    provider_id: 7,
    department_id: 'dept-alpha',
    department_name: 'Department Alpha',
    platform: 'openai',
    template_group_id: 42,
    template_group_name: 'Group Alpha',
    source_group_id: 42,
    source_group_name: 'Group Alpha',
    group_ids: [101],
    status: 'active',
    weekly_cost_target: 2500,
    account_management_initialized: true,
    desired_accounts: {},
    account_pools: [],
    updated_at: '2026-08-27T00:00:00Z',
    ...overrides,
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((done, fail) => {
    resolve = done
    reject = fail
  })
  return { promise, resolve, reject }
}

function workflowOptions() {
  const previewInitial = vi.fn<RelayPlanningWorkflowOptions['previewInitial']>(async (request) => reviewedPlan({
    assignments: structuredClone(request.assignments ?? reviewedPlan().assignments),
  }))
  return {
    previewInitial,
    previewReplan: vi.fn<RelayPlanningWorkflowOptions['previewReplan']>(async () => reviewedPlan()),
    executeInitial: vi.fn<RelayPlanningWorkflowOptions['executeInitial']>(async () => ({ plan: reviewedPlan(), groups: [], accounts: [], members: [] } satisfies RelayPlanningExecution)),
    executeReplan: vi.fn<RelayPlanningWorkflowOptions['executeReplan']>(async () => ({ plan: reviewedPlan(), groups: [], accounts: [], members: [] } satisfies RelayPlanningExecution)),
    searchUsers: vi.fn<RelayPlanningWorkflowOptions['searchUsers']>(async () => ({ items: [], total: 0, page: 1, page_size: 20 })),
    searchAccounts: vi.fn<RelayPlanningWorkflowOptions['searchAccounts']>(async () => ({ items: [], total: 0, page: 1, page_size: 20 })),
    createOperationKey: vi.fn(() => 'operation-1'),
    reservedGroups: () => [{ id: 42, name: 'Group Alpha' }],
    searchError: (error: unknown) => (error as Error).message,
  }
}

describe('useRelayPlanningWorkflow', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('keeps Preview read-only and confirms the exact reviewed Target state', async () => {
    const options = workflowOptions()
    const workflow = useRelayPlanningWorkflow(options)
    const request: RelayPlanningRequest = {
      provider_id: 7,
      department_id: 'dept-alpha',
      platform: 'openai',
      template_group_id: 42,
      source_group_id: 42,
      weekly_cost_target: 2500,
    }

    await workflow.preview(request)
    workflow.setTargetName(0, 'Reviewed Target')
    workflow.addPreviewAccount(0, {
      id: 12,
      name: 'Account Beta',
      platform: 'openai',
      type: 'apikey',
      status: 'active',
      schedulable: true,
    } satisfies RelayPlanningAccount)
    workflow.movePreviewAccount(0, 12, -1)

    expect(options.executeInitial).not.toHaveBeenCalled()
    expect(options.executeReplan).not.toHaveBeenCalled()

    await workflow.requestConfirmation()

    expect(options.previewInitial).toHaveBeenLastCalledWith(expect.objectContaining({
      provider_id: 7,
      selected_user_ids: [1],
      member_sources: { '1': 42 },
      adopt_relay_user_ids: [],
      assignments: [expect.objectContaining({
        target_group_name: 'Reviewed Target',
        user_ids: [1],
        desired_accounts: [
          { account_id: 12, priority: 1 },
          { account_id: 11, priority: 2 },
        ],
      })],
    }))
    expect(workflow.confirmDialogOpen.value).toBe(true)
    expect(options.executeInitial).not.toHaveBeenCalled()
    workflow.dispose()
  })

  it('restores failed Replan intent once with a fresh fingerprint and operation key', async () => {
    const options = workflowOptions()
    options.createOperationKey
      .mockReturnValueOnce('operation-retry-1')
      .mockReturnValueOnce('operation-retry-2')
    const replan = reviewedPlan({
      mapping_id: 9,
      relationship_fingerprint: 'v2:fresh-retry-fingerprint',
      assignments: [{
        ...reviewedPlan().assignments[0],
        target_group_id: 101,
        user_ids: [2],
      }],
      candidates: [
        reviewedPlan().candidates[0],
        { ...reviewedPlan().candidates[0], user_id: 2, relay_user_id: 102, username: 'bob', email: 'bob@example.org' },
      ],
    })
    options.previewReplan.mockResolvedValue(replan)
    const mapping = relayMapping({
      status: 'needs_retry',
      member_assignments: { '1': 101, '2': 101 },
      member_sources: { '1': 42, '2': 42 },
      operation_state: {
        'member:1': { action: 'remove', target_group_id: '101', source_removal: 'failed', error: 'synthetic removal failure' },
        'member:2': { action: 'move_here', from_mapping_id: '8', source_removal: 'failed', error: 'synthetic move failure' },
        'member:3': { action: 'remove', target_group_id: '101', status: 'succeeded' },
      },
    })
    const workflow = useRelayPlanningWorkflow(options)

    await workflow.openReplan(mapping)

    expect(options.previewReplan).toHaveBeenCalledWith(9, {
      removed_user_ids: [1],
      member_actions: { '2': { mode: 'move_here', from_mapping_id: 8 } },
    })
    expect(workflow.removedUserIDs.value).toEqual(new Set([1]))
    expect(workflow.memberActions.value).toEqual({ '2': { mode: 'move_here', from_mapping_id: 8 } })
    expect(workflow.operationKey.value).toBe('operation-retry-1')
    expect(workflow.plan.value?.relationship_fingerprint).toBe('v2:fresh-retry-fingerprint')

    await workflow.requestConfirmation()

    expect(options.previewReplan).toHaveBeenLastCalledWith(9, expect.objectContaining({
      removed_user_ids: [1],
      member_actions: { '2': { mode: 'move_here', from_mapping_id: 8 } },
    }))
    expect(workflow.operationKey.value).toBe('operation-retry-1')

    await workflow.openReplan(mapping)
    expect(workflow.operationKey.value).toBe('operation-retry-2')
    workflow.dispose()
  })

	it('adds and removes a proposed Target while reviewing Replan', async () => {
		const options = workflowOptions()
		options.reservedGroups = () => [
			{ id: 42, name: 'Group Alpha' },
			{ id: 101, name: 'Department Alpha-openai-01' },
		]
		const replan = reviewedPlan({
			mapping_id: 9,
			template_accounts: [{
				id: 13,
				name: 'Template Account',
				platform: 'openai',
				type: 'oauth',
				status: 'active',
				schedulable: true,
				priority: 1,
			}],
			assignments: [{
				...reviewedPlan().assignments[0],
				target_group_id: 101,
				target_group_name: 'Department Alpha-openai-01',
			}],
		})
		options.previewReplan.mockResolvedValue(replan)
		const workflow = useRelayPlanningWorkflow(options)

		await workflow.openReplan(relayMapping())
		workflow.addSuggestedGroup()

		expect(workflow.plan.value?.assignments).toHaveLength(2)
		expect(workflow.plan.value?.assignments[1]).toEqual(expect.objectContaining({
			index: 1,
			target_group_name: 'Department Alpha-openai-02',
			user_ids: [],
			accounts: [expect.objectContaining({ id: 13 })],
		}))
		expect(workflow.plan.value?.assignments[1].target_group_id).toBeUndefined()
		expect(options.executeReplan).not.toHaveBeenCalled()
		workflow.setTargetName(1, 'Invalid\nTarget')
		expect(workflow.targetNameErrorCodes.value[1]).toBe('control')
		workflow.setTargetName(1, 'Department Alpha-openai-02')

		workflow.removeSuggestedGroup(1)
		expect(workflow.plan.value?.assignments).toHaveLength(1)
		workflow.addSuggestedGroup()
		await workflow.requestConfirmation()

		expect(options.previewReplan).toHaveBeenLastCalledWith(9, expect.objectContaining({
			assignments: [
				expect.objectContaining({ index: 0, target_group_id: 101 }),
				expect.objectContaining({ index: 1, target_group_id: undefined, desired_accounts: [{ account_id: 13, priority: 1 }] }),
			],
		}))
		expect(options.executeReplan).not.toHaveBeenCalled()
		workflow.dispose()
	})

  it('replaces a stale confirmation visibly without replaying execution', async () => {
    const options = workflowOptions()
    const refreshedPlan = reviewedPlan({
      relationship_fingerprint: 'v2:refreshed-fingerprint',
      assignments: [{
        ...reviewedPlan().assignments[0],
        target_group_name: 'Fresh Server State',
        user_ids: [],
      }],
    })
    options.executeInitial.mockRejectedValue({
      response: {
        status: 409,
        data: {
          details: {
            error_code: 'stale_relay_plan',
            refreshed_plan: refreshedPlan,
          },
        },
      },
    })
    const workflow = useRelayPlanningWorkflow(options)

    await workflow.preview({
      provider_id: 7,
      department_id: 'dept-alpha',
      platform: 'openai',
      template_group_id: 42,
      source_group_id: 42,
      weekly_cost_target: 2500,
    })
    workflow.setTargetName(0, 'Old Reviewed Intent')
    await workflow.requestConfirmation()

    const outcome = await workflow.executeConfirmed()

    expect(options.executeInitial).toHaveBeenCalledTimes(1)
    expect(options.executeInitial).toHaveBeenCalledWith(expect.objectContaining({
      expected_relationship_fingerprint: 'v2:preview-fingerprint',
      operation_key: 'operation-1',
    }))
    expect(outcome).toEqual({ kind: 'stale' })
    expect(workflow.confirmDialogOpen.value).toBe(false)
    expect(workflow.plan.value?.relationship_fingerprint).toBe('v2:refreshed-fingerprint')
    expect(workflow.plan.value?.assignments[0].target_group_name).toBe('Fresh Server State')
    expect(workflow.operationKey.value).toBe('operation-1')
    workflow.dispose()
  })

  it('adds a searched user once and confirms the final cross-mapping action', async () => {
    const options = workflowOptions()
    const mapping = relayMapping({
      source_group_id: 0,
      source_group_name: '',
      member_assignments: { '1': 101 },
    })
    const initialReplan = reviewedPlan({
      mapping_id: 9,
      source_group_id: 0,
      source_group_name: '',
      assignments: [{ ...reviewedPlan().assignments[0], target_group_id: 101 }],
    })
    const searchedUser: RelayPlanningUserSearchItem = {
      user_id: 2,
      relay_user_id: 102,
      username: 'bob',
      email: 'bob@example.org',
      selectable: true,
      managed_assignments: [{
        mapping_id: 8,
        department_id: 'dept-beta',
        department_name: 'Department Beta',
        target_group_id: 88,
      }],
    }
    options.previewReplan
      .mockResolvedValueOnce(initialReplan)
      .mockImplementation(async (_mappingID, request) => reviewedPlan({
        mapping_id: 9,
        source_group_id: 0,
        source_group_name: '',
        candidates: [
          reviewedPlan().candidates[0],
          { ...reviewedPlan().candidates[0], user_id: 2, relay_user_id: 102, username: 'bob', email: 'bob@example.org', source_group_id: 0 },
        ],
        assignments: structuredClone(request.assignments ?? initialReplan.assignments),
      }))
    const workflow = useRelayPlanningWorkflow(options)

    await workflow.openReplan(mapping)
    await workflow.addSearchedUser(0, searchedUser)

    expect(options.previewReplan).toHaveBeenLastCalledWith(9, expect.objectContaining({
      selected_user_ids: [1, 2],
      assignments: [expect.objectContaining({ user_ids: [1, 2] })],
      member_sources: { '1': 42, '2': 0 },
      removed_user_ids: [],
      member_actions: { '2': { mode: 'move_here', from_mapping_id: 8 } },
    }))
    expect(workflow.memberActions.value).toEqual({ '2': { mode: 'move_here', from_mapping_id: 8 } })
    expect(workflow.managedAssignmentsByUser.value['2']).toEqual(searchedUser.managed_assignments)
    expect(options.executeReplan).not.toHaveBeenCalled()

    workflow.setMemberAction(2, 'add_additionally')
    await workflow.requestConfirmation()

    expect(options.previewReplan).toHaveBeenLastCalledWith(9, expect.objectContaining({
      member_actions: { '2': { mode: 'add_additionally', from_mapping_id: 8 } },
    }))
    expect(options.executeReplan).not.toHaveBeenCalled()
    workflow.dispose()
  })

  it('keeps only the latest target search and preserves Account results when paging fails', async () => {
    vi.useFakeTimers()
    const options = workflowOptions()
    const olderUsers = deferred<RelayPlanningUserSearchPage>()
    const newerUsers = deferred<RelayPlanningUserSearchPage>()
    options.searchUsers
      .mockImplementationOnce(() => olderUsers.promise)
      .mockImplementationOnce(() => newerUsers.promise)
    options.searchAccounts
      .mockResolvedValueOnce({
        items: [{ id: 11, name: 'Stable Account', platform: 'openai', type: 'oauth', status: 'active', schedulable: true }],
        total: 45,
        page: 1,
        page_size: 20,
      })
      .mockRejectedValueOnce(new Error('synthetic Account page failure'))
    const workflow = useRelayPlanningWorkflow(options)
    await workflow.preview({
      provider_id: 7,
      department_id: 'dept-alpha',
      platform: 'openai',
      template_group_id: 42,
      source_group_id: 42,
      weekly_cost_target: 2500,
    })

    workflow.scheduleUserSearch(0, 'old')
    await vi.advanceTimersByTimeAsync(300)
    workflow.scheduleUserSearch(0, 'new')
    await vi.advanceTimersByTimeAsync(300)

    newerUsers.resolve({
      items: [{ user_id: 2, relay_user_id: 102, username: 'new', email: 'new@example.org', selectable: true }],
      total: 1,
      page: 1,
      page_size: 20,
    })
    await Promise.resolve()
    olderUsers.resolve({
      items: [{ user_id: 3, relay_user_id: 103, username: 'old', email: 'old@example.net', selectable: true }],
      total: 1,
      page: 1,
      page_size: 20,
    })
    await Promise.resolve()

    expect(workflow.targetSearches[0]).toMatchObject({
      query: 'new',
      loading: false,
      error: '',
      page: 1,
      items: [expect.objectContaining({ username: 'new' })],
    })

    workflow.schedulePreviewAccountSearch(0, 'Account')
    await vi.advanceTimersByTimeAsync(300)
    await Promise.resolve()
    expect(workflow.previewAccountSearches[0]).toMatchObject({
      page: 1,
      items: [expect.objectContaining({ name: 'Stable Account' })],
    })

    await workflow.searchPreviewAccountPage(0, 2)
    expect(workflow.previewAccountSearches[0]).toMatchObject({
      page: 1,
      items: [expect.objectContaining({ name: 'Stable Account' })],
      error: 'synthetic Account page failure',
    })
    workflow.dispose()
  })

  it('keeps old plan searches from overwriting the same Target in a new plan', async () => {
    vi.useFakeTimers()
    const options = workflowOptions()
    options.previewInitial.mockImplementation(async (request) => reviewedPlan({
      provider_id: request.provider_id,
      platform: request.platform,
    }))
    const oldUsers = deferred<RelayPlanningUserSearchPage>()
    const newUsers = deferred<RelayPlanningUserSearchPage>()
    const oldAccounts = deferred<{ items: RelayPlanningAccount[]; total: number; page: number; page_size: number }>()
    const newAccounts = deferred<{ items: RelayPlanningAccount[]; total: number; page: number; page_size: number }>()
    options.searchUsers
      .mockImplementationOnce(() => oldUsers.promise)
      .mockImplementationOnce(() => newUsers.promise)
    options.searchAccounts
      .mockImplementationOnce(() => oldAccounts.promise)
      .mockImplementationOnce(() => newAccounts.promise)
    const workflow = useRelayPlanningWorkflow(options)
    const preview = (providerID: number, platform: string) => workflow.preview({
      provider_id: providerID,
      department_id: 'dept-alpha',
      platform,
      template_group_id: 42,
      source_group_id: 42,
      weekly_cost_target: 2500,
    })

    await preview(7, 'openai')
    workflow.scheduleUserSearch(0, 'old user')
    workflow.schedulePreviewAccountSearch(0, 'old Account')
    await vi.advanceTimersByTimeAsync(300)

    await preview(8, 'anthropic')
    workflow.scheduleUserSearch(0, 'new user')
    workflow.schedulePreviewAccountSearch(0, 'new Account')
    await vi.advanceTimersByTimeAsync(300)

    newUsers.resolve({ items: [{ user_id: 2, relay_user_id: 102, username: 'new', email: 'new@example.org', selectable: true }], total: 1, page: 1, page_size: 20 })
    newAccounts.resolve({ items: [{ id: 12, name: 'New Account', platform: 'anthropic', type: 'oauth', status: 'active', schedulable: true }], total: 1, page: 1, page_size: 20 })
    await Promise.resolve()
    oldUsers.resolve({ items: [{ user_id: 3, relay_user_id: 103, username: 'old', email: 'old@example.net', selectable: true }], total: 1, page: 1, page_size: 20 })
    oldAccounts.resolve({ items: [{ id: 11, name: 'Old Account', platform: 'openai', type: 'oauth', status: 'active', schedulable: true }], total: 1, page: 1, page_size: 20 })
    await Promise.resolve()

    expect(workflow.targetSearches[0]?.items).toEqual([expect.objectContaining({ username: 'new' })])
    expect(workflow.previewAccountSearches[0]?.items).toEqual([expect.objectContaining({ name: 'New Account' })])
    expect(options.searchUsers).toHaveBeenLastCalledWith(expect.objectContaining({ provider_id: 8, platform: 'anthropic' }))
    expect(options.searchAccounts).toHaveBeenLastCalledWith(expect.objectContaining({ provider_id: 8, platform: 'anthropic' }))
    workflow.dispose()
  })

  it('keeps initial Target resize and member edits explicit in the reviewed request', async () => {
    const options = workflowOptions()
    const workflow = useRelayPlanningWorkflow(options)
    await workflow.preview({
      provider_id: 7,
      department_id: 'dept-alpha',
      platform: 'openai',
      template_group_id: 42,
      source_group_id: 42,
      weekly_cost_target: 2500,
    })

    workflow.setTargetName(0, 'Group Alpha')
    expect(workflow.targetNameErrorCodes.value).toEqual({ 0: 'occupied' })
    workflow.setTargetName(0, 'Reviewed Target')
    workflow.addSuggestedGroup()
    workflow.addSuggestedGroup()
    expect(workflow.plan.value?.assignments).toHaveLength(3)

    workflow.removeSuggestedGroup(0)
    expect(workflow.plan.value?.assignments.map((assignment) => assignment.index)).toEqual([0, 1])
    expect(workflow.selectedUserIDs.value).toEqual(new Set())

    workflow.moveCandidate(1, 0)
    workflow.setMemberSource(1, 0)
    workflow.toggleUnmanagedRelayUser(501, true)
    await workflow.requestConfirmation()

    expect(options.previewInitial).toHaveBeenLastCalledWith(expect.objectContaining({
      selected_user_ids: [1],
      member_sources: { '1': 0 },
      adopt_relay_user_ids: [501],
      assignments: [
        expect.objectContaining({ index: 0, user_ids: [1] }),
        expect.objectContaining({ index: 1, user_ids: [] }),
      ],
    }))
    expect(options.executeInitial).not.toHaveBeenCalled()
    workflow.dispose()
  })

  it('resets the complete reviewed-plan lifecycle when planning context changes', async () => {
    vi.useFakeTimers()
    const options = workflowOptions()
    const workflow = useRelayPlanningWorkflow(options)
    await workflow.preview({
      provider_id: 7,
      department_id: 'dept-alpha',
      platform: 'openai',
      template_group_id: 42,
      source_group_id: 42,
      weekly_cost_target: 2500,
    })
    workflow.scheduleUserSearch(0, 'alice')
    workflow.schedulePreviewAccountSearch(0, 'Account')
    workflow.toggleUnmanagedRelayUser(501, true)
    await workflow.requestConfirmation()

    workflow.reset()
    await vi.advanceTimersByTimeAsync(300)

    expect(workflow.plan.value).toBeNull()
    expect(workflow.lastExecution.value).toBeNull()
    expect(workflow.activeMappingID.value).toBeNull()
    expect(workflow.selectedUserIDs.value).toEqual(new Set())
    expect(workflow.selectedUnmanagedRelayIDs.value).toEqual(new Set())
    expect(workflow.removedUserIDs.value).toEqual(new Set())
    expect(workflow.memberActions.value).toEqual({})
    expect(workflow.memberSources.value).toEqual({})
    expect(workflow.operationKey.value).toBe('')
    expect(workflow.confirmDialogOpen.value).toBe(false)
    expect(workflow.targetSearches).toEqual({})
    expect(workflow.previewAccountSearches).toEqual({})
    expect(options.searchUsers).not.toHaveBeenCalled()
    expect(options.searchAccounts).not.toHaveBeenCalled()
    workflow.dispose()
  })

  it('opens the confirmed Replan roster and submits only an explicit removal', async () => {
    const options = workflowOptions()
    const mapping = relayMapping({
      member_assignments: { '1': 101 },
      member_sources: { '1': 42 },
    })
    const replan = reviewedPlan({
      mapping_id: 9,
      candidates: [
        reviewedPlan().candidates[0],
        { ...reviewedPlan().candidates[0], user_id: 2, relay_user_id: 102, username: 'bob', email: 'bob@example.org', selected: false },
      ],
      assignments: [{ ...reviewedPlan().assignments[0], target_group_id: 101, user_ids: [1] }],
    })
    options.previewReplan.mockImplementation(async (_mappingID, request) => ({
      ...structuredClone(replan),
      assignments: structuredClone(request.assignments ?? replan.assignments),
    }))
    const workflow = useRelayPlanningWorkflow(options)

    await workflow.openReplan(mapping)
    expect(workflow.selectedUserIDs.value).toEqual(new Set([1]))
    expect(workflow.candidateAssignmentIndex(2)).toBeNull()

    workflow.moveCandidate(1, null)
    await workflow.requestConfirmation()

    expect(options.previewReplan).toHaveBeenLastCalledWith(9, expect.objectContaining({
      selected_user_ids: [],
      removed_user_ids: [1],
      assignments: [expect.objectContaining({ target_group_id: 101, user_ids: [] })],
    }))
    await workflow.executeConfirmed()
    expect(options.executeReplan).toHaveBeenCalledWith(9, expect.objectContaining({
      selected_user_ids: [],
      removed_user_ids: [1],
      assignments: [expect.objectContaining({ target_group_id: 101, user_ids: [] })],
    }))
    workflow.dispose()
  })

	 it('requires a reviewed Source before removing a legacy managed member', async () => {
		 const options = workflowOptions()
		 const mapping = relayMapping({
			 member_assignments: { '1': 101 },
			 member_sources: {},
		 })
		 const replan = reviewedPlan({
			 mapping_id: 9,
			 candidates: [{ ...reviewedPlan().candidates[0], source_group_id: 0, source_member: false }],
			 assignments: [{ ...reviewedPlan().assignments[0], target_group_id: 101, user_ids: [1] }],
		 })
		 options.previewReplan.mockImplementation(async (_mappingID, request) => ({
			 ...structuredClone(replan),
			 candidates: replan.candidates.map((candidate) => ({
				 ...candidate,
				 source_group_id: request.member_sources?.[String(candidate.user_id)] ?? candidate.source_group_id,
			 })),
			 assignments: structuredClone(request.assignments ?? replan.assignments),
		 }))
		 const workflow = useRelayPlanningWorkflow(options)

		 await workflow.openReplan(mapping)
		 workflow.moveCandidate(1, null)

		 expect(workflow.hasUnreviewedRemovalSources.value).toBe(true)
		 await expect(workflow.requestConfirmation()).resolves.toEqual({ kind: 'unreviewed_removal_sources' })
		 expect(options.previewReplan).toHaveBeenCalledTimes(1)

		 workflow.setMemberSource(1, 42)
		 expect(workflow.hasUnreviewedRemovalSources.value).toBe(false)
		 await workflow.requestConfirmation()
		 expect(options.previewReplan).toHaveBeenLastCalledWith(9, expect.objectContaining({
			 removed_user_ids: [1],
			 member_sources: { '1': 42 },
		 }))
		 await workflow.executeConfirmed()
		 expect(options.executeReplan).toHaveBeenCalledWith(9, expect.objectContaining({
			 removed_user_ids: [1],
			 member_sources: { '1': 42 },
		 }))
		 workflow.dispose()
	 })

  it('carries only explicitly selected Target renames into confirmation', async () => {
    const options = workflowOptions()
    const mapping = relayMapping({
      source_group_id: 0,
      source_group_name: '',
    })
    const replan = reviewedPlan({
      mapping_id: 9,
      candidates: [],
      assignments: [{
        ...reviewedPlan().assignments[0],
        target_group_id: 101,
        user_ids: [],
        current_target_group_name: 'Legacy Target',
        suggested_target_group_name: 'Department Alpha-openai-01',
        target_group_name: 'Legacy Target',
        rename_selected: false,
      }],
    })
    options.previewReplan.mockResolvedValue(replan)
    const workflow = useRelayPlanningWorkflow(options)

    await workflow.openReplan(mapping)
    workflow.toggleTargetRename(0, true)
    workflow.setTargetName(0, 'Reviewed Rename')
    await workflow.requestConfirmation()

    expect(options.previewReplan).toHaveBeenLastCalledWith(9, expect.objectContaining({
      assignments: [expect.objectContaining({
        target_group_id: 101,
        target_group_name: 'Reviewed Rename',
        rename_selected: true,
      })],
    }))
    workflow.dispose()
  })

  it('executes the reviewed request only after explicit confirmation', async () => {
    const options = workflowOptions()
    const execution = { plan: reviewedPlan(), groups: [], accounts: [], members: [] } satisfies RelayPlanningExecution
    options.executeInitial.mockResolvedValue(execution)
    const workflow = useRelayPlanningWorkflow(options)
    await workflow.preview({
      provider_id: 7,
      department_id: 'dept-alpha',
      platform: 'openai',
      template_group_id: 42,
      source_group_id: 42,
      weekly_cost_target: 2500,
    })
    await workflow.requestConfirmation()
    expect(options.executeInitial).not.toHaveBeenCalled()

    const outcome = await workflow.executeConfirmed()

    expect(outcome).toEqual({ kind: 'success', execution })
    expect(options.executeInitial).toHaveBeenCalledTimes(1)
    expect(options.executeInitial).toHaveBeenCalledWith(expect.objectContaining({
      expected_relationship_fingerprint: 'v2:preview-fingerprint',
      operation_key: 'operation-1',
    }))
    expect(workflow.lastExecution.value).toEqual(execution)
    expect(workflow.confirmDialogOpen.value).toBe(false)
    workflow.dispose()
  })

  it('does not carry Replan-only retry intent into a new initial Preview', async () => {
    const options = workflowOptions()
    const mapping = relayMapping({
      status: 'needs_retry',
      operation_state: {
        'member:1': { action: 'remove', source_removal: 'failed', error: 'synthetic removal failure' },
        'member:2': { action: 'move_here', from_mapping_id: '8', source_removal: 'failed', error: 'synthetic move failure' },
      },
    })
    options.previewReplan.mockResolvedValue(reviewedPlan({ mapping_id: 9 }))
    const workflow = useRelayPlanningWorkflow(options)
    await workflow.openReplan(mapping)
    expect(workflow.removedUserIDs.value).toEqual(new Set([1]))
    expect(workflow.memberActions.value).toHaveProperty('2')

    await workflow.preview({
      provider_id: 7,
      department_id: 'dept-alpha',
      platform: 'openai',
      template_group_id: 42,
      source_group_id: 42,
      weekly_cost_target: 2500,
    })

    expect(workflow.activeMappingID.value).toBeNull()
    expect(workflow.removedUserIDs.value).toEqual(new Set())
    expect(workflow.memberActions.value).toEqual({})
    expect(workflow.managedAssignmentsByUser.value).toEqual({})
    await workflow.requestConfirmation()
    await workflow.executeConfirmed()
    expect(options.executeInitial).toHaveBeenLastCalledWith(expect.objectContaining({
      removed_user_ids: [],
      member_actions: {},
    }))
    workflow.dispose()
  })

  it('locks a reviewed removal destination while its retry is pending', async () => {
    const options = workflowOptions()
    const mapping = relayMapping({
      status: 'needs_retry',
      member_assignments: {},
      member_sources: {},
      operation_state: {
        'member:1': {
          action: 'remove',
          source_reviewed: 'true',
          source_group_id: '42',
          source_removal: 'failed',
          error: 'synthetic removal failure',
        },
      },
    })
    options.previewReplan.mockResolvedValue(reviewedPlan({ mapping_id: 9 }))
    const workflow = useRelayPlanningWorkflow(options)

    await workflow.openReplan(mapping)
    expect(workflow.lockedRemovalSourceUserIDs.value).toEqual(new Set([1]))
    expect(workflow.removalSources.value).toEqual({ '1': 42 })

    workflow.setMemberSource(1, 0)
    expect(workflow.removalSources.value).toEqual({ '1': 42 })
    workflow.dispose()
  })

  it('ignores superseded plan responses after reset or a newer explicit edit', async () => {
    const options = workflowOptions()
    const oldPreview = deferred<RelayPlanningPlan | null>()
    options.previewInitial.mockImplementationOnce(() => oldPreview.promise)
    const workflow = useRelayPlanningWorkflow(options)
    const request: RelayPlanningRequest = {
      provider_id: 7,
      department_id: 'dept-alpha',
      platform: 'openai',
      template_group_id: 42,
      source_group_id: 42,
      weekly_cost_target: 2500,
    }

    const pendingPreview = workflow.preview(request)
    workflow.reset()
    oldPreview.resolve(reviewedPlan())
    await pendingPreview
    expect(workflow.plan.value).toBeNull()
    expect(workflow.operationKey.value).toBe('')

    options.previewInitial.mockResolvedValueOnce(reviewedPlan())
    await workflow.preview(request)
    const oldConfirmation = deferred<RelayPlanningPlan | null>()
    options.previewInitial.mockImplementationOnce(() => oldConfirmation.promise)
    const pendingConfirmation = workflow.requestConfirmation()
    workflow.setTargetName(0, 'Newer Local Intent')
    oldConfirmation.resolve(reviewedPlan({
      assignments: [{ ...reviewedPlan().assignments[0], target_group_name: 'Older Server Response' }],
    }))
    await pendingConfirmation

    expect(workflow.plan.value?.assignments[0].target_group_name).toBe('Newer Local Intent')
    expect(workflow.confirmDialogOpen.value).toBe(false)
    workflow.dispose()
  })

  it('ignores a superseded Replan after a newer initial Preview', async () => {
    const options = workflowOptions()
    const oldReplan = deferred<RelayPlanningPlan | null>()
    options.previewReplan.mockImplementationOnce(() => oldReplan.promise)
    options.previewInitial.mockResolvedValueOnce(reviewedPlan({ relationship_fingerprint: 'v2:new-initial' }))
    const workflow = useRelayPlanningWorkflow(options)

    const pendingReplan = workflow.openReplan(relayMapping())
    await workflow.preview({
      provider_id: 7,
      department_id: 'dept-alpha',
      platform: 'openai',
      template_group_id: 42,
      source_group_id: 42,
      weekly_cost_target: 2500,
    })
    oldReplan.resolve(reviewedPlan({ mapping_id: 9, relationship_fingerprint: 'v2:old-replan' }))
    await pendingReplan

    expect(workflow.activeMappingID.value).toBeNull()
    expect(workflow.plan.value?.relationship_fingerprint).toBe('v2:new-initial')
    workflow.dispose()
  })

  it('ignores a superseded searched-user Preview and its staged cross-mapping intent', async () => {
    const options = workflowOptions()
    options.previewReplan.mockResolvedValueOnce(reviewedPlan({ mapping_id: 9 }))
    const oldAddition = deferred<RelayPlanningPlan | null>()
    options.previewReplan.mockImplementationOnce(() => oldAddition.promise)
    const workflow = useRelayPlanningWorkflow(options)
    await workflow.openReplan(relayMapping())
    const pendingAddition = workflow.addSearchedUser(0, {
      user_id: 2,
      relay_user_id: 102,
      username: 'bob',
      email: 'bob@example.org',
      selectable: true,
      managed_assignments: [{ mapping_id: 8, department_id: 'dept-beta', department_name: 'Department Beta', target_group_id: 88 }],
    })

    workflow.setTargetName(0, 'Newer Local Intent')
    oldAddition.resolve(reviewedPlan({
      mapping_id: 9,
      candidates: [
        reviewedPlan().candidates[0],
        { ...reviewedPlan().candidates[0], user_id: 2, relay_user_id: 102, username: 'bob', email: 'bob@example.org' },
      ],
      assignments: [{ ...reviewedPlan().assignments[0], target_group_id: 101, user_ids: [1, 2] }],
    }))
    await pendingAddition

    expect(workflow.plan.value?.assignments[0].target_group_name).toBe('Newer Local Intent')
    expect(workflow.plan.value?.assignments[0].user_ids).toEqual([1])
    expect(workflow.memberActions.value).not.toHaveProperty('2')
    expect(workflow.managedAssignmentsByUser.value).not.toHaveProperty('2')
    workflow.dispose()
  })

  it('ignores a superseded Execute response after reset', async () => {
    const options = workflowOptions()
    const oldExecution = deferred<RelayPlanningExecution | null>()
    options.executeInitial.mockImplementationOnce(() => oldExecution.promise)
    const workflow = useRelayPlanningWorkflow(options)
    await workflow.preview({
      provider_id: 7,
      department_id: 'dept-alpha',
      platform: 'openai',
      template_group_id: 42,
      source_group_id: 42,
      weekly_cost_target: 2500,
    })
    await workflow.requestConfirmation()

    const pendingExecution = workflow.executeConfirmed()
    workflow.reset()
    oldExecution.resolve({ plan: reviewedPlan({ relationship_fingerprint: 'v2:old-execution' }), groups: [], accounts: [], members: [] })
    const outcome = await pendingExecution

    expect(outcome).toEqual({ kind: 'superseded' })
    expect(workflow.plan.value).toBeNull()
    expect(workflow.lastExecution.value).toBeNull()
    workflow.dispose()
  })

  it('does not retain hidden cross-mapping intent when searched-user Preview fails', async () => {
    const options = workflowOptions()
    const mapping = relayMapping({ source_group_id: 0, source_group_name: '', member_assignments: { '1': 101 } })
    options.previewReplan
      .mockResolvedValueOnce(reviewedPlan({ mapping_id: 9, source_group_id: 0, source_group_name: '' }))
      .mockRejectedValueOnce(new Error('synthetic searched-user failure'))
    const workflow = useRelayPlanningWorkflow(options)
    await workflow.openReplan(mapping)
    const searchedUser: RelayPlanningUserSearchItem = {
      user_id: 2,
      relay_user_id: 102,
      username: 'bob',
      email: 'bob@example.org',
      selectable: true,
      managed_assignments: [{ mapping_id: 8, department_id: 'dept-beta', department_name: 'Department Beta', target_group_id: 88 }],
    }

    await expect(workflow.addSearchedUser(0, searchedUser)).rejects.toThrow('synthetic searched-user failure')

    expect(workflow.plan.value?.assignments[0].user_ids).toEqual([1])
    expect(workflow.memberSources.value).not.toHaveProperty('2')
    expect(workflow.memberActions.value).not.toHaveProperty('2')
    expect(workflow.managedAssignmentsByUser.value).not.toHaveProperty('2')
    workflow.dispose()
  })

	it('locks every reviewed edit while an exact legacy retry is open', async () => {
		const options = workflowOptions()
		options.previewReplan.mockResolvedValue(reviewedPlan({ mapping_id: 9, assignments: [{ ...reviewedPlan().assignments[0], target_group_id: 101, user_ids: [1] }] }))
		const workflow = useRelayPlanningWorkflow(options)
		await workflow.openReplan(relayMapping({
			status: 'needs_retry',
			member_assignments: { '1': 101 },
			operation_state: { operation: { status: 'needs_retry', intent_hash: 'v1:reviewed' } },
		}))
		const before = JSON.parse(JSON.stringify(workflow.plan.value))
		workflow.setTargetName(0, 'Edited Target')
		workflow.addSuggestedGroup()
		workflow.moveCandidate(1, null)
		workflow.toggleTargetRename(0, true)
		workflow.addPreviewAccount(0, { id: 99, name: 'Account Locked', platform: 'openai', type: 'oauth', status: 'active', schedulable: true })
		await workflow.addSearchedUser(0, { user_id: 2, relay_user_id: 102, username: 'bob', email: 'bob@example.org', selectable: true })

		expect(workflow.reviewLocked.value).toBe(true)
		expect(workflow.plan.value).toEqual(before)
		expect(options.previewReplan).toHaveBeenCalledTimes(1)
		workflow.dispose()
	})
})
