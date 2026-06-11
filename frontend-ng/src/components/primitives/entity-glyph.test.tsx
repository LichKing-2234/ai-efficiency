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

  test('sources the entity shell directly from the shared primitive markup without a local class constant', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./entity-glyph.tsx', import.meta.url), 'utf8')
    )

    expect(source).not.toContain("const entityGlyphClass = 'grid size-9 shrink-0 place-items-center rounded-[var(--r-md)] border border-[var(--ai-line)] bg-[var(--ai-soft)] text-[var(--ai-deep)]'")
    expect(source).toContain("data-slot='entity-glyph'")
  })
})
