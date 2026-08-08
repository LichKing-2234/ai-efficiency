import { describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { useOrganizationBranches } from '@/composables/useOrganizationBranches'

interface Department { id: string }
interface Member { id: string }
interface Context { range: string }

describe('useOrganizationBranches', () => {
  it('loads and recovers one branch without leaking business DTOs into the shared state', async () => {
    const load = vi.fn()
      .mockResolvedValueOnce({
        departments: [{ id: 'child-a' }],
        members: [{ id: 'member-a' }],
        nextDepartmentCursor: 'departments-2',
        nextMemberCursor: 'members-2',
      })
      .mockResolvedValueOnce({ departments: [{ id: 'child-b' }], members: [] })
      .mockRejectedValueOnce({ response: { status: 409, data: { message: 'snapshot_expired' } } })
      .mockResolvedValueOnce({ departments: [{ id: 'child-fresh' }], members: [{ id: 'member-fresh' }] })

    const browser = useOrganizationBranches<Context, Department, Member>({
      load,
      departmentKey: (department) => department.id,
      memberKey: (member) => member.id,
    })

    browser.reset({ range: '30d' })
    await flushPromises()
    browser.loadMoreDepartments(null)
    await flushPromises()
    browser.loadMoreMembers(null)
    await flushPromises()

    expect(load).toHaveBeenNthCalledWith(2, { range: '30d' }, {
      parentID: null,
      departmentCursor: 'departments-2',
      memberCursor: undefined,
    })
    expect(load).toHaveBeenNthCalledWith(3, { range: '30d' }, {
      parentID: null,
      departmentCursor: undefined,
      memberCursor: 'members-2',
    })
    expect(load).toHaveBeenNthCalledWith(4, { range: '30d' }, {
      parentID: null,
      departmentCursor: undefined,
      memberCursor: undefined,
    })
    expect(browser.rootBranch.value?.departments).toEqual([{ id: 'child-fresh' }])
    expect(browser.rootBranch.value?.members).toEqual([{ id: 'member-fresh' }])
    expect(browser.rootBranch.value?.error).toBe(false)
  })
})
