import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { SlideOverStack } from './slide-over-stack'

describe('SlideOverStack', () => {
  test('renders reference detail spacing with a stable slot', () => {
    const html = renderToStaticMarkup(
      <SlideOverStack>
        <section>Session</section>
      </SlideOverStack>
    )

    expect(html).toContain('data-slot="slide-over-stack"')
    expect(html).toContain('flex-col')
    expect(html).toContain('gap-5')
    expect(html).toContain('Session')
  })

  test('sources detail spacing from the shared stack primitive', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./slide-over-stack.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/stack'")
    expect(source).not.toContain("className={cn('flex flex-col gap-[18px]', className)}")
  })
})
