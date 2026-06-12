import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'link-action.tsx'), 'utf8')

describe('LinkAction', () => {
  test('renders a shared small link-style action shell with optional trailing icon', () => {
    expect(source).toContain("<Button asChild size='sm' variant='link' {...props}>")
    expect(source).toContain("<Button size='sm' variant='link' {...props}>")
    expect(source).toContain("<IconEnd data-icon='inline-end' />")
  })
})
