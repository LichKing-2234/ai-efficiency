import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'app-shell.tsx'), 'utf8')

describe('AppShell composition', () => {
  test('uses shared topbar title typography', () => {
    expect(source).toContain("from './topbar-title'")
    expect(source).toContain('<TopbarTitle')
    expect(source).not.toContain("<div className='font-semibold text-[10.5px] text-[var(--ink-4)] uppercase tracking-[0.04em]'>")
    expect(source).not.toContain("<div className='truncate font-semibold text-[15px] leading-tight'>")
  })
})
