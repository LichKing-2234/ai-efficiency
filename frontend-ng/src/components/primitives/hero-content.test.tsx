import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { HeroContent } from './hero-content'

describe('HeroContent', () => {
  test('renders reference hero copy and action layout through stable slots', () => {
    const html = renderToStaticMarkup(
      <HeroContent
        badge={<span>June live</span>}
        title='AI assisted delivery'
        description='admin@example.com · admin · relay'
        action={<a href='/user'>Open setup</a>}
      />
    )

    expect(html).toContain('data-slot="hero-content"')
    expect(html).toContain('data-slot="hero-copy"')
    expect(html).toContain('data-slot="hero-description"')
    expect(html).toContain('data-slot="hero-action"')
    expect(html).toContain('flex')
    expect(html).toContain('lg:flex-row')
    expect(html).toContain('text-muted-foreground')
    expect(html).toContain('AI assisted delivery')
    expect(html).toContain('Open setup')
  })
})
