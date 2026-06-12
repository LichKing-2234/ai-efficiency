import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { EntityCardHeader } from './entity-card-header'

describe('EntityCardHeader', () => {
  test('renders leading content, title, description, and actions in a shared card header', () => {
    const html = renderToStaticMarkup(
      <EntityCardHeader
        actions={<button type='button'>Filter</button>}
        description='https://example.com'
        leading={<span data-testid='ring'>3/4</span>}
        title='Provider detail'
      />
    )

    expect(html).toContain('data-testid="ring"')
    expect(html).toContain('Provider detail')
    expect(html).toContain('https://example.com')
    expect(html).toContain('Filter')
    expect(html).toContain('data-slot="entity-card-header-content"')
    expect(html).toContain('[&amp;&gt;*]:flex-1')
    expect(html).toContain('min-[920px]:justify-end')
  })

  test('omits optional regions without empty controls', () => {
    const html = renderToStaticMarkup(<EntityCardHeader title='Pull requests' />)

    expect(html).toContain('Pull requests')
    expect(html).not.toContain('<button')
  })

  test('keeps description rhythm inside the entity card description slot', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./entity-card-header.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain("from '@/components/primitives/stack'")
    expect(source).toContain("dataSlot='entity-card-header-content'")
    expect(source).toContain("<ActionGroup align='responsive-end'")
    expect(source).toContain("className={cn('min-[920px]:items-start', contentClassName)}")
    expect(source).toContain("className='text-[14px] font-[650] leading-none'")
    expect(source).toContain("className='mt-0.5 break-words text-[12px] text-[var(--ink-3)]'")
    expect(source).not.toContain("className='mt-1 break-words'")
    expect(source).not.toContain("className={cn('lg:flex-row lg:items-start lg:justify-between', contentClassName)}")
    expect(source).not.toContain("className='flex shrink-0 flex-wrap items-center justify-start gap-2 lg:justify-end'")
  })
})
