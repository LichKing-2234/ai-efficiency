import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'repo-detail-page.tsx'), 'utf8')

describe('Repo detail page composition', () => {
  test('uses shared linked record items for pull request links', () => {
    expect(source).toContain("from '@/components/primitives/linked-record-list'")
    expect(source).toContain('<LinkedRecordItem')
    expect(source).not.toContain("<a className='flex min-w-0 items-center gap-2 font-semibold")
  })

  test('uses the shared KPI grid utility for repository detail metrics', () => {
    expect(source).toContain("<div className='kpi-grid'>")
    expect(source).not.toContain("<div className='grid gap-4 sm:grid-cols-4'>")
  })

  test('uses shared filter rows for pull request range controls', () => {
    expect(source).toContain("from '@/components/primitives/filter-row'")
    expect(source).toContain("<FilterRow className='text-sm'>")
    expect(source).not.toContain("<div className='flex flex-wrap items-center gap-2 text-sm'>")
  })

  test('uses shared stacks for repair and expanded detail vertical rhythm', () => {
    expect(source).toContain("from '@/components/primitives/stack'")
    expect(source).not.toContain("<div className='flex flex-col gap-3'>")
    expect(source).not.toContain("<div className='flex flex-col gap-4'>")
  })

  test('uses the shared inset panel flush variant for expanded pull request details', () => {
    expect(source).toContain('<InsetPanel flush>')
    expect(source).not.toContain("className='rounded-none border-x-0 border-t-0 p-4'")
  })
})
