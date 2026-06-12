import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'quiet-action-button.tsx'), 'utf8')

describe('QuietActionButton', () => {
  test('renders a shared small ghost action button shell', () => {
    expect(source).toContain("<Button size='sm' variant='ghost' {...props} />")
  })
})
