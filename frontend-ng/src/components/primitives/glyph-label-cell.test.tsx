import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { GlyphLabelCell } from './glyph-label-cell'

describe('GlyphLabelCell', () => {
  test('renders a shared tool glyph with primary and secondary grid copy', () => {
    const html = renderToStaticMarkup(
      <GlyphLabelCell description='Source path' glyphLabel='Claude Sonnet' glyphTool='claude' mono truncate>
        Claude Sonnet 4
      </GlyphLabelCell>
    )

    expect(html).toContain('data-slot="glyph-label-cell"')
    expect(html).toContain('Claude Sonnet 4')
    expect(html).toContain('Source path')
    expect(html).toContain('flex min-w-0 items-center gap-2')
    expect(html).toContain('Claude Sonnet')
  })

  test('uses shared grid and glyph primitives instead of page-local row layout', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./glyph-label-cell.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/data-grid'")
    expect(source).toContain("from '@/components/primitives/tool-glyph'")
    expect(source).toContain("<ToolGlyph label={glyphLabel} tool={glyphTool} size={size} />")
    expect(source).toContain("<DataGridCell description={description} mono={mono} truncate={truncate}>{children}</DataGridCell>")
  })
})
