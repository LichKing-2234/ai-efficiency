import { computed, ref, type Ref } from 'vue'

export interface OrganizationBranchRequest {
  parentID: string | null
  departmentCursor?: string
  memberCursor?: string
}

export interface OrganizationBranchPage<Department, Member> {
  departments: Department[]
  members: Member[]
  nextDepartmentCursor?: string
  nextMemberCursor?: string
}

export interface OrganizationBranchState<Department, Member> {
  parentID: string | null
  departments: Department[]
  members: Member[]
  nextDepartmentCursor?: string
  nextMemberCursor?: string
  loading: boolean
  departmentLoading: boolean
  memberLoading: boolean
  loaded: boolean
  error: boolean
  requestSequence: number
}

export interface OrganizationBranchOptions<Context, Department, Member> {
  load: (context: Context, request: OrganizationBranchRequest) => Promise<OrganizationBranchPage<Department, Member>>
  departmentKey: (department: Department) => string
  memberKey: (member: Member) => string
  sortMembers?: (left: Member, right: Member) => number
  snapshotExpired?: (error: unknown) => boolean
}

type LoadMode = 'replace' | 'departments' | 'members'
const ROOT_KEY = '__root__'

function keyFor(parentID: string | null) {
  return parentID ?? ROOT_KEY
}

function defaultSnapshotExpired(error: unknown) {
  if (typeof error !== 'object' || error == null) return false
  const response = (error as { response?: { status?: number; data?: { message?: string } } }).response
  return response?.status === 409 && response.data?.message === 'snapshot_expired'
}

