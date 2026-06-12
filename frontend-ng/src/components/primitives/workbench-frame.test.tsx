import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { WorkbenchFrame } from './workbench-frame'

describe('WorkbenchFrame', () => {
  test('renders a shared framed workbench shell with top bar, body, and footer slots', () => {
    const html = renderToStaticMarkup(
      <WorkbenchFrame
        body={<div>Body</div>}
        footer={<div>Footer</div>}
        topBar={<div>Top</div>}
      />
    )

    expect(html).toContain('data-slot="workbench-frame"')
    expect(html).toContain('Top')
    expect(html).toContain('Body')
    expect(html).toContain('Footer')
  })
})
