import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import OIDCSettings from '@/components/settings/OIDCSettings.vue'
import { setLocale } from '@/i18n'
import { getOIDCSettings, resetOIDCSettings, testOIDCSettings, updateOIDCSettings } from '@/api/settings'

vi.mock('@/api/settings', () => ({
  getOIDCSettings: vi.fn(),
  updateOIDCSettings: vi.fn(),
  resetOIDCSettings: vi.fn(),
  testOIDCSettings: vi.fn(),
}))

function apiResponse(data: unknown) {
  return { data: { data } }
}

function environmentSettings() {
  return {
    enabled: true,
    label: 'Company SSO',
    issuer: 'https://idp.example.test/',
    client_id: 'client-id',
    client_secret: '***',
    redirect_urls: ['https://app.example.test/auth/oidc/callback'],
    allow_email_link: true,
    scopes: ['openid', 'email'],
    source: 'environment',
    callback_path: '/auth/oidc/callback',
  }
}

function findButton(wrapper: ReturnType<typeof mount>, label: string) {
  return wrapper.findAll('button').find((button) => button.text() === label)
}

describe('OIDCSettings', () => {
  beforeEach(async () => {
    vi.clearAllMocks()
    await setLocale('en-US')
    vi.mocked(getOIDCSettings).mockResolvedValue(apiResponse(environmentSettings()) as any)
  })

  it('renders the stored settings and their source', async () => {
    const wrapper = mount(OIDCSettings)
    await flushPromises()
    expect(wrapper.text()).toContain('Browser SSO (OIDC)')
    expect(wrapper.text()).toContain('Deployment configuration')
    const callbackInput = wrapper.findAll('input').find((input) => (input.element as HTMLInputElement).value === '/auth/oidc/callback')
    expect(callbackInput).toBeTruthy()
    expect((wrapper.find('#oidc-enabled').element as HTMLInputElement).checked).toBe(true)
  })

  it('saves the edited settings and applies the response', async () => {
    const wrapper = mount(OIDCSettings)
    await flushPromises()
    vi.mocked(updateOIDCSettings).mockResolvedValue(
      apiResponse({ ...environmentSettings(), label: 'Company Login', source: 'database' }) as any,
    )
    const saveButton = findButton(wrapper, 'Save')
    expect(saveButton).toBeTruthy()
    await saveButton!.trigger('click')
    await flushPromises()

    expect(updateOIDCSettings).toHaveBeenCalledTimes(1)
    const payload = vi.mocked(updateOIDCSettings).mock.calls[0][0]
    expect(payload.issuer).toBe('https://idp.example.test/')
    expect(payload.client_secret).toBe('')
    expect(payload.redirect_urls).toEqual(['https://app.example.test/auth/oidc/callback'])
    expect(wrapper.text()).toContain('Admin configuration')
    expect(wrapper.text()).toContain('OIDC settings saved and applied')
  })

  it('tests discovery without saving', async () => {
    const wrapper = mount(OIDCSettings)
    await flushPromises()
    vi.mocked(testOIDCSettings).mockResolvedValue(
      apiResponse({ issuer: 'https://idp.example.test/', authorization_endpoint: 'https://idp.example.test/authorize' }) as any,
    )
    const testButton = findButton(wrapper, 'Test discovery')
    expect(testButton).toBeTruthy()
    await testButton!.trigger('click')
    await flushPromises()

    expect(testOIDCSettings).toHaveBeenCalledTimes(1)
    expect(updateOIDCSettings).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Discovery succeeded')
  })

  it('restores deployment configuration', async () => {
    const wrapper = mount(OIDCSettings)
    await flushPromises()
    vi.mocked(resetOIDCSettings).mockResolvedValue(
      apiResponse({ ...environmentSettings(), enabled: false, source: 'environment', label: '' }) as any,
    )
    const resetButton = findButton(wrapper, 'Restore deployment configuration')
    expect(resetButton).toBeTruthy()
    await resetButton!.trigger('click')
    await flushPromises()

    expect(resetOIDCSettings).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('Deployment OIDC configuration restored')
  })
})
