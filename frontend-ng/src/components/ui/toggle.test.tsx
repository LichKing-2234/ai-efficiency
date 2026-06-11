import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'toggle.tsx'), 'utf8')

describe('Toggle', () => {
  test('keeps shared toggle sizing and active state aligned with the reference segmented controls', () => {
    expect(source).toContain('gap-[7px]')
    expect(source).toContain('rounded-[var(--r-md)]')
    expect(source).toContain('text-[13px]')
    expect(source).toContain('data-[state=on]:border-[var(--line)]')
    expect(source).toContain('data-[state=on]:bg-[var(--surface)]')
    expect(source).toContain('focus-visible:border-ring')
    expect(source).not.toContain('focus-visible:shadow-[var(--sh-focus)]')
    expect(source).toContain('h-[34px]')
    expect(source).toContain('px-[11px]')
    expect(source).not.toContain('focus-visible:ring-[3px] focus-visible:ring-ring/50')
    expect(source).not.toContain('hover:bg-muted')
    expect(source).not.toContain('data-[state=on]:bg-muted')
  })
})
