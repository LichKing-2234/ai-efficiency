import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { SplitActions } from './split-actions'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'split-actions.tsx'), 'utf8')

describe('SplitActions', () => {
  test('renders a shared equal-width split action row shell', () => {
    const html = renderToStaticMarkup(
      <SplitActions>
        <button type='button'>Primary</button>
        <button type='button'>Secondary</button>
      </SplitActions>
    )

    expect(html).toContain('data-slot="split-actions"')
    expect(html).toContain('Primary')
    expect(html).toContain('Secondary')
  })

  test('sources layout from the shared action-group split mode', () => {
    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain("<ActionGroup dataSlot='split-actions' layout='split'>{children}</ActionGroup>")
  })
})
