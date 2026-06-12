import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { RepoPrActions } from './repo-pr-actions'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'repo-pr-actions.tsx'), 'utf8')

describe('RepoPrActions', () => {
  test('renders shared detail and refresh actions for compact PR rows', () => {
    const html = renderToStaticMarkup(
      <RepoPrActions
        detailsLabel='Details'
        refreshLabel='Refresh usage'
        expanded={false}
        onRefresh={() => undefined}
        onToggleDetails={() => undefined}
      />
    )

    expect(html).toContain('data-slot="form-actions"')
    expect(html).toContain('Details')
    expect(html).toContain('Refresh usage')
  })

  test('supports an optional resolve action for expanded usage summaries', () => {
    const html = renderToStaticMarkup(
      <RepoPrActions
        refreshLabel='Refresh usage'
        resolveLabel='Resolve attribution'
        onRefresh={() => undefined}
        onResolve={() => undefined}
      />
    )

    expect(html).toContain('Refresh usage')
    expect(html).toContain('Resolve attribution')
  })

  test('keeps row action layout inside the shared primitive', () => {
    expect(source).toContain("from '@/components/primitives/form-actions'")
    expect(source).toContain('<FormActions wrap>')
    expect(source).toContain("variant='ghost'")
    expect(source).toContain("variant='outline'")
  })
})
