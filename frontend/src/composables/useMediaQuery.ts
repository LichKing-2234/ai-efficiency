import { onBeforeUnmount, onMounted, readonly, ref, type Ref } from 'vue'

export function useMediaQuery(
  query: string,
  fallback = false,
): Readonly<Ref<boolean>> {
  const initialMediaQuery = typeof window !== 'undefined' && typeof window.matchMedia === 'function'
    ? window.matchMedia(query)
    : null
  const matches = ref(initialMediaQuery?.matches ?? fallback)
  let mediaQuery: MediaQueryList | null = initialMediaQuery

  const handleChange = (event: MediaQueryListEvent) => {
    matches.value = event.matches
  }

  onMounted(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return

    mediaQuery ??= window.matchMedia(query)
    matches.value = mediaQuery.matches
    if (typeof mediaQuery.addEventListener === 'function') {
      mediaQuery.addEventListener('change', handleChange)
    } else {
      mediaQuery.addListener(handleChange)
    }
  })

  onBeforeUnmount(() => {
    if (!mediaQuery) return
    if (typeof mediaQuery.removeEventListener === 'function') {
      mediaQuery.removeEventListener('change', handleChange)
    } else {
      mediaQuery.removeListener(handleChange)
    }
  })

  return readonly(matches)
}

export function useDesktopLayout() {
  return useMediaQuery('(min-width: 768px)')
}

export function useWideContentLayout() {
  return useMediaQuery('(min-width: 1280px)')
}
