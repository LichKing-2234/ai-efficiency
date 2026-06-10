import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { Badge } from '@/components/ui/badge'
import { WorkbenchContent, WorkbenchRail } from './workbench-rail'

describe('WorkbenchRail', () => {
  test('renders the reference workbench side rail with stable slots', () => {
    const html = renderToStaticMarkup(
      <WorkbenchRail
        title='Scopes'
        actions={<Badge variant='secondary'>2</Badge>}
      >
        <button type='button'>platform-team</button>
      </WorkbenchRail>
    )

    expect(html).toContain('data-slot="workbench-rail"')
    expect(html).toContain('data-slot="workbench-rail-header"')
    expect(html).toContain('data-slot="workbench-rail-content"')
    expect(html).toContain('border-border bg-[var(--surface-2)] p-3 lg:border-r')
    expect(html).toContain('Scopes')
    expect(html).toContain('platform-team')
  })

  test('renders the reference workbench content pane with stable slots', () => {
    const html = renderToStaticMarkup(
      <WorkbenchContent>
        <div>Repository table</div>
      </WorkbenchContent>
    )

    expect(html).toContain('data-slot="workbench-content"')
    expect(html).toContain('class="min-w-0"')
    expect(html).toContain('Repository table')
  })
})
