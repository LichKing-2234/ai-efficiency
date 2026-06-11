import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { StatusWithReason } from './status-with-reason'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'status-with-reason.tsx'), 'utf8')

describe('StatusWithReason', () => {
  test('renders a status badge with optional truncated reason copy', () => {
    const html = renderToStaticMarkup(
      <StatusWithReason
        reason='Refresh failed because the source usage window is not ready yet'
        reasonClassName='max-w-48'
        value='refresh_failed'
      />
    )

    expect(html).toContain('data-slot="status-with-reason"')
    expect(html).toContain('refresh failed')
    expect(html).toContain('Refresh failed because')
    expect(html).toContain('flex')
    expect(html).toContain('flex-col')
    expect(html).toContain('gap-1')
    expect(html).toContain('max-w-48')
    expect(html).toContain('truncate')
  })

  test('omits empty reason copy without rendering an empty row', () => {
    const html = renderToStaticMarkup(<StatusWithReason value='completed' />)

    expect(html).toContain('completed')
    expect(html).not.toContain('data-slot="status-with-reason-copy"')
  })

  test('renders compact metadata next to the status badge', () => {
    const html = renderToStaticMarkup(<StatusWithReason inline meta='3/10' metaNumeric value='running' />)

    expect(html).toContain('data-slot="status-with-reason-meta"')
    expect(html).toContain('data-slot="status-with-reason-primary"')
    expect(html).toContain('3/10')
    expect(html).toContain('tnum')
    expect(html).toContain('gap-2')
  })

  test('uses shared stack and action primitives for inline and stacked status layout', () => {
    expect(source).toContain("from './stack'")
    expect(source).toContain("from './action-group'")
    expect(source).toContain("className={cn('min-w-0', inline ? 'gap-0' : 'gap-[2px]', className)}")
    expect(source).toContain("className={cn('min-w-0 gap-2', !inline && 'gap-1')}")
    expect(source).toContain("className={cn('text-[11.5px] text-[var(--ink-3)]', metaNumeric && 'tnum')}")
    expect(source).toContain("className={cn('truncate text-[11.5px] text-[var(--ink-3)]', reasonClassName)}")
    expect(source).not.toContain("className={cn('flex min-w-0 gap-1', inline ? 'flex-row items-center' : 'flex-col', className)}")
  })
})
