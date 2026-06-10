import { renderToStaticMarkup } from 'react-dom/server'
import { FolderGit2Icon } from 'lucide-react'
import { describe, expect, test } from 'vitest'
import { EntityGlyph } from './entity-glyph'

describe('EntityGlyph', () => {
  test('renders an icon inside a semantic entity glyph surface', () => {
    const html = renderToStaticMarkup(<EntityGlyph icon={FolderGit2Icon} label='Repository' />)

    expect(html).toContain("data-slot=\"entity-glyph\"")
    expect(html).toContain("aria-label=\"Repository\"")
    expect(html).toContain("bg-[var(--ai-soft)]")
    expect(html).toContain("text-[var(--ai-deep)]")
  })
})
