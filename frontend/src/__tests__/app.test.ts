import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import App from '@/App.vue'
import { preloadI18nForTest, setLocale } from '@/i18n'

describe('App', () => {
  beforeEach(async () => {
    await preloadI18nForTest()
  })

  it('owns the Element Plus locale provider at the application root', async () => {
    const wrapper = mount(App, {
      global: {
        stubs: {
          RouterView: { template: '<div data-testid="route-view" />' },
        },
      },
    })

    const provider = wrapper.getComponent({ name: 'ElConfigProvider' })
    expect(provider.props('locale').name).toBe('en')

    await setLocale('zh-CN')
    await vi.dynamicImportSettled()
    await flushPromises()

    expect(wrapper.getComponent({ name: 'ElConfigProvider' }).props('locale').name).toBe('zh-cn')
  })
})
