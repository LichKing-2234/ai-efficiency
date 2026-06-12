import { describe, expect, test } from 'vitest'

describe('FormDialog', () => {
  test('composes the shared dialog header and body shell through the shadcn dialog primitive', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./form-dialog.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/ui/dialog'")
    expect(source).toContain('<Dialog open={open} onOpenChange={onOpenChange}>')
    expect(source).toContain('<DialogContent>')
    expect(source).toContain('<DialogHeader>')
    expect(source).toContain('<DialogTitle>{title}</DialogTitle>')
    expect(source).toContain("{description ? <DialogDescription>{description}</DialogDescription> : null}")
    expect(source).not.toContain("className='fixed left-1/2 top-[13vh]")
  })

  test('keeps the dialog body delegated to children instead of baking in form-specific markup', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./form-dialog.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain('{children}')
    expect(source).not.toContain('<form')
    expect(source).not.toContain('<SubmitCancelActions')
  })
})
