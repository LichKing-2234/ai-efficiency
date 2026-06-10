import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'repos-page.tsx'), 'utf8')

describe('Repos page composition', () => {
  test('uses shared filter and section primitives for the workbench shell', () => {
    expect(source).toContain("from '@/components/primitives/card-filter-bar'")
    expect(source).toContain("from '@/components/primitives/section-card-header'")
    expect(source).toContain('<CardFilterBar>')
    expect(source).toContain('<SectionCardHeader')
    expect(source).not.toContain("<div className='flex flex-wrap items-center gap-2'>")
    expect(source).not.toContain("<div className='border-border border-b px-3.5 py-3'>")
    expect(source).not.toContain("<div className='mb-3 flex items-center justify-between gap-2'>")
    expect(source).not.toContain("<div className='flex flex-col gap-2 border-b border-border px-5 py-4 md:flex-row md:items-center md:justify-between'>")
  })

  test('uses shared record metadata for clone URLs', () => {
    expect(source).toContain("from '@/components/primitives/record-meta'")
    expect(source).toContain('<RecordMeta>')
    expect(source).not.toContain("<span className='mono block truncate text-[11px] text-[var(--ink-4)]'>")
  })
})
