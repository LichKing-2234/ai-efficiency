import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'tooltip.tsx'), 'utf8')

describe('Tooltip', () => {
  test('keeps tooltip sizing and density aligned with the collapsed-rail reference affordance', () => {
    expect(source).toContain('rounded-[var(--r-sm)]')
    expect(source).toContain('px-[9px]')
    expect(source).toContain('py-[5px]')
    expect(source).toContain('text-[11.5px]')
    expect(source).toContain('font-medium')
    expect(source).not.toContain('rounded-md')
    expect(source).not.toContain('px-3 py-1.5 text-xs')
  })
})
