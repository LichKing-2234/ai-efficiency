import { DOMWrapper, type VueWrapper } from '@vue/test-utils'

const mountedWrappers = new Set<VueWrapper>()

function lastTeleportedElement(selector: string) {
  const elements = document.body.querySelectorAll(selector)
  return elements.item(elements.length - 1)
}

export function cleanupTeleportedContent() {
  for (const wrapper of [...mountedWrappers]) wrapper.unmount()
  document.body.innerHTML = ''
}

export function withTeleportedContent<T extends VueWrapper>(wrapper: T): T {
  const find = wrapper.find.bind(wrapper)
  const findAll = wrapper.findAll.bind(wrapper)
  const get = wrapper.get.bind(wrapper)
  const text = wrapper.text.bind(wrapper)

  const proxy = new Proxy(wrapper, {
    get(target, property, receiver) {
      if (property === 'unmount') {
        return () => {
          mountedWrappers.delete(proxy)
          return target.unmount()
        }
      }
      if (property === 'find') {
        return (selector: string) => {
          const local = find(selector)
          const teleported = lastTeleportedElement(selector)
          return local.exists() || !teleported ? local : new DOMWrapper(teleported)
        }
      }
      if (property === 'findAll') {
        return (selector: string) => [
          ...findAll(selector),
          ...Array.from(document.body.querySelectorAll(selector), (element) => new DOMWrapper(element)).reverse(),
        ]
      }
      if (property === 'get') {
        return (selector: string) => {
          const local = find(selector)
          const teleported = lastTeleportedElement(selector)
          return local.exists() ? local : teleported ? new DOMWrapper(teleported) : get(selector)
        }
      }
      if (property === 'text') {
        return () => `${text()} ${document.body.textContent ?? ''}`.trim()
      }

      const value = Reflect.get(target, property, receiver)
      return typeof value === 'function' ? value.bind(target) : value
    },
  })
  mountedWrappers.add(proxy)
  return proxy
}
