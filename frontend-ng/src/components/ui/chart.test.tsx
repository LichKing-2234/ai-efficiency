import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'chart.tsx'), 'utf8')

describe('Chart', () => {
  test('keeps recharts helpers aligned with the reference chart tokens and tooltip shell', () => {
    expect(source).toContain('text-[11.5px]')
    expect(source).toContain("fill-[var(--ink-4)]")
    expect(source).toContain("stroke-[var(--grid-line)]")
    expect(source).toContain("fill-[var(--surface-3)]")
    expect(source).toContain('rounded-[var(--r-sm)]')
    expect(source).toContain('border-[var(--line-strong)]')
    expect(source).toContain('bg-[var(--surface)]')
    expect(source).toContain('px-[10px]')
    expect(source).toContain('py-[8px]')
    expect(source).toContain('text-[var(--ink-3)]')
    expect(source).not.toContain('fill-muted-foreground')
    expect(source).not.toContain('rounded-lg border border-border/50 bg-background px-2.5 py-1.5 text-xs')
  })
})
