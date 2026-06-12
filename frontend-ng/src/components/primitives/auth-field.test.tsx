import { describe, expect, test } from 'vitest'
import { authFieldControlClassName, formInsetControlClassName, formInsetTextareaClassName } from './auth-field'

describe('authFieldControlClassName', () => {
  test('keeps the shared auth-control sizing and inset treatment in one token', () => {
    expect(authFieldControlClassName).toBe('h-10 rounded-[var(--r-md)] bg-[var(--surface-inset)] px-3.5 text-[13px] shadow-none')
  })

  test('keeps shared inset form control tokens for standard inputs and multi-line prompts', () => {
    expect(formInsetControlClassName).toBe('h-10 rounded-[var(--r-md)] bg-[var(--surface-inset)] px-3.5 text-[13px] shadow-none')
    expect(formInsetTextareaClassName).toBe('min-h-24 rounded-[var(--r-md)] bg-[var(--surface-inset)] px-3.5 py-2.5 text-[13px] shadow-none')
  })
})
