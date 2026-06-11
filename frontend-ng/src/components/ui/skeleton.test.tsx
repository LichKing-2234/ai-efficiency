import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { Skeleton } from './skeleton'

describe('Skeleton', () => {
  test('keeps loading placeholders on the reference surface token shell', () => {
    const html = renderToStaticMarkup(<Skeleton className='h-8 w-24' />)

    expect(html).toContain('data-slot="skeleton"')
    expect(html).toContain('rounded-[var(--r-sm)]')
    expect(html).toContain('bg-[var(--surface-3)]')
    expect(html).not.toContain('rounded-md')
    expect(html).not.toContain('bg-muted')
  })
})
