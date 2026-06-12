import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { ToolbarActions } from './toolbar-actions'

describe('ToolbarActions', () => {
  test('renders a shared responsive toolbar row with stable height', () => {
    const html = renderToStaticMarkup(
      <ToolbarActions>
        <button type='button'>Refresh</button>
      </ToolbarActions>
    )

    expect(html).toContain('Refresh')
    expect(html).toContain('data-slot="toolbar-actions"')
    expect(html).toContain('min-h-9')
    expect(html).toContain('min-[920px]:justify-end')
  })

  test('keeps content toolbars on the shared action-group contract', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./toolbar-actions.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain("<ActionGroup align='responsive-end' className={cn('min-h-9', className)} dataSlot='toolbar-actions' wrap>")
    expect(source).not.toContain("className='flex items-center justify-end gap-2'")
    expect(source).not.toContain("className='toolbar-actions'")
  })
})
