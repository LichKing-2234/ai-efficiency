import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { InfoTile, InfoTileGrid } from './info-tile'

describe('InfoTile', () => {
  test('renders label and value with accent and mono options', () => {
    const html = renderToStaticMarkup(<InfoTile accent label='Version' mono value='v2.8.0' />)

    expect(html).toContain('Version')
    expect(html).toContain('v2.8.0')
    expect(html).toContain('text-[var(--pos)]')
    expect(html).toContain('mono')
  })

  test('renders numeric compact tiles with ai accent styling', () => {
    const html = renderToStaticMarkup(<InfoTile accent='ai' compact label='Credit' numeric value='12.5' />)

    expect(html).toContain('border-[var(--ai-line)]')
    expect(html).toContain('bg-[var(--ai-soft)]')
    expect(html).toContain('text-[20px]')
    expect(html).toContain('tnum')
    expect(html).not.toContain('uppercase')
  })

  test('renders a responsive shared grid for info tiles', () => {
    const html = renderToStaticMarkup(
      <InfoTileGrid columns={4}>
        <InfoTile label='Status' value='done' />
      </InfoTileGrid>
    )

    expect(html).toContain('data-slot="info-tile-grid"')
    expect(html).toContain('grid gap-[10px]')
    expect(html).toContain('md:grid-cols-4')
    expect(html).toContain('Status')
  })

  test('keeps the info tile density inside the shared primitive', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./info-tile.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("className={cn('grid gap-[10px]'")
    expect(source).toContain("'rounded-[var(--r-md)] border bg-[var(--surface-inset)] p-[14px]'")
    expect(source).toContain("cn('font-semibold text-[11px]', compact ? 'text-[var(--ink-3)]' : 'text-[var(--ink-3)] uppercase'")
    expect(source).toContain("'mt-1 break-all font-semibold text-[14.5px]'")
    expect(source).toContain("compact && 'text-[20px]'")
    expect(source).not.toContain("cn('font-semibold text-[11px]', compact ? 'text-muted-foreground' : 'text-muted-foreground uppercase'")
    expect(source).not.toContain("'mt-1 break-all font-semibold text-sm'")
  })
})
