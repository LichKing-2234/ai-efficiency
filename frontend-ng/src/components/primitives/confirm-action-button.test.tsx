import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { ConfirmActionButton } from './confirm-action-button'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'confirm-action-button.tsx'), 'utf8')

describe('ConfirmActionButton', () => {
  test('sources confirmed ghost triggers from the shared confirm-action primitive', () => {
    expect(source).toContain("from '@/components/primitives/confirm-action'")
    expect(source).toContain("trigger={<Button size='sm' variant='ghost' disabled={disabled}>{label}</Button>}")
    expect(source).toContain('<ConfirmAction')
    expect(source).toContain('confirmLabel={confirmLabel}')
    expect(source).toContain('cancelLabel={cancelLabel}')
  })
})
