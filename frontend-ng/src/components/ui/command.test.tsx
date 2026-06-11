import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'command.tsx'), 'utf8')

describe('Command', () => {
  test('keeps command items and empty copy on the reference typography instead of generic text-sm muted defaults', () => {
    expect(source).toContain("px-4 py-8 text-center text-[12px] text-[var(--ink-3)]")
    expect(source).toContain('text-[13px]')
    expect(source).not.toContain('text-muted-foreground text-sm')
    expect(source).not.toContain('text-sm outline-none')
  })
})
