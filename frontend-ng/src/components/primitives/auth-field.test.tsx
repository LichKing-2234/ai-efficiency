import { describe, expect, test } from 'vitest'
import {
  authDeviceCodeControlClassName,
  authFieldControlClassName,
  formInsetControlClassName,
  formInsetTextareaClassName
} from './auth-field'

describe('authFieldControlClassName', () => {
  test('keeps the shared auth-control sizing and inset treatment in one token', () => {
    expect(authFieldControlClassName).toBe('h-10 rounded-[var(--r-md)] bg-[var(--surface-inset)] px-3.5 text-[13px] shadow-none')
  })

  test('keeps shared inset form control tokens for standard inputs and multi-line prompts', () => {
    expect(formInsetControlClassName).toBe('h-10 rounded-[var(--r-md)] bg-[var(--surface-inset)] px-3.5 text-[13px] shadow-none')
    expect(formInsetTextareaClassName).toBe('min-h-24 rounded-[var(--r-md)] bg-[var(--surface-inset)] px-3.5 py-2.5 text-[13px] shadow-none')
  })

  test('keeps a shared auth device-code control token for oauth entry screens', () => {
    expect(authDeviceCodeControlClassName).toBe('h-11 rounded-[var(--r-md)] bg-[var(--surface-inset)] px-3.5 text-center text-[15px] font-semibold tracking-[0.18em] uppercase shadow-none')
  })
})
