import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'accordion.tsx'), 'utf8')

describe('Accordion', () => {
  test('keeps trigger and content typography aligned with the reference detail panels', () => {
    expect(source).toContain('rounded-[var(--r-sm)]')
    expect(source).toContain('text-[12.5px]')
    expect(source).toContain('text-[var(--ink-2)]')
    expect(source).toContain('focus-visible:border-ring')
    expect(source).not.toContain('focus-visible:shadow-[var(--sh-focus)]')
    expect(source).toContain('text-[var(--ink-3)]')
    expect(source).not.toContain('focus-visible:ring-3')
    expect(source).not.toContain('hover:underline')
    expect(source).not.toContain('text-sm')
  })
})
