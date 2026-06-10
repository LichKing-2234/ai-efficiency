import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { Button } from '@/components/ui/button'
import { RowInsetPanel } from './row-inset-panel'

describe('RowInsetPanel', () => {
  test('renders compact inline detail content for data grid rows', () => {
    const html = renderToStaticMarkup(
      <RowInsetPanel indent='selection' maxWidth='xl'>
        <span>Confirm reveal warning</span>
        <Button variant='outline' size='sm'>Confirm</Button>
      </RowInsetPanel>
    )

    expect(html).toContain('data-slot="row-inset-panel"')
    expect(html).toContain('Confirm reveal warning')
    expect(html).toContain('col-span-7')
    expect(html).toContain('ml-11')
    expect(html).toContain('max-w-xl')
    expect(html).toContain('text-left')
    expect(html).toContain('text-xs')
  })
})
