import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { DetailFieldSection } from './detail-field-section'
import { FieldItem } from './field-list'

describe('DetailFieldSection', () => {
  test('renders the shared detail-section plus field-list shell for detail drawers', async () => {
    const html = renderToStaticMarkup(
      <DetailFieldSection title='Configuration'>
        <FieldItem label='Clone URL' value='https://example.com/repo.git' mono />
        <FieldItem label='Default branch' value='main' mono />
      </DetailFieldSection>
    )
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./detail-field-section.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/detail-section'")
    expect(source).toContain("from '@/components/primitives/field-list'")
    expect(html).toContain('data-slot="detail-section"')
    expect(html).toContain('data-slot="field-list"')
    expect(html).toContain('Configuration')
    expect(html).toContain('Clone URL')
    expect(html).toContain('Default branch')
  })
})
