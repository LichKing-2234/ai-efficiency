import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { lineTabClassName, LineTabs } from './line-tabs'

describe('LineTabs', () => {
  test('renders a shared line-tab shell with count badges', () => {
    const html = renderToStaticMarkup(
      <LineTabs
        ariaLabel='Providers'
        items={[
          { value: 'github', label: 'GitHub', count: '12' },
          { value: 'gitlab', label: 'GitLab', count: '8' }
        ]}
        onChange={() => {}}
        value='github'
      />
    )

    expect(html).toContain('GitHub')
    expect(html).toContain('GitLab')
    expect(html).toContain('data-slot="count-badge"')
    expect(html).toContain('role="tablist"')
  })

  test('exposes the shared trigger sizing token', () => {
    expect(lineTabClassName).toContain('h-8')
    expect(lineTabClassName).toContain('gap-2')
    expect(lineTabClassName).toContain('px-3')
  })
})
