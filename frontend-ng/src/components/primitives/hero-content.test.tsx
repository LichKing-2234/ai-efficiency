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
    expect(html).not.toContain('data-slot="card-content"')
    expect(html).toContain('data-slot="hero-copy"')
    expect(html).toContain('data-slot="hero-description"')
    expect(html).toContain('data-slot="hero-action"')
    expect(html).toContain('data-slot="hero-shell"')
    expect(html).toContain('flex')
    expect(html).toContain('[&amp;&gt;*]:flex-1')
    expect(html).toContain('min-[920px]:justify-end')
    expect(html).toContain('text-[13.5px]')
    expect(html).toContain('AI assisted delivery')
    expect(html).toContain('Open setup')
  })

  test('keeps hero title and description rhythm inside semantic slots', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./hero-content.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain("from '@/components/primitives/card-content-stack'")
    expect(source).toContain("from '@/components/primitives/stack'")
    expect(source).toContain("dataSlot='hero-shell'")
    expect(source).toContain("dataSlot='hero-content'")
    expect(source).toContain("<ActionGroup align='responsive-end'")
    expect(source).toContain("className={cn('p-[22px]'")
    expect(source).toContain("className='max-w-[540px]'")
    expect(source).toContain("text-[25px]")
    expect(source).toContain("text-[13.5px]")
    expect(source).toContain("className='items-center gap-6'")
    expect(source).not.toContain("import { CardContent } from '@/components/ui/card'")
    expect(source).not.toContain('<CardContent ')
    expect(source).not.toContain("const heroTitleClass = 'mt-4 font-semibold text-2xl tracking-tight md:text-3xl'")
    expect(source).not.toContain("className='mt-4 font-semibold text-2xl tracking-tight md:text-3xl'")
    expect(source).not.toContain("const heroDescriptionClass = 'mt-2 text-muted-foreground text-sm'")
    expect(source).not.toContain("className='mt-2 text-muted-foreground text-sm'")
    expect(source).not.toContain("className={cn('p-6 lg:flex-row lg:items-center lg:justify-between', className)}")
  })
})
