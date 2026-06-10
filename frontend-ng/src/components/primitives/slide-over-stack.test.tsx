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
    expect(html).toContain('gap-[18px]')
    expect(html).toContain('Session')
  })
})
