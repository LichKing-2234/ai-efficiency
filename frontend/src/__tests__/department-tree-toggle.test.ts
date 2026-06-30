import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import DepartmentTreeToggle from '@/components/DepartmentTreeToggle.vue'

function mountToggle(expanded = true) {
  const onToggle = vi.fn()
  const wrapper = mount({
    components: { DepartmentTreeToggle },
    template: `
      <div @click="onParentClick" @keydown.enter="onParentKeydown" @keydown.space="onParentKeydown">
        <DepartmentTreeToggle
          :expanded="expanded"
          expanded-label="Collapse department"
          collapsed-label="Expand department"
          @toggle="onToggle"
        />
      </div>
    `,
    setup() {
      return {
        expanded,
        onParentClick: vi.fn(),
        onParentKeydown: vi.fn(),
        onToggle,
      }
    },
  })

  return {
    button: wrapper.get('button'),
    onParentClick: wrapper.vm.onParentClick,
    onParentKeydown: wrapper.vm.onParentKeydown,
    onToggle,
    wrapper,
  }
}

describe('DepartmentTreeToggle', () => {
  it('renders plus and minus labels for collapsed and expanded states', () => {
    expect(mountToggle(true).button.text()).toBe('-')
    expect(mountToggle(true).button.attributes('aria-label')).toBe('Collapse department')
    expect(mountToggle(false).button.text()).toBe('+')
    expect(mountToggle(false).button.attributes('aria-label')).toBe('Expand department')
  })

  it('emits toggle without bubbling click or keyboard activation to parent rows', async () => {
    const { button, onParentClick, onParentKeydown, onToggle } = mountToggle()

    await button.trigger('click')
    expect(onToggle).toHaveBeenCalledTimes(1)
    expect(onParentClick).not.toHaveBeenCalled()

    await button.trigger('keydown', { key: 'Enter' })
    expect(onToggle).toHaveBeenCalledTimes(2)
    expect(onParentKeydown).not.toHaveBeenCalled()

    await button.trigger('keydown', { key: ' ' })
    expect(onToggle).toHaveBeenCalledTimes(3)
    expect(onParentKeydown).not.toHaveBeenCalled()
  })
})
