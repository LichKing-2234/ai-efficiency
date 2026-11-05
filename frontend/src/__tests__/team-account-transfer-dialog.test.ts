import { beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { setLocale } from '@/i18n'
import TeamAccountTransferDialog from '@/components/team-accounts/TeamAccountTransferDialog.vue'
import TeamAccountUserPicker from '@/components/team-accounts/TeamAccountUserPicker.vue'

vi.mock('@/api/teamAccounts', () => ({
  transferTeamAccount: vi.fn(),
  transferAdminTeamAccount: vi.fn(),
  listTeamAccountUserOptions: vi.fn(),
  listAdminTeamAccountUserOptions: vi.fn(),
}))

const account = {
  id: 6, name: 'test', department_external_id: '1684078',
  department_display_path: '声网 / SDK框架与效能组', status: 'active',
  owner_user_id: 447, owner_username: 'sdkrep', created_at: '2026-11-05T10:00:00Z',
}

describe('TeamAccountTransferDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setActivePinia(createPinia())
    void setLocale('zh-CN')
  })

  it('requires the account name before transferring', async () => {
    const body = () => new DOMWrapper(document.body)
    const wrapper = mount(TeamAccountTransferDialog, {
      props: { modelValue: true, account: account as any, mode: 'user' },
      global: { plugins: [createPinia()] },
      attachTo: document.body,
    })
    await flushPromises()

    wrapper.findComponent(TeamAccountUserPicker).vm.$emit('update:modelValue', 448)
    await flushPromises()
    await body().get('[data-testid="team-account-transfer-confirm-input"]').setValue('wrong')
    await body().get('[data-testid="team-account-transfer-submit"]').trigger('click')
    await flushPromises()

    const api = await import('@/api/teamAccounts')
    expect(api.transferTeamAccount).not.toHaveBeenCalled()
    expect(body().text()).toContain('名称')
  })

  it('transfers to the selected person after confirmation', async () => {
    const api = await import('@/api/teamAccounts')
    vi.mocked(api.transferTeamAccount).mockResolvedValue({ data: { code: 0, data: { transferred: true } } } as any)
    const body = () => new DOMWrapper(document.body)
    const wrapper = mount(TeamAccountTransferDialog, {
      props: { modelValue: true, account: account as any, mode: 'user' },
      global: { plugins: [createPinia()] },
      attachTo: document.body,
    })
    await flushPromises()

    wrapper.findComponent(TeamAccountUserPicker).vm.$emit('update:modelValue', 448)
    await flushPromises()
    ;(body().get('[data-testid="team-account-transfer-confirm-input"]').element as HTMLInputElement).value = 'test'
    await body().get('[data-testid="team-account-transfer-confirm-input"]').trigger('input')
    await body().get('[data-testid="team-account-transfer-submit"]').trigger('click')
    await flushPromises()

    expect(api.transferTeamAccount).toHaveBeenCalledWith(6, 448)
  })
})
