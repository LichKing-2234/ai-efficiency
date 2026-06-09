import { cva, type VariantProps } from 'class-variance-authority'
import type * as React from 'react'
import { cn } from '@/lib/utils'

const badgeVariants = cva(
  'inline-flex h-[21px] w-fit shrink-0 items-center gap-1 rounded-full border px-2 font-semibold text-[11.5px]',
  {
    variants: {
      variant: {
        default: 'border-border bg-[var(--surface-3)] text-[var(--ink-2)]',
        secondary: 'border-border bg-[var(--surface-3)] text-[var(--ink-2)]',
        outline: 'border-border bg-transparent text-foreground',
        neutral: 'border-border bg-[var(--surface-3)] text-[var(--ink-2)]',
        pos: 'border-[var(--pos-line)] bg-[var(--pos-soft)] text-[var(--pos)]',
        success: 'border-[var(--pos-line)] bg-[var(--pos-soft)] text-[var(--pos)]',
        warn: 'border-[var(--warn-line)] bg-[var(--warn-soft)] text-[var(--warn)]',
        warning: 'border-[var(--warn-line)] bg-[var(--warn-soft)] text-[var(--warn)]',
        neg: 'border-[var(--neg-line)] bg-[var(--neg-soft)] text-[var(--neg)]',
        ai: 'border-[var(--ai-line)] bg-[var(--ai-soft)] text-[var(--ai-deep)]'
      }
    },
    defaultVariants: {
      variant: 'secondary'
    }
  }
)

function Badge({ className, variant, ...props }: React.ComponentProps<'span'> & VariantProps<typeof badgeVariants>) {
  return <span data-slot='badge' className={cn(badgeVariants({ variant, className }))} {...props} />
}

export { Badge, badgeVariants }
