import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { FieldItem } from './field-list'
import { InsetFieldList } from './inset-field-list'

describe('InsetFieldList', () => {
  test('renders the shared inset panel plus field list shell for compact metadata groups', async () => {
    const html = renderToStaticMarkup(
      <InsetFieldList>
        <FieldItem label='Full name' value='PROJ/service' truncate />
        <FieldItem label='Provider' value='Bitbucket' truncate />
      </InsetFieldList>
    )
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./inset-field-list.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/inset-panel'")
    expect(source).toContain("from '@/components/primitives/field-list'")
    expect(html).toContain('data-slot="inset-panel"')
    expect(html).toContain('data-slot="field-list"')
    expect(html).toContain('Full name')
    expect(html).toContain('PROJ/service')
    expect(html).toContain('Provider')
    expect(html).toContain('Bitbucket')
  })
})
