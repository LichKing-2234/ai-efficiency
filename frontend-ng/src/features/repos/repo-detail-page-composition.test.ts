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
})
