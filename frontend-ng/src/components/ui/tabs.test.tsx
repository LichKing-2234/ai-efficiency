import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { Tabs, TabsList, TabsTrigger } from './tabs'

describe('Tabs', () => {
  test('supports wrapped tab lists for toolbar provider filters', () => {
    const html = renderToStaticMarkup(
      <Tabs defaultValue='one'>
        <TabsList wrap>
          <TabsTrigger value='one'>One</TabsTrigger>
          <TabsTrigger value='two'>Two</TabsTrigger>
        </TabsList>
      </Tabs>
    )

    expect(html).toContain('data-wrap="true"')
    expect(html).toContain('h-auto')
    expect(html).toContain('flex-wrap')
    expect(html).toContain('justify-start')
  })
})
