import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'dialog.tsx'), 'utf8')

describe('Dialog', () => {
  test('keeps shared dialog sizing and typography aligned with the reference modal shell', () => {
    expect(source).toContain('top-[13vh]')
    expect(source).toContain('w-[min(580px,92vw)]')
    expect(source).toContain('rounded-[var(--r-lg)]')
    expect(source).toContain('bg-[var(--surface)]')
    expect(source).toContain('p-[18px]')
    expect(source).toContain("font-[650] text-[14px]")
    expect(source).toContain("text-[12px] text-[var(--ink-3)]")
    expect(source).not.toContain('rounded-lg')
    expect(source).not.toContain('bg-card p-5')
    expect(source).not.toContain('text-muted-foreground text-sm')
  })
})
