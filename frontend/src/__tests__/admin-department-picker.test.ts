import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/api/client', () => ({
  default: {
    get: vi.fn(),
  },
}))

import client from '@/api/client'
import {
  listAdminUserDepartmentChildren,
  listAdminUserDepartmentOptions,
} from '@/api/adminUsers'
import AdminDepartmentPicker from '@/components/admin/AdminDepartmentPicker.vue'
import type {
  AdminDepartmentChildrenResponse,
  AdminDepartmentOptionsResponse,
  AdminDepartmentOption,
} from '@/types'

const mockGet = client.get as ReturnType<typeof vi.fn>
const mountedWrappers = new Set<VueWrapper>()

const alpha: AdminDepartmentOption = {
  external_id: 'dept-alpha',
  name: 'Department Alpha',
  display_path: 'Company / Department Alpha',
}

const beta: AdminDepartmentOption = {
  external_id: 'dept-beta',
  name: 'Department Beta',
  display_path: 'Company / Department Beta',
}

function optionsResponse(
  items: AdminDepartmentOption[],
  overrides: Partial<AdminDepartmentOptionsResponse> = {},
) {
  const data: AdminDepartmentOptionsResponse = {
    items,
    selected: null,
    total: items.length,
    page: 1,
    page_size: 20,
    ...overrides,
  }
  return Promise.resolve({ data: { data } })
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

function mountPicker(
  modelValue = '',
  options: { labelledBy?: string; attachToDocument?: boolean } = {},
) {
  const wrapper = mount(AdminDepartmentPicker, {
    attachTo: options.attachToDocument ? document.body : undefined,
    props: {
      modelValue,
      ...(options.labelledBy ? { labelledBy: options.labelledBy } : {}),
    },
  })
  mountedWrappers.add(wrapper)
  return wrapper
}

describe('admin users bounded department API', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('uses the exact option and immediate-child paths and parameters', async () => {
    mockGet.mockResolvedValue({ data: { data: {} } })

    await listAdminUserDepartmentOptions({
      q: 'alpha',
      selected_id: 'dept-beta',
      page: 2,
      page_size: 20,
    })
    expect(mockGet).toHaveBeenLastCalledWith('/admin/users/department-options', {
      params: {
        q: 'alpha',
        selected_id: 'dept-beta',
        page: 2,
        page_size: 20,
      },
    })

    await listAdminUserDepartmentChildren({
      parent_department_id: 'dept-alpha',
      page: 3,
      page_size: 25,
    })
    expect(mockGet).toHaveBeenLastCalledWith('/admin/users/department-children', {
      params: {
        parent_department_id: 'dept-alpha',
        page: 3,
        page_size: 25,
      },
    })
  })

  it('keeps exact DTO fields for option and child pages', () => {
    const options: AdminDepartmentOptionsResponse = {
      items: [alpha],
      selected: beta,
      total: 1,
      page: 1,
      page_size: 20,
    }
    const children: AdminDepartmentChildrenResponse = {
      items: [
        {
          external_id: 'dept-alpha-team-one',
          parent_external_id: 'dept-alpha',
          name: 'Team One',
          path: '1.10.20',
          display_path: 'Company / Department Alpha / Team One',
          depth: 2,
          child_count: 0,
          has_children: false,
          member_count: 8,
          matched_user_count: 7,
          subtree_member_count: 8,
          subtree_matched_user_count: 7,
          representative_count: 1,
          matched_representative_count: 1,
        },
      ],
      parent_department_id: 'dept-alpha',
      total: 1,
      page: 1,
      page_size: 25,
    }

    expect(options.selected?.external_id).toBe('dept-beta')
    expect(children.items[0].has_children).toBe(false)
  })
})

