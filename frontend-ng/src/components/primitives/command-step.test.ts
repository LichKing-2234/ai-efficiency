import { describe, expect, test } from 'vitest'
import { commandStepClipboardText, commandStepDisplayText } from './command-step'

describe('command step helpers', () => {
  test('prefixes visible shell commands without changing copied text', () => {
    expect(commandStepDisplayText('ae-cli doctor')).toBe('$ ae-cli doctor')
    expect(commandStepClipboardText('ae-cli doctor')).toBe('ae-cli doctor')
  })

  test('keeps empty commands inert for disabled placeholder rows', () => {
    expect(commandStepDisplayText('')).toBe('$')
    expect(commandStepClipboardText('')).toBe('')
  })
})
