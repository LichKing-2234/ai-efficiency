import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { TopbarTitle } from './topbar-title'

describe('TopbarTitle', () => {
  test('renders section and title copy with stable slots', () => {
    const html = renderToStaticMarkup(<TopbarTitle section='AE' title='section 1' />)

    expect(html).toContain('data-slot="topbar-title"')
    expect(html).toContain('data-slot="topbar-title-section"')
    expect(html).toContain('data-slot="topbar-title-text"')
    expect(html).toContain('AE')
    expect(html).toContain('section 1')
  })

  test('keeps the compact reference title rhythm', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./topbar-title.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("className='truncate text-[15px] leading-[1.1] font-[650] tracking-[-0.01em]'")
    expect(source).not.toContain("className='truncate font-semibold text-[15px] leading-tight'")
  })
})
