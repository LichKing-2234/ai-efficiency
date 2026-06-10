import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'admin-users-page.tsx'), 'utf8')

describe('Admin users page composition', () => {
  test('uses a shared row inset panel for plaintext reveal confirmation', () => {
    expect(source).toContain("from '@/components/primitives/row-inset-panel'")
    expect(source).toContain('<RowInsetPanel')
    expect(source).not.toContain("className='col-span-7 ml-11 flex max-w-xl flex-col gap-2 text-left text-xs'")
  })

  test('uses shared data grid cells for relay id and updated metadata', () => {
    expect(source).toContain('DataGridCell')
    expect(source).not.toContain("className='mono truncate text-muted-foreground text-xs'")
    expect(source).not.toContain("className='tnum text-muted-foreground text-xs'")
  })

  test('uses shared data grid cell description slots for user identity metadata', () => {
    expect(source).not.toContain("className='block truncate text-muted-foreground text-xs'")
    expect(source).toContain('description={user.email}')
  })
})
