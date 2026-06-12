import { describe, expect, test } from 'vitest'
import { commandStepAriaLabel, commandStepClipboardText, commandStepDisplayText } from './command-step'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'command-step.tsx'), 'utf8')

describe('command step helpers', () => {
  test('prefixes visible shell commands without changing copied text', () => {
    expect(commandStepDisplayText('ae-cli doctor')).toBe('$ ae-cli doctor')
    expect(commandStepClipboardText('ae-cli doctor')).toBe('ae-cli doctor')
  })

  test('keeps empty commands inert for disabled placeholder rows', () => {
    expect(commandStepDisplayText('')).toBe('$')
    expect(commandStepClipboardText('')).toBe('')
  })

  test('derives accessible labels from step title and command text', () => {
    expect(commandStepAriaLabel('ae-cli doctor', 'Verify setup')).toBe('Verify setup: ae-cli doctor')
    expect(commandStepAriaLabel('ae-cli login')).toBe('ae-cli login')
    expect(commandStepAriaLabel('', 'Install the CLI')).toBe('Install the CLI')
  })

  test('uses shared row primitives for step layout and copy affordance', () => {
    expect(source).toContain("from './action-group'")
    expect(source).toContain("from './stack'")
    expect(source).toContain("className='min-w-0 gap-[10px]'")
    expect(source).toContain("className='grid size-5 shrink-0 place-items-center rounded-full bg-[var(--ai-soft)] font-bold text-[11px] text-[var(--ai-deep)] tnum'")
    expect(source).toContain("rounded-[var(--r-sm)] border border-border bg-[var(--surface-inset)] px-[10px] py-[8px] text-left text-[12px]")
    expect(source).toContain('aria-label={commandStepAriaLabel(command, label)}')
    expect(source).toContain('title={label}')
    expect(source).toContain("text-[11.5px] text-[var(--ai-deep)]")
    expect(source).toContain("className='gap-1 text-[var(--ink-4)]'")
    expect(source).not.toContain('{copyLabel}')
    expect(source).not.toContain("<div className='flex min-w-0 items-center gap-3'>")
    expect(source).not.toContain("className='inline-flex shrink-0 items-center gap-1 text-muted-foreground'")
  })

  test('keeps the setup workflow row on the denser reference shell', () => {
    expect(source).toContain("hover:border-[var(--line-strong)] hover:bg-card")
    expect(source).toContain("className='mono min-w-0 flex-1 truncate text-[11.5px] text-[var(--ai-deep)]'")
    expect(source).not.toContain("text-[12.5px]")
    expect(source).not.toContain("gap-3")
  })
})
