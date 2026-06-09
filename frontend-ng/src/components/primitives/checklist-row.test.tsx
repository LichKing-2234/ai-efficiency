import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { ChecklistRow } from './checklist-row'

describe('ChecklistRow', () => {
  test('renders ready and pending checklist states with optional action', () => {
    const html = renderToStaticMarkup(
      <>
        <ChecklistRow label='Account' ok value='Ready' />
        <ChecklistRow action={<a href='/user'>Fix</a>} label='AI access' ok={false} value='Needs setup' />
      </>
    )

    expect(html).toContain('data-slot="checklist-row"')
    expect(html).toContain('Account')
    expect(html).toContain('Ready')
    expect(html).toContain('AI access')
    expect(html).toContain('Needs setup')
    expect(html).toContain('Fix')
    expect(html).toContain('data-state="ready"')
    expect(html).toContain('data-state="pending"')
  })
})
