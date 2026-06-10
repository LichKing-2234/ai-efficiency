import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'user-page.tsx'), 'utf8')

describe('User setup page composition', () => {
  test('uses shared action grouping for access group selectors', () => {
    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain("<ActionGroup align='responsive-end' wrap>")
    expect(source).not.toContain("<ActionGroup wrap className='justify-start sm:justify-end'>")
    expect(source).not.toContain('actions={(selectedProvider?.groups ?? []).map((group) => (')
  })

  test('uses shared card content stacks for setup card bodies', () => {
    expect(source).toContain("from '@/components/primitives/card-content-stack'")
    expect(source).not.toContain("<CardContent className='flex flex-col gap-2'>")
    expect(source).not.toContain("<CardContent className='flex flex-col gap-4'>")
  })

  test('uses shared record metadata for provider base URLs', () => {
    expect(source).toContain("from '@/components/primitives/record-meta'")
    expect(source).toContain('<RecordMeta wrap>')
    expect(source).not.toContain("description={<span className='mono break-all'>")
  })

  test('uses shared count badges for access group readiness totals', () => {
    expect(source).toContain("from '@/components/primitives/count-badge'")
    expect(source).toContain("<CountBadge variant='ai'>")
    expect(source).not.toContain("className='shrink-0 tnum'")
  })
})
