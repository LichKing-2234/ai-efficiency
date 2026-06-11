import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'sheet.tsx'), 'utf8')

describe('Sheet', () => {
  test('keeps mobile drawer chrome aligned with the reference overlay and typography', () => {
    expect(source).toContain('bg-black/40')
    expect(source).toContain('text-[13px]')
    expect(source).toContain("px-[18px] pt-[18px] pb-0")
    expect(source).toContain("px-[18px] pt-0 pb-[18px]")
    expect(source).toContain('font-[650] text-[14px]')
    expect(source).toContain("text-[12px] text-[var(--ink-3)]")
    expect(source).not.toContain('bg-black/10')
    expect(source).not.toContain('text-sm text-muted-foreground')
  })
})
