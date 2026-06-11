import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'alert-dialog.tsx'), 'utf8')

describe('AlertDialog', () => {
  test('keeps confirm dialogs aligned with the reference modal shell', () => {
    expect(source).toContain('top-[13vh]')
    expect(source).toContain('w-[min(520px,calc(100vw-32px))]')
    expect(source).toContain('rounded-[var(--r-lg)]')
    expect(source).toContain('border border-border')
    expect(source).toContain('bg-[var(--surface)]')
    expect(source).toContain('p-[18px]')
    expect(source).toContain('bg-[var(--surface-inset)]')
    expect(source).toContain("font-[650] text-[14px]")
    expect(source).toContain('text-[12px]')
    expect(source).toContain('text-[var(--ink-3)]')
    expect(source).not.toContain('ring-1 ring-foreground/10')
    expect(source).not.toContain('bg-muted/50')
  })
})
