import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { useGlobalConfig } from 'element-plus/es/components/config-provider/index.mjs'
import App from '@/App.vue'
import ElementPlusLocaleProvider from '@/components/ElementPlusLocaleProvider.vue'
import { preloadI18nForTest, setLocale } from '@/i18n'

const RouteLocaleProbe = defineComponent({
  setup() {
    return { elementLocale: useGlobalConfig('locale') }
  },
  template: '<div data-testid="route-locale">{{ elementLocale.name }}</div>',
})

const RouteViewStub = defineComponent({
  template: '<div data-testid="route-view" />',
})

describe('App', () => {
  beforeEach(async () => {
    await preloadI18nForTest()
  })

  it('renders the active route', () => {
    const wrapper = mount(App, {
      global: { stubs: { RouterView: RouteViewStub } },
    })

    expect(wrapper.find('[data-testid="route-view"]').exists()).toBe(true)
  })

  it('provides the reactive Element Plus locale at the shared route shell boundary', async () => {
    const wrapper = mount(ElementPlusLocaleProvider, {
      slots: { default: RouteLocaleProbe },
    })

    expect(wrapper.get('[data-testid="route-locale"]').text()).toBe('en')

    await setLocale('zh-CN')
    await vi.dynamicImportSettled()
    await flushPromises()

    expect(wrapper.get('[data-testid="route-locale"]').text()).toBe('zh-cn')
  })
})