export function useOrganizationBranches<Context, Department, Member>(
  options: OrganizationBranchOptions<Context, Department, Member>,
) {
  const branches = ref({}) as Ref<Record<string, OrganizationBranchState<Department, Member>>>
  const invalidatedDepartmentIDs = ref<string[]>([])
  const resetVersion = ref(0)
  let context: Context | null = null
  let generation = 0
  let requestSequence = 0

  const rootBranch = computed(() => branches.value[ROOT_KEY])

  function branchFor(parentID: string) {
    return branches.value[keyFor(parentID)]
  }

  function emptyBranch(parentID: string | null): OrganizationBranchState<Department, Member> {
    return {
      parentID,
      departments: [],
      members: [],
      loading: false,
      departmentLoading: false,
      memberLoading: false,
      loaded: false,
      error: false,
      requestSequence: 0,
    }
  }

  function writeBranch(key: string, branch: OrganizationBranchState<Department, Member>) {
    branches.value = { ...branches.value, [key]: branch }
  }

  function merge<T>(current: T[], incoming: T[], itemKey: (item: T) => string) {
    const items = new Map(current.map((item) => [itemKey(item), item]))
    for (const item of incoming) items.set(itemKey(item), item)
    return Array.from(items.values())
  }

  function mergeMembers(current: Member[], incoming: Member[]) {
    const result = merge(current, incoming, options.memberKey)
    return options.sortMembers ? result.sort(options.sortMembers) : result
  }

  function invalidateDescendants(parentID: string | null) {
    const parent = branches.value[keyFor(parentID)]
    if (!parent) return
    const descendants: string[] = []
    const seen = new Set(parentID == null ? [] : [parentID])
    const pending = parent.departments.map(options.departmentKey)
    while (pending.length > 0) {
      const departmentID = pending.pop()
      if (!departmentID || seen.has(departmentID)) continue
      seen.add(departmentID)
      descendants.push(departmentID)
      const child = branches.value[keyFor(departmentID)]
      if (child) pending.push(...child.departments.map(options.departmentKey))
    }
    if (descendants.length === 0) return
    const remaining = { ...branches.value }
    for (const departmentID of descendants) delete remaining[keyFor(departmentID)]
    branches.value = remaining
    invalidatedDepartmentIDs.value = descendants
  }

  async function loadBranch(
    parentID: string | null,
    mode: LoadMode,
    recoverSnapshot = true,
    expectedGeneration = generation,
  ): Promise<void> {
    if (context == null || expectedGeneration !== generation) return
    const key = keyFor(parentID)
    const current = branches.value[key] ?? emptyBranch(parentID)
    const sequence = ++requestSequence
    const next: OrganizationBranchState<Department, Member> = {
      ...current,
      requestSequence: sequence,
      error: false,
      loading: mode === 'replace',
      departmentLoading: mode === 'departments',
      memberLoading: mode === 'members',
    }
    if (mode === 'replace') {
      next.departments = []
      next.members = []
      next.nextDepartmentCursor = undefined
      next.nextMemberCursor = undefined
      next.loaded = false
    }
    writeBranch(key, next)

    try {
      const page = await options.load(context, {
        parentID,
        departmentCursor: mode === 'departments' ? current.nextDepartmentCursor : undefined,
        memberCursor: mode === 'members' ? current.nextMemberCursor : undefined,
      })
      if (expectedGeneration !== generation || branches.value[key]?.requestSequence !== sequence) return
      const latest = branches.value[key] ?? next
      if (mode === 'departments') {
        writeBranch(key, {
          ...latest,
          departments: merge(latest.departments, page.departments, options.departmentKey),
          nextDepartmentCursor: page.nextDepartmentCursor,
          departmentLoading: false,
          loaded: true,
        })
      } else if (mode === 'members') {
        writeBranch(key, {
          ...latest,
          members: mergeMembers(latest.members, page.members),
          nextMemberCursor: page.nextMemberCursor,
          memberLoading: false,
          loaded: true,
        })
      } else {
        writeBranch(key, {
          ...latest,
          departments: page.departments,
          members: options.sortMembers ? [...page.members].sort(options.sortMembers) : page.members,
          nextDepartmentCursor: page.nextDepartmentCursor,
          nextMemberCursor: page.nextMemberCursor,
          loading: false,
          loaded: true,
        })
      }
    } catch (error) {
      if (expectedGeneration !== generation || branches.value[key]?.requestSequence !== sequence) return
      const snapshotExpired = options.snapshotExpired ?? defaultSnapshotExpired
      if (recoverSnapshot && mode !== 'replace' && snapshotExpired(error)) {
        invalidateDescendants(parentID)
        return loadBranch(parentID, 'replace', false, expectedGeneration)
      }
      const latest = branches.value[key] ?? next
      writeBranch(key, {
        ...latest,
        loading: false,
        departmentLoading: false,
        memberLoading: false,
        error: true,
      })
    } finally {
      const latest = branches.value[key]
      if (expectedGeneration === generation && latest?.requestSequence === sequence) {
        writeBranch(key, {
          ...latest,
          loading: false,
          departmentLoading: false,
          memberLoading: false,
        })
      }
    }
  }

  function reset(nextContext: Context) {
    generation += 1
    resetVersion.value += 1
    context = nextContext
    branches.value = {}
    invalidatedDepartmentIDs.value = []
    void loadBranch(null, 'replace', true, generation)
  }

  function ensureBranch(parentID: string) {
    const branch = branchFor(parentID)
    if (branch?.loaded || branch?.loading) return
    void loadBranch(parentID, 'replace')
  }

  function loadMoreDepartments(parentID: string | null) {
    const branch = branches.value[keyFor(parentID)]
    if (!branch?.nextDepartmentCursor || branch.loading || branch.departmentLoading || branch.memberLoading) return
    void loadBranch(parentID, 'departments')
  }

  function loadMoreMembers(parentID: string | null) {
    const branch = branches.value[keyFor(parentID)]
    if (!branch?.nextMemberCursor || branch.loading || branch.departmentLoading || branch.memberLoading) return
    void loadBranch(parentID, 'members')
  }

  return {
    branches,
    rootBranch,
    invalidatedDepartmentIDs,
    resetVersion,
    branchFor,
    reset,
    ensureBranch,
    loadMoreDepartments,
    loadMoreMembers,
  }
}
