import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { PencilIcon, Trash2Icon } from 'lucide-react'
import { describe, expect, test } from 'vitest'
import { RowIconActions } from './row-icon-actions'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'row-icon-actions.tsx'), 'utf8')

describe('RowIconActions', () => {
  test('renders compact shared icon actions with confirmable delete affordance', () => {
    const html = renderToStaticMarkup(
      <RowIconActions
        cancelLabel='Cancel'
        deleteDescription='Delete this provider?'
        deleteIcon={Trash2Icon}
        deleteLabel='Delete'
        deleteTitle='Delete provider'
        editIcon={PencilIcon}
        editLabel='Update'
        onDelete={() => undefined}
        onEdit={() => undefined}
      />
    )

    expect(html).toContain('data-slot="row-icon-actions"')
    expect(html).toContain('aria-label="Update"')
    expect(html).toContain('aria-label="Delete"')
    expect(html).toContain('type="button"')
    expect(html).toContain('size-8')
  })

  test('keeps reference square icon affordances through the shared icon-sm button size', () => {
    expect(source).toContain("size='icon-sm'")
    expect(source).toContain("variant='ghost'")
    expect(source).toContain("fit")
    expect(source).toContain("gap-1")
    expect(source).not.toContain("size='sm'")
    expect(source).not.toContain("className='h-8 w-8'")
  })
})