describe('AdminDepartmentPicker', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    for (const wrapper of mountedWrappers) {
      if (wrapper.exists()) wrapper.unmount()
    }
    mountedWrappers.clear()
    vi.useRealTimers()
    document.body.innerHTML = ''
  })

  it('loads nothing while closed and empty, then loads page 1/20 when opened', async () => {
    mockGet.mockImplementation(() => optionsResponse([alpha, beta]))
    const wrapper = mountPicker()

    await flushPromises()
    expect(mockGet).not.toHaveBeenCalled()

    const trigger = wrapper.get('[data-testid="admin-department-picker-trigger"]')
    expect(trigger.classes()).toEqual(
      expect.arrayContaining(['el-button', 'w-full']),
    )
    expect(wrapper.get('[data-testid="admin-department-picker-trigger-content"]').classes()).toEqual(
      expect.arrayContaining(['flex', 'w-full', 'items-center', 'justify-between']),
    )
    expect(wrapper.get('[data-testid="admin-department-picker-trigger-label"]').classes()).toEqual(
      expect.arrayContaining(['min-w-0', 'truncate']),
    )
    await trigger.trigger('click')
    await flushPromises()

    expect(mockGet).toHaveBeenCalledTimes(1)
    expect(mockGet).toHaveBeenCalledWith('/admin/users/department-options', {
      params: { page: 1, page_size: 20 },
    })
    expect(wrapper.text()).toContain('Company / Department Alpha')
  })

  it('resolves one initial deep-linked selection with selected_id and no duplicate request on open', async () => {
    mockGet.mockImplementation(() => optionsResponse([alpha], { selected: beta, total: 1 }))
    const wrapper = mountPicker('dept-beta')

    await flushPromises()

    expect(mockGet).toHaveBeenCalledTimes(1)
    expect(mockGet).toHaveBeenCalledWith('/admin/users/department-options', {
      params: { selected_id: 'dept-beta', page: 1, page_size: 20 },
    })
    expect(wrapper.get('[data-testid="admin-department-picker-trigger"]').text()).toContain('Company / Department Beta')

    await wrapper.get('[data-testid="admin-department-picker-trigger"]').trigger('click')
    await flushPromises()
    expect(mockGet).toHaveBeenCalledTimes(1)
  })

  it('never renders a stale resolved label after a controlled selection changes and rejects', async () => {
    const pendingBeta = deferred<any>()
    mockGet
      .mockImplementationOnce(() => optionsResponse([alpha], { selected: alpha }))
      .mockImplementationOnce(() => pendingBeta.promise)
    const wrapper = mountPicker('dept-alpha')
    await flushPromises()
    const trigger = wrapper.get('[data-testid="admin-department-picker-trigger"]')

    expect(trigger.text()).toContain('Company / Department Alpha')

    await wrapper.setProps({ modelValue: 'dept-beta' })

    expect(mockGet).toHaveBeenLastCalledWith('/admin/users/department-options', {
      params: { selected_id: 'dept-beta', page: 1, page_size: 20 },
    })
    expect(trigger.text()).toContain('dept-beta')
    expect(trigger.text()).not.toContain('Company / Department Alpha')

    pendingBeta.reject(new Error('selection failed'))
    await flushPromises()

    expect(trigger.text()).toContain('dept-beta')
    expect(trigger.text()).not.toContain('Company / Department Alpha')
  })

  it('does not cancel a closed deep-link label request on an unrelated pointer click', async () => {
    const pending = deferred<any>()
    mockGet.mockImplementation(() => pending.promise)
    const wrapper = mountPicker('dept-beta', { attachToDocument: true })

    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    pending.resolve(await optionsResponse([alpha], { selected: beta }))
    await flushPromises()

    expect(wrapper.get('[data-testid="admin-department-picker-trigger"]').text()).toContain('Company / Department Beta')
  })

  it('keeps deep-link label resolution alive across open-close without committing its option page', async () => {
    const pendingSelection = deferred<any>()
    mockGet
      .mockImplementationOnce(() => pendingSelection.promise)
      .mockImplementationOnce(() => optionsResponse([alpha]))
    const wrapper = mountPicker('dept-beta', { attachToDocument: true })
    const trigger = wrapper.get('[data-testid="admin-department-picker-trigger"]')

    await trigger.trigger('click')
    expect(wrapper.find('[data-testid="admin-department-picker-menu"]').exists()).toBe(true)
    await trigger.trigger('click')
    expect(wrapper.find('[data-testid="admin-department-picker-menu"]').exists()).toBe(false)

    pendingSelection.resolve(await optionsResponse([beta], { selected: beta }))
    await flushPromises()

    expect(trigger.text()).toContain('Company / Department Beta')

    await trigger.trigger('click')
    await flushPromises()

    expect(mockGet).toHaveBeenCalledTimes(2)
    expect(mockGet).toHaveBeenLastCalledWith('/admin/users/department-options', {
      params: { page: 1, page_size: 20 },
    })
    expect(wrapper.find('[data-testid="admin-department-picker-option-dept-alpha"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="admin-department-picker-option-dept-beta"]').exists()).toBe(false)
  })

  it('retries an uncached failed deep-link selection with selected_id when reopened', async () => {
    let selectionAttempts = 0
    mockGet.mockImplementation((_path: string, config: { params: { selected_id?: string } }) => {
      if (config.params.selected_id) {
        selectionAttempts += 1
        if (selectionAttempts === 1) return Promise.reject(new Error('selection failed'))
        return optionsResponse([alpha], { selected: beta })
      }
      return optionsResponse([alpha])
    })
    const wrapper = mountPicker('dept-beta')
    await flushPromises()

    expect(wrapper.get('[data-testid="admin-department-picker-trigger"]').text()).toContain('dept-beta')

    await wrapper.get('[data-testid="admin-department-picker-trigger"]').trigger('click')
    await flushPromises()

    const selectionCalls = mockGet.mock.calls.filter(([, config]) => config.params.selected_id === 'dept-beta')
    expect(selectionCalls).toHaveLength(2)
    expect(wrapper.get('[data-testid="admin-department-picker-trigger"]').text()).toContain('Company / Department Beta')
    expect(wrapper.find('[data-testid="admin-department-picker-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="admin-department-picker-option-dept-alpha"]').exists()).toBe(true)
  })

  it('trims and debounces search while preventing stale results from replacing newer results', async () => {
    vi.useFakeTimers()
    const older = deferred<any>()
    const newer = deferred<any>()
    mockGet
      .mockImplementationOnce(() => optionsResponse([alpha, beta]))
      .mockImplementationOnce(() => older.promise)
      .mockImplementationOnce(() => newer.promise)

    const wrapper = mountPicker()
    await wrapper.get('[data-testid="admin-department-picker-trigger"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="admin-department-picker-search"]').classes()).toContain('el-input__inner')
    await wrapper.get('[data-testid="admin-department-picker-search"]').setValue('  old  ')
    await vi.advanceTimersByTimeAsync(300)
    expect(mockGet).toHaveBeenLastCalledWith('/admin/users/department-options', {
      params: { q: 'old', page: 1, page_size: 20 },
    })

    await wrapper.get('[data-testid="admin-department-picker-search"]').setValue('  new  ')
    await vi.advanceTimersByTimeAsync(300)
    expect(mockGet).toHaveBeenLastCalledWith('/admin/users/department-options', {
      params: { q: 'new', page: 1, page_size: 20 },
    })

    newer.resolve(await optionsResponse([{ ...beta, name: 'New Result', display_path: 'New Result' }]))
    await flushPromises()
    expect(wrapper.text()).toContain('New Result')

    older.resolve(await optionsResponse([{ ...alpha, name: 'Old Result', display_path: 'Old Result' }]))
    await flushPromises()
    expect(wrapper.text()).toContain('New Result')
    expect(wrapper.text()).not.toContain('Old Result')
  })

  it('pages from server page and total and emits a clear change', async () => {
    mockGet.mockImplementation((_path: string, config: { params: { page: number } }) => {
      if (config.params.page === 2) {
        return optionsResponse([beta], { page: 2, total: 21 })
      }
      return optionsResponse([alpha], { selected: alpha, page: 1, total: 21 })
    })
    const wrapper = mountPicker('dept-alpha')
    await flushPromises()
    await wrapper.get('[data-testid="admin-department-picker-trigger"]').trigger('click')

    expect(wrapper.get('[data-testid="admin-department-picker-all"]').classes()).toContain('el-button')
    await wrapper.get('[data-testid="admin-department-picker-next"]').trigger('click')
    await flushPromises()
    expect(mockGet).toHaveBeenLastCalledWith('/admin/users/department-options', {
      params: { page: 2, page_size: 20 },
    })
    expect(wrapper.get('[data-testid="admin-department-picker-page"]').text()).toContain('2')

    await wrapper.get('[data-testid="admin-department-picker-prev"]').trigger('click')
    await flushPromises()
    expect(mockGet).toHaveBeenLastCalledWith('/admin/users/department-options', {
      params: { page: 1, page_size: 20 },
    })

    await wrapper.get('[data-testid="admin-department-picker-all"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([['']])
    expect(wrapper.emitted('change')).toEqual([['']])
  })

  it('can reopen and retry after clearing while the first option request is pending', async () => {
    const pending = deferred<any>()
    mockGet
      .mockImplementationOnce(() => pending.promise)
      .mockImplementationOnce(() => optionsResponse([alpha]))
    const wrapper = mountPicker()

    await wrapper.get('[data-testid="admin-department-picker-trigger"]').trigger('click')
    expect(mockGet).toHaveBeenCalledTimes(1)

    await wrapper.get('[data-testid="admin-department-picker-all"]').trigger('click')
    await wrapper.get('[data-testid="admin-department-picker-trigger"]').trigger('click')
    await flushPromises()

    expect(mockGet).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('Company / Department Alpha')
  })

  it('atomically clears a failed search page and retries that query on reopen', async () => {
    vi.useFakeTimers()
    mockGet
      .mockImplementationOnce(() => optionsResponse([alpha, beta], { page: 2, total: 45 }))
      .mockRejectedValueOnce(new Error('search failed'))
      .mockImplementationOnce(() => optionsResponse([{ ...beta, name: 'Recovered', display_path: 'Recovered' }]))
    const wrapper = mountPicker()

    await wrapper.get('[data-testid="admin-department-picker-trigger"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="admin-department-picker-search"]').setValue('  recover  ')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()

    expect(wrapper.get('[data-testid="admin-department-picker-error"]').text()).toContain('search failed')
    expect(wrapper.get('[data-testid="admin-department-picker-error"]').find('.el-alert').exists()).toBe(true)
    expect(wrapper.find('[data-testid="admin-department-picker-option-dept-alpha"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="admin-department-picker-option-dept-beta"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="admin-department-picker-page"]').text()).toContain('1 / 1')
    expect((wrapper.get('[data-testid="admin-department-picker-next"]').element as HTMLButtonElement).disabled).toBe(true)

    await wrapper.get('[data-testid="admin-department-picker-trigger"]').trigger('click')
    await wrapper.get('[data-testid="admin-department-picker-trigger"]').trigger('click')
    await flushPromises()

    expect(mockGet).toHaveBeenCalledTimes(3)
    expect(mockGet).toHaveBeenLastCalledWith('/admin/users/department-options', {
      params: { q: 'recover', page: 1, page_size: 20 },
    })
    expect(wrapper.text()).toContain('Recovered')
  })

  it('cancels a debounced search on Escape and restores trigger focus', async () => {
    vi.useFakeTimers()
    mockGet.mockImplementation(() => optionsResponse([alpha, beta]))
    const wrapper = mountPicker('', { attachToDocument: true })

    const trigger = wrapper.get('[data-testid="admin-department-picker-trigger"]')
    await trigger.trigger('click')
    await flushPromises()
    const search = wrapper.get('[data-testid="admin-department-picker-search"]')
    await search.setValue('cancelled')
    await search.trigger('keydown', { key: 'Escape' })
    await wrapper.vm.$nextTick()

    expect(wrapper.find('[data-testid="admin-department-picker-menu"]').exists()).toBe(false)
    expect(document.activeElement).toBe(trigger.element)
    await vi.advanceTimersByTimeAsync(300)
    expect(mockGet).toHaveBeenCalledTimes(1)
  })

  it('cancels a debounced search when a pointer click closes the picker', async () => {
    vi.useFakeTimers()
    mockGet.mockImplementation(() => optionsResponse([alpha, beta]))
    const wrapper = mountPicker('', { attachToDocument: true })

    await wrapper.get('[data-testid="admin-department-picker-trigger"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="admin-department-picker-search"]').setValue('cancelled')
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await wrapper.vm.$nextTick()

    expect(wrapper.find('[data-testid="admin-department-picker-menu"]').exists()).toBe(false)
    await vi.advanceTimersByTimeAsync(300)
    expect(mockGet).toHaveBeenCalledTimes(1)
  })

  it('invalidates an in-flight search when Escape closes the picker', async () => {
    vi.useFakeTimers()
    const pending = deferred<any>()
    mockGet
      .mockImplementationOnce(() => optionsResponse([alpha]))
      .mockImplementationOnce(() => pending.promise)
    const wrapper = mountPicker('', { attachToDocument: true })

    await wrapper.get('[data-testid="admin-department-picker-trigger"]').trigger('click')
    await flushPromises()
    const search = wrapper.get('[data-testid="admin-department-picker-search"]')
    await search.setValue('new')
    await vi.advanceTimersByTimeAsync(300)
    await search.trigger('keydown', { key: 'Escape' })
    pending.resolve(await optionsResponse([{ ...beta, name: 'Hidden Result', display_path: 'Hidden Result' }]))
    await flushPromises()

    await wrapper.get('[data-testid="admin-department-picker-trigger"]').trigger('click')
    expect(wrapper.text()).toContain('Company / Department Alpha')
    expect(wrapper.text()).not.toContain('Hidden Result')
  })

  it('cancels a debounced search on unmount', async () => {
    vi.useFakeTimers()
    mockGet.mockImplementation(() => optionsResponse([alpha]))
    const wrapper = mountPicker()

    await wrapper.get('[data-testid="admin-department-picker-trigger"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="admin-department-picker-search"]').setValue('cancelled')
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(300)

    expect(mockGet).toHaveBeenCalledTimes(1)
  })

  it('supports listbox arrow, boundary, and Enter activation from the search field', async () => {
    mockGet.mockImplementation(() => optionsResponse([alpha, beta]))
    const wrapper = mountPicker('', { attachToDocument: true })

    const trigger = wrapper.get('[data-testid="admin-department-picker-trigger"]')
    await trigger.trigger('click')
    await flushPromises()
    const search = wrapper.get('[data-testid="admin-department-picker-search"]')
    expect(document.activeElement).toBe(search.element)

    await search.trigger('keydown', { key: 'ArrowDown' })
    expect(search.attributes('aria-activedescendant')).toBe(
      wrapper.get('[data-testid="admin-department-picker-option-dept-alpha"]').attributes('id'),
    )
    await search.trigger('keydown', { key: 'ArrowDown' })
    expect(search.attributes('aria-activedescendant')).toBe(
      wrapper.get('[data-testid="admin-department-picker-option-dept-beta"]').attributes('id'),
    )
    await search.trigger('keydown', { key: 'ArrowUp' })
    expect(search.attributes('aria-activedescendant')).toBe(
      wrapper.get('[data-testid="admin-department-picker-option-dept-alpha"]').attributes('id'),
    )
    await search.trigger('keydown', { key: 'End' })
    expect(search.attributes('aria-activedescendant')).toBe(
      wrapper.get('[data-testid="admin-department-picker-option-dept-beta"]').attributes('id'),
    )
    await search.trigger('keydown', { key: 'Home' })
    expect(search.attributes('aria-activedescendant')).toBe(
      wrapper.get('[data-testid="admin-department-picker-all"]').attributes('id'),
    )
    await search.trigger('keydown', { key: 'ArrowDown' })
    await search.trigger('keydown', { key: 'Enter' })

    expect(wrapper.emitted('update:modelValue')).toEqual([['dept-alpha']])
    expect(wrapper.emitted('change')).toEqual([['dept-alpha']])
    expect(wrapper.find('[data-testid="admin-department-picker-menu"]').exists()).toBe(false)
    await wrapper.vm.$nextTick()
    expect(document.activeElement).toBe(trigger.element)
  })

  it('visibly highlights All departments when keyboard focus moves there from a selection', async () => {
    mockGet.mockImplementation(() => optionsResponse([alpha, beta], { selected: beta }))
    const wrapper = mountPicker('dept-beta', { attachToDocument: true })

    await flushPromises()
    await wrapper.get('[data-testid="admin-department-picker-trigger"]').trigger('click')
    await flushPromises()

    const search = wrapper.get('[data-testid="admin-department-picker-search"]')
    const allOption = wrapper.get('[data-testid="admin-department-picker-all"]')
    const assertAllIsActive = () => {
      expect(search.attributes('aria-activedescendant')).toBe(allOption.attributes('id'))
      expect(allOption.classes()).toContain('bg-gray-50')
      expect(allOption.attributes('aria-selected')).toBe('false')
    }

    await search.trigger('keydown', { key: 'Home' })
    assertAllIsActive()

    await search.trigger('keydown', { key: 'ArrowDown' })
    await search.trigger('keydown', { key: 'ArrowUp' })
    assertAllIsActive()
  })

  it('closes on Tab without preventing normal focus movement', async () => {
    mockGet.mockImplementation(() => optionsResponse([alpha, beta]))
    const wrapper = mountPicker('', { attachToDocument: true })
    const outside = document.createElement('button')
    document.body.appendChild(outside)

    await wrapper.get('[data-testid="admin-department-picker-trigger"]').trigger('click')
    await flushPromises()
    const search = wrapper.get('[data-testid="admin-department-picker-search"]')
    const event = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })

    const dispatched = search.element.dispatchEvent(event)
    await flushPromises()

    expect(dispatched).toBe(true)
    expect(event.defaultPrevented).toBe(false)
    expect(wrapper.find('[data-testid="admin-department-picker-menu"]').exists()).toBe(true)

    outside.focus()
    await wrapper.vm.$nextTick()

    expect(document.activeElement).toBe(outside)
    expect(wrapper.find('[data-testid="admin-department-picker-menu"]').exists()).toBe(false)
  })
})
