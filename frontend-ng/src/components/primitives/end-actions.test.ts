import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'end-actions.tsx'), 'utf8')

describe('EndActions', () => {
  test('renders a shared pushed wrapped action row shell', () => {
    expect(source).toContain("<ActionGroup push wrap className='min-h-9' dataSlot='end-actions'>{children}</ActionGroup>")
  })
})
