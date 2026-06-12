import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { RailTooltip } from './rail-tooltip'

describe('RailTooltip', () => {
  const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'rail-tooltip.tsx'), 'utf8')

  test('renders a shared collapsed-rail tooltip shell', () => {
    const html = renderToStaticMarkup(
      <RailTooltip content='Overview'>
        <button type='button'>Item</button>
      </RailTooltip>
    )

    expect(html).toContain('data-slot="rail-tooltip"')
    expect(html).toContain('Item')
  })

  test('sources collapsed-rail affordance from shared tooltip primitives', () => {
    expect(source).toContain("from '@/components/ui/tooltip'")
    expect(source).toContain('<TooltipProvider>')
    expect(source).toContain('<Tooltip>')
    expect(source).toContain("<TooltipTrigger asChild data-slot='rail-tooltip-trigger'>")
    expect(source).toContain("<TooltipContent className='hidden group-data-[collapsed=true]/sidebar-wrapper:inline-flex' side='right' sideOffset={10}>")
  })
})
