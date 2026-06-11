import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { Button } from './button'

describe('Button', () => {
  test('renders reference-sized primary, ghost, and icon shells', () => {
    const primary = renderToStaticMarkup(<Button>Run</Button>)
    const ghost = renderToStaticMarkup(
      <Button variant='ghost' size='sm'>
        Filter
      </Button>
    )
    const icon = renderToStaticMarkup(
      <Button aria-label='Search' size='icon' variant='ghost'>
        <span>S</span>
      </Button>
    )

    expect(primary).toContain('data-slot="button"')
    expect(primary).toContain('h-9')
    expect(primary).toContain('px-[15px]')
    expect(primary).toContain('gap-[7px]')
    expect(ghost).toContain('h-[34px]')
    expect(ghost).toContain('px-[13px]')
    expect(ghost).toContain('bg-[var(--surface)]')
    expect(icon).toContain('size-9')
  })

  test('keeps shared buttons shadow-free outside focus state', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./button.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain('focus-visible:border-ring')
    expect(source).not.toContain('focus-visible:shadow-[var(--sh-focus)]')
    expect(source).not.toContain('shadow-[var(--sh-sm)]')
    expect(source).not.toContain('shadow-[var(--sh-md)]')
    expect(source).not.toContain('bg-card')
  })

  test('keeps icon-sized buttons strictly square across shell controls', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./button.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("icon: 'size-9 rounded-[var(--r-sm)] p-0")
    expect(source).toContain("'icon-sm': 'size-8 rounded-[var(--r-sm)] p-0")
    expect(source).not.toContain("icon: 'h-9 w-9")
    expect(source).not.toContain("'icon-sm': 'h-8 w-8")
  })
})
