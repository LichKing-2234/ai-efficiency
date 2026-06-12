import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { DetailDrawerActions } from './detail-drawer-actions'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'detail-drawer-actions.tsx'), 'utf8')

describe('DetailDrawerActions', () => {
  test('renders shared split and destructive footer actions for detail drawers', () => {
    const html = renderToStaticMarkup(
      <DetailDrawerActions
        actions={(
          <>
            <button type='button'>Sync</button>
            <button type='button'>Open</button>
          </>
        )}
        armed={false}
        cancelLabel='Cancel'
        confirmLabel='Confirm'
        triggerLabel='Delete'
        onArm={() => undefined}
        onCancel={() => undefined}
        onConfirm={() => undefined}
      />
    )

    expect(html).toContain('data-slot="split-actions"')
    expect(html).toContain('data-slot="action-group"')
    expect(html).toContain('Sync')
    expect(html).toContain('Open')
    expect(html).toContain('Delete')
  })

  test('composes shared split and inline destructive primitives', () => {
    expect(source).toContain("from '@/components/primitives/inline-destructive-actions'")
    expect(source).toContain("from '@/components/primitives/split-actions'")
    expect(source).toContain('<SplitActions>{actions}</SplitActions>')
    expect(source).toContain('<InlineDestructiveActions')
  })
})
